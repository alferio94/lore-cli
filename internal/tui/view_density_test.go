package tui

import "testing"

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
	viewport = renderBodyViewport("alpha beta gamma delta", 10, 2, 99)
	if !viewport.Above || viewport.Below {
		t.Fatalf("bottom indicators above=%v below=%v, want only above", viewport.Above, viewport.Below)
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
