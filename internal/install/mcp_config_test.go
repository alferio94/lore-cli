package install

import (
	"context"
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

// TestPiAdapterRenderMaterializesBearerTokenPlaintext verifies that the Pi adapter's
// rendered mcp.json contains the Bearer token in plaintext (via SavedToken replacement),
// matching Antigravity mcp_config.json behavior. The installed file at ~/.pi/agent/mcp.json
// must contain "Authorization": "Bearer <actual-token>" directly, not an env-var placeholder.
//
// This test covers the contract fix for pi-default-hosted-mcp-install:
// - The source template uses {{LORE_API_TOKEN}} which is replaced at render time
// - The installed file contains the plaintext token, not ${LORE_API_TOKEN}
func TestPiAdapterRenderMaterializesBearerTokenPlaintext(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()
	const testToken = "test-lore-token-plaintext-verification"

	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetPi,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP},
		ServerURL:  "https://lore.example.test",
		SavedToken: testToken,
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	// Find the rendered mcp.json file.
	var mcpContent string
	for _, file := range rendered {
		if file.RelativePath == "mcp.json" {
			mcpContent = string(file.Content)
			break
		}
	}
	if mcpContent == "" {
		t.Fatal("rendered files missing mcp.json for hosted MCP default")
	}

	// The installed mcp.json must contain the plaintext Bearer token,
	// matching how Antigravity renders mcp_config.json with the actual token.
	wantBearerLine := `"Authorization": "Bearer ` + testToken + `"`
	if !strings.Contains(mcpContent, wantBearerLine) {
		t.Fatalf("mcp.json does not contain plaintext Bearer token.\ngot mcp.json:\n%s\n\nwant Authorization line: %q", mcpContent, wantBearerLine)
	}

	// The installed file must NOT contain the old env-var shell placeholder.
	if strings.Contains(mcpContent, "${LORE_API_TOKEN}") {
		t.Fatalf("mcp.json contains forbidden env-var placeholder ${LORE_API_TOKEN}.\ngot mcp.json:\n%s", mcpContent)
	}

	// The installed file must NOT contain the double-brace template placeholder.
	if strings.Contains(mcpContent, "{{LORE_API_TOKEN}}") {
		t.Fatalf("mcp.json contains unrendered template placeholder {{LORE_API_TOKEN}}.\ngot mcp.json:\n%s", mcpContent)
	}

	// Verify the hosted MCP HTTP endpoint is present.
	if !strings.Contains(mcpContent, `"url":`) {
		t.Fatalf("mcp.json missing url field, want HTTP endpoint config")
	}
	if !strings.Contains(mcpContent, "https://lore.example.test/v1/mcp") {
		t.Fatalf("mcp.json missing server URL, want https://lore.example.test/v1/mcp")
	}
}

// TestPiAdapterRenderRedactsTokenInOtherFiles verifies that no other rendered file
// (besides mcp.json) contains the plaintext token. The token should only be
// materialized in the MCP config file.
func TestPiAdapterRenderRedactsTokenInOtherFiles(t *testing.T) {
	adapter := defaultPiAdapter()
	definition := agentpack.DefaultDefinition()
	const testToken = "super-secret-token-redaction-test"

	rendered, err := adapter.Render(context.Background(), RenderRequest{
		Target:     TargetPi,
		Definition: definition,
		Components: []ComponentID{ComponentCorePack, ComponentLoreServerMCP, ComponentExtendedSkills},
		ServerURL:  "https://lore.example.test",
		SavedToken: testToken,
	})
	if err != nil {
		t.Fatalf("Render error = %v, want nil", err)
	}

	for _, file := range rendered {
		content := string(file.Content)
		// mcp.json is the only file that should contain the plaintext token.
		if file.RelativePath == "mcp.json" {
			if !strings.Contains(content, "super-secret-token-redaction-test") {
				t.Errorf("mcp.json missing plaintext token, want token present in Authorization header")
			}
			continue
		}
		// settings.json and all other files must NOT contain the plaintext token.
		if strings.Contains(content, testToken) {
			t.Errorf("file %q contains plaintext token %q, want token omitted from non-adapter files", file.RelativePath, testToken)
		}
	}
}
