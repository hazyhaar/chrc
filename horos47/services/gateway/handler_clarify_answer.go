package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"horos47/core/data"

	"github.com/go-chi/chi/v5"
)

// ClarifyAnswerRequest is the body for POST /api/v1/sas/clarify/{envelope_id}.
type ClarifyAnswerRequest struct {
	RequestID string          `json:"request_id"`
	Answers   []ClarifyAnswer `json:"answers"`
}

// ClarifyAnswer is a single answer to a clarification question.
type ClarifyAnswer struct {
	QuestionID      string   `json:"question_id"`
	SelectedOptions []string `json:"selected_options"`
	OtherText       string   `json:"other_text,omitempty"`
}

// handleClarifyAnswer receives clarification answers and resumes the workflow.
// POST /api/v1/sas/clarify/{envelope_id}
func (s *Service) handleClarifyAnswer(w http.ResponseWriter, r *http.Request) {
	envelopeIDStr := chi.URLParam(r, "envelope_id")
	envelopeID, err := data.ParseUUID(envelopeIDStr)
	if err != nil {
		http.Error(w, "Invalid envelope_id", http.StatusBadRequest)
		return
	}

	var req ClarifyAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RequestID == "" || len(req.Answers) == 0 {
		http.Error(w, "request_id and answers required", http.StatusBadRequest)
		return
	}

	requestID, err := data.ParseUUID(req.RequestID)
	if err != nil {
		http.Error(w, "Invalid request_id", http.StatusBadRequest)
		return
	}

	// Verify clarification_request exists and is pending
	var status string
	err = s.db.QueryRow(`
		SELECT status FROM clarification_requests
		WHERE request_id = ? AND envelope_id = ?
	`, requestID, envelopeID).Scan(&status)
	if err != nil {
		http.Error(w, "Clarification request not found", http.StatusNotFound)
		return
	}
	if status != "pending" {
		http.Error(w, fmt.Sprintf("Clarification request is %s, not pending", status), http.StatusConflict)
		return
	}

	// Store answers
	answersJSON, _ := json.Marshal(req.Answers)
	now := time.Now().Unix()

	_, err = data.ExecWithRetry(s.db, `
		UPDATE clarification_requests
		SET status = 'answered', answers = ?, answered_at = ?
		WHERE request_id = ?
	`, string(answersJSON), now, requestID)
	if err != nil {
		s.logger.Error("Failed to update clarification", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Reset envelope status to routing so the worker picks it up again
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'routing'
		WHERE envelope_id = ?
	`, envelopeID)

	// Re-submit clarify_intent job with answers
	var agentName, payloadJSON, provenanceJSON string
	var workflowName string
	err = s.db.QueryRow(`
		SELECT agent_name, payload_json, provenance_json, COALESCE(workflow_name, '')
		FROM task_envelopes WHERE envelope_id = ?
	`, envelopeID).Scan(&agentName, &payloadJSON, &provenanceJSON, &workflowName)
	if err != nil {
		s.logger.Error("Failed to read envelope for re-submit", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Rebuild full steps chain (clarify_intent is step 0 again, but with answers)
	var stepsChainJSON string
	err = s.db.QueryRow(`
		SELECT steps_chain FROM workflow_definitions WHERE agent_name = ?
	`, agentName).Scan(&stepsChainJSON)
	if err != nil {
		s.logger.Error("Failed to read workflow steps", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	var steps []string
	if err := json.Unmarshal([]byte(stepsChainJSON), &steps); err != nil || len(steps) == 0 {
		http.Error(w, "Invalid workflow definition", http.StatusInternalServerError)
		return
	}

	// Submit clarify_intent again, this time with answers enriching the payload
	remainingSteps := steps[1:] // skip clarify_intent (step 0) since we're re-running it
	remainingJSON, _ := json.Marshal(remainingSteps)

	jobPayload := map[string]interface{}{
		"envelope_id":            envelopeID.String(),
		"agent_name":             agentName,
		"payload":                payloadJSON,
		"provenance":             provenanceJSON,
		"clarification_answers":  string(answersJSON),
		"_workflow.chain":        string(remainingJSON),
		"_workflow.name":         workflowName,
	}

	// Set envelope back to processing
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'processing'
		WHERE envelope_id = ?
	`, envelopeID)

	jobID, err := s.queue.Submit("clarify_intent", jobPayload)
	if err != nil {
		s.logger.Error("Failed to resubmit clarify job", "error", err)
		http.Error(w, "Failed to resume workflow", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Clarification answered, workflow resumed",
		"envelope_id", envelopeID.String(),
		"request_id", requestID.String(),
		"job_id", jobID.String())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "resumed",
		"job_id": jobID.String(),
	})
}

// processClarificationAnswerInternal handles clarification answers received via the pull poller.
// It finds the pending clarification_request for the envelope, stores answers, and resumes the workflow.
func (s *Service) processClarificationAnswerInternal(ctx context.Context, envelopeIDStr string, answersJSON []byte) {
	envelopeID, err := data.ParseUUID(envelopeIDStr)
	if err != nil {
		s.logger.Error("Invalid envelope_id for clarification", "envelope_id", envelopeIDStr)
		return
	}

	// Find pending clarification for this envelope
	var requestID data.UUID
	var status string
	err = s.db.QueryRowContext(ctx, `
		SELECT request_id, status FROM clarification_requests
		WHERE envelope_id = ? AND status = 'pending'
		ORDER BY created_at DESC LIMIT 1
	`, envelopeID).Scan(&requestID, &status)
	if err != nil {
		s.logger.Warn("No pending clarification found for envelope", "envelope_id", envelopeIDStr)
		return
	}

	now := time.Now().Unix()

	// Store answers
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE clarification_requests SET status = 'answered', answers = ?, answered_at = ?
		WHERE request_id = ?
	`, string(answersJSON), now, requestID)

	// Reset envelope to routing
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'routing'
		WHERE envelope_id = ?
	`, envelopeID)

	// Re-submit workflow (same logic as HTTP handler)
	var agentName, payloadJSON, provenanceJSON, workflowName string
	err = s.db.QueryRowContext(ctx, `
		SELECT agent_name, payload_json, provenance_json, COALESCE(workflow_name, '')
		FROM task_envelopes WHERE envelope_id = ?
	`, envelopeID).Scan(&agentName, &payloadJSON, &provenanceJSON, &workflowName)
	if err != nil {
		s.logger.Error("Failed to read envelope for clarification resume", "error", err)
		return
	}

	var stepsChainJSON string
	err = s.db.QueryRowContext(ctx, `
		SELECT steps_chain FROM workflow_definitions WHERE agent_name = ?
	`, agentName).Scan(&stepsChainJSON)
	if err != nil {
		s.logger.Warn("No workflow definition for agent", "agent", agentName)
		return
	}

	var steps []string
	if err := json.Unmarshal([]byte(stepsChainJSON), &steps); err != nil || len(steps) == 0 {
		return
	}

	remainingSteps := steps[1:]
	remainingJSON, _ := json.Marshal(remainingSteps)

	jobPayload := map[string]interface{}{
		"envelope_id":           envelopeID.String(),
		"agent_name":            agentName,
		"payload":               payloadJSON,
		"provenance":            provenanceJSON,
		"clarification_answers": string(answersJSON),
		"_workflow.chain":       string(remainingJSON),
		"_workflow.name":        workflowName,
	}

	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'processing'
		WHERE envelope_id = ?
	`, envelopeID)

	jobID, err := s.queue.Submit("clarify_intent", jobPayload)
	if err != nil {
		s.logger.Error("Failed to resubmit clarify job from poller", "error", err)
		return
	}

	s.logger.Info("Clarification answered via poller, workflow resumed",
		"envelope_id", envelopeIDStr,
		"job_id", jobID.String())
}
