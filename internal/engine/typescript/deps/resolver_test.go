package deps

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

func fileWithModuleDependency(path, specifier, className string) (*pb.File, *pb.StmtExternalDependency) {
	dep := &pb.StmtExternalDependency{
		Namespace: specifier,
		ClassName: className,
		From:      "source",
	}
	return &pb.File{
		Path:                path,
		ProgrammingLanguage: "TypeScript",
		Stmts: &pb.Stmts{StmtNamespace: []*pb.StmtNamespace{{
			Stmts: &pb.Stmts{StmtExternalDependencies: []*pb.StmtExternalDependency{dep}},
		}}},
	}, dep
}

func typeScriptFile(path string) *pb.File {
	return &pb.File{Path: path, ProgrammingLanguage: "TypeScript", Stmts: &pb.Stmts{}}
}

func resolveTypeScriptDependency(files []*pb.File, source *pb.File, dep *pb.StmtExternalDependency) (string, bool) {
	// A TypeScript specifier names one module, so the tests below read the
	// single target the resolver is expected to produce.
	targets, handled := NewFileDependencyResolver().ForFiles(files).Resolve(source, dep)
	if len(targets) == 0 {
		return "", handled
	}
	return targets[0], handled
}

func TestFileDependencyResolverResolvesRelativeModules(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath string
		specifier  string
		targetPath string
	}{
		{
			name:       "extensionless sibling",
			sourcePath: "/tmp/project/Foo.ts",
			specifier:  "./Bar",
			targetPath: "/tmp/project/Bar.ts",
		},
		{
			name:       "directory index",
			sourcePath: "/tmp/project/Foo.ts",
			specifier:  "./bar",
			targetPath: "/tmp/project/bar/index.ts",
		},
		{
			name:       "parent directory",
			sourcePath: "/tmp/project/feature/Foo.ts",
			specifier:  "../Bar",
			targetPath: "/tmp/project/Bar.ts",
		},
		{
			name:       "explicit vue extension",
			sourcePath: "/tmp/project/Modal.ts",
			specifier:  "./steps/CelebrationStep.vue",
			targetPath: "/tmp/project/steps/CelebrationStep.vue",
		},
		{
			name:       "modern mts extension",
			sourcePath: "/tmp/project/Foo.ts",
			specifier:  "./Bar",
			targetPath: "/tmp/project/Bar.mts",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, dep := fileWithModuleDependency(test.sourcePath, test.specifier, "Bar")
			target := typeScriptFile(test.targetPath)

			got, handled := resolveTypeScriptDependency([]*pb.File{source, target}, source, dep)
			if !handled || got != target.Path {
				t.Fatalf("expected %q to resolve to %q, got %q (handled=%t)", test.specifier, target.Path, got, handled)
			}
		})
	}
}

func TestFileDependencyResolverUsesDeterministicPrecedence(t *testing.T) {
	source, dep := fileWithModuleDependency("/tmp/project/Foo.ts", "./Bar", "Bar")
	targetTS := typeScriptFile("/tmp/project/Bar.ts")
	targetTSX := typeScriptFile("/tmp/project/Bar.tsx")

	for _, files := range [][]*pb.File{
		{source, targetTS, targetTSX},
		{source, targetTSX, targetTS},
	} {
		got, handled := resolveTypeScriptDependency(files, source, dep)
		if !handled || got != targetTS.Path {
			t.Fatalf("expected stable .ts precedence, got %q (handled=%t)", got, handled)
		}
	}
}

func TestFileDependencyResolverUsesDeterministicExactPath(t *testing.T) {
	source, dep := fileWithModuleDependency("/tmp/project/Foo.ts", "./Bar.ts", "Bar")
	canonical := typeScriptFile("/tmp/project/Bar.ts")
	alias := typeScriptFile("/tmp/project/sub/../Bar.ts")

	for _, files := range [][]*pb.File{
		{source, canonical, alias},
		{source, alias, canonical},
	} {
		got, handled := resolveTypeScriptDependency(files, source, dep)
		if !handled || got != canonical.Path {
			t.Fatalf("expected stable exact path %q, got %q (handled=%t)", canonical.Path, got, handled)
		}
	}
}

func TestFileDependencyResolverPrefersExplicitExtension(t *testing.T) {
	source, dep := fileWithModuleDependency("/tmp/project/Foo.ts", "./Bar.tsx", "Bar")
	targetTS := typeScriptFile("/tmp/project/Bar.ts")
	targetTSX := typeScriptFile("/tmp/project/Bar.tsx")

	got, handled := resolveTypeScriptDependency([]*pb.File{source, targetTS, targetTSX}, source, dep)
	if !handled || got != targetTSX.Path {
		t.Fatalf("expected explicit .tsx target, got %q (handled=%t)", got, handled)
	}
}

func TestFileDependencyResolverOnlySubstitutesJavaScriptExtensions(t *testing.T) {
	tests := []struct {
		name      string
		specifier string
		target    string
		want      string
	}{
		{name: "emitted js path", specifier: "./Bar.js", target: "/tmp/project/Bar.ts", want: "/tmp/project/Bar.ts"},
		{name: "emitted mjs path", specifier: "./Bar.mjs", target: "/tmp/project/Bar.mts", want: "/tmp/project/Bar.mts"},
		{name: "missing vue file", specifier: "./Bar.vue", target: "/tmp/project/Bar.ts"},
		{name: "missing ts file", specifier: "./Bar.ts", target: "/tmp/project/Bar.tsx"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, dep := fileWithModuleDependency("/tmp/project/Foo.ts", test.specifier, "Bar")
			target := typeScriptFile(test.target)
			got, handled := resolveTypeScriptDependency([]*pb.File{source, target}, source, dep)
			if !handled || got != test.want {
				t.Fatalf("expected target %q, got %q (handled=%t)", test.want, got, handled)
			}
		})
	}
}

func TestFileDependencyResolverClaimsUnresolvedModules(t *testing.T) {
	tests := []struct {
		name      string
		specifier string
	}{
		{name: "external package", specifier: "react"},
		{name: "missing relative module", specifier: "./missing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, dep := fileWithModuleDependency("/tmp/project/Foo.ts", test.specifier, "React")
			got, handled := resolveTypeScriptDependency([]*pb.File{source}, source, dep)
			if !handled || got != "" {
				t.Fatalf("expected an unresolved handled module, got %q (handled=%t)", got, handled)
			}
		})
	}
}

func TestFileDependencyResolverIgnoresSymbolDependencies(t *testing.T) {
	source := typeScriptFile("/tmp/project/Foo.ts")
	dep := &pb.StmtExternalDependency{ClassName: "Bar", Namespace: "pkg"}
	target := typeScriptFile("/tmp/project/Bar.ts")

	got, handled := resolveTypeScriptDependency([]*pb.File{source, target}, source, dep)
	if handled || got != "" {
		t.Fatalf("expected symbol dependency to fall through, got %q (handled=%t)", got, handled)
	}
}
