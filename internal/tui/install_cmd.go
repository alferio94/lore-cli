package tui

import (
	"context"
	"os"

	"github.com/alferio94/lore-cli/internal/install"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

type installPreparedMsg struct {
	prepared install.Prepared
	result   install.Result
}
type installEventMsg struct{ event install.Event }
type installDoneMsg struct{ result install.Result }

type tuiUsageError struct{}

func (tuiUsageError) Error() string {
	return "interactive TUI requires a TTY; use lore install --explain for noninteractive output"
}
func (tuiUsageError) Usage() bool { return true }

func requireTTY(files ...*os.File) error {
	for _, file := range files {
		if file == nil || !isatty.IsTerminal(file.Fd()) && !isatty.IsCygwinTerminal(file.Fd()) {
			return tuiUsageError{}
		}
	}
	return nil
}

func (m *installModel) prepareCmd() tea.Cmd {
	request := m.request.Clone()
	return func() tea.Msg {
		prepared, result := m.workflow.Prepare(context.Background(), request)
		return installPreparedMsg{prepared: prepared, result: result.Clone()}
	}
}

func (m *installModel) executeCmd() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	prepared := m.prepared
	return func() tea.Msg {
		result := m.workflow.Execute(ctx, prepared, install.ObserverFunc(func(event install.Event) {}))
		return installDoneMsg{result: result.Clone()}
	}
}
