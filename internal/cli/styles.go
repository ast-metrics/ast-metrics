package cli

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/charmbracelet/lipgloss"
	osterm "golang.org/x/term"
)

// Colours are fixed, never lipgloss.AdaptiveColor: resolving an adaptive colour
// makes lipgloss ask the terminal for its background colour (OSC 11), and a
// terminal that does not answer costs termenv.OSCTimeout, five seconds, before
// the analysis has even started. The palette below assumes a dark background,
// like the rest of the interface.
var (
	special = lipgloss.Color("#73F59F")

	titleStyle = lipgloss.NewStyle().
			Padding(1).
			Italic(true).
			Align(lipgloss.Center).
			Foreground(lipgloss.Color("#FFF7DB")).
			Background(lipgloss.Color("#5A56E0"))

	subtitleStyle = lipgloss.NewStyle().
			Padding(1).
			Italic(true).
			Align(lipgloss.Center)
)

func windowWidth() int {
	width, _, _ := osterm.GetSize(0)
	return min(max(width-1, 120), 120)
}

func StyleTitle(text string) lipgloss.Style {
	return titleStyle.Width(windowWidth()).SetString(text)
}

// ScreenHeader renders a compact header for sub-screens:
// a breadcrumb-style line with the page title, plus a separator, and an esc hint
// when there is a keyboard to press it on. Commands such as lint and ci print
// this header while running non-interactively, where "(esc to go back)" would be
// an instruction the reader cannot follow, in a CI log nobody can type into.
func ScreenHeader(pageTitle string) string {
	logoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#73F59F")).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	titleTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#333333"))

	header := logoStyle.Render("◉ AST Metrics") +
		sepStyle.Render(" > ") +
		titleTextStyle.Render(pageTitle)

	w := windowWidth()
	line := lineStyle.Render(fmt.Sprintf("%*s", 0, "") + fmt.Sprintf("%s", repeatRune('─', w)))

	if osterm.IsTerminal(int(os.Stdin.Fd())) {
		header += "  " + hintStyle.Render("(esc to go back)")
	}

	return header + "\n" + line + "\n"
}

func repeatRune(r rune, count int) string {
	b := make([]byte, 0, count*3)
	for i := 0; i < count; i++ {
		b = append(b, string(r)...)
	}
	return string(b)
}

func StyleHowToQuit(text string) lipgloss.Style {
	if text == "" {
		text = "Press 'Ctrl + C' to quit"
	}
	return lipgloss.NewStyle().
		Width(windowWidth()).
		SetString(text).
		MarginTop(2).Align(lipgloss.Center)
}

func StyleSubTitle(text string) lipgloss.Style {
	return subtitleStyle.Width(windowWidth()).SetString(text)
}

func StyleUrl(text string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(special).SetString(text)
}

func StyleHelp(text string) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(text).
		Italic(true).
		Foreground(lipgloss.Color("#666666"))
}

func StyleChoices(text string) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(text).
		MarginLeft(4)
}

func StyleCommand(text string) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(text).
		Foreground(lipgloss.Color("#666666"))
}
func StyleScreen(text string) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(text).
		Width(windowWidth()).
		MarginLeft(1).
		MarginTop(1)
}

func StyleNumberBox(number string, label string, sublabel string) lipgloss.Style {
	return lipgloss.NewStyle().
		PaddingTop(1).
		PaddingBottom(1).
		MarginRight(2).
		Border(lipgloss.RoundedBorder(), true).
		Background(lipgloss.Color("#2563eb")).
		Foreground(lipgloss.Color("#FFFFFF")).
		BorderForeground(lipgloss.Color("#1e3a8a")).
		Width(35).
		Height(13).
		Align(lipgloss.Center).
		SetString(fmt.Sprintf(
			"%s\n\n%s\n\n%s",
			lipgloss.NewStyle().Bold(true).SetString(number).Render(),
			lipgloss.NewStyle().Bold(false).SetString(label).Render(),
			lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC")).Italic(true).SetString(sublabel).Render(),
		))

}

func DecorateMaintainabilityIndex(mi int, analyze *pb.Analyze) string {

	min := int32(1)

	if analyze != nil && analyze.Volume != nil && analyze.Volume.Lloc != nil && *analyze.Volume.Lloc < min {
		return "-"
	}

	if mi < 64 {
		return "🔴 " + strconv.Itoa(mi)
	}
	if mi < 85 {
		return "🟡 " + strconv.Itoa(mi)
	}

	return "🟢 " + strconv.Itoa(mi)
}

func Round(num float64) int {
	return int(num + float64(math.Copysign(0.5, float64(num))))
}

func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(Round(num*float64(output))) / float64(output)
}

// PressAnyKeyToContinue waits for the user to press any key before continuing.
// Supports Enter, Esc, q, and any other key in raw terminal mode.
func PressAnyKeyToContinue() {
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Italic(true)
	fmt.Print(hint.Render("  Press any key to continue..."))

	fd := int(os.Stdin.Fd())
	oldState, err := osterm.MakeRaw(fd)
	if err != nil {
		// Fallback to line-buffered read if raw mode fails
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		return
	}
	defer osterm.Restore(fd, oldState)

	// Read a single byte (any key)
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
	fmt.Println()
}

// PrintSuccess prints a success message styled to match the TUI look.
func PrintSuccess(msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#73F59F")).Bold(true)
	fmt.Println(style.Render("  ✓ " + msg))
}

// PrintError prints an error message styled to match the TUI look.
func PrintError(msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true)
	fmt.Println(style.Render("  ✗ " + msg))
}

// PrintWarning prints a warning message on stdout, styled to match the TUI
// look. Use it for warnings that belong to the analysis output itself.
func PrintWarning(msg string) {
	PrintWarningTo(os.Stdout, msg)
}

// PrintWarningTo prints a warning message on the given stream. Warnings about
// how the CLI was invoked go to stderr, so they never end up in the output a
// script parses or redirects to a file.
func PrintWarningTo(writer io.Writer, msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C")).Bold(true)
	fmt.Fprintln(writer, style.Render("  ⚠ "+msg))
}

// PrintInfo prints an info message styled to match the TUI look.
func PrintInfo(msg string) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	fmt.Println(style.Render("  " + msg))
}
