package engine

import "testing"

func TestNamespaceReducerKeepsTheDefaultDepthWhenTheRootIsShallow(t *testing.T) {
	owned := []string{
		`App\Service\Mailer`,
		`App\Http\Router`,
		`App\Http\Middleware\Auth`,
	}
	reducer := NewNamespaceReducer(owned, DefaultNamespaceDepth)

	for _, test := range []struct{ namespace, expected string }{
		{`App\Service\Mailer`, `App\Service\Mailer`},
		{`App\Http\Middleware\Auth`, `App\Http\Middleware`},
		{`Symfony\Component\HttpFoundation\Request`, `Symfony\Component\HttpFoundation`},
	} {
		if got := reducer.Reduce(test.namespace); got != test.expected {
			t.Errorf("Reduce(%q) = %q, expected %q", test.namespace, got, test.expected)
		}
	}
}

// The case of https://github.com/ast-metrics/ast-metrics/issues/140: a root as
// deep as the depth used to leave every class on the same node.
func TestNamespaceReducerSeesPastARootAsDeepAsTheDepth(t *testing.T) {
	owned := []string{
		`Company\Project\SubProject\Artifact\Importer`,
		`Company\Project\SubProject\Clearing\Settlement`,
		`Company\Project\SubProject\Shared\Clock`,
	}
	reducer := NewNamespaceReducer(owned, DefaultNamespaceDepth)

	for _, test := range []struct{ namespace, expected string }{
		{`Company\Project\SubProject\Artifact\Importer`, `Company\Project\SubProject\Artifact`},
		{`Company\Project\SubProject\Clearing\Settlement`, `Company\Project\SubProject\Clearing`},
		// A module deeper than the others is cut at the same level, so that a
		// module is one node and not one per class.
		{`Company\Project\SubProject\Shared\Time\Clock`, `Company\Project\SubProject\Shared`},
		// Outside the project, the root means nothing and the plain depth applies.
		{`Symfony\Component\HttpFoundation\Request`, `Symfony\Component\HttpFoundation`},
	} {
		if got := reducer.Reduce(test.namespace); got != test.expected {
			t.Errorf("Reduce(%q) = %q, expected %q", test.namespace, got, test.expected)
		}
	}
}

func TestNamespaceReducerFallsBackToTheDepthWhenNoRootIsShared(t *testing.T) {
	owned := []string{
		`Company\Project\SubProject\Artifact\Importer`,
		`Company\Project\SubProject\Clearing\Settlement`,
		// One name at the root of the project is enough to share nothing.
		`helper_function`,
	}
	reducer := NewNamespaceReducer(owned, DefaultNamespaceDepth)

	namespace := `Company\Project\SubProject\Artifact\Importer`
	expected := `Company\Project\SubProject`
	if got := reducer.Reduce(namespace); got != expected {
		t.Errorf("Reduce(%q) = %q, expected %q", namespace, got, expected)
	}
}

// An import path names a package, and a package is what the graph draws: it is
// left whole, whether it belongs to the project or to a third party. Cutting it
// would put every package of a repository on its module path.
func TestNamespaceReducerLeavesImportPathsWhole(t *testing.T) {
	owned := []string{
		"github.com/owner/repo/internal/analyzer",
		"github.com/owner/repo/internal/engine",
	}
	reducer := NewNamespaceReducer(owned, DefaultNamespaceDepth)

	for _, namespace := range []string{
		"github.com/owner/repo/internal/analyzer",
		"github.com/owner/repo/internal/engine/php",
		"github.com/charmbracelet/bubbletea",
		"golang.org/x/tools/go/packages",
	} {
		if got := reducer.Reduce(namespace); got != namespace {
			t.Errorf("Reduce(%q) = %q, expected it left whole", namespace, got)
		}
	}

	// A package of the standard library is named by no host, and is cut like
	// any other namespace.
	if got := reducer.Reduce("net/http/httptest"); got != "net/http/httptest" {
		t.Errorf("Reduce(%q) = %q", "net/http/httptest", got)
	}
}

// A relative directory is not a host, or file paths would stop being cut.
func TestNamespaceReducerCutsRelativePaths(t *testing.T) {
	reducer := NewNamespaceReducer(nil, 2)

	for _, test := range []struct{ namespace, expected string }{
		{"./src/domain/model", "src"},
		{"../src/domain/model", "src"},
		{"/home/user/project/src", "home"},
	} {
		if got := reducer.Reduce(test.namespace); got != test.expected {
			t.Errorf("Reduce(%q) = %q, expected %q", test.namespace, got, test.expected)
		}
	}
}

func TestNamespaceReducerWithoutNamespaces(t *testing.T) {
	for name, reducer := range map[string]*NamespaceReducer{
		"nil":   nil,
		"empty": NewNamespaceReducer(nil, DefaultNamespaceDepth),
		"blank": NewNamespaceReducer([]string{"", ""}, DefaultNamespaceDepth),
	} {
		namespace := `Company\Project\SubProject\Artifact\Importer`
		expected := `Company\Project\SubProject`
		if got := reducer.Reduce(namespace); got != expected {
			t.Errorf("%s reducer: Reduce(%q) = %q, expected %q", name, namespace, got, expected)
		}
	}
}

func TestNamespaceReducerWithASingleNamespace(t *testing.T) {
	// The project shares its whole namespace with itself. Nothing can be split
	// apart, and the reduction must at least stay stable.
	reducer := NewNamespaceReducer([]string{`App\Service\Mailer`}, DefaultNamespaceDepth)

	namespace := `App\Service\Mailer`
	if got := reducer.Reduce(namespace); got != namespace {
		t.Errorf("Reduce(%q) = %q, expected %q", namespace, got, namespace)
	}
}

func TestCommonDepthOfNamespaces(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []string
		expected   int
	}{
		{"nothing shared", []string{`App\Service`, `Domain\Model`}, 0},
		{"one level", []string{`App\Service`, `App\Http`}, 1},
		{"three levels", []string{`A\B\C\D`, `A\B\C\E`}, 3},
		{"one namespace is the root of the others", []string{`A\B`, `A\B\C`}, 2},
		{"the same namespace twice", []string{`A\B`, `A\B`}, 2},
		{"a single namespace", []string{`A\B\C`}, 3},
		// github.com/owner/repo is three path segments but two levels: the host
		// counts as one, here as in ReduceDepthOfNamespace.
		{"the host of a go import path counts as one", []string{
			"github.com/owner/repo/a.T",
			"github.com/owner/repo/b.T",
		}, 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commonDepthOfNamespaces(test.namespaces); got != test.expected {
				t.Errorf("commonDepthOfNamespaces(%q) = %d, expected %d", test.namespaces, got, test.expected)
			}
		})
	}
}
