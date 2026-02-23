package gateway

import (
	"context"
	"time"

	"horos47/core/data"
)

// StartWorker démarre les boucles de traitement gateway
func (s *Service) StartWorker(ctx context.Context) {
	// Boucle 1: router les envelopes received
	go s.routeLoop(ctx)

	// Boucle 2: dispatcher les envelopes completed → callback HORUM
	go s.dispatchLoop(ctx)

	// Boucle 3: sweep expired clarification requests (every 60s)
	go s.clarificationSweepLoop(ctx)

	// Boucle 4: poll HORUM for pending mentions (pull mode)
	go s.horumPollerLoop(ctx)

	s.logger.Info("Gateway worker started")
}

// horumPollerLoop starts the HORUM pull poller if horum_pull_url is configured.
func (s *Service) horumPollerLoop(ctx context.Context) {
	horumURL, err := s.GetConfigParam("horum_pull_url")
	if err != nil || horumURL == "" {
		s.logger.Info("horum_pull_url not configured, poller disabled")
		return
	}
	poller := NewHorumPoller(s, horumURL, s.logger)
	poller.Run(ctx)
}

// routeLoop poll les envelopes status='received' et les route vers workflows
func (s *Service) routeLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.routePending(ctx)
		}
	}
}

// routePending traite un batch d'envelopes en attente de routage
func (s *Service) routePending(ctx context.Context) {
	rows, err := data.QueryWithRetry(s.db, `
		SELECT envelope_id FROM task_envelopes
		WHERE status = 'received'
		ORDER BY priority DESC, created_at ASC
		LIMIT 10
	`)
	if err != nil {
		s.logger.Error("Failed to query pending envelopes", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var envelopeID data.UUID
		if err := rows.Scan(&envelopeID); err != nil {
			s.logger.Error("Failed to scan envelope_id", "error", err)
			continue
		}

		if err := s.RouteEnvelope(ctx, envelopeID); err != nil {
			s.logger.Error("Failed to route envelope",
				"envelope_id", envelopeID.String(),
				"error", err)
		}
	}
}

// dispatchLoop poll les envelopes status='completed' et les pousse vers HORUM
func (s *Service) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchCompleted(ctx)
		}
	}
}

// dispatchCompleted envoie les résultats terminés vers HORUM
func (s *Service) dispatchCompleted(ctx context.Context) {
	rows, err := data.QueryWithRetry(s.db, `
		SELECT envelope_id FROM task_envelopes
		WHERE status = 'completed'
		ORDER BY completed_at ASC
		LIMIT 10
	`)
	if err != nil {
		s.logger.Error("Failed to query completed envelopes", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var envelopeID data.UUID
		if err := rows.Scan(&envelopeID); err != nil {
			s.logger.Error("Failed to scan envelope_id", "error", err)
			continue
		}

		if err := s.DispatchResult(ctx, envelopeID); err != nil {
			s.logger.Error("Failed to dispatch result",
				"envelope_id", envelopeID.String(),
				"error", err)
		}
	}
}

// clarificationSweepLoop expires stale clarification requests every 60 seconds.
func (s *Service) clarificationSweepLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireClarifications(ctx)
		}
	}
}

// expireClarifications marks pending clarification_requests past their expires_at as expired,
// then resumes the corresponding workflows in degraded mode (no clarification).
func (s *Service) expireClarifications(ctx context.Context) {
	now := time.Now().Unix()

	rows, err := data.QueryWithRetry(s.db, `
		SELECT request_id, envelope_id FROM clarification_requests
		WHERE status = 'pending' AND expires_at < ?
	`, now)
	if err != nil {
		s.logger.Error("Failed to query expired clarifications", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var requestID, envelopeID data.UUID
		if err := rows.Scan(&requestID, &envelopeID); err != nil {
			s.logger.Error("Failed to scan expired clarification", "error", err)
			continue
		}

		// Mark request expired
		_, _ = data.ExecWithRetry(s.db, `
			UPDATE clarification_requests SET status = 'expired'
			WHERE request_id = ?
		`, requestID)

		// Resume envelope workflow in degraded mode (no answers)
		_, _ = data.ExecWithRetry(s.db, `
			UPDATE task_envelopes SET status = 'routing'
			WHERE envelope_id = ? AND status = 'awaiting_clarification'
		`, envelopeID)

		// Re-submit the envelope for routing (it will go through clarify_intent again,
		// but this time without awaiting — it will pass through since there are no answers
		// and the envelope has already been attempted)
		if err := s.RouteEnvelope(ctx, envelopeID); err != nil {
			s.logger.Warn("Failed to re-route expired clarification envelope",
				"envelope_id", envelopeID.String(), "error", err)
			// Set back to received so the normal routing loop picks it up
			_, _ = data.ExecWithRetry(s.db, `
				UPDATE task_envelopes SET status = 'received'
				WHERE envelope_id = ?
			`, envelopeID)
		}

		s.logger.Info("Clarification expired, workflow resumed in degraded mode",
			"request_id", requestID.String(),
			"envelope_id", envelopeID.String())
	}
}
