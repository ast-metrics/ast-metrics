package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/cli"
	cliV2 "github.com/urfave/cli/v2"
)

// decision is what the interactivity helpers concluded for one invocation.
type decision struct {
	interactive bool
	welcome     bool
	called      bool
}

// decide runs the given command line through an application declaring the same
// interactivity flags as the real one, and reports what the helpers concluded.
// The flags are declared both globally and on the command, exactly like in
// main(), so the lineage lookup is really exercised.
func decide(t *testing.T, terminal bool, args ...string) decision {
	t.Helper()

	restore := hasTerminal
	hasTerminal = func() bool { return terminal }
	t.Cleanup(func() { hasTerminal = restore })

	result := decision{}
	record := func(cCtx *cliV2.Context) error {
		result.interactive = isInteractiveSession(cCtx)
		result.welcome = shouldShowWelcomeScreen(cCtx)
		result.called = true
		return nil
	}

	interactivityFlags := func() []cliV2.Flag {
		return []cliV2.Flag{
			&cliV2.BoolFlag{Name: "tui", Aliases: []string{"interactive"}},
			&cliV2.BoolFlag{Name: "non-interactive"},
		}
	}

	app := &cliV2.App{
		Name:   "ast-metrics",
		Flags:  interactivityFlags(),
		Action: record,
		Commands: []*cliV2.Command{
			{
				Name:    "analyze",
				Aliases: []string{"a"},
				Flags:   append(interactivityFlags(), &cliV2.BoolFlag{Name: "ci"}),
				Action:  record,
			},
		},
	}

	if err := app.Run(append([]string{"ast-metrics"}, args...)); err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	if !result.called {
		t.Fatalf("running %v never reached an action", args)
	}

	return result
}

func TestDashboardIsOptIn(t *testing.T) {
	cases := []struct {
		name     string
		terminal bool
		args     []string
		want     bool
	}{
		{
			name:     "plain output by default, even in a terminal",
			terminal: true,
			args:     []string{"analyze", "."},
			want:     false,
		},
		{
			name:     "--tui asks for the dashboard",
			terminal: true,
			args:     []string{"analyze", "--tui", "."},
			want:     true,
		},
		{
			name:     "--tui is honoured when given before the command",
			terminal: true,
			args:     []string{"--tui", "analyze", "."},
			want:     true,
		},
		{
			name:     "--interactive still works as an alias",
			terminal: true,
			args:     []string{"analyze", "--interactive", "."},
			want:     true,
		},
		{
			name:     "--tui falls back to plain output without a terminal",
			terminal: false,
			args:     []string{"analyze", "--tui", "."},
			want:     false,
		},
		{
			name:     "--non-interactive wins over --tui",
			terminal: true,
			args:     []string{"analyze", "--tui", "--non-interactive", "."},
			want:     false,
		},
		{
			name:     "--ci wins over --tui",
			terminal: true,
			args:     []string{"analyze", "--tui", "--ci", "."},
			want:     false,
		},
		{
			name:     "the alias of the command keeps the same rules",
			terminal: true,
			args:     []string{"a", "--tui", "."},
			want:     true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := decide(t, testCase.terminal, testCase.args...).interactive
			if got != testCase.want {
				t.Errorf("isInteractiveSession() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestShortInteractiveFlagIsGone guards the flag that changed meaning: "-i" used
// to be an alias of --non-interactive, so accepting it again would silently turn
// the plain output of existing scripts into a full-screen dashboard.
func TestShortInteractiveFlagIsGone(t *testing.T) {
	restore := hasTerminal
	hasTerminal = func() bool { return true }
	t.Cleanup(func() { hasTerminal = restore })

	app := &cliV2.App{
		Name:      "ast-metrics",
		Writer:    io.Discard,
		ErrWriter: io.Discard,
		Commands: []*cliV2.Command{
			{
				Name:   "analyze",
				Flags:  []cliV2.Flag{&cliV2.BoolFlag{Name: "tui", Aliases: []string{"interactive"}}},
				Action: func(cCtx *cliV2.Context) error { return nil },
			},
		},
	}

	if err := app.Run([]string{"ast-metrics", "analyze", "-i", "."}); err == nil {
		t.Fatal("expected -i to be rejected, so it cannot silently mean the opposite of what it used to")
	}
}

// TestWelcomeScreenRunsCommandsInTheDashboard covers the one place where a
// command becomes interactive without the user typing --tui: reaching it
// through the menu. Typing the same command directly must stay plain and give
// the shell back.
func TestWelcomeScreenRunsCommandsInTheDashboard(t *testing.T) {
	args := welcomeCommandArgs("ast-metrics", cli.WelcomeResult{
		Command: "analyze",
		Args:    []string{"./src"},
	})

	expected := []string{"ast-metrics", "--tui", "analyze", "./src"}
	if len(args) != len(expected) {
		t.Fatalf("built %v, want %v", args, expected)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("built %v, want %v", args, expected)
		}
	}

	// The flag has to come before the command name: it is declared on the
	// application, not on the command.
	if decide(t, true, args[1:]...).interactive != true {
		t.Error("a command started from the welcome screen should show the dashboard")
	}
}

func TestWelcomeScreenNeedsATerminalAndNoCommand(t *testing.T) {
	cases := []struct {
		name     string
		terminal bool
		args     []string
		want     bool
	}{
		{
			name:     "no command in a terminal opens the welcome screen",
			terminal: true,
			args:     []string{},
			want:     true,
		},
		{
			name:     "no command in a pipe prints the help instead",
			terminal: false,
			args:     []string{},
			want:     false,
		},
		{
			name:     "--non-interactive prints the help instead",
			terminal: true,
			args:     []string{"--non-interactive"},
			want:     false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := decide(t, testCase.terminal, testCase.args...).welcome
			if got != testCase.want {
				t.Errorf("shouldShowWelcomeScreen() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestWarnOnceDoesNotRepeatItself covers the helpers being called from several
// places for a single run, and the welcome screen re-running the application
// in-process: the same notice must not pile up.
func TestWarnOnceDoesNotRepeatItself(t *testing.T) {
	message := "a warning printed once"
	warnedMessages.Delete(message)
	t.Cleanup(func() { warnedMessages.Delete(message) })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot capture stderr: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer

	warnOnce(message)
	warnOnce(message)

	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatalf("cannot close the capture: %v", err)
	}

	printed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("cannot read the capture: %v", err)
	}

	if occurrences := strings.Count(string(printed), message); occurrences != 1 {
		t.Errorf("the warning was printed %d times, want 1", occurrences)
	}
}

// TestWarningsGoToStderr keeps the invocation notices out of the output a script
// parses or redirects to a file.
func TestWarningsGoToStderr(t *testing.T) {
	message := "a warning that belongs to stderr"
	warnedMessages.Delete(message)
	t.Cleanup(func() { warnedMessages.Delete(message) })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("cannot capture stdout: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer

	warnOnce(message)

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("cannot close the capture: %v", err)
	}

	printed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("cannot read the capture: %v", err)
	}

	if strings.Contains(string(printed), message) {
		t.Error("the warning was printed on stdout, it belongs to stderr")
	}
}
