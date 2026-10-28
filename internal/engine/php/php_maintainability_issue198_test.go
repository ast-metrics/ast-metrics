package php

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
)

func TestMaintainabilityIndexIsUndefinedForDeclarationOnlyDto(t *testing.T) {
	const source = `<?php
final class FooDto
{
    public function __construct(
        public ?string $bar = null
    ) {
    }
}
`

	file, err := engine.CreateTestFileWithCode(&PhpRunner{}, source)
	if err != nil {
		t.Fatalf("parse synthetic DTO: %v", err)
	}
	analyzer.AnalyzeFile(file)
	if len(file.Stmts.StmtClass) != 1 {
		t.Fatalf("expected one parsed class, got %d", len(file.Stmts.StmtClass))
	}

	class := file.Stmts.StmtClass[0]
	if class.Stmts == nil || class.Stmts.Analyze == nil {
		t.Fatal("expected class analysis to be populated")
	}
	volume := class.Stmts.Analyze.Volume
	if volume == nil || volume.Lloc == nil || *volume.Lloc != 0 {
		t.Fatalf("expected DTO to have zero logical LOC, got volume %+v", volume)
	}
	if volume.HalsteadVolume == nil || *volume.HalsteadVolume <= 0 {
		t.Fatalf("expected promoted property to produce positive Halstead volume, got %+v", volume.HalsteadVolume)
	}
	if metrics := class.Stmts.Analyze.Maintainability; metrics != nil && metrics.MaintainabilityIndex != nil {
		t.Fatalf("declaration-only DTO has no executable logical lines; want undefined maintainability index, got %g", *metrics.MaintainabilityIndex)
	}
}
