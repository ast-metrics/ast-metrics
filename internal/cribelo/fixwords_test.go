package cribelo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsFixMessageLanguages(t *testing.T) {
	yes := []string{
		"fix: null pointer", "Fixes #12", "Revert \"add cache\"", "hotfix prod", "Regression in parser",
		"Correction du calcul de TVA", "corrige le bug d'affichage", "Corrigé: pagination", "réparation de l'export",
		"Répare la connexion SSO", "régression sur le login", "Arreglar el formulario", "Correção do cálculo",
		"conserta importação", "Fehler behoben im Export",
		// conventional commits and gitmoji
		"fix(parser): off by one", "fix!: drop legacy flag", "revert: \"feat: cache\"",
		"🐛 handle nil map", "🚑️ prod down", "🩹 typo in error message", "⏪ import pipeline",
		":bug: handle nil map", ":ambulance: prod down", ":adhesive_bandage: typo", ":rewind: import pipeline",
	}
	no := []string{
		"add fixture for users", "prefix all routes", "bugle sound", "Refactor repository layer",
		"reparse the config on reload", "Corrigan street data", "regressions dataset import",
		"feat: add parser", "✨ add parser", "🔥 remove dead code", "♻️ refactor repository layer",
	}
	for _, s := range yes {
		assert.True(t, IsFixMessage(s), s)
	}
	for _, s := range no {
		assert.False(t, IsFixMessage(s), s)
	}
}

func TestSetFixPattern(t *testing.T) {
	assert.Error(t, SetFixPattern("("))
	assert.NoError(t, SetFixPattern(`BUG-\d+`))
	defer SetFixPattern("")
	assert.True(t, IsFixMessage("BUG-1234 handle timeout"))
	assert.False(t, IsFixMessage("FEAT-1234 add timeout"))
}
