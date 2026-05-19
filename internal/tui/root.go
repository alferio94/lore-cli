package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/cli"
	"github.com/alferio94/lore-cli/internal/install"
	"github.com/alferio94/lore-cli/internal/output"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type menuItem struct {
	key         string
	title       string
	description string
	disabled    bool
}

type focusArea int

const (
	focusMenu focusArea = iota
	focusDetail
	focusLogin
)

type actionKind string

const (
	actionStatus  actionKind = "status"
	actionLogin   actionKind = "login"
	actionLogout  actionKind = "logout"
	actionDoctor  actionKind = "doctor"
	actionInstall actionKind = "install"
)

type actionMsg struct {
	kind    actionKind
	title   string
	body    string
	isError bool
}

type model struct {
	actions                 cli.InteractiveActions
	items                   []menuItem
	selected                int
	focus                   focusArea
	width                   int
	height                  int
	ready                   bool
	loading                 bool
	quitting                bool
	loginInputs             []textinput.Model
	loginError              string
	statusTitle             string
	statusBody              string
	statusTone              string
	installSelectionPending bool
	spinner                 spinner.Model
	help                    help.Model
}

func newModel(actions cli.InteractiveActions) model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot

	serverInput := textinput.New()
	serverInput.Prompt = "Server URL "
	serverInput.Placeholder = "https://lore.example"
	serverInput.CharLimit = 256
	serverInput.Width = 36

	tokenInput := textinput.New()
	tokenInput.Prompt = "API token  "
	tokenInput.Placeholder = "Paste a normal user token"
	tokenInput.CharLimit = 512
	tokenInput.Width = 36
	tokenInput.EchoMode = textinput.EchoPassword
	tokenInput.EchoCharacter = '•'

	m := model{
		actions: actions,
		items: []menuItem{
			{key: "status", title: "Status", description: "Inspect config, health, readiness, and authenticated identity."},
			{key: "login", title: "Login", description: "Validate a user API token, store it in secure credential storage, and save login metadata locally."},
			{key: "logout", title: "Logout", description: "Remove the local session only. Safe to repeat."},
			{key: "doctor", title: "Doctor", description: "Run actionable diagnostics, including Pi availability."},
			{key: "install", title: "Install", description: "Pi is recommended today; Claude Code, OpenCode, Codex, and Antigravity remain Coming soon."},
			{key: "quit", title: "Quit", description: "Leave the interactive shell."},
		},
		focus:       focusMenu,
		loginInputs: []textinput.Model{serverInput, tokenInput},
		statusTitle: "Welcome to Lore",
		statusBody:  "Choose an action from the left. Keyboard hints stay visible, secrets stay masked, and explicit subcommands remain available for automation.",
		statusTone:  toneInfo,
		spinner:     spin,
		help:        help.New(),
	}
	m.help.ShowAll = false
	return m
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil
	case tea.KeyMsg:
		if m.loading {
			switch msg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch m.focus {
		case focusLogin:
			return m.updateLogin(msg)
		case focusDetail:
			return m.updateDetail(msg)
		default:
			return m.updateMenu(msg)
		}
	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case actionMsg:
		m.loading = false
		m.focus = focusDetail
		m.statusTitle = msg.title
		m.statusBody = msg.body
		if msg.isError {
			m.statusTone = toneError
		} else {
			m.statusTone = toneSuccess
			if msg.kind == actionStatus || msg.kind == actionDoctor {
				m.statusTone = toneInfo
			}
		}
		if msg.kind == actionLogin && !msg.isError {
			for i := range m.loginInputs {
				m.loginInputs[i].SetValue("")
			}
			m.loginError = ""
		}
		if msg.kind == actionInstall {
			m.installSelectionPending = false
		}
		return m, nil
	}
	return m, nil
}

func (m model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		m.installSelectionPending = false
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
		m.installSelectionPending = false
	case "tab", "right", "l":
		m.focus = focusDetail
	case "enter", " ":
		return m.activateSelection()
	}
	return m, nil
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "left", "h", "tab":
		m.focus = focusMenu
	case "enter":
		return m.activateSelection()
	}
	return m, nil
}

func (m model) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusMenu
		m.loginError = ""
		return m, nil
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "tab", "shift+tab", "up", "down", "enter":
		if msg.String() == "enter" && m.loginInputs[1].Focused() {
			return m.submitLogin()
		}
		if msg.String() == "shift+tab" || msg.String() == "up" {
			m.focusPrevInput()
		} else {
			m.focusNextInput()
		}
		return m, nil
	}

	var cmds []tea.Cmd
	for i := range m.loginInputs {
		var cmd tea.Cmd
		m.loginInputs[i], cmd = m.loginInputs[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) focusNextInput() {
	focused := 0
	for i := range m.loginInputs {
		if m.loginInputs[i].Focused() {
			focused = i
			m.loginInputs[i].Blur()
			break
		}
	}
	next := (focused + 1) % len(m.loginInputs)
	m.loginInputs[next].Focus()
}

func (m *model) focusPrevInput() {
	focused := 0
	for i := range m.loginInputs {
		if m.loginInputs[i].Focused() {
			focused = i
			m.loginInputs[i].Blur()
			break
		}
	}
	prev := focused - 1
	if prev < 0 {
		prev = len(m.loginInputs) - 1
	}
	m.loginInputs[prev].Focus()
}

func (m model) activateSelection() (tea.Model, tea.Cmd) {
	item := m.items[m.selected]
	if item.disabled {
		m.statusTitle = item.title
		m.statusBody = item.description
		m.statusTone = toneMuted
		m.focus = focusDetail
		return m, nil
	}
	switch item.key {
	case "status":
		m.installSelectionPending = false
		return m.runAsync(actionStatus, "Checking status", func(ctx context.Context) actionMsg {
			report := m.actions.Status(ctx)
			return actionMsg{kind: actionStatus, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	case "doctor":
		m.installSelectionPending = false
		return m.runAsync(actionDoctor, "Running doctor", func(ctx context.Context) actionMsg {
			report := m.actions.Doctor(ctx)
			return actionMsg{kind: actionDoctor, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	case "logout":
		m.installSelectionPending = false
		return m.runAsync(actionLogout, "Removing local session", func(ctx context.Context) actionMsg {
			result, err := m.actions.Logout(ctx)
			if err != nil {
				return actionMsg{kind: actionLogout, title: "Logout failed", body: err.Error(), isError: true}
			}
			return actionMsg{kind: actionLogout, title: "Logout complete", body: result.Summary}
		})
	case "login":
		m.installSelectionPending = false
		m.focus = focusLogin
		m.loginInputs[0].Focus()
		m.loginInputs[1].Blur()
		m.loginError = ""
		m.statusTitle = "Login"
		m.statusBody = "Enter your server URL and a normal user API token. The token stays masked, is validated first, and is stored in secure credential storage while only login metadata is saved locally."
		m.statusTone = toneInfo
		return m, nil
	case "install":
		if !m.installSelectionPending {
			m.installSelectionPending = true
			m.focus = focusDetail
			m.statusTitle = "Install Lore for Pi"
			m.statusBody = install.FormatTargetSelection(install.DefaultTargets())
			m.statusTone = toneInfo
			return m, nil
		}
		m.installSelectionPending = false
		if m.actions.Install == nil {
			return m, nil
		}
		return m.runAsync(actionInstall, "Install Lore for Pi", func(ctx context.Context) actionMsg {
			report := m.actions.Install(ctx)
			return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	case "quit":
		m.installSelectionPending = false
		m.quitting = true
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m model) submitLogin() (tea.Model, tea.Cmd) {
	serverURL := strings.TrimSpace(m.loginInputs[0].Value())
	token := strings.TrimSpace(m.loginInputs[1].Value())
	if serverURL == "" || token == "" {
		m.loginError = "Server URL and API token are both required."
		return m, nil
	}
	m.loginError = ""
	return m.runAsync(actionLogin, "Validating credentials", func(ctx context.Context) actionMsg {
		result, err := m.actions.Login(ctx, serverURL, token)
		if err != nil {
			return actionMsg{kind: actionLogin, title: "Login failed", body: err.Error(), isError: true}
		}
		return actionMsg{kind: actionLogin, title: "Login complete", body: result.Summary}
	})
}

func (m model) runAsync(kind actionKind, title string, fn func(context.Context) actionMsg) (tea.Model, tea.Cmd) {
	m.loading = true
	m.focus = focusDetail
	m.statusTitle = title
	m.statusBody = "Please wait…"
	m.statusTone = toneInfo
	return m, func() tea.Msg {
		msg := fn(context.Background())
		msg.kind = kind
		return msg
	}
}

func renderReport(report cli.ActionReport) string {
	return output.RenderChecks(report.Title, report.Checks)
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	return renderView(m)
}

// Run starts the Lore root TUI.
func Run(_ context.Context, actions cli.InteractiveActions) error {
	p := tea.NewProgram(newModel(actions))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("start lore TUI: %w", err)
	}
	return nil
}
