package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	toneInfo    = "info"
	toneSuccess = "success"
	toneError   = "error"
	toneMuted   = "muted"
)

var (
	appStyle          = lipgloss.NewStyle().Padding(1, 2)
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtitleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(1, 2)
	focusedPanelStyle = panelStyle.Copy().BorderForeground(lipgloss.Color("212"))
	selectedItemStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	disabledStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	hintStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("248"))
	errorStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	successStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	infoStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("117")).Bold(true)
)

func renderView(m model) string {
	menu := renderMenuPanel(m)
	detail := renderDetailPanel(m)
	headerLines := []string{
		titleStyle.Render("Lore"),
		subtitleStyle.Render("Interactive shell for status, login, logout, diagnostics, install, and binary-only updates"),
	}
	if banner := renderUpdateBanner(m); banner != "" {
		headerLines = append(headerLines, banner)
	}
	header := strings.Join(headerLines, "\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top, menu, detail)
	footer := hintStyle.Render(renderFooter(m))
	return appStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer))
}

func renderMenuPanel(m model) string {
	style := panelStyle
	if m.focus == focusMenu {
		style = focusedPanelStyle
	}
	var rows []string
	rows = append(rows, titleStyle.Render("Actions"))
	rows = append(rows, subtitleStyle.Render("Use ↑/↓ to move, Enter to open."))
	for i, item := range m.items {
		prefix := "  "
		lineStyle := lipgloss.NewStyle()
		if i == m.selected {
			prefix = "› "
			lineStyle = selectedItemStyle
		}
		label := prefix + item.title
		if item.disabled {
			label = disabledStyle.Render(label + " (coming soon)")
		} else {
			label = lineStyle.Render(label)
		}
		rows = append(rows, label)
		rows = append(rows, mutedStyle.Render("  "+item.description))
	}
	return style.Width(menuWidth(m.width)).Render(strings.Join(rows, "\n"))
}

func renderDetailPanel(m model) string {
	style := panelStyle
	if m.focus == focusDetail || m.focus == focusLogin {
		style = focusedPanelStyle
	}
	content := []string{renderToneTitle(m.statusTone, m.statusTitle), mutedStyle.Render(currentModeLabel(m))}
	if m.loading {
		content = append(content, "", infoStyle.Render(m.spinner.View()+" Working…"), mutedStyle.Render("You can quit with q if needed."))
		return style.Width(detailWidth(m.width)).Render(strings.Join(content, "\n"))
	}
	if m.focus == focusLogin {
		content = append(content, "", mutedStyle.Render(m.statusBody), "")
		for i := range m.loginInputs {
			content = append(content, m.loginInputs[i].View())
		}
		if m.loginError != "" {
			content = append(content, "", errorStyle.Render(m.loginError))
		}
		content = append(content, "", hintStyle.Render("Tab to switch fields • Enter on password submits • Automation: --password-stdin • Compatibility: --token • Esc returns to menu"))
		return style.Width(detailWidth(m.width)).Render(strings.Join(content, "\n"))
	}
	content = append(content, "", m.statusBody)
	return style.Width(detailWidth(m.width)).Render(strings.Join(content, "\n"))
}

func renderToneTitle(tone, title string) string {
	switch tone {
	case toneError:
		return errorStyle.Render(title)
	case toneSuccess:
		return successStyle.Render(title)
	case toneMuted:
		return mutedStyle.Render(title)
	default:
		return infoStyle.Render(title)
	}
}

func currentModeLabel(m model) string {
	switch {
	case m.focus == focusLogin:
		return "Secure password-first login form"
	case m.updateConfirmationPending:
		return "Update confirmation"
	case m.installBackupDecisionPending:
		return "Install confirmation"
	case m.installSelectionPending:
		return "Install target selection"
	case m.loading:
		return "Running action"
	default:
		return "Action details"
	}
}

func renderUpdateBanner(m model) string {
	switch {
	case m.updateAvailable:
		return successStyle.Render(fmt.Sprintf("Update available: %s → %s • select Update to continue • binary-only, Pi runtime untouched", fallbackUpdateValue(m.updateCurrentVersion, "current"), fallbackUpdateValue(m.updateLatestVersion, "latest")))
	case !m.updateChecked && m.actions.CheckForUpdate != nil:
		return mutedStyle.Render("Checking for Lore CLI updates in the background…")
	case m.updateNotice != "":
		return mutedStyle.Render(m.updateNotice)
	default:
		return ""
	}
}

func renderFooter(m model) string {
	if m.focus == focusLogin {
		return "Esc back • Tab next field • Enter submit • --password-stdin for automation • q quit"
	}
	return "↑/↓ navigate • Enter select • Tab switch panel • q quit • Explicit subcommands remain available"
}

func menuWidth(total int) int {
	if total >= 120 {
		return 42
	}
	return 36
}

func detailWidth(total int) int {
	if total <= 0 {
		return 64
	}
	width := total - menuWidth(total) - 10
	if width < 48 {
		return 48
	}
	return width
}

func debugString(m model) string {
	return fmt.Sprintf("selected=%d focus=%d loading=%v title=%q", m.selected, m.focus, m.loading, m.statusTitle)
}
