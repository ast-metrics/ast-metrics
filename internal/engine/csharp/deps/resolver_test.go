package deps

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
)

func TestFileDependencyResolverTellsTheStandardLibrary(t *testing.T) {
	teller := NewFileDependencyResolver().ForFiles(nil).(dependency.StandardLibraryTeller)
	for module, standard := range map[string]bool{"System": true, "System.IO": true, "SystemX": false, "Serilog": false, "Microsoft.Extensions.Logging": false} {
		if got := teller.IsStandardLibrary(module); got != standard {
			t.Errorf("IsStandardLibrary(%q) = %v, expected %v", module, got, standard)
		}
	}
}
