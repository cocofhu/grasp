package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// JSON-RPC 2.0 helpers: classify inbound lines from the Agent.

// RPCError is the error member of a JSON-RPC response; Call returns it when
// the peer rejected the request (as opposed to a transport failure).
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// JSONRPCIDKey normalizes a JSON-RPC "id" value for map lookups.
func JSONRPCIDKey(id json.RawMessage) string {
	if len(id) == 0 || string(id) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(id, &v); err != nil {
		return string(id)
	}
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case string:
		return x
	default:
		return string(id)
	}
}

// InboundKind describes a single line read from the Agent stdout.
type InboundKind int

const (
	InboundUnknown InboundKind = iota
	InboundResponse
	InboundRequest
	InboundNotification
)

type Inbound struct {
	Kind   InboundKind
	ID     string          // normalized key for routing (empty for notifications)
	RawID  json.RawMessage // original id field for JSON-RPC responses; nil if absent
	Method string
	Raw    json.RawMessage
}

func classifyInbound(line []byte) (Inbound, error) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Inbound{}, fmt.Errorf("empty line")
	}
	var probe struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return Inbound{}, err
	}
	idk := JSONRPCIDKey(probe.ID)
	switch {
	case idk != "" && (probe.Result != nil || probe.Error != nil):
		return Inbound{Kind: InboundResponse, ID: idk, RawID: probe.ID, Raw: line}, nil
	case idk != "" && probe.Method != "":
		return Inbound{Kind: InboundRequest, ID: idk, RawID: probe.ID, Method: probe.Method, Raw: line}, nil
	case probe.Method != "" && idk == "":
		return Inbound{Kind: InboundNotification, Method: probe.Method, Raw: line}, nil
	default:
		return Inbound{Kind: InboundUnknown, Raw: line}, nil
	}
}
