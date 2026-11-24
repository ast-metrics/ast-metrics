package cli

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WelcomeResult holds the user's input from the welcome screen.
type WelcomeResult struct {
	Command string   // "analyze", "lint", "clean", "self-update", "init", "version", "ci", "ruleset", "help", "quit"
	Args    []string // optional arguments (e.g., paths)
}

type completionItem struct {
	trigger     string
	description string
}

// maxCompletionLines is the default number of lines reserved for the
// completion area, preventing bubbletea rendering artifacts.
const maxCompletionLines = 12

// effectiveCompletionLines returns a reduced completion area for short terminals.
func effectiveCompletionLines(windowHeight int) int {
	if windowHeight > 0 && windowHeight < 30 {
		lines := windowHeight - 15 // reserve ~15 lines for header+input+footer
		if lines < 3 {
			lines = 3
		}
		if lines > maxCompletionLines {
			lines = maxCompletionLines
		}
		return lines
	}
	return maxCompletionLines
}

// completionMode distinguishes command completions from path completions.
type completionMode int

const (
	compModeCommand completionMode = iota
	compModePath
)

// tips are random helpful hints shown on the welcome screen.
var tips = []string{
	"Use \"ast-metrics lint\" to check your code against quality rules.",
	"Run \"ast-metrics ci\" to generate all reports at once for your CI pipeline.",
	"Add a .ast-metrics.yaml config file with \"/init\" to customize analysis.",
	"Use \"--compare-with main\" to see how your branch differs from main.",
	"Explore rulesets with \"ast-metrics ruleset list\" to find built-in quality rules.",
	"Add a ruleset to your config with \"ast-metrics ruleset add <name>\".",
	"Use \"--watch\" flag with analyze to re-run analysis when files change.",
	"AST Metrics supports Go, PHP, Python, Rust, Java, C#, TypeScript and C++ out of the box.",
	"Use \"--exclude\" to skip files matching a pattern (e.g., vendor, node_modules).",
	"Maintainability Index below 64 is a red flag. Above 85 is excellent.",
}

// allCompletions lists every token the user can type.
var allCompletions = []completionItem{
	// Slash shortcuts
	{"/clean", "Clean workdir"},
	{"/self-update", "Update current binary"},
	{"/init", "Create default configuration file"},
	{"/version", "Print version information"},
	{"/help", "Show help"},
	// Commands
	{"analyze", "Analyze a project"},
	{"lint", "Run lint rules on a project"},
	{"review", "Compare with a base branch and report only new findings"},
	{"ci", "Run lint + analysis with reports (CI mode)"},
	{"ruleset list", "List available rulesets"},
	{"ruleset show", "Show rules inside a ruleset"},
	{"ruleset add", "Add a ruleset to configuration"},
}

type modelWelcome struct {
	textInput    textinput.Model
	version      string
	showHelp     bool
	errorMsg     string
	result       WelcomeResult
	quitting     bool
	completions  []completionItem
	compMode     completionMode
	compIndex    int
	compOffset   int    // scroll offset for visible window
	lastInput    string // tracks last input to detect real changes
	tip          string
	windowWidth  int
	windowHeight int
}

func newModelWelcome(version string, initialError string) modelWelcome {
	ti := textinput.New()
	ti.Placeholder = `Try "analyze ./src"...`
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#73F59F")).Bold(true)
	ti.Prompt = "❯ "

	return modelWelcome{
		textInput: ti,
		version:   version,
		errorMsg:  initialError,
		result:    WelcomeResult{Command: "quit"},
		compIndex: -1,
		tip:       tips[rand.Intn(len(tips))],
	}
}

// matchCommandCompletions returns command completions matching the prefix.
func matchCommandCompletions(input string) []completionItem {
	if input == "" {
		return nil
	}
	lower := strings.ToLower(input)
	var matches []completionItem
	for _, c := range allCompletions {
		if strings.HasPrefix(strings.ToLower(c.trigger), lower) && strings.ToLower(c.trigger) != lower {
			matches = append(matches, c)
		}
	}
	return matches
}

// matchPathCompletions returns filesystem path completions for a partial path.
func matchPathCompletions(partial string) []completionItem {
	if partial == "" {
		return nil
	}

	// Expand ~ to home directory
	expandedPartial := partial
	if strings.HasPrefix(partial, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			expandedPartial = filepath.Join(home, partial[1:])
		}
	}

	// Get the directory to list and the prefix to match
	dir := filepath.Dir(expandedPartial)
	prefix := filepath.Base(expandedPartial)

	// If the partial ends with /, list the directory contents
	if strings.HasSuffix(partial, "/") || strings.HasSuffix(partial, string(os.PathSeparator)) {
		dir = expandedPartial
		prefix = ""
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var matches []completionItem
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			continue
		}

		if prefix == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			fullPath := filepath.Join(dir, name)

			// Build display path: use the original partial's directory part
			displayDir := filepath.Dir(partial)
			if strings.HasSuffix(partial, "/") || strings.HasSuffix(partial, string(os.PathSeparator)) {
				displayDir = partial
			}
			displayPath := filepath.Join(displayDir, name)

			desc := "file"
			if entry.IsDir() {
				displayPath += "/"
				fullPath += "/"
				desc = "directory"
			}

			_ = fullPath
			matches = append(matches, completionItem{
				trigger:     displayPath,
				description: desc,
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		// Directories first, then alphabetical
		di := strings.HasSuffix(matches[i].trigger, "/")
		dj := strings.HasSuffix(matches[j].trigger, "/")
		if di != dj {
			return di
		}
		return matches[i].trigger < matches[j].trigger
	})

	return matches
}

// updateCompletions refreshes the completion list based on current input.
func (m *modelWelcome) updateCompletions() {
	val := m.textInput.Value()

	// If input hasn't changed, don't reset selection
	if val == m.lastInput {
		return
	}
	m.lastInput = val

	// No input → no completions
	if val == "" {
		m.completions = nil
		m.compMode = compModeCommand
		m.compIndex = -1
		m.compOffset = 0
		return
	}

	// If input has a space, we're in path completion mode for the arg part
	if spaceIdx := strings.IndexByte(val, ' '); spaceIdx >= 0 {
		pathPart := val[spaceIdx+1:]
		if pathPart != "" {
			m.completions = matchPathCompletions(pathPart)
			m.compMode = compModePath
		} else {
			m.completions = nil
			m.compMode = compModePath
		}
		m.compIndex = -1
		m.compOffset = 0
		return
	}

	// Otherwise, command completion
	m.completions = matchCommandCompletions(val)
	m.compMode = compModeCommand
	m.compIndex = -1
	m.compOffset = 0
}

// acceptCompletion applies the selected completion to the text input.
func (m *modelWelcome) acceptCompletion(idx int) {
	if idx < 0 || idx >= len(m.completions) {
		return
	}

	completed := m.completions[idx].trigger

	if m.compMode == compModePath {
		// Replace only the path part (after the first space)
		val := m.textInput.Value()
		spaceIdx := strings.IndexByte(val, ' ')
		if spaceIdx >= 0 {
			newVal := val[:spaceIdx+1] + completed
			m.textInput.SetValue(newVal)
			m.textInput.SetCursor(len(newVal))
		}
	} else {
		// Command completion: add a space after for args
		if !strings.HasPrefix(completed, "/") {
			completed += " "
		}
		m.textInput.SetValue(completed)
		m.textInput.SetCursor(len(completed))
	}

	m.completions = nil
	m.compIndex = -1
	m.compOffset = 0
}

func (m modelWelcome) Init() tea.Cmd {
	return textinput.Blink
}

func (m modelWelcome) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		if msg.Width > 6 {
			m.textInput.Width = msg.Width - 6
		}
		return m, nil

	case tea.KeyMsg:
		// Clear error on any keypress
		if m.errorMsg != "" {
			m.errorMsg = ""
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.result = WelcomeResult{Command: "quit"}
			return m, tea.Quit

		case "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			if len(m.completions) > 0 {
				m.completions = nil
				m.compIndex = -1
				return m, nil
			}
			m.quitting = true
			m.result = WelcomeResult{Command: "quit"}
			return m, tea.Quit

		case "?":
			if m.textInput.Value() == "" {
				m.showHelp = !m.showHelp
				return m, nil
			}

		case "tab":
			// Bash-style: tab accepts the first/selected completion
			if len(m.completions) > 0 {
				idx := m.compIndex
				if idx < 0 {
					idx = 0
				}
				m.acceptCompletion(idx)
				m.updateCompletions()
				return m, nil
			}
			return m, nil

		case "down":
			if len(m.completions) > 0 {
				if m.compIndex < len(m.completions)-1 {
					m.compIndex++
				}
				// Keep selection visible
				m.ensureCompVisible()
				return m, nil
			}

		case "up":
			if len(m.completions) > 0 {
				if m.compIndex > 0 {
					m.compIndex--
				} else if m.compIndex == -1 {
					m.compIndex = 0
				}
				// Keep selection visible
				m.ensureCompVisible()
				return m, nil
			}

		case "enter":
			// If completions are visible and one is selected, accept it first
			if len(m.completions) > 0 && m.compIndex >= 0 {
				m.acceptCompletion(m.compIndex)
				return m, nil
			}

			input := strings.TrimSpace(m.textInput.Value())
			if input == "" {
				return m, nil
			}
			parsed := parseWelcomeInput(input)
			if parsed.Command == "unknown" {
				m.errorMsg = "Unknown command: " + input
				m.textInput.SetValue("")
				m.completions = nil
				m.compIndex = -1
				return m, nil
			}
			m.result = parsed
			return m, tea.Quit
		}
	}

	if !m.showHelp {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		// Update completions live as the user types (both command and path mode)
		m.updateCompletions()
		return m, cmd
	}

	return m, nil
}

func (m modelWelcome) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header with tree art + info
	b.WriteString("\n")
	b.WriteString(RenderHeader(m.version, true, m.windowWidth, m.windowHeight))
	b.WriteString("\n\n")

	// Error message (if any)
	if m.errorMsg != "" {
		errStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555")).
			Bold(true)
		b.WriteString(errStyle.Render("  " + m.errorMsg))
		b.WriteString("\n\n")
	}

	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))
	width := 80
	if m.windowWidth > 2 {
		width = m.windowWidth - 2
	}
	sep := sepStyle.Render(strings.Repeat("─", width))

	if m.showHelp {
		b.WriteString(m.renderHelp())
	} else {
		// Input box with top/bottom borders
		b.WriteString(sep)
		b.WriteString("\n")
		b.WriteString(m.textInput.View())
		b.WriteString("\n")
		b.WriteString(sep)
		b.WriteString("\n")

		// Autocomplete suggestions (fixed-height block)
		compRendered := ""
		compLines := 0
		if len(m.completions) > 0 {
			compRendered = m.renderCompletions()
			compLines = strings.Count(compRendered, "\n")
		}
		// Hint (right below the input separator, before completions)
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))
		b.WriteString(hintStyle.Render("  ? for shortcuts"))
		b.WriteString("\n")

		b.WriteString(compRendered)
		// Pad remaining lines to keep total height constant
		effLines := effectiveCompletionLines(m.windowHeight)
		usedLines := compLines + 1 // +1 for the hint line
		for i := usedLines; i < effLines; i++ {
			b.WriteString("\n")
		}
	}

	// Build the tip footer (separator + tip)
	tipStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#73F59F")).
		Italic(true)
	footer := sep + "\n" + tipStyle.Render("  💡 "+m.tip)

	// Push the footer to the bottom of the window if there's space
	mainContent := b.String()
	mainLines := strings.Count(mainContent, "\n")
	footerLines := 2 // separator + tip line
	if m.windowHeight > 0 {
		available := m.windowHeight - mainLines - footerLines
		if available > 0 {
			mainContent += strings.Repeat("\n", available)
		}
	}

	return mainContent + footer
}

// visibleSlots returns how many completion items fit in the visible area,
// accounting for scroll indicator lines.
func (m modelWelcome) visibleSlots() int {
	slots := effectiveCompletionLines(m.windowHeight)
	if m.compOffset > 0 {
		slots-- // ↑ more indicator
	}
	if m.compOffset+slots < len(m.completions) {
		slots-- // ↓ more indicator
	}
	if slots < 1 {
		slots = 1
	}
	return slots
}

// ensureCompVisible adjusts compOffset so that compIndex is in the visible window.
func (m *modelWelcome) ensureCompVisible() {
	if m.compIndex < 0 {
		return
	}
	// Scroll down if needed
	for m.compIndex >= m.compOffset+m.visibleSlots() {
		m.compOffset++
	}
	// Scroll up if needed
	for m.compIndex < m.compOffset {
		m.compOffset--
	}
	if m.compOffset < 0 {
		m.compOffset = 0
	}
}

func (m modelWelcome) renderCompletions() string {
	var b strings.Builder

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#73F59F")).
		Bold(true)
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555"))
	scrollHint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Italic(true)

	hasAbove := m.compOffset > 0
	slots := m.visibleSlots()
	end := m.compOffset + slots
	if end > len(m.completions) {
		end = len(m.completions)
	}
	hasBelow := end < len(m.completions)

	if hasAbove {
		b.WriteString(scrollHint.Render("  ↑ more") + "\n")
	}

	for i := m.compOffset; i < end; i++ {
		c := m.completions[i]
		style := normalStyle
		prefix := "  "
		if i == m.compIndex {
			style = selectedStyle
			prefix = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			prefix,
			style.Render(fmt.Sprintf("%-30s", c.trigger)),
			descStyle.Render(c.description),
		))
	}

	if hasBelow {
		b.WriteString(scrollHint.Render("  ↓ more") + "\n")
	}

	return b.String()
}

func (m modelWelcome) renderHelp() string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)
	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#73F59F"))
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	b.WriteString(headerStyle.Render("  Shortcuts"))
	b.WriteString("\n\n")

	for _, c := range allCompletions {
		if strings.HasPrefix(c.trigger, "/") {
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				cmdStyle.Render(fmt.Sprintf("%-16s", c.trigger)),
				descStyle.Render(c.description),
			))
		}
	}

	b.WriteString("\n")
	b.WriteString(headerStyle.Render("  Commands"))
	b.WriteString("\n\n")

	for _, c := range allCompletions {
		if !strings.HasPrefix(c.trigger, "/") {
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				cmdStyle.Render(fmt.Sprintf("%-16s", c.trigger)),
				descStyle.Render(c.description),
			))
		}
	}

	b.WriteString("\n")
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Italic(true)
	b.WriteString(hintStyle.Render("  esc to dismiss"))
	b.WriteString("\n")

	return b.String()
}

// parseWelcomeInput parses the user's text input into a WelcomeResult.
func parseWelcomeInput(input string) WelcomeResult {
	// Handle slash commands
	if strings.HasPrefix(input, "/") {
		cmd := strings.Fields(input)[0]
		switch cmd {
		case "/clean":
			return WelcomeResult{Command: "clean"}
		case "/self-update":
			return WelcomeResult{Command: "self-update"}
		case "/init":
			return WelcomeResult{Command: "init"}
		case "/version":
			return WelcomeResult{Command: "version"}
		case "/help":
			return WelcomeResult{Command: "help"}
		default:
			return WelcomeResult{Command: "unknown", Args: []string{input}}
		}
	}

	// Handle regular commands
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return WelcomeResult{Command: "quit"}
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "analyze", "a":
		return WelcomeResult{Command: "analyze", Args: args}
	case "lint", "l":
		return WelcomeResult{Command: "lint", Args: args}
	case "ci":
		return WelcomeResult{Command: "ci", Args: args}
	case "clean", "c":
		return WelcomeResult{Command: "clean"}
	case "self-update", "u":
		return WelcomeResult{Command: "self-update"}
	case "init":
		return WelcomeResult{Command: "init"}
	case "version", "v":
		return WelcomeResult{Command: "version"}
	case "help", "h":
		return WelcomeResult{Command: "help"}
	case "ruleset":
		// "ruleset list", "ruleset show <name>", "ruleset add <name>"
		if len(args) == 0 {
			return WelcomeResult{Command: "ruleset", Args: []string{"list"}}
		}
		return WelcomeResult{Command: "ruleset", Args: args}
	default:
		return WelcomeResult{Command: "unknown", Args: []string{input}}
	}
}

// ShowWelcomeScreen displays the interactive welcome screen and returns the user's choice.
// An optional errorMsg is displayed above the input if non-empty.
func ShowWelcomeScreen(version string, errorMsg ...string) WelcomeResult {
	initialErr := ""
	if len(errorMsg) > 0 {
		initialErr = errorMsg[0]
	}
	m := newModelWelcome(version, initialErr)

	p := tea.NewProgram(&m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return WelcomeResult{Command: "quit"}
	}

	if result == nil {
		return WelcomeResult{Command: "quit"}
	}

	mm, ok := result.(modelWelcome)
	if !ok {
		return WelcomeResult{Command: "quit"}
	}

	return mm.result
}
