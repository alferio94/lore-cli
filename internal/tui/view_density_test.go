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
