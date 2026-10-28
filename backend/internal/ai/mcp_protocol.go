package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// MCPServer speaks the Model Context Protocol over JSON-RPC 2.0.
//
// It is a transport in front of ToolRegistry, not a second implementation: every
// call reaches the same tool, the same permission check and the same audit entry
// as the REST API. That is what makes it safe to expose to an agent — the
// protocol layer can decide how a call arrives, never whether it is allowed.
type MCPServer struct {
	tools *ToolRegistry
}

// NewMCPServer builds the protocol handler over a tool registry.
func NewMCPServer(tools *ToolRegistry) *MCPServer { return &MCPServer{tools: tools} }

// Handle processes one JSON-RPC message and returns the response to send.
//
// The second return value is false for a notification, which must not be
// answered at all. A notification that produced a response would break clients
// that frame strictly.
func (s *MCPServer) Handle(ctx context.Context, p *rbac.Principal, body []byte) (Response, bool) {
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return errorResponse(nil, CodeParseError, "the request body is not valid JSON", nil), true
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return errorResponse(req.ID, CodeInvalidRequest,
			fmt.Sprintf(`jsonrpc must be "2.0", got %q`, req.JSONRPC), nil), true
	}
	if req.Method == "" {
		return errorResponse(req.ID, CodeInvalidRequest, "method is required", nil), true
	}

	switch req.Method {
	case "initialize":
		return resultResponse(req.ID, s.initialize(req.Params)), true
	case "notifications/initialized", "notifications/cancelled":
		// Notifications: acknowledged by not replying.
		return Response{}, false
	case "ping":
		// The specification defines ping as an empty result; it is used to check
		// that the connection is alive without touching any tool.
		return resultResponse(req.ID, map[string]any{}), true
	case "tools/list":
		return resultResponse(req.ID, map[string]any{"tools": s.tools.MCPDescriptors()}), true
	case "tools/call":
		result, err := s.callTool(ctx, p, req.Params)
		if err != nil {
			return rpcErrorFor(req.ID, err), true
		}
		return resultResponse(req.ID, result), true
	default:
		return errorResponse(req.ID, CodeMethodNotFound,
			fmt.Sprintf("method %q is not implemented", req.Method), nil), true
	}
}

// initialize answers the handshake.
//
// The negotiated version is echoed back even when it differs from the requested
// one. The specification requires this so a client learns which revision it is
// actually talking to, instead of assuming it got what it asked for.
func (s *MCPServer) initialize(params json.RawMessage) map[string]any {
	version := MCPProtocolVersion
	var req struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &req) == nil && req.ProtocolVersion != "" {
		if supported(req.ProtocolVersion) {
			version = req.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": version,
		// Tools only. Advertising resources or prompts that are not implemented
		// would have a client request a method this server answers with
		// "not implemented", which reads as a broken server rather than a
		// deliberate one.
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    "forgecrm",
			"version": "1.0.0",
		},
		"instructions": "ForgeCRM CRM tools. Every tool runs with the calling user's own " +
			"permissions; a tool the user cannot perform is refused, not silently narrowed.",
	}
}

// callTool validates the parameters and invokes a tool.
//
// The parameters are re-encoded and passed through the same path as a REST call,
// so a protocol-level mistake cannot produce a weaker authorization check.
func (s *MCPServer) callTool(ctx context.Context, p *rbac.Principal,
	params json.RawMessage) (any, error) {

	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, httpx.NewError(400, httpx.CodeBadRequest, "params must be an object")
		}
	}
	if call.Name == "" {
		return nil, httpx.NewError(400, httpx.CodeBadRequest, "params.name is required")
	}
	if call.Arguments == nil {
		call.Arguments = map[string]any{}
	}

	// A mutating tool needs an explicit acknowledgement from the caller. The
	// confirmation travels in the arguments so that it is visible in the tool's
	// published schema: a client can see the requirement before calling, and the
	// check cannot be satisfied by a transport-level flag an agent never sets.
	if err := s.tools.RequireConfirmation(call.Name, call.Arguments); err != nil {
		return nil, err
	}

	result, err := s.tools.Call(ctx, p, call.Name, call.Arguments)
	if err != nil {
		return nil, err
	}

	// The result of a tool call is content, not an error, even when the tool
	// reports a problem: MCP distinguishes a failed request (an error object) from
	// a successful call whose result describes a failure.
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, httpx.NewError(500, httpx.CodeInternal, "the tool result could not be encoded")
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		"isError": false,
	}, nil
}

// rpcErrorFor maps a domain error onto a JSON-RPC error.
//
// The code is chosen from the HTTP status the domain layer already uses, so one
// error cannot be a 403 over REST and a success over MCP.
func rpcErrorFor(id json.RawMessage, err error) Response {
	status := 500
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) && apiErr.Status != 0 {
		status = apiErr.Status
	}
	switch status {
	case 400, 422:
		return errorResponse(id, CodeInvalidParams, err.Error(), nil)
	case 404:
		return errorResponse(id, CodeMethodNotFound, err.Error(), nil)
	case 401, 403:
		// A permission failure is a failed request, not a hidden result. Reporting
		// it as invalid params would tell a client to fix its arguments when the
		// problem is the caller's authority.
		return errorResponse(id, CodeInvalidRequest, err.Error(), map[string]any{"status": status})
	default:
		msg := "the tool call failed"
		if apiErr != nil && apiErr.Message != "" {
			msg = apiErr.Message
		}
		return errorResponse(id, CodeInternalError, msg, nil)
	}
}

func supported(v string) bool {
	for _, s := range supportedProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}
