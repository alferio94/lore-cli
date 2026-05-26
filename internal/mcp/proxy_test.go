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

func TestSupportedMethodsRegistryMatchesMVPBoundary(t *testing.T) {
	for _, method := range []string{"initialize", "tools/list", "tools/call"} {
		if _, ok := SupportedMethods[method]; !ok {
			t.Fatalf("SupportedMethods[%q] missing", method)
		}
		if !isSupportedMethod(method) {
			t.Fatalf("isSupportedMethod(%q) = false, want true", method)
		}
	}
	for _, method := range []string{"notifications/initialized", "prompts/list"} {
		if _, ok := SupportedMethods[method]; ok {
			t.Fatalf("SupportedMethods[%q] unexpectedly present", method)
		}
		if isSupportedMethod(method) {
			t.Fatalf("isSupportedMethod(%q) = true, want false", method)
		}
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

func TestServeReturnsFrameReadErrorsAsJSONRPCResponses(t *testing.T) {
	input := strings.NewReader("Content-Length: nope\r\n\r\n" + testFrame(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	upstream := &fakeUpstream{results: map[string]json.RawMessage{"tools/list": json.RawMessage(`{"tools":[]}`)}}
	var output bytes.Buffer

	if err := Serve(context.Background(), input, &output, upstream); err != nil {
		t.Fatalf("Serve() error = %v, want nil", err)
	}

	reader := bufio.NewReader(&output)
	first := decodeProxyFrame(t, reader)
	second := decodeProxyFrame(t, reader)

	var firstEnvelope struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(first, &firstEnvelope); err != nil {
		t.Fatalf("json.Unmarshal(first): %v", err)
	}
	if string(firstEnvelope.ID) != `null` || firstEnvelope.Error.Code != -32700 || !strings.Contains(firstEnvelope.Error.Message, "invalid Content-Length") {
		t.Fatalf("first response = %s", first)
	}
	assertProxyResult(t, second, `2`, `{"tools":[]}`)
}

func TestServeDualModeHappyPathAndMirroredResponses(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		decode         func(*testing.T, *bufio.Reader) []byte
		wantNoHeader   bool
		wantInitialize string
		wantList       string
	}{
		{
			name: "jsonl session",
			input: `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tester"}}}` + "\n" +
				`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
			decode:         decodeJSONLResponse,
			wantNoHeader:   true,
			wantInitialize: `{"protocolVersion":"2025-03-26"}`,
			wantList:       `{"tools":[]}`,
		},
		{
			name: "content-length session",
			input: testFrame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tester"}}}`) +
				testFrame(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`),
			decode:         decodeProxyFrame,
			wantInitialize: `{"protocolVersion":"2025-03-26"}`,
			wantList:       `{"tools":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &fakeUpstream{results: map[string]json.RawMessage{
				"initialize": json.RawMessage(tt.wantInitialize),
				"tools/list": json.RawMessage(tt.wantList),
			}}
			var output bytes.Buffer

			if err := Serve(context.Background(), strings.NewReader(tt.input), &output, upstream); err != nil {
				t.Fatalf("Serve() error = %v, want nil", err)
			}
			if len(upstream.calls) != 2 {
				t.Fatalf("len(calls) = %d, want 2", len(upstream.calls))
			}
			if tt.wantNoHeader && strings.Contains(output.String(), "Content-Length:") {
				t.Fatalf("output unexpectedly used Content-Length framing: %q", output.String())
			}

			reader := bufio.NewReader(&output)
			assertProxyResult(t, tt.decode(t, reader), `1`, tt.wantInitialize)
			assertProxyResult(t, tt.decode(t, reader), `2`, tt.wantList)
		})
	}
}

func TestServeRejectsMixedFramingAfterModeLock(t *testing.T) {
	t.Run("jsonl session rejects content-length frame and recovers", func(t *testing.T) {
		input := strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n" +
				testFrame(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`) +
				`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}` + "\n",
		)
		upstream := &fakeUpstream{results: map[string]json.RawMessage{"tools/list": json.RawMessage(`{"tools":[]}`)}}
		var output bytes.Buffer

		if err := Serve(context.Background(), input, &output, upstream); err != nil {
			t.Fatalf("Serve() error = %v, want nil", err)
		}
		if len(upstream.calls) != 2 {
			t.Fatalf("len(calls) = %d, want 2", len(upstream.calls))
		}

		reader := bufio.NewReader(&output)
		assertProxyResult(t, decodeJSONLResponse(t, reader), `1`, `{"tools":[]}`)
		assertProxyError(t, decodeJSONLResponse(t, reader), `null`, -32600, "mixed framing")
		assertProxyResult(t, decodeJSONLResponse(t, reader), `3`, `{"tools":[]}`)
	})

	t.Run("content-length session rejects jsonl frame and recovers", func(t *testing.T) {
		input := strings.NewReader(
			testFrame(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`) +
				`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n" +
				testFrame(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`),
		)
		upstream := &fakeUpstream{results: map[string]json.RawMessage{"tools/list": json.RawMessage(`{"tools":[]}`)}}
		var output bytes.Buffer

		if err := Serve(context.Background(), input, &output, upstream); err != nil {
			t.Fatalf("Serve() error = %v, want nil", err)
		}
		if len(upstream.calls) != 2 {
			t.Fatalf("len(calls) = %d, want 2", len(upstream.calls))
		}

		reader := bufio.NewReader(&output)
		assertProxyResult(t, decodeProxyFrame(t, reader), `1`, `{"tools":[]}`)
		assertProxyError(t, decodeProxyFrame(t, reader), `null`, -32600, "mixed framing")
		assertProxyResult(t, decodeProxyFrame(t, reader), `3`, `{"tools":[]}`)
	})
}

func TestServeReturnsModeSpecificParseErrorsWithoutSwitchingModes(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		decode    func(*testing.T, *bufio.Reader) []byte
		wantCode  int
		wantSub   string
		wantCalls int
	}{
		{
			name: "malformed jsonl recovers",
			input: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n" +
				`{"jsonrpc":"2.0","id":` + "\n" +
				`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}` + "\n",
			decode:    decodeJSONLResponse,
			wantCode:  -32700,
			wantSub:   "invalid JSON-RPC payload",
			wantCalls: 2,
		},
		{
			name: "malformed content-length frame recovers",
			input: testFrame(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`) +
				"Content-Length: nope\r\n\r\n" +
				testFrame(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`),
			decode:    decodeProxyFrame,
			wantCode:  -32700,
			wantSub:   "invalid Content-Length",
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &fakeUpstream{results: map[string]json.RawMessage{"tools/list": json.RawMessage(`{"tools":[]}`)}}
			var output bytes.Buffer

			if err := Serve(context.Background(), strings.NewReader(tt.input), &output, upstream); err != nil {
				t.Fatalf("Serve() error = %v, want nil", err)
			}
			if len(upstream.calls) != tt.wantCalls {
				t.Fatalf("len(calls) = %d, want %d", len(upstream.calls), tt.wantCalls)
			}

			reader := bufio.NewReader(&output)
			assertProxyResult(t, tt.decode(t, reader), `1`, `{"tools":[]}`)
			assertProxyError(t, tt.decode(t, reader), `null`, tt.wantCode, tt.wantSub)
			assertProxyResult(t, tt.decode(t, reader), `3`, `{"tools":[]}`)
		})
	}
}

func TestReadPayloadDetectsFramingMode(t *testing.T) {
	tests := []struct {
		name        string
		mode        frameMode
		input       string
		wantMode    frameMode
		wantPayload string
	}{
		{name: "unknown detects content-length", mode: frameModeUnknown, input: testFrame(`{"jsonrpc":"2.0","id":1}`), wantMode: frameModeContentLength, wantPayload: `{"jsonrpc":"2.0","id":1}`},
		{name: "unknown detects jsonl", mode: frameModeUnknown, input: `{"jsonrpc":"2.0","id":2}` + "\n", wantMode: frameModeJSONL, wantPayload: `{"jsonrpc":"2.0","id":2}`},
		{name: "jsonl skips blank lines", mode: frameModeJSONL, input: "\n \n" + `{"jsonrpc":"2.0","id":3}` + "\n", wantMode: frameModeJSONL, wantPayload: `{"jsonrpc":"2.0","id":3}`},
		{name: "content-length explicit mode", mode: frameModeContentLength, input: testFrame(`{"jsonrpc":"2.0","id":4}`), wantMode: frameModeContentLength, wantPayload: `{"jsonrpc":"2.0","id":4}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := readPayload(bufio.NewReader(strings.NewReader(tt.input)), tt.mode)
			if err != nil {
				t.Fatalf("readPayload() error = %v", err)
			}
			if payload.mode != tt.wantMode {
				t.Fatalf("mode = %v, want %v", payload.mode, tt.wantMode)
			}
			if string(payload.payload) != tt.wantPayload {
				t.Fatalf("payload = %q, want %q", payload.payload, tt.wantPayload)
			}
		})
	}
}

func TestWriteResponseWithModeMirrorsFraming(t *testing.T) {
	response := responseEnvelope{JSONRPC: "2.0", ID: json.RawMessage(`1`), Result: json.RawMessage(`{"ok":true}`)}

	t.Run("content-length remains byte compatible", func(t *testing.T) {
		var output bytes.Buffer
		if err := writeResponseWithMode(&output, frameModeContentLength, response); err != nil {
			t.Fatalf("writeResponseWithMode() error = %v", err)
		}
		want := testFrame(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
		if output.String() != want {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	})

	t.Run("jsonl emits compact payload plus newline", func(t *testing.T) {
		var output bytes.Buffer
		if err := writeResponseWithMode(&output, frameModeJSONL, response); err != nil {
			t.Fatalf("writeResponseWithMode() error = %v", err)
		}
		want := `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n"
		if output.String() != want {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	})
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

func decodeJSONLResponse(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	payload, err := readJSONLFrame(reader)
	if err != nil {
		t.Fatalf("readJSONLFrame() error = %v", err)
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

func assertProxyError(t *testing.T, payload []byte, wantID string, wantCode int, wantSubstr string) {
	t.Helper()
	var envelope struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("json.Unmarshal(payload): %v", err)
	}
	if string(envelope.ID) != wantID {
		t.Fatalf("id = %s, want %s", envelope.ID, wantID)
	}
	if envelope.Error.Code != wantCode || !strings.Contains(envelope.Error.Message, wantSubstr) {
		t.Fatalf("error = %+v, want code=%d substring %q", envelope.Error, wantCode, wantSubstr)
	}
}
