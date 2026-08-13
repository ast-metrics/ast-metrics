package deps

import "testing"

func TestModuleOf(t *testing.T) {
	const sourceRoot = "/project/src"

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "library root", path: "/project/src/lib.rs", want: ""},
		{name: "binary root", path: "/project/src/main.rs", want: ""},
		{name: "module written as a file", path: "/project/src/model.rs", want: "model"},
		{name: "module written as a directory", path: "/project/src/store/mod.rs", want: "store"},
		{name: "nested module", path: "/project/src/store/inner.rs", want: "store::inner"},
		{name: "nested directory module", path: "/project/src/a/b/mod.rs", want: "a::b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := moduleOf(sourceRoot, test.path); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestPackageNameIn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "plain package",
			content: "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n",
			want:    "demo",
		},
		{
			// Cargo lets a crate be named with dashes and referred to in code
			// with underscores, which is the form a use path carries.
			name:    "dashes become underscores",
			content: "[package]\nname = \"demo-core\"\n",
			want:    "demo_core",
		},
		{
			name:    "a name outside the package section is not the crate name",
			content: "[dependencies]\nname = \"serde\"\n\n[package]\nname = \"demo\"\n",
			want:    "demo",
		},
		{
			name:    "a virtual workspace manifest declares no package",
			content: "[workspace]\nmembers = [\"core\", \"app\"]\n",
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := packageNameIn(test.content); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestRebaseAnchorsAPath(t *testing.T) {
	resolver := &scopedFileDependencyResolver{
		crateNames: map[string]string{"demo_core": "/other"},
	}
	from := crateModule{crate: "/project", module: "a::b"}

	tests := []struct {
		name       string
		path       string
		wantCrate  string
		wantJoined string
		understood bool
	}{
		{name: "crate root", path: "crate::model::User", wantCrate: "/project", wantJoined: "model::User", understood: true},
		{name: "self", path: "self::inner::Thing", wantCrate: "/project", wantJoined: "a::b::inner::Thing", understood: true},
		{name: "super", path: "super::Store", wantCrate: "/project", wantJoined: "a::Store", understood: true},
		{name: "chained super", path: "super::super::Top", wantCrate: "/project", wantJoined: "Top", understood: true},
		{name: "another crate of the workspace", path: "demo_core::Config", wantCrate: "/other", wantJoined: "Config", understood: true},
		{name: "read from the crate root otherwise", path: "model::User", wantCrate: "/project", wantJoined: "model::User", understood: true},
		{name: "climbing above the crate root", path: "super::super::super::Nope", understood: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			crate, segments, understood := resolver.rebase(from, test.path)
			if understood != test.understood {
				t.Fatalf("expected understood=%v, got %v", test.understood, understood)
			}
			if !understood {
				return
			}
			if crate != test.wantCrate {
				t.Fatalf("expected crate %q, got %q", test.wantCrate, crate)
			}
			if joined := joinPath(segments); joined != test.wantJoined {
				t.Fatalf("expected path %q, got %q", test.wantJoined, joined)
			}
		})
	}
}

func joinPath(segments []string) string {
	joined := ""
	for i, segment := range segments {
		if i > 0 {
			joined += "::"
		}
		joined += segment
	}
	return joined
}
