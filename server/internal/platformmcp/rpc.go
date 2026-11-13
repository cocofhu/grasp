// Package platformmcp provides shared JSON-RPC helpers for platform HTTP MCP hosts.
package platformmcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
)

const ProtocolVersion = "2024-11-05"

// RPCRequest is one JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// RPCResponse is one JSON-RPC 2.0 response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolCallParams is tools/call params.
type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Ok builds a success response.
func Ok(req RPCRequest, result any) (int, []byte) {
	return 200, MustJSON(RPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
}

// Fail builds an error response with HTTP 200 (JSON-RPC style).
func Fail(req RPCRequest, code int, msg string) (int, []byte) {
	return 200, MustJSON(RPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: code, Message: msg}})
}

// Unauthorized returns HTTP 401.
func Unauthorized() (int, []byte) {
	return 401, MustJSON(RPCResponse{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: -32001, Message: "unauthorized"},
	})
}

// ParseError returns HTTP 400 parse error.
func ParseError() (int, []byte) {
	return 400, MustJSON(RPCResponse{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: "parse error"}})
}

// MustJSON marshals v or "{}".
func MustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// MustString pretty-prints v as text for tool content.
func MustString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

// ServeTools answers one parsed JSON-RPC message for a tools-only MCP server
// named server. tools is read only for tools/list; call runs one tools/call and
// its result is wrapped with ToolResult.
func ServeTools(req RPCRequest, server string, tools func() []map[string]any, call func(name string, args map[string]any) (any, bool)) (int, []byte) {
	switch req.Method {
	case "initialize":
		ver := ProtocolVersion
		var ip struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &ip) == nil && ip.ProtocolVersion != "" {
			ver = ip.ProtocolVersion
		}
		return Ok(req, map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": server, "version": "1.0.0"},
		})
	case "notifications/initialized", "notifications/cancelled":
		return 202, nil
	case "ping":
		return Ok(req, map[string]any{})
	case "tools/list":
		return Ok(req, map[string]any{"tools": tools()})
	case "tools/call":
		var p ToolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return Fail(req, -32602, "invalid tools/call params")
		}
		result, isErr := call(p.Name, p.Arguments)
		return Ok(req, ToolResult(result, isErr))
	default:
		if IsNotification(req) {
			return 202, nil
		}
		return Fail(req, -32601, "method not found: "+req.Method)
	}
}

// Tool builds one tools/list entry.
func Tool(name, desc string, props map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if props != nil {
		schema["properties"] = props
	}
	return map[string]any{
		"name": name, "description": desc,
		"inputSchema": schema,
	}
}

// ToolResult wraps a tool call result as MCP content.
func ToolResult(result any, isErr bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": MustString(result)}},
		"isError": isErr,
	}
}

// StrArg reads a string argument.
func StrArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// IntArg reads an int argument with default.
func IntArg(args map[string]any, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return def
	}
}

// BoolArg reads a bool argument.
func BoolArg(args map[string]any, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// MapArg reads a map[string]any argument.
func MapArg(args map[string]any, key string) map[string]any {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// StringSliceArg reads a []string-like argument, accepting decoded []any payloads.
func StringSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	if ss, ok := v.([]string); ok {
		return ss
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// EnvEntriesArg reads optional [{key,value,secret?}] env entries for StartRun.
// Missing/nil → nil,nil. Malformed array items → error (reject start).
func EnvEntriesArg(args map[string]any, key string) ([]models.EnvEntry, error) {
	if args == nil {
		return nil, nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("env must be an array")
	}
	out := make([]models.EnvEntry, 0, len(arr))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("env[%d] must be an object", i)
		}
		e := models.EnvEntry{
			Key:   strAny(m["key"]),
			Value: strAny(m["value"]),
		}
		if b, ok := m["secret"].(bool); ok {
			e.Secret = b
		}
		out = append(out, e)
	}
	return out, nil
}

func strAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// NewToken returns a random hex token.
func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// IsNotification reports whether the request has no id.
func IsNotification(req RPCRequest) bool {
	return len(req.ID) == 0 || string(req.ID) == "null"
}
