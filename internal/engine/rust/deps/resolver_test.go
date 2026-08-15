package deps

import "testing"

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
