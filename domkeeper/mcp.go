// CLAUDE:SUMMARY Registers all domkeeper MCP tools — search, rules, folders, stats, content, GPU threshold.
package domkeeper

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hazyhaar/chrc/domkeeper/internal/store"
	"github.com/hazyhaar/pkg/idgen"
	"github.com/hazyhaar/pkg/kit"
)

// RegisterMCP registers domkeeper tools on an MCP server.
func (k *Keeper) RegisterMCP(srv *mcp.Server) {
	k.registerSearchTool(srv)
	k.registerPremiumSearchTool(srv)
	k.registerAddRuleTool(srv)
	k.registerListRulesTool(srv)
	k.registerDeleteRuleTool(srv)
	k.registerAddFolderTool(srv)
	k.registerListFoldersTool(srv)
	k.registerStatsTool(srv)
	k.registerGetContentTool(srv)
	k.registerGPUStatsTool(srv)
	k.registerGPUThresholdTool(srv)
}

// inputSchema builds a JSON Schema object with type "object".
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

// --- search ---

type searchRequest struct {
	Query      string   `json:"query"`
	FolderIDs  []string `json:"folder_ids,omitempty"`
	TrustLevel string   `json:"trust_level,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

func (k *Keeper) registerSearchTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_search",
		Description: "Search extracted web content. Returns ranked chunks matching the query.",
		InputSchema: inputSchema(map[string]any{
			"query":       map[string]any{"type": "string", "description": "Full-text search query"},
			"folder_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filter by folder IDs"},
			"trust_level": map[string]any{"type": "string", "enum": []any{"official", "institutional", "community", "unverified"}, "description": "Filter by trust level"},
			"limit":       map[string]any{"type": "integer", "description": "Max results (default 20)"},
		}, []string{"query"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*searchRequest)
		return k.Search(ctx, store.SearchOptions{
			Query:      r.Query,
			FolderIDs:  r.FolderIDs,
			TrustLevel: r.TrustLevel,
			Limit:      r.Limit,
		})
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r searchRequest
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- add_rule ---

type addRuleRequest struct {
	Name        string   `json:"name"`
	URLPattern  string   `json:"url_pattern"`
	PageID      string   `json:"page_id,omitempty"`
	Selectors   []string `json:"selectors,omitempty"`
	ExtractMode string   `json:"extract_mode,omitempty"`
	TrustLevel  string   `json:"trust_level,omitempty"`
	FolderID    string   `json:"folder_id,omitempty"`
	Priority    int      `json:"priority,omitempty"`
}

func (k *Keeper) registerAddRuleTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_add_rule",
		Description: "Add an extraction rule for a URL pattern. Content matching this pattern will be extracted and indexed.",
		InputSchema: inputSchema(map[string]any{
			"name":         map[string]any{"type": "string", "description": "Human-readable rule name"},
			"url_pattern":  map[string]any{"type": "string", "description": "URL glob pattern to match (e.g. https://example.com/*)"},
			"page_id":      map[string]any{"type": "string", "description": "Optional: specific domwatch page_id"},
			"selectors":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "CSS selectors or XPath expressions"},
			"extract_mode": map[string]any{"type": "string", "enum": []any{"css", "xpath", "density", "auto"}, "description": "Extraction mode (default: auto)"},
			"trust_level":  map[string]any{"type": "string", "enum": []any{"official", "institutional", "community", "unverified"}, "description": "Trust level"},
			"folder_id":    map[string]any{"type": "string", "description": "Target folder ID"},
			"priority":     map[string]any{"type": "integer", "description": "Priority (higher = first)"},
		}, []string{"name", "url_pattern"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*addRuleRequest)
		mode := r.ExtractMode
		if mode == "" {
			mode = "auto"
		}
		trust := r.TrustLevel
		if trust == "" {
			trust = "unverified"
		}
		rule := &store.Rule{
			ID:          idgen.New(),
			Name:        r.Name,
			URLPattern:  r.URLPattern,
			PageID:      r.PageID,
			Selectors:   r.Selectors,
			ExtractMode: mode,
			TrustLevel:  trust,
			FolderID:    r.FolderID,
			Enabled:     true,
			Priority:    r.Priority,
		}
		if err := k.AddRule(ctx, rule); err != nil {
			return nil, err
		}
		return rule, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r addRuleRequest
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- list_rules ---

func (k *Keeper) registerListRulesTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_list_rules",
		Description: "List all extraction rules.",
		InputSchema: inputSchema(map[string]any{
			"enabled_only": map[string]any{"type": "boolean", "description": "Only show enabled rules (default: false)"},
		}, nil),
	}

	type listReq struct {
		EnabledOnly bool `json:"enabled_only"`
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*listReq)
		return k.ListRules(ctx, r.EnabledOnly)
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r listReq
		json.Unmarshal(req.Params.Arguments, &r)
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- delete_rule ---

func (k *Keeper) registerDeleteRuleTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_delete_rule",
		Description: "Delete an extraction rule and all its extracted content.",
		InputSchema: inputSchema(map[string]any{
			"rule_id": map[string]any{"type": "string", "description": "Rule ID to delete"},
		}, []string{"rule_id"}),
	}

	type delReq struct {
		RuleID string `json:"rule_id"`
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*delReq)
		if err := k.DeleteRule(ctx, r.RuleID); err != nil {
			return nil, err
		}
		return map[string]string{"status": "deleted", "rule_id": r.RuleID}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r delReq
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- add_folder ---

func (k *Keeper) registerAddFolderTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_add_folder",
		Description: "Create a content folder for organizing extracted content.",
		InputSchema: inputSchema(map[string]any{
			"name":        map[string]any{"type": "string", "description": "Folder name"},
			"description": map[string]any{"type": "string", "description": "Folder description"},
			"parent_id":   map[string]any{"type": "string", "description": "Parent folder ID for nesting"},
		}, []string{"name"}),
	}

	type folderReq struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ParentID    string `json:"parent_id"`
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*folderReq)
		f := &store.Folder{
			ID:          idgen.New(),
			Name:        r.Name,
			Description: r.Description,
			ParentID:    r.ParentID,
		}
		if err := k.AddFolder(ctx, f); err != nil {
			return nil, err
		}
		return f, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r folderReq
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- list_folders ---

func (k *Keeper) registerListFoldersTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_list_folders",
		Description: "List all content folders.",
		InputSchema: inputSchema(map[string]any{}, nil),
	}

	endpoint := func(ctx context.Context, _ any) (any, error) {
		return k.ListFolders(ctx)
	}

	decode := func(_ *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		return &kit.MCPDecodeResult{Request: nil}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- stats ---

func (k *Keeper) registerStatsTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_stats",
		Description: "Get domkeeper statistics: counts of rules, folders, content, chunks, and source pages.",
		InputSchema: inputSchema(map[string]any{}, nil),
	}

	endpoint := func(ctx context.Context, _ any) (any, error) {
		return k.Stats(ctx)
	}

	decode := func(_ *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		return &kit.MCPDecodeResult{Request: nil}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- get_content ---

type getContentRequest struct {
	ContentID string `json:"content_id"`
}

func (k *Keeper) registerGetContentTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_get_content",
		Description: "Get the full extracted content by ID, including all chunks.",
		InputSchema: inputSchema(map[string]any{
			"content_id": map[string]any{"type": "string", "description": "Content ID to retrieve"},
		}, []string{"content_id"}),
	}

	type contentResponse struct {
		Content *store.Content `json:"content"`
		Chunks  []*store.Chunk `json:"chunks"`
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*getContentRequest)
		content, err := k.store.GetContent(ctx, r.ContentID)
		if err != nil {
			return nil, err
		}
		if content == nil {
			return map[string]string{"error": "content not found"}, nil
		}
		chunks, err := k.store.GetChunksByContent(ctx, r.ContentID)
		if err != nil {
			return nil, err
		}
		return &contentResponse{Content: content, Chunks: chunks}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r getContentRequest
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- premium_search ---

func (k *Keeper) registerPremiumSearchTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_premium_search",
		Description: "Tiered search on extracted content. Free tier: single FTS pass. Premium tier: multi-pass retrieval with trust-level boosting and deduplication.",
		InputSchema: inputSchema(map[string]any{
			"query":       map[string]any{"type": "string", "description": "Search query"},
			"folder_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filter by folder IDs"},
			"trust_level": map[string]any{"type": "string", "enum": []any{"official", "institutional", "community", "unverified"}, "description": "Filter by trust level"},
			"limit":       map[string]any{"type": "integer", "description": "Max results (default 20)"},
			"tier":        map[string]any{"type": "string", "enum": []any{"free", "premium"}, "description": "Search tier (default: free)"},
			"max_passes":  map[string]any{"type": "integer", "description": "Max retrieval passes for premium tier (default: 3)"},
			"user_id":     map[string]any{"type": "string", "description": "User ID for analytics"},
		}, []string{"query"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*PremiumSearchOptions)
		return k.PremiumSearch(ctx, *r)
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r PremiumSearchOptions
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- gpu_stats ---

func (k *Keeper) registerGPUStatsTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_gpu_stats",
		Description: "Get GPU pricing and serverless/dedicated threshold status.",
		InputSchema: inputSchema(map[string]any{}, nil),
	}

	endpoint := func(ctx context.Context, _ any) (any, error) {
		return k.GPUStats(ctx)
	}

	decode := func(_ *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		return &kit.MCPDecodeResult{Request: nil}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- gpu_threshold ---

type gpuThresholdRequest struct {
	BacklogUnits int `json:"backlog_units"`
}

func (k *Keeper) registerGPUThresholdTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domkeeper_gpu_threshold",
		Description: "Recompute the GPU serverless vs dedicated decision based on current backlog.",
		InputSchema: inputSchema(map[string]any{
			"backlog_units": map[string]any{"type": "integer", "description": "Number of units (pages/embeddings) in the processing backlog"},
		}, []string{"backlog_units"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*gpuThresholdRequest)
		return k.ComputeGPUThreshold(ctx, r.BacklogUnits)
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r gpuThresholdRequest
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}
