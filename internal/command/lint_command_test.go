package command

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/ast-metrics/ast-metrics/internal/storage"
)

func TestLintCommand_Execute_ReturnsErrorOnViolations(t *testing.T) {
	// Setup
	work := storage.Default()
	work.Purge()
	work.Ensure()

	cfg := configuration.NewConfiguration()
	cfg.Storage = work
	cfg.Requirements = configuration.NewConfigurationRequirements()
	intVal := func(i int) *int { return &i }
	cfg.Requirements.Rules.Volume.Loc = intVal(1)

	outWriter := bufio.NewWriter(os.Stdout)

	phpSource := `<?php
function foo() {
	echo "Hello, World!";
}
function bar() {
	echo "Hello, World!";
}
`
	runners := []engine.Engine{&php.PhpRunner{}}

	// create temporary file
	file, err := os.CreateTemp("", "lint_test_*.php")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name()) // clean up
	if _, err := file.WriteString(phpSource); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	cfg.SourcesToAnalyzePath = []string{file.Name()}
	cmd := NewLintCommand(cfg, outWriter, runners)

	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected an error when violations exist, got nil")
	}
}

func TestLintCommand_Execute_IgnoresViolationsFromBaseline(t *testing.T) {
	work := storage.Default()
	work.Purge()
	work.Ensure()

	cfg := configuration.NewConfiguration()
	cfg.Storage = work
	cfg.Requirements = configuration.NewConfigurationRequirements()
	intVal := func(i int) *int { return &i }
	cfg.Requirements.Rules.Volume.Loc = intVal(1)

	phpSource := `<?php
function foo() {
	echo "Hello, World!";
}
`
	runners := []engine.Engine{&php.PhpRunner{}}

	file, err := os.CreateTemp("", "lint_baseline_test_*.php")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(phpSource); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}
	cfg.SourcesToAnalyzePath = []string{file.Name()}

	// Without a baseline, the pre-existing violation is reported.
	outWriter := bufio.NewWriter(os.Stdout)
	if err := NewLintCommand(cfg, outWriter, runners).Execute(); err == nil {
		t.Fatalf("expected an error before generating the baseline")
	}

	// Generate a baseline capturing today's violations.
	baselinePath := filepath.Join(t.TempDir(), "baseline.yaml")
	baselineCmd := NewBaselineCommand(cfg, outWriter, runners)
	baselineCmd.Path = baselinePath
	if err := baselineCmd.Execute(); err != nil {
		t.Fatalf("failed to generate baseline: %v", err)
	}

	// With the baseline configured, the same violation is now ignored.
	cfg.Requirements.Baseline = baselinePath
	if err := NewLintCommand(cfg, outWriter, runners).Execute(); err != nil {
		t.Fatalf("expected no error once the violation is baselined, got: %v", err)
	}
}

func TestExtractPathAndStrip(t *testing.T) {
	f := &pb.File{Path: "/tmp/foo.php"}
	files := []*pb.File{f}
	msg := "Lines of code too low in file /tmp/foo.php: got 0 (min: 1)"
	p := extractPath(msg, files)
	if p != f.Path {
		t.Fatalf("extractPath failed, got %q", p)
	}
	stripped := stripPathPrefix(msg, f.Path)
	if stripped == msg {
		t.Fatalf("stripPathPrefix did not strip anything")
	}
}
