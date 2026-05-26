package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeUpstream struct {
	calls   []upstreamCall
	results map[string]json.RawMessage
	err     error
}

type upstreamCall struct {
	method string
	params string
}

func (f *fakeUpstream) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	f.calls = append(f.calls, upstreamCall{method: method, params: string(params)})
	if f.err != nil {
		return nil, f.err
	}
	return f.results[method], nil
}

func TestServeForwardsInitializeAndListToolsFrames(t *testing.T) {
	input := strings.NewReader(testFrame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tester"}}}`) +
		testFrame(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	upstream := &fakeUpstream{results: map[string]json.RawMessage{
		"initialize": json.RawMessage(`{"protocolVersion":"2025-03-26","serverInfo":{"name":"lore-cli","version":"dev"}}`),
		"tools/list": json.RawMessage(`{"tools":[{"name":"lore_project_context"}]}`),
	}}
	var output bytes.Buffer

	if err := Serve(context.Background(), input, &output, upstream); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}
	if len(upstream.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(upstream.calls))
	}
	if upstream.calls[0].method != "initialize" || upstream.calls[0].params != `{"clientInfo":{"name":"tester"}}` {
		t.Fatalf("initialize call = %+v", upstream.calls[0])
	}
	if upstream.calls[1].method != "tools/list" || upstream.calls[1].params != `{}` {
		t.Fatalf("tools/list call = %+v", upstream.calls[1])
	}

	reader := bufio.NewReader(&output)
	first := decodeProxyFrame(t, reader)
	second := decodeProxyFrame(t, reader)
	assertProxyResult(t, first, `1`, `{"protocolVersion":"2025-03-26","serverInfo":{"name":"lore-cli","version":"dev"}}`)
	assertProxyResult(t, second, `2`, `{"tools":[{"name":"lore_project_context"}]}`)
}

func TestServeReturnsMethodNotFoundForUnsupportedMethod(t *testing.T) {
	input := strings.NewReader(testFrame(`{"jsonrpc":"2.0","id":"abc","method":"prompts/list","params":{}}`))
	var output bytes.Buffer

	if err := Serve(context.Background(), input, &output, &fakeUpstream{}); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}

	response := decodeProxyFrame(t, bufio.NewReader(&output))
	var envelope struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("json.Unmarshal(response): %v", err)
	}
	if string(envelope.ID) != `"abc"` {
		t.Fatalf("id = %s, want %q", envelope.ID, `"abc"`)
	}
	if envelope.Error.Code != -32601 || !strings.Contains(envelope.Error.Message, "unsupported MCP method") {
		t.Fatalf("error = %+v, want method-not-found", envelope.Error)
	}
}

func TestUpstreamFuncCallDelegates(t *testing.T) {
	called := false
	upstream := UpstreamFunc(func(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		called = true
		if method != "tools/list" || string(params) != `{}` {
			t.Fatalf("Call() got method=%q params=%s", method, params)
		}
		return json.RawMessage(`{"tools":[]}`), nil
	})
	result, err := upstream.Call(context.Background(), "tools/list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Call() error = %v, want nil", err)
	}
	if !called || string(result) != `{"tools":[]}` {
		t.Fatalf("called=%v result=%s, want delegated result", called, result)
	}
}

func TestServeRejectsNilUpstream(t *testing.T) {
	if err := Serve(context.Background(), strings.NewReader(""), &bytes.Buffer{}, nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Serve() err = %v, want upstream configuration error", err)
	}
}

func TestServeNormalizesEmptyIDsParamsAndResults(t *testing.T) {
	input := strings.NewReader(testFrame(`{"jsonrpc":"2.0","method":"tools/list"}`))
	upstream := &fakeUpstream{results: map[string]json.RawMessage{"tools/list": nil}}
	var output bytes.Buffer

	if err := Serve(context.Background(), input, &output, upstream); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}
	if len(upstream.calls) != 1 || upstream.calls[0].params != `{}` {
		t.Fatalf("calls = %+v, want normalized empty object params", upstream.calls)
	}
	response := decodeProxyFrame(t, bufio.NewReader(&output))
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("json.Unmarshal(response): %v", err)
	}
	if string(envelope.ID) != `null` || string(envelope.Result) != `null` {
		t.Fatalf("response = %s, want null id/result normalization", response)
	}
}

func TestServeIgnoresInitializedNotificationWithoutCallingUpstream(t *testing.T) {
	input := strings.NewReader(testFrame(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	upstream := &fakeUpstream{}
	var output bytes.Buffer

	if err := Serve(context.Background(), input, &output, upstream); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}
	if len(upstream.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(upstream.calls))
	}
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty", output.String())
	}
}

func TestServeReturnsParseValidationAndUpstreamErrors(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		upstream   *fakeUpstream
		wantCode   int
		wantID     string
		wantSubstr string
	}{
		{name: "invalid json", payload: `{`, upstream: &fakeUpstream{}, wantCode: -32700, wantID: `null`, wantSubstr: "invalid JSON-RPC payload"},
		{name: "invalid request", payload: `{"jsonrpc":"1.0","id":7,"method":"tools/list"}`, upstream: &fakeUpstream{}, wantCode: -32600, wantID: `7`, wantSubstr: "invalid JSON-RPC request"},
		{name: "upstream failure", payload: `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"x"}}`, upstream: &fakeUpstream{err: errors.New("upstream boom")}, wantCode: -32000, wantID: `9`, wantSubstr: "upstream boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := Serve(context.Background(), strings.NewReader(testFrame(tt.payload)), &output, tt.upstream); err != nil {
				t.Fatalf("Serve() error = %v, want nil", err)
			}
			response := decodeProxyFrame(t, bufio.NewReader(&output))
			var envelope struct {
				ID    json.RawMessage `json:"id"`
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response, &envelope); err != nil {
				t.Fatalf("json.Unmarshal(response): %v", err)
			}
			if envelope.Error.Code != tt.wantCode || string(envelope.ID) != tt.wantID || !strings.Contains(envelope.Error.Message, tt.wantSubstr) {
				t.Fatalf("response = %s, want code=%d id=%s message containing %q", response, tt.wantCode, tt.wantID, tt.wantSubstr)
			}
		})
	}
}

func TestReadFrameRejectsHeaderProblems(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "invalid header", input: "Content-Length\r\n\r\n{}", wantErr: "invalid MCP frame header"},
		{name: "invalid length", input: "Content-Length: nope\r\n\r\n{}", wantErr: "invalid Content-Length"},
		{name: "missing length", input: "Other: 1\r\n\r\n{}", wantErr: "missing Content-Length header"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readFrame(bufio.NewReader(strings.NewReader(tt.input)))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("readFrame() err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func testFrame(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func decodeProxyFrame(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	payload, err := readFrame(reader)
	if err != nil {
		t.Fatalf("readFrame() error = %v", err)
	}
	return payload
}

func assertProxyResult(t *testing.T, payload []byte, wantID, wantResult string) {
	t.Helper()
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	if string(envelope.ID) != wantID {
		t.Fatalf("id = %s, want %s", envelope.ID, wantID)
	}
	if string(envelope.Result) != wantResult {
		t.Fatalf("result = %s, want %s", envelope.Result, wantResult)
	}
}
