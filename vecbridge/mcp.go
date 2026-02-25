// CLAUDE:SUMMARY Registers horosvec MCP tools: search, insert, stats, and similar.
package vecbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hazyhaar/pkg/kit"
)

// RegisterMCP registers vecbridge tools on an MCP server.
func (s *Service) RegisterMCP(srv *mcp.Server) {
	s.registerSearchTool(srv)
	s.registerInsertTool(srv)
	s.registerStatsTool(srv)
	s.registerSimilarTool(srv)
}

func inputSchema(properties map[string]any, required []string) map[string]any {
	sc := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		sc["required"] = required
	}
	return sc
}

// --- search ---

type searchReq struct {
	Vector   []float32 `json:"vector"`
	TopK     int       `json:"top_k,omitempty"`
	EfSearch int       `json:"ef_search,omitempty"`
}

func (s *Service) registerSearchTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "horosvec_search",
		Description: "Search the vector index for the top-K nearest neighbors of a query vector.",
		InputSchema: inputSchema(map[string]any{
			"vector": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "number"},
				"description": "Query vector (float32 array)",
			},
			"top_k":     map[string]any{"type": "integer", "description": "Number of results (default: 10)"},
			"ef_search": map[string]any{"type": "integer", "description": "Beam width for search (default: from config)"},
		}, []string{"vector"}),
	}

	endpoint := func(ctx context.Context, req any) (any, error) {
		r := req.(*searchReq)
		topK := r.TopK
		if topK <= 0 {
			topK = 10
		}
		results, err := s.Index.Search(r.Vector, topK)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, len(results))
		for i, res := range results {
			out[i] = map[string]any{
				"id":    hex.EncodeToString(res.ID),
				"score": res.Score,
			}
		}
		return map[string]any{"results": out, "count": len(out)}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r searchReq
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- insert ---

type insertReq struct {
	IDs     []string    `json:"ids"`
	Vectors [][]float32 `json:"vectors"`
}

func (s *Service) registerInsertTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "horosvec_insert",
		Description: "Insert vectors into the index with their external IDs.",
		InputSchema: inputSchema(map[string]any{
			"ids": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "External IDs (hex-encoded)",
			},
			"vectors": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
				"description": "Vectors to insert",
			},
		}, []string{"ids", "vectors"}),
	}

	endpoint := func(_ context.Context, req any) (any, error) {
		r := req.(*insertReq)
		ids := make([][]byte, len(r.IDs))
		for i, id := range r.IDs {
			b, err := hex.DecodeString(id)
			if err != nil {
				ids[i] = []byte(id)
			} else {
				ids[i] = b
			}
		}
		if err := s.Index.Insert(r.Vectors, ids); err != nil {
			return nil, err
		}
		return map[string]any{"inserted": len(r.Vectors), "count": s.Index.Count()}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r insertReq
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- stats ---

func (s *Service) registerStatsTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "horosvec_stats",
		Description: "Get vector index statistics: node count, rebuild status.",
		InputSchema: inputSchema(map[string]any{}, nil),
	}

	endpoint := func(_ context.Context, _ any) (any, error) {
		return map[string]any{
			"count":         s.Index.Count(),
			"needs_rebuild": s.Index.NeedsRebuild(),
		}, nil
	}

	decode := func(_ *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		return &kit.MCPDecodeResult{Request: nil}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}

// --- similar ---

type similarReq struct {
	ID   string `json:"id"`
	TopK int    `json:"top_k,omitempty"`
}

func (s *Service) registerSimilarTool(srv *mcp.Server) {
	tool := &mcp.Tool{
		Name:        "horosvec_similar",
		Description: "Find vectors similar to a given ID already in the index.",
		InputSchema: inputSchema(map[string]any{
			"id":    map[string]any{"type": "string", "description": "External ID (hex-encoded) of the reference vector"},
			"top_k": map[string]any{"type": "integer", "description": "Number of results (default: 10)"},
		}, []string{"id"}),
	}

	endpoint := func(_ context.Context, req any) (any, error) {
		r := req.(*similarReq)
		topK := r.TopK
		if topK <= 0 {
			topK = 10
		}

		idBytes, err := hex.DecodeString(r.ID)
		if err != nil {
			idBytes = []byte(r.ID)
		}

		// Load the vector for this ID from SQLite.
		vec, err := s.loadVector(idBytes)
		if err != nil {
			return nil, err
		}

		results, err := s.Index.Search(vec, topK+1)
		if err != nil {
			return nil, err
		}

		// Filter out the source ID.
		out := make([]map[string]any, 0, topK)
		for _, res := range results {
			resHex := hex.EncodeToString(res.ID)
			if resHex == r.ID {
				continue
			}
			out = append(out, map[string]any{
				"id":    resHex,
				"score": res.Score,
			})
			if len(out) >= topK {
				break
			}
		}
		return map[string]any{"results": out, "count": len(out)}, nil
	}

	decode := func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		var r similarReq
		if err := json.Unmarshal(req.Params.Arguments, &r); err != nil {
			return nil, err
		}
		return &kit.MCPDecodeResult{Request: &r}, nil
	}

	kit.RegisterMCPTool(srv, tool, endpoint, decode)
}
