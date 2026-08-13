package dependency

import (
	"reflect"
	"testing"
)

func TestIndexKeepsEveryFileUnderAKey(t *testing.T) {
	index := NewIndex()
	// Insertion order is the order the parsers happen to finish in, and the
	// index must not let it show.
	index.Add("pkg", "/project/zulu.go")
	index.Add("pkg", "/project/alpha.go")
	index.Add("pkg", "/project/zulu.go")

	want := []string{"/project/alpha.go", "/project/zulu.go"}
	if got := index.Get("pkg"); !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestIndexIgnoresEmptyKeysAndPaths(t *testing.T) {
	index := NewIndex()
	index.Add("", "/project/a.go")
	index.Add("pkg", "")

	if index.Has("") || index.Has("pkg") {
		t.Fatal("expected empty keys and paths to be ignored")
	}
	if got := index.Get(""); got != nil {
		t.Fatalf("expected no result for an empty key, got %v", got)
	}
}

func TestIndexUnambiguousLookupRefusesToChoose(t *testing.T) {
	index := NewIndex()
	index.Add("one", "/project/only.go")
	index.Add("many", "/project/a.go")
	index.Add("many", "/project/b.go")

	if got := index.GetUnambiguous("one"); !reflect.DeepEqual(got, []string{"/project/only.go"}) {
		t.Fatalf("expected the single candidate, got %v", got)
	}
	if got := index.GetUnambiguous("many"); got != nil {
		t.Fatalf("expected no answer when several files match, got %v", got)
	}
	if got := index.GetUnambiguous("none"); got != nil {
		t.Fatalf("expected no answer for an unknown key, got %v", got)
	}
}
