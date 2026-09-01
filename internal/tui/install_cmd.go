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
type installDoneMsg struct {
	result install.Result
	events []install.Event
}

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
		var prepared install.Prepared
		var result install.Result
		if request.Mode == install.ModeLegacyApply || request.Mode == install.ModeLegacyDryRun {
			prepared, result = m.legacy.PrepareLegacy(context.Background(), request)
		} else {
			prepared, result = m.workflow.Prepare(context.Background(), request)
		}
		return installPreparedMsg{prepared: prepared, result: result.Clone()}
	}
}

func (m *installModel) executeCmd() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	prepared := m.prepared
	return func() tea.Msg {
		var events []install.Event
		observer := install.ObserverFunc(func(event install.Event) { events = append(events, event.Clone()) })
		var result install.Result
		if prepared.Route() == install.RouteLegacy {
			result = m.legacy.ExecuteLegacy(ctx, prepared, observer)
		} else {
			result = m.workflow.Execute(ctx, prepared, observer)
		}
		return installDoneMsg{result: result.Clone(), events: events}
	}
}
