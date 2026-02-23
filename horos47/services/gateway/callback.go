package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"horos47/core/data"
)

// CallbackPayload est envoyé à HORUM quand un envelope est complété
type CallbackPayload struct {
	EnvelopeID      string `json:"envelope_id"`
	OriginMentionID string `json:"origin_mention_id"`
	AgentName       string `json:"agent_name"`
	Status          string `json:"status"`
	ResultJSON      string `json:"result_json,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// DispatchResult envoie le résultat d'un envelope vers HORUM (sas OUT)
func (s *Service) DispatchResult(ctx context.Context, envelopeID data.UUID) error {
	// 1. Lire l'envelope
	var originMentionID, agentName, status string
	var resultJSON, errorMsg sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT origin_mention_id, agent_name, status, result_json, error_message
		FROM task_envelopes WHERE envelope_id = ?
	`, envelopeID).Scan(&originMentionID, &agentName, &status, &resultJSON, &errorMsg)
	if err != nil {
		return fmt.Errorf("read envelope for dispatch: %w", err)
	}

	// 2. Lire callback URL depuis config_params
	// Use horum_callback_url first, fall back to horum_pull_url + result endpoint
	callbackURL, err := s.GetConfigParam("horum_callback_url")
	if err != nil || callbackURL == "" {
		pullURL, err2 := s.GetConfigParam("horum_pull_url")
		if err2 != nil || pullURL == "" {
			s.logger.Warn("No callback URL configured, marking as dispatched",
				"envelope_id", envelopeID.String())
			_, _ = data.ExecWithRetry(s.db, `
				UPDATE task_envelopes SET status = 'dispatched' WHERE envelope_id = ?
			`, envelopeID)
			return nil
		}
		callbackURL = pullURL + "/api/internal/edge/result"
	}

	// 3. Construire payload callback
	payload := CallbackPayload{
		EnvelopeID:      envelopeID.String(),
		OriginMentionID: originMentionID,
		AgentName:       agentName,
		Status:          status,
	}
	if resultJSON.Valid {
		payload.ResultJSON = resultJSON.String
	}
	if errorMsg.Valid {
		payload.ErrorMessage = errorMsg.String
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal callback payload: %w", err)
	}

	// 4. Envoyer via shared HTTP client
	if err := s.sendCallback(ctx, callbackURL, payloadBytes); err != nil {
		s.logger.Error("Callback failed",
			"envelope_id", envelopeID.String(),
			"url", callbackURL,
			"error", err)
		return fmt.Errorf("callback failed: %w", err)
	}

	// 5. Marquer dispatched
	_, _ = data.ExecWithRetry(s.db, `
		UPDATE task_envelopes SET status = 'dispatched' WHERE envelope_id = ?
	`, envelopeID)

	s.logger.Info("Result dispatched to HORUM",
		"envelope_id", envelopeID.String(),
		"agent", agentName,
		"callback_url", callbackURL)

	return nil
}

// sendCallback sends an HTTP POST to the callback URL using the shared persistent client.
func (s *Service) sendCallback(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("callback request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("callback returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
