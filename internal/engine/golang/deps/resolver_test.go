package deps

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

func TestPathSuffixesStopsBeforeTheLastSegment(t *testing.T) {
	want := []string{"example.com/demo/internal/model", "demo/internal/model", "internal/model"}
	if got := pathSuffixes("example.com/demo/internal/model"); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// A one-segment import path is a standard library package: offering
	// "fmt" as a suffix would let it match any directory called fmt.
	if got := pathSuffixes("fmt"); len(got) != 0 {
		t.Fatalf("expected no suffix for a single segment, got %v", got)
	}
}

func TestFileDependencyResolverTellsALibraryFromTheModuleItself(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "internal", "billing", "service.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	source := &pb.File{Path: path, ProgrammingLanguage: Language, Stmts: &pb.Stmts{}}
	teller := NewFileDependencyResolver().ForFiles([]*pb.File{source}).(dependency.LibraryTeller)
	for importPath, library := range map[string]bool{
		"fmt":                               true,
		"github.com/sirupsen/logrus":        true,
		"example.com/demonstration/x":       true,
		"example.com/demo":                  false,
		"example.com/demo/internal/leftout": false,
	} {
		if got := teller.IsLibrary(source, importPath); got != library {
			t.Errorf("IsLibrary(%q) = %v, expected %v", importPath, got, library)
		}
	}
}
