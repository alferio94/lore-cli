package cli

import (
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/install"
)

type installFormat string

const (
	installFormatHuman installFormat = "human"
	installFormatJSON  installFormat = "json"
)

type installResultEnvelope struct {
	SchemaVersion string            `json:"schema_version"`
	Result        installResultView `json:"result"`
}

type installResultView struct {
	Mode         install.Mode      `json:"mode"`
	Route        install.Route     `json:"route"`
	Target       install.TargetID  `json:"target"`
	Outcome      install.Status    `json:"outcome"`
	Admitted     bool              `json:"admitted"`
	ChangedState bool              `json:"changed_state"`
	Rollback     rollbackView      `json:"rollback"`
	ResidualRisk bool              `json:"residual_risk"`
	Report       installReportView `json:"report"`
	Guidance     []guidanceView    `json:"guidance"`
	Warnings     []guidanceView    `json:"warnings"`
	Error        *installErrorView `json:"error,omitempty"`
}

type installReportView struct {
	IRID              string          `json:"ir_id,omitempty"`
	ManifestHash      string          `json:"manifest_hash,omitempty"`
	AllAdmitted       bool            `json:"all_admitted"`
	FinalizationCount int             `json:"finalization_count"`
	MutationCount     int             `json:"mutation_count"`
	Operations        []operationView `json:"operations"`
}

type rollbackView struct {
	Attempted bool `json:"attempted"`
	Complete  bool `json:"complete"`
}

type operationView struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

type guidanceView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type installErrorView struct {
	Code         install.InstallErrorCode `json:"code"`
	Path         string                   `json:"path"`
	Message      string                   `json:"message"`
	Retryable    bool                     `json:"retryable"`
	ResidualRisk bool                     `json:"residual_risk"`
}

func parseInstallFormat(value string) (installFormat, bool) {
	format := installFormat(strings.ToLower(strings.TrimSpace(value)))
	return format, format == installFormatHuman || format == installFormatJSON
}

func (a *App) presentInstallResult(format installFormat, result install.Result) int {
	if format == installFormatJSON {
		if err := writeJSON(a.Stdout, newInstallResultEnvelope(result)); err != nil {
			fmt.Fprintln(a.Stderr, "install output failed")
			return 1
		}
		return installExitCode(result)
	}

	if installResultSucceeded(result) {
		fmt.Fprint(a.Stdout, renderHumanInstallResult(result))
	} else {
		fmt.Fprint(a.Stderr, renderHumanInstallResult(result))
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(a.Stderr, "warning[%s]: %s\n", warning.Code, warning.Message)
	}
	return installExitCode(result)
}

func newInstallResultEnvelope(result install.Result) installResultEnvelope {
	view := installResultView{
		Mode: result.Mode, Route: result.Route, Target: result.Target, Outcome: result.Status,
		Admitted: result.Admitted, ChangedState: result.ChangedState,
		Rollback:     rollbackView{Attempted: result.Rollback.Attempted, Complete: result.Rollback.Complete},
		ResidualRisk: result.ResidualRisk,
		Report: installReportView{
			IRID: result.Report.IRID, ManifestHash: result.Report.ManifestHash,
			AllAdmitted: result.Report.AllAdmitted, FinalizationCount: result.Report.FinalizationCount,
			MutationCount: result.Report.MutationCount, Operations: operationViews(result.Operations),
		},
		Guidance: guidanceViews(result.Guidance), Warnings: guidanceViews(result.Warnings),
	}
	if result.Error != nil {
		view.Error = &installErrorView{
			Code: result.Error.Code(), Path: result.Error.Path(), Message: result.Error.Error(),
			Retryable: result.Error.Retryable(), ResidualRisk: result.Error.ResidualRisk(),
		}
	}
	return installResultEnvelope{SchemaVersion: install.ResultSchemaVersion, Result: view}
}

func operationViews(operations []install.Operation) []operationView {
	views := make([]operationView, len(operations))
	for i, operation := range operations {
		views[i] = operationView{Resource: operation.Resource, Action: operation.Action}
	}
	return views
}

func guidanceViews(guidance []install.Guidance) []guidanceView {
	views := make([]guidanceView, len(guidance))
	for i, item := range guidance {
		views[i] = guidanceView{Code: item.Code, Message: item.Message}
	}
	return views
}

func renderHumanInstallResult(result install.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lore install %s\n", result.Mode)
	fmt.Fprintf(&b, "mode=%s route=%s target=%s outcome=%s admitted=%t changed_state=%t residual_risk=%t\n", result.Mode, result.Route, result.Target, result.Status, result.Admitted, result.ChangedState, result.ResidualRisk)
	if result.Error != nil {
		fmt.Fprintf(&b, "error[%s] path=%s: %s\n", result.Error.Code(), result.Error.Path(), result.Error.Error())
	} else {
		fmt.Fprintf(&b, "report ir_id=%s manifest_hash=%s admitted=%t finalizations=%d mutations=%d\n", result.Report.IRID, result.Report.ManifestHash, result.Report.AllAdmitted, result.Report.FinalizationCount, result.Report.MutationCount)
		for _, operation := range result.Operations {
			fmt.Fprintf(&b, "operation action=%s resource=%s\n", operation.Action, operation.Resource)
		}
	}
	for _, guidance := range result.Guidance {
		fmt.Fprintf(&b, "guidance[%s]: %s\n", guidance.Code, guidance.Message)
	}
	return b.String()
}

func installResultSucceeded(result install.Result) bool {
	return result.Error == nil && result.Admitted && (result.Status == install.StatusReady || result.Status == install.StatusSucceeded)
}

func installExitCode(result install.Result) int {
	if result.ResidualRisk || result.Status == install.StatusResidualRisk || result.Error != nil && result.Error.ResidualRisk() {
		return 3
	}
	if result.Status == install.StatusCancelled && !result.ChangedState && !result.Rollback.Attempted {
		return 0
	}
	if installResultSucceeded(result) {
		return 0
	}
	return 1
}
