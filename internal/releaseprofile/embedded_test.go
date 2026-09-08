package releaseprofile

import (
	"flag"
	"testing"
)

var wantProcessStatus = flag.String("releaseprofile-status", "", "expected embedded profile status")

func TestCurrentProcessSnapshot(t *testing.T) {
	want := *wantProcessStatus
	if want == "" {
		want = StatusDefaultOff
	}
	snapshot := Current()
	if snapshot.Status() != want {
		t.Fatalf("Current status = %q, want %q", snapshot.Status(), want)
	}
	for _, target := range []string{TargetPi, TargetOpenCode, TargetCodex, TargetAntigravity} {
		if snapshot.Gate(target) != GateOff {
			t.Fatalf("Current gate %q = %q, want off", target, snapshot.Gate(target))
		}
	}
	if want == StatusValid && snapshot.Profile().ID != "development-all-off" {
		t.Fatalf("Current profile ID = %q, want development-all-off", snapshot.Profile().ID)
	}

	copy := snapshot.Profile()
	copy.Gates.Pi = GateA
	if Current().Gate(TargetPi) != GateOff {
		t.Fatal("Current returned mutable process state")
	}
}
