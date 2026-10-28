package ai

import (
	"encoding/json"
	"fmt"
)

// This file is the JSON-RPC 2.0 layer that the MCP protocol is defined on.
//
// MCP is not a bespoke JSON shape: it is JSON-RPC 2.0 with a fixed method
// vocabulary (initialize, tools/list, tools/call, ping) and a version string
// negotiated at the start of a session. Speaking the protocol rather than
// something protocol-shaped is what lets an off-the-shelf client connect, so the
// envelope is implemented to the specification and not to convenience.

// JSON-RPC 2.0 error codes. The range is defined by the specification; the
// server-reserved range is -32000 to -32099.
const (
	// CodeParseError means the body was not valid JSON.
	CodeParseError = -32700
	// CodeInvalidRequest means the body was JSON but not a valid request object.
	CodeInvalidRequest = -32600
	// CodeMethodNotFound means the method is not implemented.
	CodeMethodNotFound = -32601
	// CodeInvalidParams means the parameters failed validation.
	CodeInvalidParams = -32602
	// CodeInternalError means the handler failed for a reason the client cannot fix.
	CodeInternalError = -32603
)

// MCPProtocolVersion is the protocol revision this server implements.
//
// It is sent in the initialize result. A client that requested a different
// revision is told which one it got, which the specification requires, so it can
// decide whether to continue rather than silently misinterpreting the session.
const MCPProtocolVersion = "2025-06-18"

// supportedProtocolVersions lists the revisions this server can speak.
//
// More than one is listed because clients in the field are not upgraded in step
// with the specification, and refusing an older revision would lock them out of a
// server whose messages they already understand.
var supportedProtocolVersions = []string{
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// Request is a JSON-RPC 2.0 request or notification.
//
// ID is kept as raw JSON because the specification allows a string or a number,
// and the server must echo back exactly what it received. Decoding it into any
// Go type would change a client's numeric id into a float or a string and break
// its correlation.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the message is a notification.
//
// A notification has no id and must not be answered: replying to one is a
// protocol error that some clients treat as a fatal framing failure.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// Response is a JSON-RPC 2.0 response.
//
// Result and Error are mutually exclusive; exactly one is set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

// errorResponse builds a response carrying an error.
func errorResponse(id json.RawMessage, code int, message string, data any) Response {
	if len(id) == 0 {
		// A response must carry an id member; the specification uses null when the
		// id could not be determined, which is the case for a parse error.
		id = json.RawMessage("null")
	}
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message, Data: data},
	}
}

// resultResponse builds a response carrying a result.
func resultResponse(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}
