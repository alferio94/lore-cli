package tui

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

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

const backgroundUpdateCheckTimeout = 1500 * time.Millisecond

type actionKind string

const (
	actionStatus  actionKind = "status"
	actionLogin   actionKind = "login"
	actionLogout  actionKind = "logout"
	actionDoctor  actionKind = "doctor"
	actionInstall actionKind = "install"
	actionUpdate  actionKind = "update"
)

type actionMsg struct {
	kind    actionKind
	title   string
	body    string
	isError bool
}

type updateCheckMsg struct {
	availability cli.UpdateAvailability
}

type bodyScrollState struct {
	Offset int
}

type scrollMove int

const (
	scrollLineUp scrollMove = iota
	scrollLineDown
	scrollPageUp
	scrollPageDown
	scrollTop
	scrollBottom
)

type model struct {
	actions                      cli.InteractiveActions
	items                        []menuItem
	selected                     int
	focus                        focusArea
	width                        int
	height                       int
	ready                        bool
	loading                      bool
	quitting                     bool
	loginInputs                  []textinput.Model
	loginError                   string
	statusTitle                  string
	statusBody                   string
	statusTone                   string
	installTargets               []install.Target
	installTargetIndex           int
	installSelectionPending      bool
	installConfirmationPending   bool
	installBackupDecisionPending bool
	detailsVisible               bool
	installPlan                  *install.PiInstallPlan
	updateChecked                bool
	updateAvailable              bool
	updateCurrentVersion         string
	updateLatestVersion          string
	updateNotice                 string
	updateConfirmationPending    bool
	spinner                      spinner.Model
	help                         help.Model
	bodyScroll                   bodyScrollState
}

func newModel(actions cli.InteractiveActions) model {
	spin := spinner.New()
	spin.Spinner = spinner.Dot

	serverInput := textinput.New()
	serverInput.Prompt = "Server URL "
	serverInput.Placeholder = "https://lore.example"
	serverInput.CharLimit = 256
	serverInput.Width = 36

	emailInput := textinput.New()
	emailInput.Prompt = "Email      "
	emailInput.Placeholder = "admin@example.com"
	emailInput.CharLimit = 256
	emailInput.Width = 36

	passwordInput := textinput.New()
	passwordInput.Prompt = "Password   "
	passwordInput.Placeholder = "Enter your account password"
	passwordInput.CharLimit = 512
	passwordInput.Width = 36
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '•'

	installTargets := install.DefaultTargets()
	installTargetIndex := 0
	for i, target := range installTargets {
		if target.ID == install.DefaultInstallTarget() {
			installTargetIndex = i
			break
		}
	}

	m := model{
		actions:            actions,
		installTargets:     installTargets,
		installTargetIndex: installTargetIndex,
		items: []menuItem{
			{key: "status", title: "Status", description: "Inspect config, health, readiness, and authenticated identity."},
			{key: "login", title: "Login", description: "Use email + password to mint a reusable token, store only that token in secure credential storage, and keep --token as CLI compatibility mode."},
			{key: "logout", title: "Logout", description: "Remove the local session only. Safe to repeat."},
			{key: "doctor", title: "Doctor", description: "Run actionable diagnostics, including Pi availability."},
			{key: "install", title: "Install", description: "Install Lore agent assets for supported harnesses; Pi remains recommended and roadmap targets stay visible."},
			{key: "update", title: "Update", description: "Check or apply a binary-only Lore CLI update; Pi runtime and ~/.pi stay untouched."},
			{key: "quit", title: "Quit", description: "Leave the interactive shell."},
		},
		focus:       focusMenu,
		loginInputs: []textinput.Model{serverInput, emailInput, passwordInput},
		statusTitle: "Welcome to Lore",
		statusBody:  "Choose an action. Keyboard hints stay visible, secrets stay masked, and explicit subcommands remain available for automation.",
		statusTone:  toneInfo,
		spinner:     spin,
		help:        help.New(),
	}
	m.help.ShowAll = false
	return m
}

func (m model) Init() tea.Cmd {
	if m.actions.CheckForUpdate == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), backgroundUpdateCheckTimeout)
		defer cancel()
		return updateCheckMsg{availability: m.actions.CheckForUpdate(ctx)}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.clampBodyScroll()
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
		m.replaceStatusBody(msg.body)
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
			m.installConfirmationPending = false
			m.installBackupDecisionPending = false
			m.detailsVisible = false
			m.installPlan = nil
		}
		if msg.kind == actionUpdate {
			m.updateConfirmationPending = false
		}
		return m, nil
	case updateCheckMsg:
		m.updateChecked = msg.availability.Checked
		m.updateAvailable = msg.availability.Available
		m.updateCurrentVersion = msg.availability.CurrentVersion
		m.updateLatestVersion = msg.availability.LatestVersion
		m.updateNotice = msg.availability.Detail
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
		m.installConfirmationPending = false
		m.detailsVisible = false
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
		m.installSelectionPending = false
		m.installConfirmationPending = false
		m.detailsVisible = false
	case "tab", "right", "l":
		m.focus = focusDetail
	case "enter", " ":
		return m.activateSelection()
	}
	return m, nil
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.canScrollBody() {
		if handled := m.handleBodyScrollKey(msg); handled {
			return m, nil
		}
	}
	if m.installConfirmationPending {
		switch strings.ToLower(msg.String()) {
		case "y", "yes", "enter":
			m.installConfirmationPending = false
			return m.startInstallFlow()
		case "n", "no", "esc", "left", "h", "backspace":
			m.installConfirmationPending = false
			m.installSelectionPending = true
			m.detailsVisible = false
			m.statusTitle = "Install Lore"
			m.replaceStatusBody(m.renderInstallTargetSelection())
			m.statusTone = toneInfo
			return m, nil
		case "?":
			m.detailsVisible = !m.detailsVisible
			m.replaceStatusBody(m.renderInstallConfirmation())
			return m, nil
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.installBackupDecisionPending {
		switch strings.ToLower(msg.String()) {
		case "y", "yes", "enter":
			return m.confirmInstallBackupDecision(true)
		case "n", "no":
			return m.confirmInstallBackupDecision(false)
		case "esc", "left", "h", "backspace":
			m.installBackupDecisionPending = false
			m.installSelectionPending = true
			m.installPlan = nil
			m.statusTitle = "Install Lore"
			m.replaceStatusBody(m.renderInstallTargetSelection())
			m.statusTone = toneInfo
			return m, nil
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.updateConfirmationPending {
		switch strings.ToLower(msg.String()) {
		case "y", "yes", "enter":
			return m.confirmUpdateDecision(true)
		case "n", "no", "esc", "left", "h", "backspace":
			return m.confirmUpdateDecision(false)
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.installSelectionPending {
		switch msg.String() {
		case "up", "k":
			if m.detailsVisible {
				return m, nil
			}
			m.moveInstallTargetSelection(-1)
			m.replaceStatusBody(m.renderInstallTargetSelection())
			return m, nil
		case "down", "j":
			if m.detailsVisible {
				return m, nil
			}
			m.moveInstallTargetSelection(1)
			m.replaceStatusBody(m.renderInstallTargetSelection())
			return m, nil
		case "?":
			m.detailsVisible = !m.detailsVisible
			m.replaceStatusBody(m.renderInstallTargetSelection())
			return m, nil
		case "esc", "left", "h", "backspace":
			if m.detailsVisible {
				m.detailsVisible = false
				m.replaceStatusBody(m.renderInstallTargetSelection())
				return m, nil
			}
			m.installSelectionPending = false
			m.focus = focusMenu
			m.statusTitle = "Welcome to Lore"
			m.replaceStatusBody("Choose an action. Keyboard hints stay visible, secrets stay masked, and explicit subcommands remain available for automation.")
			m.statusTone = toneInfo
			return m, nil
		}
	}
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "left", "h", "tab", "esc":
		m.focus = focusMenu
		m.resetBodyScroll()
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
		if msg.String() == "enter" && m.loginInputs[len(m.loginInputs)-1].Focused() {
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

func (m *model) resetInstallFlow() {
	m.installSelectionPending = false
	m.installConfirmationPending = false
	m.installBackupDecisionPending = false
	m.detailsVisible = false
	m.installPlan = nil
	m.resetBodyScroll()
}

func (m *model) resetBodyScroll() {
	m.bodyScroll.Offset = 0
}

func (m *model) replaceStatusBody(body string) {
	m.statusBody = body
	m.resetBodyScroll()
}

func (m model) canScrollBody() bool {
	if m.focus != focusDetail || m.loading {
		return false
	}
	if m.installSelectionPending {
		return m.detailsVisible
	}
	return strings.TrimSpace(m.statusBody) != ""
}

func (m *model) handleBodyScrollKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "k":
		m.scrollBody(scrollLineUp)
	case "down", "j":
		m.scrollBody(scrollLineDown)
	case "pgup":
		m.scrollBody(scrollPageUp)
	case "pgdown":
		m.scrollBody(scrollPageDown)
	case "g":
		m.scrollBody(scrollTop)
	case "G":
		m.scrollBody(scrollBottom)
	default:
		return false
	}
	return true
}

func (m *model) scrollBody(move scrollMove) {
	viewport := m.currentBodyViewport()
	page := viewport.Height
	if page < 1 {
		page = 1
	}
	switch move {
	case scrollLineUp:
		m.bodyScroll.Offset--
	case scrollLineDown:
		m.bodyScroll.Offset++
	case scrollPageUp:
		m.bodyScroll.Offset -= page
	case scrollPageDown:
		m.bodyScroll.Offset += page
	case scrollTop:
		m.bodyScroll.Offset = 0
	case scrollBottom:
		m.bodyScroll.Offset = viewport.maxOffset()
	}
	m.clampBodyScroll()
}

func (m *model) clampBodyScroll() {
	if !m.canScrollBody() {
		m.resetBodyScroll()
		return
	}
	viewport := m.currentBodyViewport()
	m.bodyScroll.Offset = viewport.Offset
}

func (m model) currentBodyViewport() bodyViewport {
	density := newViewportDensity(m.width, m.height)
	preBodyLines := detailPreBodyLineCount(m, density)
	bodyHeight := detailBodyViewportHeight(density, preBodyLines, rootHeaderLineCount(m, density), 1)
	return renderBodyViewport(m.statusBody, density.ContentWidth-4, bodyHeight, m.bodyScroll.Offset)
}

func (m model) activateSelection() (tea.Model, tea.Cmd) {
	item := m.items[m.selected]
	if item.disabled {
		m.statusTitle = item.title
		m.replaceStatusBody(item.description)
		m.statusTone = toneMuted
		m.focus = focusDetail
		return m, nil
	}
	switch item.key {
	case "status":
		m.resetInstallFlow()
		m.updateConfirmationPending = false
		return m.runAsync(actionStatus, "Checking status", func(ctx context.Context) actionMsg {
			report := m.actions.Status(ctx)
			return actionMsg{kind: actionStatus, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	case "doctor":
		m.resetInstallFlow()
		m.updateConfirmationPending = false
		return m.runAsync(actionDoctor, "Running doctor", func(ctx context.Context) actionMsg {
			report := m.actions.Doctor(ctx)
			return actionMsg{kind: actionDoctor, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	case "logout":
		m.resetInstallFlow()
		m.updateConfirmationPending = false
		return m.runAsync(actionLogout, "Removing local session", func(ctx context.Context) actionMsg {
			result, err := m.actions.Logout(ctx)
			if err != nil {
				return actionMsg{kind: actionLogout, title: "Logout failed", body: err.Error(), isError: true}
			}
			return actionMsg{kind: actionLogout, title: "Logout complete", body: result.Summary}
		})
	case "login":
		m.resetInstallFlow()
		m.updateConfirmationPending = false
		m.focus = focusLogin
		for i := range m.loginInputs {
			m.loginInputs[i].Blur()
		}
		m.loginInputs[0].Focus()
		m.loginError = ""
		m.statusTitle = "Login"
		m.replaceStatusBody("Enter your server URL, account email, and password. Lore mints a reusable API token, stores only that token in secure credential storage, and keeps CLI --token as the compatibility path for older servers.")
		m.statusTone = toneInfo
		return m, nil
	case "install":
		m.updateConfirmationPending = false
		if !m.installSelectionPending {
			m.installConfirmationPending = false
			m.detailsVisible = false
			m.installSelectionPending = true
			m.focus = focusDetail
			m.statusTitle = "Install Lore"
			m.replaceStatusBody(m.renderInstallTargetSelection())
			m.statusTone = toneInfo
			return m, nil
		}
		selectedTarget := m.selectedInstallTarget()
		if !selectedTarget.Available {
			m.statusTitle = "Install target unavailable"
			m.replaceStatusBody(fmt.Sprintf("%s is %s. Choose a supported target before continuing.", selectedTarget.Title, selectedTarget.Availability))
			m.statusTone = toneMuted
			return m, nil
		}
		m.installSelectionPending = false
		m.installConfirmationPending = true
		m.detailsVisible = false
		m.focus = focusDetail
		m.statusTitle = "Confirm install"
		m.replaceStatusBody(m.renderInstallConfirmation())
		m.statusTone = toneInfo
		return m, nil
	case "update":
		m.resetInstallFlow()
		if m.actions.Update == nil {
			return m, nil
		}
		if !m.updateChecked {
			m.focus = focusDetail
			m.statusTitle = "Checking for updates"
			m.replaceStatusBody("A background binary-only update check is still in progress. Pi runtime and ~/.pi remain untouched.")
			m.statusTone = toneInfo
			return m, nil
		}
		if !m.updateAvailable {
			m.focus = focusDetail
			m.statusTitle = "Lore CLI update"
			m.replaceStatusBody(fallbackUpdateDetail(m.updateNotice, "Lore CLI is already current. Pi runtime and ~/.pi remain untouched."))
			m.statusTone = toneInfo
			return m, nil
		}
		m.updateConfirmationPending = true
		m.focus = focusDetail
		m.statusTitle = "Confirm Lore CLI update"
		m.replaceStatusBody(fmt.Sprintf("Update only the Lore CLI binary from %s to %s? Press y to continue or n to cancel. Pi runtime and ~/.pi remain untouched.", fallbackUpdateValue(m.updateCurrentVersion, "current"), fallbackUpdateValue(m.updateLatestVersion, "latest")))
		m.statusTone = toneInfo
		return m, nil
	case "quit":
		m.resetInstallFlow()
		m.updateConfirmationPending = false
		m.quitting = true
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m model) startInstallFlow() (tea.Model, tea.Cmd) {
	selectedTarget := m.selectedInstallTarget()
	if selectedTarget.ID == install.TargetPi && m.actions.PlanPiInstall != nil {
		plan, report, ok := m.actions.PlanPiInstall(context.Background())
		if !ok {
			m.focus = focusDetail
			m.statusTitle = report.Title
			m.replaceStatusBody(renderReport(report))
			m.statusTone = toneError
			return m, nil
		}
		m.installPlan = &plan
		if plan.ExistingPi.Exists && plan.FullBackup != nil {
			m.installBackupDecisionPending = true
			m.focus = focusDetail
			m.statusTitle = "Full backup before install?"
			m.replaceStatusBody(fmt.Sprintf("Existing ~/.pi detected at %s. Create a full backup before Lore mutates managed Pi files? Press y to schedule the full backup at %s, or n to continue without it.", plan.ExistingPi.Path, plan.FullBackup.BackupPath))
			m.statusTone = toneInfo
			return m, nil
		}
		return m.runInstallWithPlan(plan)
	}
	if selectedTarget.ID == install.TargetPi {
		if homeDir, err := os.UserHomeDir(); err == nil {
			currentUser, currentErr := user.Current()
			if currentErr != nil || currentUser.HomeDir != homeDir {
				plan := install.PiInstallPlan{Layout: install.ResolvePiLayout(homeDir)}
				if info, statErr := os.Lstat(plan.Layout.PiDir); statErr == nil {
					plan.ExistingPi = install.ExistingPiState{Exists: true, Path: plan.Layout.PiDir, Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UTC()}
					m.installPlan = &plan
					m.installBackupDecisionPending = true
					m.focus = focusDetail
					m.statusTitle = "Full backup before install?"
					m.replaceStatusBody(fmt.Sprintf("Existing ~/.pi detected at %s. Create a full backup before Lore mutates managed Pi files? Press y to continue with a full backup, or n to continue without it.", plan.ExistingPi.Path))
					m.statusTone = toneInfo
					return m, nil
				}
			}
		}
	}
	if m.actions.InstallTarget != nil {
		return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
			report := m.actions.InstallTarget(ctx, selectedTarget.ID)
			return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	}
	if selectedTarget.ID != install.DefaultInstallTarget() || m.actions.Install == nil {
		return m, nil
	}
	return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
		report := m.actions.Install(ctx)
		return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
	})
}

func (m model) confirmInstallBackupDecision(includeBackup bool) (tea.Model, tea.Cmd) {
	m.installBackupDecisionPending = false
	if m.installPlan != nil && !includeBackup {
		planCopy := *m.installPlan
		planCopy.FullBackup = nil
		m.installPlan = &planCopy
	}
	if m.installPlan != nil {
		return m.runInstallWithPlan(*m.installPlan)
	}
	if m.actions.InstallTarget != nil {
		return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
			report := m.actions.InstallTarget(ctx, install.TargetPi)
			return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	}
	if m.actions.Install == nil {
		return m, nil
	}
	return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
		report := m.actions.Install(ctx)
		return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
	})
}

func (m model) confirmUpdateDecision(confirmed bool) (tea.Model, tea.Cmd) {
	m.updateConfirmationPending = false
	if !confirmed {
		m.focus = focusDetail
		m.statusTitle = "Lore CLI update cancelled"
		m.replaceStatusBody("Binary-only update cancelled. Pi runtime and ~/.pi remain untouched.")
		m.statusTone = toneInfo
		return m, nil
	}
	return m.runAsync(actionUpdate, "Updating Lore CLI", func(ctx context.Context) actionMsg {
		report := m.actions.Update(ctx)
		return actionMsg{kind: actionUpdate, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
	})
}

func (m model) runInstallWithPlan(plan install.PiInstallPlan) (tea.Model, tea.Cmd) {
	if m.actions.ExecutePiInstall != nil {
		return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
			report := m.actions.ExecutePiInstall(ctx, plan)
			return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	}
	if m.actions.InstallTarget != nil {
		return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
			report := m.actions.InstallTarget(ctx, install.TargetPi)
			return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
		})
	}
	if m.actions.Install == nil {
		return m, nil
	}
	return m.runAsync(actionInstall, "Install Lore", func(ctx context.Context) actionMsg {
		report := m.actions.Install(ctx)
		return actionMsg{kind: actionInstall, title: report.Title, body: renderReport(report), isError: report.ExitCode != 0}
	})
}

func (m *model) moveInstallTargetSelection(step int) {
	if len(m.installTargets) == 0 {
		return
	}
	index := m.installTargetIndex
	for attempts := 0; attempts < len(m.installTargets); attempts++ {
		index = (index + step + len(m.installTargets)) % len(m.installTargets)
		if m.installTargets[index].Available {
			m.installTargetIndex = index
			return
		}
	}
}

func (m model) selectedInstallTarget() install.Target {
	if len(m.installTargets) == 0 {
		return install.Target{ID: install.DefaultInstallTarget(), Title: "Pi", Available: true, Recommended: true}
	}
	if m.installTargetIndex < 0 || m.installTargetIndex >= len(m.installTargets) {
		return m.installTargets[0]
	}
	return m.installTargets[m.installTargetIndex]
}

func (m model) renderInstallTargetSelection() string {
	var b strings.Builder
	selected := m.selectedInstallTarget()
	b.WriteString("Choose an install target:\n")
	for i, target := range m.installTargets {
		prefix := "- "
		if i == m.installTargetIndex {
			prefix = "› "
		}
		label := target.Title
		if target.Recommended {
			label += " — Recommended"
		}
		help := target.ShortHelp
		if strings.TrimSpace(help) == "" {
			help = target.Description
		}
		if target.Available {
			fmt.Fprintf(&b, "%s%s: %s\n", prefix, label, help)
			continue
		}
		fmt.Fprintf(&b, "%s%s: %s (%s)\n", prefix, label, help, target.Availability)
	}
	if m.detailsVisible {
		details := selected.Details
		if strings.TrimSpace(details) == "" {
			details = selected.Description
		}
		fmt.Fprintf(&b, "\nDetails for %s:\n%s\n\nPress ? to hide details. Selection is preserved.", selected.Title, details)
		return b.String()
	}
	fmt.Fprintf(&b, "\nSelected target: %s. Use ↑/↓ to switch between supported targets and Enter to continue. Pi remains the default recommended path. Press ? details for technical notes.", selected.Title)
	return b.String()
}

func (m model) renderInstallConfirmation() string {
	selected := m.selectedInstallTarget()
	var b strings.Builder
	fmt.Fprintf(&b, "Install target: %s", selected.Title)
	if selected.Recommended {
		b.WriteString(" — recommended")
	}
	b.WriteString("\n")
	help := selected.ShortHelp
	if strings.TrimSpace(help) == "" {
		help = selected.Description
	}
	fmt.Fprintf(&b, "%s\n\n", help)
	switch selected.ID {
	case install.TargetPi:
		b.WriteString("Continue to the existing Pi install plan and backup check before any files are changed.")
	case install.TargetOpenCode, install.TargetCodex, install.TargetAntigravity:
		b.WriteString("Continue with the existing installer for this target; no auth, HTTP, or config persistence semantics change.")
	default:
		b.WriteString("Continue only if this target is supported.")
	}
	if m.detailsVisible {
		details := selected.Details
		if strings.TrimSpace(details) == "" {
			details = selected.Description
		}
		fmt.Fprintf(&b, "\n\nDetails:\n%s\n\nPress ? to hide details.", details)
	}
	b.WriteString("\n\nPress y/Enter to continue, n/Esc to cancel.")
	return b.String()
}

func (m model) submitLogin() (tea.Model, tea.Cmd) {
	serverURL := strings.TrimSpace(m.loginInputs[0].Value())
	email := strings.TrimSpace(m.loginInputs[1].Value())
	password := m.loginInputs[2].Value()
	if serverURL == "" || email == "" || strings.TrimSpace(password) == "" {
		m.loginError = "Server URL, email, and password are all required."
		return m, nil
	}
	if m.actions.LoginWithInput == nil {
		m.loginError = "Password login is unavailable in this build."
		return m, nil
	}
	m.loginError = ""
	return m.runAsync(actionLogin, "Validating credentials", func(ctx context.Context) actionMsg {
		result, err := m.actions.LoginWithInput(ctx, cli.LoginInput{Mode: "password", ServerURL: serverURL, Email: email, Password: password})
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
	m.replaceStatusBody("Please wait…")
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

func fallbackUpdateDetail(detail, fallback string) string {
	if strings.TrimSpace(detail) == "" {
		return fallback
	}
	return detail
}

func fallbackUpdateValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
