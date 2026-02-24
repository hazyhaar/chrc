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
	router.RegisterLocal("veille_list_chunks", svc.handleListChunks)
	router.RegisterLocal("veille_list_extractions", svc.handleListExtractions)
	router.RegisterLocal("veille_stats", svc.handleStats)
	router.RegisterLocal("veille_fetch_history", svc.handleFetchHistory)
	router.RegisterLocal("veille_create_space", svc.handleCreateSpace)
	router.RegisterLocal("veille_list_spaces", svc.handleListSpaces)
	router.RegisterLocal("veille_delete_space", svc.handleDeleteSpace)
}

func (svc *Service) handleAddSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Type     string `json:"source_type"`
		Interval int64  `json:"fetch_interval"`
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
	if err := svc.AddSource(ctx, req.UserID, req.SpaceID, src); err != nil {
		return nil, err
	}
	return json.Marshal(src)
}

func (svc *Service) handleListSources(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	sources, err := svc.ListSources(ctx, req.UserID, req.SpaceID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sources)
}

func (svc *Service) handleUpdateSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Enabled  *bool  `json:"enabled"`
		Interval int64  `json:"fetch_interval"`
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
	if err := svc.UpdateSource(ctx, req.UserID, req.SpaceID, src); err != nil {
		return nil, err
	}
	return json.Marshal(src)
}

func (svc *Service) handleDeleteSource(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.DeleteSource(ctx, req.UserID, req.SpaceID, req.SourceID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "deleted"})
}

func (svc *Service) handleFetchNow(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.FetchNow(ctx, req.UserID, req.SpaceID, req.SourceID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "fetched"})
}

func (svc *Service) handleSearchConn(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	results, err := svc.Search(ctx, req.UserID, req.SpaceID, req.Query, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(results)
}

func (svc *Service) handleListChunks(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	chunks, err := svc.ListChunks(ctx, req.UserID, req.SpaceID, req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	return json.Marshal(chunks)
}

func (svc *Service) handleListExtractions(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	exts, err := svc.ListExtractions(ctx, req.UserID, req.SpaceID, req.SourceID, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(exts)
}

func (svc *Service) handleStats(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	stats, err := svc.Stats(ctx, req.UserID, req.SpaceID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(stats)
}

func (svc *Service) handleFetchHistory(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	history, err := svc.FetchHistory(ctx, req.UserID, req.SpaceID, req.SourceID, req.Limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(history)
}

func (svc *Service) handleCreateSpace(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	space, err := svc.CreateSpace(ctx, req.UserID, req.Name)
	if err != nil {
		return nil, err
	}
	return json.Marshal(space)
}

func (svc *Service) handleListSpaces(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	spaces, err := svc.ListSpaces(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(spaces)
}

func (svc *Service) handleDeleteSpace(ctx context.Context, payload []byte) ([]byte, error) {
	var req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if err := svc.DeleteSpace(ctx, req.UserID, req.SpaceID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"status": "deleted"})
}
