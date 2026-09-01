package tui

import (
	"context"

	"github.com/alferio94/lore-cli/internal/install"
)

type installStage string

const (
	installPreparing installStage = "preparing"
	installPrepared  installStage = "prepared"
	installExecuting installStage = "executing"
	installResult    installStage = "result"
)

type installModel struct {
	workflow    install.Workflow
	legacy      install.LegacyAdapter
	request     install.Request
	prepared    install.Prepared
	result      install.Result
	events      []install.Event
	stage       installStage
	width       int
	height      int
	back        bool
	cancelling  bool
	cancel      context.CancelFunc
	noAnimation bool
}

func newInstallModel(workflow install.Workflow, request install.Request, noAnimation bool) *installModel {
	return &installModel{workflow: workflow, request: request.Clone(), stage: installPreparing, noAnimation: noAnimation}
}

func (m *installModel) canRetry() bool {
	return m.stage == installResult && !m.result.ResidualRisk && (m.result.Status == install.StatusFailed || m.result.Error != nil && m.result.Error.Retryable())
}
