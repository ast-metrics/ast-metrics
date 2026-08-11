package command

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/storage"
)

func TestBaselineCommand_Execute_RequiresRequirements(t *testing.T) {
	cfg := configuration.NewConfiguration()
	cfg.Storage = storage.Default()

	cmd := NewBaselineCommand(cfg, bufio.NewWriter(os.Stdout), []engine.Engine{&php.PhpRunner{}})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected an error when no requirements are configured")
	}
}

func TestBaselineCommand_Execute_WritesEveryCurrentViolation(t *testing.T) {
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
function bar() {
	echo "Hello, World!";
}
`
	file, err := os.CreateTemp("", "baseline_test_*.php")
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

	baselinePath := filepath.Join(t.TempDir(), "baseline.yaml")
	cmd := NewBaselineCommand(cfg, bufio.NewWriter(os.Stdout), []engine.Engine{&php.PhpRunner{}})
	cmd.Path = baselinePath
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline file was not written: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("baseline file is empty")
	}
}
