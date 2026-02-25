// CLAUDE:SUMMARY Registers 15 connectivity.Router handlers for veille CRUD operations.
package veille

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hazyhaar/pkg/connectivity"
)

// RegisterConnectivity registers veille service handlers on a connectivity Router.
func (svc *Service) RegisterConnectivity(router *connectivity.Router) {
	router.RegisterLocal("veille_add_source", svc.handleAddSource)
	router.RegisterLocal("veille_list_sources", svc.handleListSources)
	router.RegisterLocal("veille_update_source", svc.handleUpdateSource)
	router.RegisterLocal("veille_delete_source", svc.handleDeleteSource)
	router.RegisterLocal("veille_fetch_now", svc.handleFetchNow)
	router.RegisterLocal("veille_search", svc.handleSearchConn)
	router.RegisterLocal("veille_list_extractions", svc.handleListExtractions)
	router.RegisterLocal("veille_stats", svc.handleStats)
	router.RegisterLocal("veille_fetch_history", svc.handleFetchHistory)
	router.RegisterLocal("veille_add_question", svc.handleAddQuestion)
	router.RegisterLocal("veille_list_questions", svc.handleListQuestions)
	router.RegisterLocal("veille_update_question", svc.handleUpdateQuestion)
	router.RegisterLocal("veille_delete_question", svc.handleDeleteQuestion)
	router.RegisterLocal("veille_run_question", svc.handleRunQuestion)
	router.RegisterLocal("veille_question_results", svc.handleQuestionResults)
}

func (svc *Service) handleAddSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Type      string `json:"source_type"`
		Interval  int64  `json:"fetch_interval"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	src := &Source{
		Name:          req.Name,
		URL:           req.URL,
		SourceType:    req.Type,
		FetchInterval: req.Interval,
		Enabled:       true,
	}
	if err := svc.AddSource(ctx, req.DossierID, src); err != nil {
		return nil, err
	}
	return json.Marshal(src)
}

func (svc *Service) handleListSources(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	sources, err := svc.ListSources(ctx, req.DossierID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sources)
}

func (svc *Service) handleUpdateSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Enabled   *bool  `json:"enabled"`
		Interval  int64  `json:"fetch_interval"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	src := &Source{
		ID:            req.SourceID,
		Name:          req.Name,
		URL:           req.URL,
		FetchInterval: req.Interval,
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	if err := svc.UpdateSource(ctx, req.DossierID, src); err != nil {
		return nil, err
	}
	return json.Marshal(src)
}

func (svc *Service) handleDeleteSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.DeleteSource(ctx, req.DossierID, req.SourceID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "deleted"})
}

func (svc *Service) handleFetchNow(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.FetchNow(ctx, req.DossierID, req.SourceID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "fetched"})
}

func (svc *Service) handleSearchConn(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		Query     string `json:"query"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	results, err := svc.Search(ctx, req.DossierID, req.Query, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(results)
}

func (svc *Service) handleListExtractions(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	exts, err := svc.ListExtractions(ctx, req.DossierID, req.SourceID, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(exts)
}

func (svc *Service) handleStats(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	stats, err := svc.Stats(ctx, req.DossierID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(stats)
}

func (svc *Service) handleFetchHistory(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	history, err := svc.FetchHistory(ctx, req.DossierID, req.SourceID, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(history)
}

// --- Questions ---

func (svc *Service) handleAddQuestion(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID   string `json:"dossier_id"`
		Text        string `json:"text"`
		Keywords    string `json:"keywords"`
		Channels    string `json:"channels"`
		ScheduleMs  int64  `json:"schedule_ms"`
		MaxResults  int    `json:"max_results"`
		FollowLinks *bool  `json:"follow_links"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	q := &TrackedQuestion{
		Text:       req.Text,
		Keywords:   req.Keywords,
		Channels:   req.Channels,
		ScheduleMs: req.ScheduleMs,
		MaxResults: req.MaxResults,
		Enabled:    true,
	}
	if req.FollowLinks != nil {
		q.FollowLinks = *req.FollowLinks
	} else {
		q.FollowLinks = true
	}
	if err := svc.AddQuestion(ctx, req.DossierID, q); err != nil {
		return nil, err
	}
	return json.Marshal(q)
}

func (svc *Service) handleListQuestions(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID string `json:"dossier_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	questions, err := svc.ListQuestions(ctx, req.DossierID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(questions)
}

func (svc *Service) handleUpdateQuestion(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID   string `json:"dossier_id"`
		QuestionID  string `json:"question_id"`
		Text        string `json:"text"`
		Keywords    string `json:"keywords"`
		Channels    string `json:"channels"`
		ScheduleMs  int64  `json:"schedule_ms"`
		MaxResults  int    `json:"max_results"`
		FollowLinks *bool  `json:"follow_links"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	q := &TrackedQuestion{
		ID:         req.QuestionID,
		Text:       req.Text,
		Keywords:   req.Keywords,
		Channels:   req.Channels,
		ScheduleMs: req.ScheduleMs,
		MaxResults: req.MaxResults,
	}
	if req.FollowLinks != nil {
		q.FollowLinks = *req.FollowLinks
	}
	if req.Enabled != nil {
		q.Enabled = *req.Enabled
	}
	if err := svc.UpdateQuestion(ctx, req.DossierID, q); err != nil {
		return nil, err
	}
	return json.Marshal(q)
}

func (svc *Service) handleDeleteQuestion(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.DeleteQuestion(ctx, req.DossierID, req.QuestionID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "deleted"})
}

func (svc *Service) handleRunQuestion(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	count, err := svc.RunQuestionNow(ctx, req.DossierID, req.QuestionID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"status": "ok", "new_results": count})
}

func (svc *Service) handleQuestionResults(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	results, err := svc.QuestionResults(ctx, req.DossierID, req.QuestionID, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(results)
}
