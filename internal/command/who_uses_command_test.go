package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
)

// a logger wrapping a library, a service using the logger, a file on its own
func whoUsesProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range map[string]string{
		"go.mod":                      "module example.com/demo\n",
		"internal/log/logger.go":      "package log\n\nimport \"github.com/sirupsen/logrus\"\n\ntype Logger struct{ inner *logrus.Logger }\n",
		"internal/billing/service.go": "package billing\n\nimport \"example.com/demo/internal/log\"\n\ntype Service struct{ logger log.Logger }\n",
		"internal/alone/alone.go":     "package alone\n\nfunc Alone() {}\n",
	} {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func whoUsesCommand(t *testing.T, root, query string) (*WhoUsesCommand, *bytes.Buffer) {
	t.Helper()
	cfg := configuration.NewConfiguration()
	if err := cfg.SetSourcesToAnalyzePath([]string{root}); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	return NewWhoUsesCommand(cfg, out, []engine.Engine{&golang.GolangRunner{}}, query), out
}

func TestWhoUsesCommandListsTheFilesLevelByLevel(t *testing.T) {
	root := whoUsesProject(t)
	cmd, out := whoUsesCommand(t, root, "logrus")
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := out.String()
	for _, expected := range []string{
		`Who uses "logrus"?`,
		"github.com/sirupsen/logrus",
		"Reach: 2 of 3 files (67%), up to 1 level away from the import",
		"Level 0, imports it (1 file):\n  internal/log/logger.go",
		"Level 1 (1 file):\n  internal/billing/service.go",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("expected the output to contain %q, got:\n%s", expected, text)
		}
	}
}

func TestWhoUsesCommandWritesJSON(t *testing.T) {
	root := whoUsesProject(t)
	cmd, out := whoUsesCommand(t, root, "logrus")
	cmd.Format = "json"
	cmd.MaxDepth = 0
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var reach struct {
		Query   string     `json:"query"`
		Modules []string   `json:"modules"`
		Levels  [][]string `json:"levels"`
		Scope   int        `json:"scope"`
	}
	if err := json.Unmarshal(out.Bytes(), &reach); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if reach.Query != "logrus" || len(reach.Modules) != 1 || reach.Scope != 3 {
		t.Errorf("unexpected reach: %+v", reach)
	}
	if len(reach.Levels) != 2 || len(reach.Levels[0]) != 1 || len(reach.Levels[1]) != 1 {
		t.Errorf("expected two levels of one file, got %v", reach.Levels)
	}
	if !strings.HasSuffix(reach.Levels[1][0], filepath.Join("internal", "billing", "service.go")) {
		t.Errorf("expected the service one level away, got %v", reach.Levels[1])
	}
}

func TestWhoUsesCommandFailsWhenNothingMatches(t *testing.T) {
	root := whoUsesProject(t)
	cmd, out := whoUsesCommand(t, root, "log4j")
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), `no imported module matches "log4j"`) {
		t.Fatalf("expected an error naming the query, got %v", err)
	}
	if !strings.Contains(out.String(), "No imported module matches") {
		t.Errorf("expected a hint in the output, got:\n%s", out.String())
	}

	cmd, _ = whoUsesCommand(t, root, "  ")
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an empty query")
	}
	cmd, _ = whoUsesCommand(t, root, "logrus")
	cmd.Format = "xml"
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for an unknown format")
	}
}
