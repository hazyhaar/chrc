// CLAUDE:SUMMARY Registers 15 MCP tools for veille CRUD operations via kit.RegisterMCPTool.
package veille

import (
	"context"
	"encoding/json"

	"github.com/hazyhaar/pkg/kit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterMCP registers all veille tools on an MCP server.
func (svc *Service) RegisterMCP(srv *mcp.Server) {
	svc.registerAddSource(srv)
	svc.registerListSources(srv)
	svc.registerUpdateSource(srv)
	svc.registerDeleteSource(srv)
	svc.registerFetchNow(srv)
	svc.registerSearch(srv)
	svc.registerListExtractions(srv)
	svc.registerStats(srv)
	svc.registerFetchHistory(srv)
	svc.registerAddQuestion(srv)
	svc.registerListQuestions(srv)
	svc.registerUpdateQuestion(srv)
	svc.registerDeleteQuestion(srv)
	svc.registerRunQuestion(srv)
	svc.registerQuestionResults(srv)
}

func inputSchema(properties map[string]any, required []string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// --- Sources ---

func (svc *Service) registerAddSource(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Type      string `json:"source_type"`
		Interval  int64  `json:"fetch_interval"`
	}

	tool := &mcp.Tool{
		Name:        "veille_add_source",
		Description: "Add a new monitored source to a veille dossier",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":    map[string]any{"type": "string", "description": "Dossier ID"},
			"name":          map[string]any{"type": "string", "description": "Source name"},
			"url":           map[string]any{"type": "string", "description": "URL to monitor"},
			"source_type":   map[string]any{"type": "string", "description": "Source type: web, rss, api"},
			"fetch_interval": map[string]any{"type": "integer", "description": "Fetch interval in ms"},
		}, []string{"dossier_id", "name", "url"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		src := &Source{
			Name:          p.Name,
			URL:           p.URL,
			SourceType:    p.Type,
			FetchInterval: p.Interval,
			Enabled:       true,
		}
		if err := svc.AddSource(ctx, p.DossierID, src); err != nil {
			return nil, err
		}
		return src, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerListSources(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_sources",
		Description: "List all monitored sources in a veille dossier",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string", "description": "Dossier ID"},
		}, []string{"dossier_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListSources(ctx, p.DossierID)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerUpdateSource(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Name      string `json:"name"`
		URL       string `json:"url"`
		Enabled   *bool  `json:"enabled"`
		Interval  int64  `json:"fetch_interval"`
	}

	tool := &mcp.Tool{
		Name:        "veille_update_source",
		Description: "Update a monitored source",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":     map[string]any{"type": "string"},
			"source_id":     map[string]any{"type": "string"},
			"name":          map[string]any{"type": "string"},
			"url":           map[string]any{"type": "string"},
			"enabled":       map[string]any{"type": "boolean"},
			"fetch_interval": map[string]any{"type": "integer"},
		}, []string{"dossier_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		src := &Source{
			ID:            p.SourceID,
			Name:          p.Name,
			URL:           p.URL,
			FetchInterval: p.Interval,
		}
		if p.Enabled != nil {
			src.Enabled = *p.Enabled
		}
		if err := svc.UpdateSource(ctx, p.DossierID, src); err != nil {
			return nil, err
		}
		return src, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerDeleteSource(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_delete_source",
		Description: "Delete a monitored source and all its content",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
			"source_id":  map[string]any{"type": "string"},
		}, []string{"dossier_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.DeleteSource(ctx, p.DossierID, p.SourceID); err != nil {
			return nil, err
		}
		return map[string]string{"status": "deleted"}, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerFetchNow(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_fetch_now",
		Description: "Trigger an immediate fetch for a source",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
			"source_id":  map[string]any{"type": "string"},
		}, []string{"dossier_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.FetchNow(ctx, p.DossierID, p.SourceID); err != nil {
			return nil, err
		}
		return map[string]string{"status": "fetched"}, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- Read operations ---

func (svc *Service) registerSearch(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		Query     string `json:"query"`
		Limit     int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_search",
		Description: "Full-text search on extractions",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
			"query":      map[string]any{"type": "string", "description": "FTS5 search query"},
			"limit":      map[string]any{"type": "integer", "description": "Max results"},
		}, []string{"dossier_id", "query"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.Search(ctx, p.DossierID, p.Query, p.Limit)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerListExtractions(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Limit     int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_extractions",
		Description: "List extractions for a source",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
			"source_id":  map[string]any{"type": "string"},
			"limit":      map[string]any{"type": "integer"},
		}, []string{"dossier_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListExtractions(ctx, p.DossierID, p.SourceID, p.Limit)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerStats(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_stats",
		Description: "Get dossier statistics (sources, extractions, fetch logs)",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
		}, []string{"dossier_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.Stats(ctx, p.DossierID)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerFetchHistory(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
		SourceID  string `json:"source_id"`
		Limit     int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_fetch_history",
		Description: "Get fetch history for a source",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
			"source_id":  map[string]any{"type": "string"},
			"limit":      map[string]any{"type": "integer"},
		}, []string{"dossier_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.FetchHistory(ctx, p.DossierID, p.SourceID, p.Limit)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- Questions ---

func (svc *Service) registerAddQuestion(srv *mcp.Server) {
	type req struct {
		DossierID   string `json:"dossier_id"`
		Text        string `json:"text"`
		Keywords    string `json:"keywords"`
		Channels    string `json:"channels"`
		ScheduleMs  int64  `json:"schedule_ms"`
		MaxResults  int    `json:"max_results"`
		FollowLinks *bool  `json:"follow_links"`
	}

	tool := &mcp.Tool{
		Name:        "veille_add_question",
		Description: "Add a tracked question to periodically search",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":  map[string]any{"type": "string"},
			"text":        map[string]any{"type": "string", "description": "Question in natural language"},
			"keywords":    map[string]any{"type": "string", "description": "Search terms (optional, defaults to text)"},
			"channels":    map[string]any{"type": "string", "description": "JSON array of search engine IDs"},
			"schedule_ms": map[string]any{"type": "integer", "description": "Run interval in ms (default 86400000 = 24h)"},
			"max_results": map[string]any{"type": "integer", "description": "Max results per run (default 20)"},
			"follow_links": map[string]any{"type": "boolean", "description": "Fetch full page or snippet only"},
		}, []string{"dossier_id", "text"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		q := &TrackedQuestion{
			Text:       p.Text,
			Keywords:   p.Keywords,
			Channels:   p.Channels,
			ScheduleMs: p.ScheduleMs,
			MaxResults: p.MaxResults,
			Enabled:    true,
		}
		if p.FollowLinks != nil {
			q.FollowLinks = *p.FollowLinks
		} else {
			q.FollowLinks = true
		}
		if err := svc.AddQuestion(ctx, p.DossierID, q); err != nil {
			return nil, err
		}
		return q, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerListQuestions(srv *mcp.Server) {
	type req struct {
		DossierID string `json:"dossier_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_questions",
		Description: "List all tracked questions in a dossier",
		InputSchema: inputSchema(map[string]any{
			"dossier_id": map[string]any{"type": "string"},
		}, []string{"dossier_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListQuestions(ctx, p.DossierID)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerUpdateQuestion(srv *mcp.Server) {
	type req struct {
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

	tool := &mcp.Tool{
		Name:        "veille_update_question",
		Description: "Update a tracked question",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":  map[string]any{"type": "string"},
			"question_id": map[string]any{"type": "string"},
			"text":        map[string]any{"type": "string"},
			"keywords":    map[string]any{"type": "string"},
			"channels":    map[string]any{"type": "string"},
			"schedule_ms": map[string]any{"type": "integer"},
			"max_results": map[string]any{"type": "integer"},
			"follow_links": map[string]any{"type": "boolean"},
			"enabled":     map[string]any{"type": "boolean"},
		}, []string{"dossier_id", "question_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		q := &TrackedQuestion{
			ID:         p.QuestionID,
			Text:       p.Text,
			Keywords:   p.Keywords,
			Channels:   p.Channels,
			ScheduleMs: p.ScheduleMs,
			MaxResults: p.MaxResults,
		}
		if p.FollowLinks != nil {
			q.FollowLinks = *p.FollowLinks
		}
		if p.Enabled != nil {
			q.Enabled = *p.Enabled
		}
		if err := svc.UpdateQuestion(ctx, p.DossierID, q); err != nil {
			return nil, err
		}
		return q, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerDeleteQuestion(srv *mcp.Server) {
	type req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_delete_question",
		Description: "Delete a tracked question and its backing source",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":  map[string]any{"type": "string"},
			"question_id": map[string]any{"type": "string"},
		}, []string{"dossier_id", "question_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.DeleteQuestion(ctx, p.DossierID, p.QuestionID); err != nil {
			return nil, err
		}
		return map[string]string{"status": "deleted"}, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerRunQuestion(srv *mcp.Server) {
	type req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_run_question",
		Description: "Run a tracked question immediately",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":  map[string]any{"type": "string"},
			"question_id": map[string]any{"type": "string"},
		}, []string{"dossier_id", "question_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		count, err := svc.RunQuestionNow(ctx, p.DossierID, p.QuestionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "new_results": count}, nil
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

func (svc *Service) registerQuestionResults(srv *mcp.Server) {
	type req struct {
		DossierID  string `json:"dossier_id"`
		QuestionID string `json:"question_id"`
		Limit      int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_question_results",
		Description: "Get extraction results for a tracked question",
		InputSchema: inputSchema(map[string]any{
			"dossier_id":  map[string]any{"type": "string"},
			"question_id": map[string]any{"type": "string"},
			"limit":       map[string]any{"type": "integer"},
		}, []string{"dossier_id", "question_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.QuestionResults(ctx, p.DossierID, p.QuestionID, p.Limit)
	}

	decode := func(r *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var p req
		if err := json.Unmarshal(r.Params.Arguments, &p); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &p}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}
