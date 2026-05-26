package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Upstream interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

type UpstreamFunc func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)

func (f UpstreamFunc) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	return f(ctx, method, params)
}

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type responseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type frameReadError struct {
	Code    int
	Message string
}

func (e *frameReadError) Error() string {
	if e == nil {
		return "invalid MCP frame"
	}
	return e.Message
}

var SupportedMethods = map[string]struct{}{
	"initialize": {},
	"tools/list": {},
	"tools/call": {},
}

func Serve(ctx context.Context, in io.Reader, out io.Writer, upstream Upstream) error {
	if upstream == nil {
		return errors.New("mcp upstream is not configured")
	}
	reader := bufio.NewReader(in)
	for {
		payload, err := readFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			var frameErr *frameReadError
			if errors.As(err, &frameErr) {
				if writeErr := writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: frameErr.Code, Message: frameErr.Message}}); writeErr != nil {
					return writeErr
				}
				continue
			}
			return err
		}
		if err := handleFrame(ctx, out, upstream, payload); err != nil {
			return err
		}
	}
}

func handleFrame(ctx context.Context, out io.Writer, upstream Upstream, payload []byte) error {
	var req requestEnvelope
	if err := json.Unmarshal(payload, &req); err != nil {
		return writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "invalid JSON-RPC payload"}})
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		return writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: normalizeID(req.ID), Error: &rpcError{Code: -32600, Message: "invalid JSON-RPC request"}})
	}
	if strings.TrimSpace(req.Method) == "notifications/initialized" && len(bytes.TrimSpace(req.ID)) == 0 {
		return nil
	}
	if !isSupportedMethod(req.Method) {
		return writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: normalizeID(req.ID), Error: &rpcError{Code: -32601, Message: fmt.Sprintf("unsupported MCP method %q", req.Method)}})
	}
	params := normalizeParams(req.Params)
	result, err := upstream.Call(ctx, req.Method, params)
	if err != nil {
		return writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: normalizeID(req.ID), Error: &rpcError{Code: -32000, Message: err.Error()}})
	}
	return writeResponse(out, responseEnvelope{JSONRPC: "2.0", ID: normalizeID(req.ID), Result: normalizeResult(result)})
}

func isSupportedMethod(method string) bool {
	_, ok := SupportedMethods[method]
	return ok
}

func normalizeID(id json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(id)) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func normalizeParams(params json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(params)) == 0 {
		return json.RawMessage(`{}`)
	}
	return params
}

func normalizeResult(result json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(result)) == 0 {
		return json.RawMessage("null")
	}
	return result
}

func writeResponse(out io.Writer, response responseEnvelope) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("encode MCP response: %w", err)
	}
	_, err = fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(payload), payload)
	return err
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	var frameErr *frameReadError
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				if frameErr != nil {
					return nil, frameErr
				}
				return nil, io.EOF
			}
			return nil, &frameReadError{Code: -32700, Message: "invalid MCP frame: could not read frame header"}
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		if frameErr != nil {
			continue
		}
		name, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			frameErr = &frameReadError{Code: -32700, Message: fmt.Sprintf("invalid MCP frame header %q", trimmed)}
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length < 0 {
				frameErr = &frameReadError{Code: -32700, Message: fmt.Sprintf("invalid Content-Length %q", strings.TrimSpace(value))}
				continue
			}
			contentLength = length
		}
	}
	if frameErr != nil {
		return nil, frameErr
	}
	if contentLength < 0 {
		return nil, &frameReadError{Code: -32700, Message: "missing Content-Length header"}
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, &frameReadError{Code: -32700, Message: "invalid MCP frame payload"}
	}
	return payload, nil
}
