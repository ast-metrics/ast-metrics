package deps

import (
	"reflect"
	"testing"
)

func TestModulePathIn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "plain declaration",
			content: "module example.com/demo\n\ngo 1.21\n",
			want:    "example.com/demo",
		},
		{
			name:    "leading blank lines and comments",
			content: "// a comment\n\n   module   example.com/demo   \n",
			want:    "example.com/demo",
		},
		{
			name:    "trailing comment",
			content: "module example.com/demo // the main module\n",
			want:    "example.com/demo",
		},
		{
			name:    "quoted path",
			content: "module \"example.com/demo\"\n",
			want:    "example.com/demo",
		},
		{
			// The require block also holds lines starting with a module path,
			// but the directive itself comes first.
			name:    "require block does not win",
			content: "module example.com/demo\n\nrequire (\n\tmodule.example/other v1.0.0\n)\n",
			want:    "example.com/demo",
		},
		{
			name:    "no module directive",
			content: "go 1.21\n",
			want:    "",
		},
		{
			// "modules" is not "module": a prefix match alone would take it.
			name:    "a longer word is not the directive",
			content: "modules example.com/demo\n",
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modulePathIn(test.content); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

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
