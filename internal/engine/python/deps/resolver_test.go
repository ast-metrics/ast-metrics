package deps

import (
	"reflect"
	"testing"
)

func TestRebaseCountsTheLeadingDots(t *testing.T) {
	resolver := &scopedFileDependencyResolver{moduleOfFile: map[string]string{
		"/project/pkg/sub/store.py":    "pkg.sub.store",
		"/project/pkg/sub/__init__.py": "pkg.sub",
	}}

	tests := []struct {
		name       string
		sourcePath string
		specifier  string
		want       string
		understood bool
	}{
		{
			name:       "an absolute specifier is already a module path",
			sourcePath: "/project/pkg/sub/store.py",
			specifier:  "pkg.model",
			want:       "pkg.model",
			understood: true,
		},
		{
			// One dot is the package holding the file, not its parent.
			name:       "one dot stays in the package",
			sourcePath: "/project/pkg/sub/store.py",
			specifier:  ".model",
			want:       "pkg.sub.model",
			understood: true,
		},
		{
			name:       "two dots climb to the parent package",
			sourcePath: "/project/pkg/sub/store.py",
			specifier:  "..model",
			want:       "pkg.model",
			understood: true,
		},
		{
			name:       "a bare dot is the package itself",
			sourcePath: "/project/pkg/sub/store.py",
			specifier:  ".",
			want:       "pkg.sub",
			understood: true,
		},
		{
			// An __init__.py is its package, so it does not shed a segment
			// before climbing.
			name:       "relative import from a package initializer",
			sourcePath: "/project/pkg/sub/__init__.py",
			specifier:  ".store",
			want:       "pkg.sub.store",
			understood: true,
		},
		{
			name:       "climbing past the top of the tree",
			sourcePath: "/project/pkg/sub/store.py",
			specifier:  "....model",
			understood: false,
		},
		{
			name:       "a relative import from an unknown file",
			sourcePath: "/project/elsewhere.py",
			specifier:  ".model",
			understood: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, understood := resolver.rebase(test.sourcePath, test.specifier)
			if understood != test.understood {
				t.Fatalf("expected understood=%v, got %v", test.understood, understood)
			}
			if understood && got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestDottedPathSuffixes(t *testing.T) {
	want := []string{"project.pkg.sub.store", "pkg.sub.store", "sub.store"}
	if got := dottedPathSuffixes("/project/pkg/sub/store.py"); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	// An __init__.py is the package holding it, not a module of its own.
	want = []string{"project.pkg.sub", "pkg.sub"}
	if got := dottedPathSuffixes("/project/pkg/sub/__init__.py"); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	// The last segment alone is never offered: "store" would match every
	// module of that name in the project.
	if got := dottedPathSuffixes("/store.py"); len(got) != 0 {
		t.Fatalf("expected no suffix for a single segment, got %v", got)
	}
}
