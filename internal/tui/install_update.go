package tui

import (
	"strings"

	"github.com/alferio94/lore-cli/internal/install"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *installModel) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case installPreparedMsg:
		m.prepared, m.result = msg.prepared, msg.result.Clone()
		m.stage = installPrepared
		if msg.result.Error != nil {
			m.stage = installResult
		}
	case installEventMsg:
		m.events = append(m.events, msg.event.Clone())
	case installDoneMsg:
		m.result, m.events, m.stage, m.cancel, m.cancelling = msg.result.Clone(), append(m.events, msg.events...), installResult, nil, false
	case tea.KeyMsg:
		key := strings.ToLower(msg.String())
		if m.stage == installExecuting {
			if (key == "esc" || key == "ctrl+c") && !m.cancelling {
				m.cancelling = true
				if m.cancel != nil {
					m.cancel()
				}
			}
			return nil
		}
		if key == "esc" || key == "backspace" || key == "left" {
			m.prepared, m.back = install.Prepared{}, true
			return nil
		}
		if m.canRetry() && key == "r" {
			m.prepared, m.events, m.stage = install.Prepared{}, nil, installPreparing
			return m.prepareCmd()
		}
		if key == "enter" && m.stage == installResult && m.request.Mode == install.ModeDryRun && m.result.Status == install.StatusSucceeded {
			m.request.Mode, m.prepared, m.events, m.stage = install.ModeApply, install.Prepared{}, nil, installPreparing
			return m.prepareCmd()
		}
		if m.stage == installPrepared && key == "enter" && m.result.Admitted {
			if m.request.Mode == install.ModeExplain {
				m.request.Mode, m.prepared, m.events, m.stage = install.ModeDryRun, install.Prepared{}, nil, installPreparing
				return m.prepareCmd()
			}
			m.stage = installExecuting
			return m.executeCmd()
		}
	}
	return nil
}
