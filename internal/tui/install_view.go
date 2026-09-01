package tui

import (
	"fmt"
	"strings"
)

func renderInstallView(m *installModel, width int) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Canonical install %s\n", m.request.Mode)
	fmt.Fprintf(&b, "phase=%s", m.stage)
	if m.cancelling {
		b.WriteString(" cancellation=requested; waiting for final rollback result")
	}
	b.WriteByte('\n')
	if len(m.events) > 0 {
		event := m.events[len(m.events)-1]
		fmt.Fprintf(&b, "progress phase=%s kind=%s completed=%d total=%d\n", event.Phase, event.Kind, event.Progress.Completed, event.Progress.Total)
	}
	if m.stage != installPreparing {
		result := m.result
		fmt.Fprintf(&b, "mode=%s route=%s target=%s outcome=%s admitted=%t changed_state=%t\n", result.Mode, result.Route, result.Target, result.Status, result.Admitted, result.ChangedState)
		fmt.Fprintf(&b, "rollback_attempted=%t rollback_complete=%t residual_risk=%t\n", result.Rollback.Attempted, result.Rollback.Complete, result.ResidualRisk)
		if result.Error != nil {
			fmt.Fprintf(&b, "error[%s] path=%s: %s\n", result.Error.Code(), result.Error.Path(), result.Error.Error())
		}
		for _, operation := range result.Operations {
			fmt.Fprintf(&b, "operation action=%s resource=%s\n", operation.Action, operation.Resource)
		}
		for _, guidance := range result.Guidance {
			fmt.Fprintf(&b, "guidance[%s]: %s\n", guidance.Code, guidance.Message)
		}
		for _, warning := range result.Warnings {
			fmt.Fprintf(&b, "warning[%s]: %s\n", warning.Code, warning.Message)
		}
	}
	if m.stage == installExecuting {
		b.WriteString("Esc/Ctrl-C cancel • navigation locked")
	} else if m.stage == installResult && m.result.ResidualRisk {
		b.WriteString("Retry disabled • follow recovery guidance • Esc back")
	} else if m.canRetry() {
		b.WriteString("r retry from fresh facts • Esc back")
	} else {
		b.WriteString("Esc back")
	}
	if width < 5 {
		width = 5
	}
	return strings.TrimSpace(strings.Join(wrapBodyLines(b.String(), width-4), "\n"))
}
