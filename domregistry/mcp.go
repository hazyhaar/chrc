// CLAUDE:SUMMARY Registers all domregistry MCP tools — search profiles, submit corrections, report failures, leaderboard, stats, publish.
package domregistry

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hazyhaar/chrc/domregistry/internal/store"
	"github.com/hazyhaar/pkg/idgen"
	"github.com/hazyhaar/pkg/kit"
)

// RegisterMCP registers domregistry tools on an MCP server.
func (r *Registry) RegisterMCP(srv *mcp.Server) {
	r.registerSearchProfilesTool(srv)
	r.registerSubmitCorrectionTool(srv)
	r.registerReportFailureTool(srv)
	r.registerLeaderboardTool(srv)
	r.registerStatsTool(srv)
	r.registerPublishProfileTool(srv)
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

// --- search_profiles ---

type searchProfilesRequest struct {
	Domain     string `json:"domain"`
	TrustLevel string `json:"trust_level,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

func (r *Registry) registerSearchProfilesTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_search_profiles",
		Description: "Search community DOM profiles by domain. Returns extraction strategies and DOM structure for matching sites.",
		InputSchema: inputSchema(map[string]any{
			"domain":      map[string]any{"type": "string", "description": "Domain to search for (e.g. example.com)"},
			"trust_level": map[string]any{"type": "string", "enum": []any{"official", "institutional", "community"}, "description": "Filter by trust level"},
			"limit":       map[string]any{"type": "integer", "description": "Max results (default 20)"},
		}, []string{"domain"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		rr := req.(*searchProfilesRequest)
		if rr.Domain != "" {
			return r.SearchProfiles(ctx, rr.Domain)
		}
		return r.ListProfiles(ctx, rr.TrustLevel, rr.Limit)
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var rr searchProfilesRequest
		if err := json.Unmarshal(req.Params.Arguments, &rr); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &rr}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- submit_correction ---

type submitCorrectionRequest struct {
	ProfileID     string `json:"profile_id"`
	InstanceID    string `json:"instance_id"`
	OldExtractors string `json:"old_extractors"`
	NewExtractors string `json:"new_extractors"`
	Reason        string `json:"reason"`
}

func (r *Registry) registerSubmitCorrectionTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_submit_correction",
		Description: "Submit a correction to a community DOM profile's extractors. Auto-accepted if the instance has good reputation.",
		InputSchema: inputSchema(map[string]any{
			"profile_id":     map[string]any{"type": "string", "description": "Profile ID to correct"},
			"instance_id":    map[string]any{"type": "string", "description": "ID of the submitting domkeeper instance"},
			"old_extractors": map[string]any{"type": "string", "description": "JSON of the old extractors being replaced"},
			"new_extractors": map[string]any{"type": "string", "description": "JSON of the corrected extractors"},
			"reason":         map[string]any{"type": "string", "enum": []any{"layout_change", "new_field", "selector_broken"}, "description": "Reason for the correction"},
		}, []string{"profile_id", "instance_id", "new_extractors", "reason"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		rr := req.(*submitCorrectionRequest)
		c := &store.Correction{
			ID:            idgen.New(),
			ProfileID:     rr.ProfileID,
			InstanceID:    rr.InstanceID,
			OldExtractors: rr.OldExtractors,
			NewExtractors: rr.NewExtractors,
			Reason:        rr.Reason,
		}
		if err := r.SubmitCorrection(ctx, c); err != nil {
			return nil, err
		}
		updated, err := r.store.GetCorrection(ctx, c.ID)
		if err != nil {
			return c, nil
		}
		return updated, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var rr submitCorrectionRequest
		if err := json.Unmarshal(req.Params.Arguments, &rr); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &rr}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- report_failure ---

type reportFailureRequest struct {
	ProfileID  string `json:"profile_id"`
	InstanceID string `json:"instance_id"`
	ErrorType  string `json:"error_type"`
	Message    string `json:"message"`
}

func (r *Registry) registerReportFailureTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_report_failure",
		Description: "Report a failure for a community DOM profile. Decrements the profile's success rate.",
		InputSchema: inputSchema(map[string]any{
			"profile_id":  map[string]any{"type": "string", "description": "Profile ID that failed"},
			"instance_id": map[string]any{"type": "string", "description": "ID of the reporting domkeeper instance"},
			"error_type":  map[string]any{"type": "string", "description": "Type of error (e.g. selector_broken, timeout, empty_result)"},
			"message":     map[string]any{"type": "string", "description": "Error details"},
		}, []string{"profile_id", "instance_id"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		rr := req.(*reportFailureRequest)
		rpt := &store.Report{
			ID:         idgen.New(),
			ProfileID:  rr.ProfileID,
			InstanceID: rr.InstanceID,
			ErrorType:  rr.ErrorType,
			Message:    rr.Message,
		}
		if err := r.ReportFailure(ctx, rpt); err != nil {
			return nil, err
		}
		return map[string]string{"status": "reported", "report_id": rpt.ID}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var rr reportFailureRequest
		if err := json.Unmarshal(req.Params.Arguments, &rr); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &rr}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- leaderboard ---

type leaderboardRequest struct {
	Type  string `json:"type"`
	Limit int    `json:"limit,omitempty"`
}

func (r *Registry) registerLeaderboardTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_leaderboard",
		Description: "Get the community leaderboard. Shows domain reliability rankings or instance contribution rankings.",
		InputSchema: inputSchema(map[string]any{
			"type":  map[string]any{"type": "string", "enum": []any{"domain", "instance"}, "description": "Leaderboard type (default: domain)"},
			"limit": map[string]any{"type": "integer", "description": "Max entries (default 50)"},
		}, nil),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		rr := req.(*leaderboardRequest)
		if rr.Limit <= 0 {
			rr.Limit = 50
		}
		switch rr.Type {
		case "instance":
			return r.InstanceLeaderboard(ctx, rr.Limit)
		default:
			return r.DomainLeaderboard(ctx, rr.Limit)
		}
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var rr leaderboardRequest
		json.Unmarshal(req.Params.Arguments, &rr)
		return &kit.MCPDecodeResult{Request: &rr}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- stats ---

func (r *Registry) registerStatsTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_stats",
		Description: "Get community registry statistics: profile count, correction count, report count.",
		InputSchema: inputSchema(map[string]any{}, nil),
	}

	endpoint := func(ctx context.Context, _ any) (any, error) {
		return r.Stats(ctx)
	}

	decode := func(_ *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		return &kit.MCPDecodeResult{Request: nil}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- publish_profile ---

type publishProfileRequest struct {
	URLPattern string   `json:"url_pattern"`
	Domain     string   `json:"domain"`
	SchemaID   string   `json:"schema_id,omitempty"`
	Extractors string   `json:"extractors"`
	DOMProfile string   `json:"dom_profile"`
	TrustLevel string   `json:"trust_level,omitempty"`
	InstanceID string   `json:"instance_id,omitempty"`
}

func (r *Registry) registerPublishProfileTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "domregistry_publish_profile",
		Description: "Publish a DOM profile to the community registry. Creates or updates a profile for a URL pattern.",
		InputSchema: inputSchema(map[string]any{
			"url_pattern": map[string]any{"type": "string", "description": "URL pattern (regex or glob) this profile matches"},
			"domain":      map[string]any{"type": "string", "description": "Domain name (e.g. example.com)"},
			"schema_id":   map[string]any{"type": "string", "description": "Optional JSON Schema ID for output validation"},
			"extractors":  map[string]any{"type": "string", "description": "JSON: extraction strategy and selectors"},
			"dom_profile": map[string]any{"type": "string", "description": "JSON: landmarks, zones, fingerprint"},
			"trust_level": map[string]any{"type": "string", "enum": []any{"official", "institutional", "community"}, "description": "Trust level (default: community)"},
			"instance_id": map[string]any{"type": "string", "description": "Publishing instance ID"},
		}, []string{"url_pattern", "domain", "extractors", "dom_profile"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		rr := req.(*publishProfileRequest)
		trust := rr.TrustLevel
		if trust == "" {
			trust = "community"
		}
		var contribs []string
		if rr.InstanceID != "" {
			contribs = []string{rr.InstanceID}
		}
		p := &store.Profile{
			ID:           idgen.New(),
			URLPattern:   rr.URLPattern,
			Domain:       rr.Domain,
			SchemaID:     rr.SchemaID,
			Extractors:   rr.Extractors,
			DOMProfile:   rr.DOMProfile,
			TrustLevel:   trust,
			Contributors: contribs,
		}
		if err := r.PublishProfile(ctx, p); err != nil {
			return nil, err
		}
		return p, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var rr publishProfileRequest
		if err := json.Unmarshal(req.Params.Arguments, &rr); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &rr}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}
