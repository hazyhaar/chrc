package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"horos47/core/data"
	"horos47/core/jobs"
)

// RouteEnvelope résout agent_name → workflow_name et crée le premier job
func (s *Service) RouteEnvelope(ctx context.Context, envelopeID data.UUID) error {
	// 1. Lire l'envelope
	var agentName, payloadJSON, provenanceJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT agent_name, payload_json, provenance_json
		FROM task_envelopes WHERE envelope_id = ? AND status IN ('received','routing')
	`, envelopeID).Scan(&agentName, &payloadJSON, &provenanceJSON)
	if err != nil {
		return fmt.Errorf("read envelope: %w", err)
	}

	// 2. Lookup workflow par agent_name
	var workflowName, stepsChainJSON string
	err = s.db.QueryRowContext(ctx, `
		SELECT id, steps_chain FROM workflow_definitions WHERE agent_name = ?
	`, agentName).Scan(&workflowName, &stepsChainJSON)
	if err == sql.ErrNoRows {
		// Pas de workflow défini pour cet agent → marquer failed
		_, _ = data.ExecWithRetry(s.db, `
			UPDATE task_envelopes SET status = 'failed',
				error_message = 'No workflow defined for agent',
				completed_at = ?
			WHERE envelope_id = ?
		`, time.Now().Unix(), envelopeID)
		return fmt.Errorf("no workflow for agent %q", agentName)
	}
	if err != nil {
		return fmt.Errorf("lookup workflow: %w", err)
	}

	// 3. Parse steps chain
	var steps []string
	if err := json.Unmarshal([]byte(stepsChainJSON), &steps); err != nil {
		return fmt.Errorf("parse steps_chain: %w", err)
	}
	if len(steps) == 0 {
		return fmt.Errorf("workflow %q has no steps", workflowName)
	}

	// 4. Mettre à jour envelope status → routing
	_, err = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET workflow_name = ?, status = 'routing', started_at = ?
		WHERE envelope_id = ?
	`, workflowName, time.Now().Unix(), envelopeID)
	if err != nil {
		return fmt.Errorf("update envelope routing: %w", err)
	}

	// 5. Créer le premier job dans la queue
	// Le payload du job contient l'envelope_id + le workflow chain restant
	firstStep := steps[0]
	remainingSteps := steps[1:]

	remainingJSON, _ := json.Marshal(remainingSteps)

	jobPayload := map[string]interface{}{
		"envelope_id":     envelopeID.String(),
		"agent_name":      agentName,
		"payload":         payloadJSON,
		"provenance":      provenanceJSON,
		"_workflow.chain":  string(remainingJSON),
		"_workflow.name":   workflowName,
	}

	jobID, err := s.queue.Submit(firstStep, jobPayload)
	if err != nil {
		return fmt.Errorf("submit first job: %w", err)
	}

	// 6. Mettre à jour envelope status → processing
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'processing' WHERE envelope_id = ?
	`, envelopeID)

	s.logger.Info("Envelope routed",
		"envelope_id", envelopeID.String(),
		"workflow", workflowName,
		"first_step", firstStep,
		"job_id", jobID.String(),
		"total_steps", len(steps))

	return nil
}

// CompleteEnvelope marque un envelope comme terminé avec résultat
func (s *Service) CompleteEnvelope(envelopeID data.UUID, resultJSON string) error {
	_, err := data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'completed', result_json = ?, completed_at = ?
		WHERE envelope_id = ?
	`, resultJSON, time.Now().Unix(), envelopeID)
	return err
}

// FailEnvelope marque un envelope comme échoué
func (s *Service) FailEnvelope(envelopeID data.UUID, errMsg string) error {
	_, err := data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'failed', error_message = ?, completed_at = ?
		WHERE envelope_id = ?
	`, errMsg, time.Now().Unix(), envelopeID)
	return err
}

// SubmitNextStep crée le prochain job dans la chaîne workflow ou complète l'envelope
func (s *Service) SubmitNextStep(envelopeID data.UUID, currentResult map[string]interface{}, remainingChainJSON string) error {
	var remaining []string
	if err := json.Unmarshal([]byte(remainingChainJSON), &remaining); err != nil || len(remaining) == 0 {
		// Plus d'étapes → compléter l'envelope
		resultJSON, _ := json.Marshal(currentResult)
		return s.CompleteEnvelope(envelopeID, string(resultJSON))
	}

	// Récupérer metadata envelope pour le prochain job
	var agentName, payloadJSON, provenanceJSON, workflowName string
	err := s.db.QueryRow(`
		SELECT agent_name, payload_json, provenance_json, COALESCE(workflow_name, '')
		FROM task_envelopes WHERE envelope_id = ?
	`, envelopeID).Scan(&agentName, &payloadJSON, &provenanceJSON, &workflowName)
	if err != nil {
		return fmt.Errorf("read envelope for next step: %w", err)
	}

	nextStep := remaining[0]
	nextRemaining := remaining[1:]
	nextRemainingJSON, _ := json.Marshal(nextRemaining)

	// Enrichir le payload avec le résultat de l'étape précédente
	previousResultJSON, _ := json.Marshal(currentResult)

	jobPayload := map[string]interface{}{
		"envelope_id":       envelopeID.String(),
		"agent_name":        agentName,
		"payload":           payloadJSON,
		"provenance":        provenanceJSON,
		"previous_result":   string(previousResultJSON),
		"_workflow.chain":   string(nextRemainingJSON),
		"_workflow.name":    workflowName,
	}

	jobID, err := s.queue.Submit(nextStep, jobPayload)
	if err != nil {
		return fmt.Errorf("submit next step %q: %w", nextStep, err)
	}

	s.logger.Info("Next workflow step submitted",
		"envelope_id", envelopeID.String(),
		"step", nextStep,
		"job_id", jobID.String(),
		"remaining", len(nextRemaining))

	return nil
}

// EnvelopeIDFromPayload extrait envelope_id depuis un job payload
func EnvelopeIDFromPayload(payload map[string]interface{}) (data.UUID, error) {
	idStr, ok := payload["envelope_id"].(string)
	if !ok {
		return data.UUID{}, fmt.Errorf("missing envelope_id in payload")
	}
	return data.ParseUUID(idStr)
}

// WorkflowChainFromPayload extrait le _workflow.chain depuis un job payload
func WorkflowChainFromPayload(payload map[string]interface{}) string {
	chain, _ := payload["_workflow.chain"].(string)
	return chain
}

// Queue retourne la queue du service (pour enregistrer des handlers depuis main)
func (s *Service) Queue() *jobs.Queue {
	return s.queue
}
