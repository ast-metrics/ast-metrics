package deps

import (
	"reflect"
	"testing"
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
