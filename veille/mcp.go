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
	svc.registerListChunks(srv)
	svc.registerListExtractions(srv)
	svc.registerStats(srv)
	svc.registerFetchHistory(srv)
	svc.registerCreateSpace(srv)
	svc.registerListSpaces(srv)
	svc.registerDeleteSpace(srv)
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Type     string `json:"source_type"`
		Interval int64  `json:"fetch_interval"`
	}

	tool := &mcp.Tool{
		Name:        "veille_add_source",
		Description: "Add a new monitored source to a veille space",
		InputSchema: inputSchema(map[string]any{
			"user_id":        map[string]any{"type": "string", "description": "User ID"},
			"space_id":       map[string]any{"type": "string", "description": "Space ID"},
			"name":           map[string]any{"type": "string", "description": "Source name"},
			"url":            map[string]any{"type": "string", "description": "URL to monitor"},
			"source_type":    map[string]any{"type": "string", "description": "Source type: web, rss, api"},
			"fetch_interval": map[string]any{"type": "integer", "description": "Fetch interval in ms"},
		}, []string{"user_id", "space_id", "name", "url"}),
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
		if err := svc.AddSource(ctx, p.UserID, p.SpaceID, src); err != nil {
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
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_sources",
		Description: "List all monitored sources in a veille space",
		InputSchema: inputSchema(map[string]any{
			"user_id":  map[string]any{"type": "string", "description": "User ID"},
			"space_id": map[string]any{"type": "string", "description": "Space ID"},
		}, []string{"user_id", "space_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListSources(ctx, p.UserID, p.SpaceID)
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		Enabled  *bool  `json:"enabled"`
		Interval int64  `json:"fetch_interval"`
	}

	tool := &mcp.Tool{
		Name:        "veille_update_source",
		Description: "Update a monitored source",
		InputSchema: inputSchema(map[string]any{
			"user_id":        map[string]any{"type": "string"},
			"space_id":       map[string]any{"type": "string"},
			"source_id":      map[string]any{"type": "string"},
			"name":           map[string]any{"type": "string"},
			"url":            map[string]any{"type": "string"},
			"enabled":        map[string]any{"type": "boolean"},
			"fetch_interval": map[string]any{"type": "integer"},
		}, []string{"user_id", "space_id", "source_id"}),
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
		if err := svc.UpdateSource(ctx, p.UserID, p.SpaceID, src); err != nil {
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_delete_source",
		Description: "Delete a monitored source and all its content",
		InputSchema: inputSchema(map[string]any{
			"user_id":   map[string]any{"type": "string"},
			"space_id":  map[string]any{"type": "string"},
			"source_id": map[string]any{"type": "string"},
		}, []string{"user_id", "space_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.DeleteSource(ctx, p.UserID, p.SpaceID, p.SourceID); err != nil {
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_fetch_now",
		Description: "Trigger an immediate fetch for a source",
		InputSchema: inputSchema(map[string]any{
			"user_id":   map[string]any{"type": "string"},
			"space_id":  map[string]any{"type": "string"},
			"source_id": map[string]any{"type": "string"},
		}, []string{"user_id", "space_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.FetchNow(ctx, p.UserID, p.SpaceID, p.SourceID); err != nil {
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
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_search",
		Description: "Full-text search on indexed chunks",
		InputSchema: inputSchema(map[string]any{
			"user_id":  map[string]any{"type": "string"},
			"space_id": map[string]any{"type": "string"},
			"query":    map[string]any{"type": "string", "description": "FTS5 search query"},
			"limit":    map[string]any{"type": "integer", "description": "Max results"},
		}, []string{"user_id", "space_id", "query"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.Search(ctx, p.UserID, p.SpaceID, p.Query, p.Limit)
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

func (svc *Service) registerListChunks(srv *mcp.Server) {
	type req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_chunks",
		Description: "List indexed chunks with pagination",
		InputSchema: inputSchema(map[string]any{
			"user_id":  map[string]any{"type": "string"},
			"space_id": map[string]any{"type": "string"},
			"limit":    map[string]any{"type": "integer"},
			"offset":   map[string]any{"type": "integer"},
		}, []string{"user_id", "space_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListChunks(ctx, p.UserID, p.SpaceID, p.Limit, p.Offset)
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Limit    int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_extractions",
		Description: "List extractions for a source",
		InputSchema: inputSchema(map[string]any{
			"user_id":   map[string]any{"type": "string"},
			"space_id":  map[string]any{"type": "string"},
			"source_id": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "integer"},
		}, []string{"user_id", "space_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListExtractions(ctx, p.UserID, p.SpaceID, p.SourceID, p.Limit)
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
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_stats",
		Description: "Get space statistics (sources, extractions, chunks, fetch logs)",
		InputSchema: inputSchema(map[string]any{
			"user_id":  map[string]any{"type": "string"},
			"space_id": map[string]any{"type": "string"},
		}, []string{"user_id", "space_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.Stats(ctx, p.UserID, p.SpaceID)
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
		UserID   string `json:"user_id"`
		SpaceID  string `json:"space_id"`
		SourceID string `json:"source_id"`
		Limit    int    `json:"limit"`
	}

	tool := &mcp.Tool{
		Name:        "veille_fetch_history",
		Description: "Get fetch history for a source",
		InputSchema: inputSchema(map[string]any{
			"user_id":   map[string]any{"type": "string"},
			"space_id":  map[string]any{"type": "string"},
			"source_id": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "integer"},
		}, []string{"user_id", "space_id", "source_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.FetchHistory(ctx, p.UserID, p.SpaceID, p.SourceID, p.Limit)
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

// --- Spaces ---

func (svc *Service) registerCreateSpace(srv *mcp.Server) {
	type req struct {
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	}

	tool := &mcp.Tool{
		Name:        "veille_create_space",
		Description: "Create a new veille monitoring space",
		InputSchema: inputSchema(map[string]any{
			"user_id": map[string]any{"type": "string"},
			"name":    map[string]any{"type": "string", "description": "Space name"},
		}, []string{"user_id", "name"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.CreateSpace(ctx, p.UserID, p.Name)
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

func (svc *Service) registerListSpaces(srv *mcp.Server) {
	type req struct {
		UserID string `json:"user_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_list_spaces",
		Description: "List all veille spaces for a user",
		InputSchema: inputSchema(map[string]any{
			"user_id": map[string]any{"type": "string"},
		}, []string{"user_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		return svc.ListSpaces(ctx, p.UserID)
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

func (svc *Service) registerDeleteSpace(srv *mcp.Server) {
	type req struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}

	tool := &mcp.Tool{
		Name:        "veille_delete_space",
		Description: "Delete a veille space and all its data",
		InputSchema: inputSchema(map[string]any{
			"user_id":  map[string]any{"type": "string"},
			"space_id": map[string]any{"type": "string"},
		}, []string{"user_id", "space_id"}),
	}

	endpoint := func(ctx context.Context, r any) (any, error) {
		p := r.(*req)
		if err := svc.DeleteSpace(ctx, p.UserID, p.SpaceID); err != nil {
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
