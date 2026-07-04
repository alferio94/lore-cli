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

type viewportDensity struct {
	Width        int
	Height       int
	ContentWidth int
	Compact      bool
	Short        bool
}

func newViewportDensity(width, height int) viewportDensity {
	if width <= 0 {
		width = 80
	}
	contentWidth := width - 4
	if contentWidth > 76 {
		contentWidth = 76
	}
	if width < 48 {
		contentWidth = width - 2
	}
	if contentWidth < 24 {
		contentWidth = 24
	}
	return viewportDensity{
		Width:        width,
		Height:       height,
		ContentWidth: contentWidth,
		Compact:      width < 72,
		Short:        height > 0 && height < 20,
	}
}

func (d viewportDensity) OuterPadding() (int, int) {
	vertical := 1
	horizontal := 2
	if d.Compact {
		horizontal = 1
	}
	if d.Short {
		vertical = 0
	}
	return vertical, horizontal
}

func (d viewportDensity) ShowSecondaryCopy() bool {
	return !d.Compact && !d.Short
}

func (d viewportDensity) ShowDecoration() bool {
	return !d.Compact && !d.Short && (d.Height == 0 || d.Height >= 24)
}

func truncateLine(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
}

func renderView(m model) string {
	density := newViewportDensity(m.width, m.height)
	headerLines := []string{titleStyle.Render("Lore")}
	if density.ShowSecondaryCopy() {
		headerLines = append(headerLines, subtitleStyle.Render("Interactive shell for status, login, logout, diagnostics, install, and binary-only updates"))
	}
	if banner := renderUpdateBanner(m); banner != "" && density.ShowDecoration() {
		headerLines = append(headerLines, banner)
	}
	header := strings.Join(headerLines, "\n")
	body := renderShell(m, density)
	footer := hintStyle.Render(renderFooter(m))
	verticalPadding, horizontalPadding := density.OuterPadding()
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	return appStyle.Copy().Padding(verticalPadding, horizontalPadding).Render(lipgloss.PlaceHorizontal(density.Width, lipgloss.Center, content))
}

func renderShell(m model, density viewportDensity) string {
	style := focusedPanelStyle.Width(density.ContentWidth)
	switch {
	case m.focus == focusMenu:
		return style.Render(renderRootPicker(m, density))
	case m.focus == focusLogin:
		return style.Render(renderLoginScreen(m, density))
	default:
		return style.Render(renderDetailScreen(m, density))
	}
}

func renderRootPicker(m model, density viewportDensity) string {
	rows := []string{titleStyle.Render("Choose an action")}
	if density.ShowSecondaryCopy() {
		rows = append(rows, subtitleStyle.Render("Use ↑/↓ to move, Enter to open."))
	}
	for i, item := range m.items {
		prefix := "  "
		labelStyle := lipgloss.NewStyle()
		helpStyle := mutedStyle
		if i == m.selected {
			prefix = "› "
			labelStyle = selectedItemStyle
			helpStyle = selectedItemStyle.Copy().Bold(false)
		}
		label := prefix + item.title
		if item.disabled {
			label = disabledStyle.Render(label + " (coming soon)")
		} else {
			label = labelStyle.Render(label)
		}
		rows = append(rows, label)
		if i == m.selected {
			rows = append(rows, helpStyle.Render("  "+truncateLine(item.description, density.ContentWidth-6)))
		} else if density.ShowSecondaryCopy() {
			rows = append(rows, mutedStyle.Render("  "+truncateLine(item.description, density.ContentWidth-6)))
		}
	}
	return strings.Join(rows, "\n")
}

func renderLoginScreen(m model, density viewportDensity) string {
	content := []string{renderToneTitle(m.statusTone, m.statusTitle)}
	if density.ShowSecondaryCopy() {
		content = append(content, mutedStyle.Render(currentModeLabel(m)), "", mutedStyle.Render(m.statusBody))
	}
	content = append(content, "")
	for i := range m.loginInputs {
		content = append(content, m.loginInputs[i].View())
	}
	if m.loginError != "" {
		content = append(content, "", errorStyle.Render(m.loginError))
	}
	content = append(content, "", hintStyle.Render("Tab fields • Enter submit • --password-stdin • --token compatibility • Esc back • q quit"))
	return strings.Join(content, "\n")
}

func renderDetailScreen(m model, density viewportDensity) string {
	content := []string{renderToneTitle(m.statusTone, m.statusTitle)}
	if density.ShowSecondaryCopy() {
		content = append(content, mutedStyle.Render(currentModeLabel(m)))
	}
	if m.loading {
		content = append(content, "", infoStyle.Render(m.spinner.View()+" Working…"), mutedStyle.Render("You can quit with q if needed."))
		return strings.Join(content, "\n")
	}
	content = append(content, "", m.statusBody)
	return strings.Join(content, "\n")
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
		return "Install backup confirmation"
	case m.installConfirmationPending:
		return "Install confirmation"
	case m.installSelectionPending && m.detailsVisible:
		return "Install target details"
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
		return "Esc back • Tab next field • Enter submit • --password-stdin • --token • q quit"
	}
	if m.installSelectionPending {
		if m.detailsVisible {
			return "? hide details • Esc back • q quit"
		}
		return "↑/↓ target • Enter confirm • ? details • Esc back • q quit"
	}
	if m.installConfirmationPending || m.installBackupDecisionPending || m.updateConfirmationPending {
		return "y/Enter continue • n/Esc cancel • q quit"
	}
	if m.focus == focusMenu {
		return "↑/↓ navigate • Enter select • q quit • Explicit subcommands remain available"
	}
	return "Esc back • q quit • Explicit subcommands remain available"
}

func debugString(m model) string {
	return fmt.Sprintf("selected=%d focus=%d loading=%v title=%q", m.selected, m.focus, m.loading, m.statusTitle)
}
