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
		m.result, m.stage, m.cancel, m.cancelling = msg.result.Clone(), installResult, nil, false
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
		if m.stage == installPrepared && key == "enter" && m.request.Mode != install.ModeExplain && m.result.Admitted {
			m.stage = installExecuting
			return m.executeCmd()
		}
	}
	return nil
}
