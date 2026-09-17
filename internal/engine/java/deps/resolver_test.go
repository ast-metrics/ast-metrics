package deps

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
)

func TestFileDependencyResolverTellsTheStandardLibrary(t *testing.T) {
	teller := NewFileDependencyResolver().ForFiles(nil).(dependency.StandardLibraryTeller)
	for module, standard := range map[string]bool{"java.util": true, "javax.persistence": true, "jdk.internal": true, "javafx.scene": false, "org.apache.logging.log4j": false} {
		if got := teller.IsStandardLibrary(module); got != standard {
			t.Errorf("IsStandardLibrary(%q) = %v, expected %v", module, got, standard)
		}
	}
}
