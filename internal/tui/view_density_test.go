package tui

import (
	"strings"
	"testing"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/charmbracelet/lipgloss"
)

func TestViewportDensityModes(t *testing.T) {
	tests := []struct {
		name         string
		width        int
		height       int
		wantCompact  bool
		wantShort    bool
		wantCopy     bool
		wantDecorate bool
	}{
		{name: "comfortable", width: 100, height: 30, wantCopy: true, wantDecorate: true},
		{name: "very narrow", width: 38, height: 30, wantCompact: true},
		{name: "very short", width: 100, height: 12, wantShort: true},
		{name: "narrow and short", width: 38, height: 12, wantCompact: true, wantShort: true},
		{name: "no artwork room", width: 80, height: 19, wantShort: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			density := newViewportDensity(tt.width, tt.height)
			if density.Compact != tt.wantCompact {
				t.Fatalf("Compact = %v, want %v", density.Compact, tt.wantCompact)
			}
			if density.Short != tt.wantShort {
				t.Fatalf("Short = %v, want %v", density.Short, tt.wantShort)
			}
			if density.ShowSecondaryCopy() != tt.wantCopy {
				t.Fatalf("ShowSecondaryCopy = %v, want %v", density.ShowSecondaryCopy(), tt.wantCopy)
			}
			if density.ShowDecoration() != tt.wantDecorate {
				t.Fatalf("ShowDecoration = %v, want %v", density.ShowDecoration(), tt.wantDecorate)
			}
			if density.ContentWidth < 24 {
				t.Fatalf("ContentWidth = %d, want safe minimum", density.ContentWidth)
			}
		})
	}
}

func TestTruncateLine(t *testing.T) {
	if got := truncateLine("short", 10); got != "short" {
		t.Fatalf("truncateLine short = %q", got)
	}
	if got := truncateLine("long technical help", 8); got != "long te…" {
		t.Fatalf("truncateLine long = %q", got)
	}
	if got := truncateLine("abc", 1); got != "…" {
		t.Fatalf("truncateLine tiny = %q", got)
	}
}

func TestRenderBodyViewportWrapsSlicesAndIndicators(t *testing.T) {
	viewport := renderBodyViewport("alpha beta gamma delta", 10, 2, 0)
	if viewport.Total <= 2 {
		t.Fatalf("Total = %d, want wrapped/clipped content", viewport.Total)
	}
	if viewport.Above || !viewport.Below {
		t.Fatalf("top indicators above=%v below=%v, want only below", viewport.Above, viewport.Below)
	}
	lines := renderBodyViewportLines(viewport)
	if got := len(lines); got > viewport.Height {
		t.Fatalf("rendered viewport lines = %d, want <= height %d", got, viewport.Height)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "↓ more") {
		t.Fatalf("rendered viewport missing below indicator: %#v", lines)
	}
	viewport = renderBodyViewport("alpha beta gamma delta", 10, 2, 99)
	if !viewport.Above || viewport.Below {
		t.Fatalf("bottom indicators above=%v below=%v, want only above", viewport.Above, viewport.Below)
	}
	lines = renderBodyViewportLines(viewport)
	if got := len(lines); got > viewport.Height {
		t.Fatalf("rendered viewport lines = %d, want <= height %d", got, viewport.Height)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "↑ more") {
		t.Fatalf("rendered viewport missing above indicator: %#v", lines)
	}
	if viewport.Offset != viewport.maxOffset() {
		t.Fatalf("Offset = %d, want clamped max %d", viewport.Offset, viewport.maxOffset())
	}
}

func TestRenderBodyViewportNoIndicatorWhenContentFits(t *testing.T) {
	viewport := renderBodyViewport("short\nbody", 20, 5, 0)
	if viewport.Above || viewport.Below {
		t.Fatalf("fit indicators above=%v below=%v, want none", viewport.Above, viewport.Below)
	}
	if got := len(viewport.Lines); got != 2 {
		t.Fatalf("visible lines = %d, want 2", got)
	}
}

func TestRenderBodyViewportTinyHeightsKeepContentVisible(t *testing.T) {
	viewport := renderBodyViewport("alpha\nbeta\ngamma", 20, 1, 1)
	lines := renderBodyViewportLines(viewport)
	if got := len(lines); got != 1 {
		t.Fatalf("rendered viewport lines = %d, want 1", got)
	}
	if !strings.Contains(lines[0], "beta") || strings.Contains(lines[0], "more") {
		t.Fatalf("height-1 viewport should show content, not only an indicator: %#v", lines)
	}

	viewport = renderBodyViewport("alpha\nbeta\ngamma\ndelta", 20, 2, 1)
	lines = renderBodyViewportLines(viewport)
	if got := len(lines); got != 2 {
		t.Fatalf("rendered viewport lines = %d, want 2", got)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "beta") {
		t.Fatalf("height-2 viewport should preserve a visible content row: %#v", lines)
	}
	if !strings.Contains(joined, "↕ more") {
		t.Fatalf("height-2 interior viewport should keep a useful combined indicator: %#v", lines)
	}
}

func TestDetailBodyViewportHeightAdaptsToTinyTerminal(t *testing.T) {
	comfortable := newViewportDensity(100, 30)
	short := newViewportDensity(100, 8)
	comfortableHeight := detailBodyViewportHeight(comfortable, 3, 2, 1)
	shortHeight := detailBodyViewportHeight(short, 2, 1, 1)
	if comfortableHeight <= shortHeight {
		t.Fatalf("comfortable height = %d, short height = %d; want adaptive reduction", comfortableHeight, shortHeight)
	}
	if shortHeight < 1 {
		t.Fatalf("short height = %d, want functional minimum", shortHeight)
	}
}

func TestNarrowWrappingChangesScrollableLineCount(t *testing.T) {
	body := "one two three four five six seven eight"
	wide := renderBodyViewport(body, 80, 10, 0)
	narrow := renderBodyViewport(body, 8, 10, 0)
	if narrow.Total <= wide.Total {
		t.Fatalf("narrow total = %d, wide total = %d; want more wrapped lines", narrow.Total, wide.Total)
	}
}

func TestRenderViewKeepsLinesWithinTerminalWidth(t *testing.T) {
	for _, tt := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "comfortable", width: 80, height: 24},
		{name: "compact short", width: 38, height: 12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(cli.InteractiveActions{})
			m.width = tt.width
			m.height = tt.height
			for i, line := range strings.Split(m.View(), "\n") {
				if got := lipgloss.Width(line); got > tt.width {
					t.Fatalf("line %d width = %d, want <= %d: %q", i+1, got, tt.width, line)
				}
			}
		})
	}
}

func TestShortRootPickerCollapsesWithinViewportBudget(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	m.width = 80
	m.height = 12
	density := newViewportDensity(m.width, m.height)
	picker := renderRootPicker(m, density)
	maxRows := shellContentHeight(density, rootHeaderLineCount(m, density), 1)
	if got := len(strings.Split(picker, "\n")); got > maxRows {
		t.Fatalf("picker rows = %d, want <= %d:\n%s", got, maxRows, picker)
	}
	for _, want := range []string{"Choose an action", "Status", "Inspect config"} {
		if !strings.Contains(picker, want) {
			t.Fatalf("collapsed picker missing %q:\n%s", want, picker)
		}
	}
	view := m.View()
	if got := len(strings.Split(view, "\n")); got > m.height {
		t.Fatalf("view rows = %d, want <= terminal height %d:\n%s", got, m.height, view)
	}
	if !strings.Contains(view, "↑/↓ navigate") {
		t.Fatalf("short view should keep footer visible:\n%s", view)
	}
}

func TestShortRootPickerKeepsActiveSelectionWhenCollapsed(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	m.width = 80
	m.height = 12
	m.selected = 5
	picker := renderRootPicker(m, newViewportDensity(m.width, m.height))
	for _, want := range []string{"Update", "binary-only Lore CLI update"} {
		if !strings.Contains(picker, want) {
			t.Fatalf("collapsed picker missing active %q:\n%s", want, picker)
		}
	}
}

func TestShortLoginViewKeepsFooterVisible(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	m.width = 80
	m.height = 12
	m.focus = focusLogin
	m.statusTitle = "Login"
	m.statusBody = "Enter your server URL, account email, and password."
	m.loginInputs[0].Focus()

	view := m.View()
	if got := len(strings.Split(view, "\n")); got > m.height {
		t.Fatalf("login view rows = %d, want <= terminal height %d:\n%s", got, m.height, view)
	}
	for _, want := range []string{"Server URL", "Email", "Password", "Esc back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("short login view missing %q:\n%s", want, view)
		}
	}

	m.loginError = "password required"
	view = m.View()
	if got := len(strings.Split(view, "\n")); got > m.height {
		t.Fatalf("short login error view rows = %d, want <= terminal height %d:\n%s", got, m.height, view)
	}
	if !strings.Contains(view, "password required") {
		t.Fatalf("short login error view should keep actionable validation visible:\n%s", view)
	}
}

func TestBackupDecisionFooterDistinguishesSkipFromCancel(t *testing.T) {
	m := newModel(cli.InteractiveActions{})
	m.focus = focusDetail
	m.installBackupDecisionPending = true
	footer := renderFooter(m)
	for _, want := range []string{"y/Enter backup", "n skip backup", "Esc cancel"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("backup footer missing %q: %s", want, footer)
		}
	}
	if strings.Contains(footer, "n/Esc cancel") {
		t.Fatalf("backup footer should not describe n as cancel: %s", footer)
	}
}
