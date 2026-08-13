package report

import (
	"encoding/json"
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

func TestPruneFile_KeepsClassesAndOutsideFunctions(t *testing.T) {
	method := &pb.StmtFunction{
		Name:  &pb.Name{Short: "M", Qualified: "analyzer\\CommunityAggregator::M"},
		Stmts: &pb.Stmts{Analyze: &pb.Analyze{Complexity: &pb.Complexity{}}},
	}
	outside := &pb.StmtFunction{
		Name:  &pb.Name{Short: "density", Qualified: "analyzer\\density"},
		Stmts: &pb.Stmts{Analyze: &pb.Analyze{Complexity: &pb.Complexity{}}},
	}

	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtClass: []*pb.StmtClass{
							{
								Name: &pb.Name{Short: "CommunityAggregator", Qualified: "analyzer\\CommunityAggregator"},
								Stmts: &pb.Stmts{
									StmtFunction: []*pb.StmtFunction{method},
								},
							},
						},
						StmtFunction: []*pb.StmtFunction{method, outside},
					},
				},
			},
		},
	}

	pruneFile(file)

	if file.Stmts == nil {
		t.Fatalf("expected file stmts")
	}
	if file.Stmts.StmtNamespace != nil {
		t.Fatalf("expected namespace to be pruned")
	}

	if len(file.Stmts.StmtClass) != 1 {
		t.Fatalf("expected 1 class/struct, got %d", len(file.Stmts.StmtClass))
	}
	if len(file.Stmts.StmtClass[0].Stmts.StmtFunction) != 1 {
		t.Fatalf("expected 1 method in class/struct, got %d", len(file.Stmts.StmtClass[0].Stmts.StmtFunction))
	}

	if len(file.Stmts.StmtFunction) != 1 {
		t.Fatalf("expected 1 outside function, got %d", len(file.Stmts.StmtFunction))
	}
	if file.Stmts.StmtFunction[0].Name == nil || file.Stmts.StmtFunction[0].Name.Qualified != "analyzer\\density" {
		t.Fatalf("unexpected outside function: %+v", file.Stmts.StmtFunction[0].Name)
	}
}

func TestBuildFilesJSONPruned_ContainsOutsideFunctions(t *testing.T) {
	outside := &pb.StmtFunction{
		Name:  &pb.Name{Short: "density", Qualified: "analyzer\\density"},
		Stmts: &pb.Stmts{Analyze: &pb.Analyze{Complexity: &pb.Complexity{}}},
	}
	file := &pb.File{
		Path:                "/tmp/community_aggregator.go",
		ShortPath:           "internal/analyzer/community_aggregator.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Stmts: &pb.Stmts{
						StmtFunction: []*pb.StmtFunction{outside},
					},
				},
			},
		},
	}

	raw := buildFilesJSONPruned([]*pb.File{file}, nil)
	var decoded []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid json, got error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected one file in json, got %d", len(decoded))
	}

	stmtsVal, ok := decoded[0]["stmts"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected stmts object in json")
	}
	functionsVal, ok := stmtsVal["stmtFunction"].([]interface{})
	if !ok {
		t.Fatalf("expected stmtFunction array in json")
	}
	if len(functionsVal) != 1 {
		t.Fatalf("expected 1 outside function in json, got %d", len(functionsVal))
	}

	pathHash, ok := decoded[0]["pathHash"].(string)
	if !ok || pathHash == "" {
		t.Fatalf("expected non-empty pathHash in json, got %#v", decoded[0]["pathHash"])
	}
}

func TestBuildRisksJSON_ValidJSON(t *testing.T) {
	dict := NewStringDictionary()
	risks := map[string][]riskItemForTpl{
		"/tmp/foo.go": {
			{ID: "R1", Title: "High complexity", Severity: 0.8, Details: "cyclomatic > 20"},
		},
		"/tmp/bar.go": {},
	}
	raw := buildRisksJSON(risks, dict)
	var decoded map[string][]riskItemForTpl
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid json, got error: %v\nraw: %s", err, raw)
	}
	// Keys should be hashes, not paths
	for k := range decoded {
		if len(k) != 16 {
			t.Fatalf("expected 16-char hash key, got %q", k)
		}
	}
	// Should have 2 entries
	if len(decoded) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(decoded))
	}
}

func TestBuildRisksJSON_EmptySliceNotNull(t *testing.T) {
	dict := NewStringDictionary()
	risks := map[string][]riskItemForTpl{
		"/tmp/bar.go": {},
	}
	raw := buildRisksJSON(risks, dict)
	// Ensure the empty list produces [] not null
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid json: %s", raw)
	}
	var decoded map[string][]riskItemForTpl
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	for _, v := range decoded {
		if v == nil {
			t.Fatalf("expected empty slice, got nil")
		}
	}
}

func TestBuildNodeToCommunityJSON_ValidJSON(t *testing.T) {
	n2c := map[string]string{
		"App\\Controller": "0",
		"App\\Service":    "1",
	}
	raw := buildNodeToCommunityJSON(n2c)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid json: %s", raw)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded["App\\Controller"] != "0" {
		t.Fatalf("expected community 0 for App\\Controller, got %q", decoded["App\\Controller"])
	}
}

func TestBuildNodeToCommunityJSON_Empty(t *testing.T) {
	raw := buildNodeToCommunityJSON(map[string]string{})
	if raw != "{}" {
		t.Fatalf("expected empty json object, got %s", raw)
	}
}

func TestBuildFileDepsJSON_EmptySlicesNotNull(t *testing.T) {
	dict := NewStringDictionary()
	// File with a class that references itself (self-dep should be skipped)
	file := &pb.File{
		Path:                "/tmp/a.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "Foo", Qualified: "pkg\\Foo"}},
			},
		},
	}
	raw := buildFileDepsJSON([]*pb.File{file}, nil, dict)
	if raw != "{}" {
		t.Fatalf("expected empty json for no deps, got %s", raw)
	}
}

func TestBuildFileDepsJSON_HashedKeys(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/a.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "Foo", Qualified: "pkg\\Foo"}},
			},
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{ClassName: "Bar"},
			},
		},
	}
	fileB := &pb.File{
		Path:                "/tmp/b.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "Bar", Qualified: "pkg\\Bar"}},
			},
		},
	}
	raw := buildFileDepsJSON([]*pb.File{fileA, fileB}, nil, dict)
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid json: %s", raw)
	}
	var decoded map[string]fileDepsEntry
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	for k, v := range decoded {
		if len(k) != 16 {
			t.Fatalf("expected 16-char hash key, got %q", k)
		}
		if v.Efferent == nil || v.Afferent == nil {
			t.Fatalf("expected non-nil slices for key %s", k)
		}
	}
}

func TestBuildFileDepsJSON_ResolvesRelativeTypeScriptImport(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/proj/Foo.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{Namespace: "./Bar", ClassName: "Bar"},
			},
		},
	}
	fileB := &pb.File{
		Path:                "/tmp/proj/Bar.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts:               &pb.Stmts{},
	}
	raw := buildFileDepsJSON([]*pb.File{fileA, fileB}, nil, dict)
	var decoded map[string]fileDepsEntry
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	entry, ok := decoded[dict.Add(fileA.Path)]
	if !ok {
		t.Fatalf("expected an entry for %s, got %v", fileA.Path, decoded)
	}
	if len(entry.Efferent) != 1 || entry.Efferent[0].Path != dict.Add(fileB.Path) {
		t.Fatalf("expected %s to depend on %s, got %+v", fileA.Path, fileB.Path, entry.Efferent)
	}
}

func TestBuildFileDepsJSON_ResolvesRelativeTypeScriptImportThroughIndex(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/proj/Foo.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{Namespace: "./bar", ClassName: "Bar"},
			},
		},
	}
	fileB := &pb.File{
		Path:                "/tmp/proj/bar/index.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts:               &pb.Stmts{},
	}
	raw := buildFileDepsJSON([]*pb.File{fileA, fileB}, nil, dict)
	var decoded map[string]fileDepsEntry
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	entry, ok := decoded[dict.Add(fileA.Path)]
	if !ok {
		t.Fatalf("expected an entry for %s, got %v", fileA.Path, decoded)
	}
	if len(entry.Efferent) != 1 || entry.Efferent[0].Path != dict.Add(fileB.Path) {
		t.Fatalf("expected %s to depend on %s, got %+v", fileA.Path, fileB.Path, entry.Efferent)
	}
}

func TestBuildFileDepsJSON_ResolvesRelativeImportWithExplicitExtension(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/proj/Modal.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{Namespace: "./Steps/CelebrationStep.vue", ClassName: "CelebrationStep"},
			},
		},
	}
	fileB := &pb.File{
		Path:                "/tmp/proj/Steps/CelebrationStep.vue",
		ProgrammingLanguage: "TypeScript",
		Stmts:               &pb.Stmts{},
	}
	raw := buildFileDepsJSON([]*pb.File{fileA, fileB}, nil, dict)
	var decoded map[string]fileDepsEntry
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	entry, ok := decoded[dict.Add(fileA.Path)]
	if !ok {
		t.Fatalf("expected an entry for %s, got %v", fileA.Path, decoded)
	}
	if len(entry.Efferent) != 1 || entry.Efferent[0].Path != dict.Add(fileB.Path) {
		t.Fatalf("expected %s to depend on %s, got %+v", fileA.Path, fileB.Path, entry.Efferent)
	}
}

func TestBuildFileDepsJSON_BareImportSpecifierStaysUnresolved(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/proj/Foo.ts",
		ProgrammingLanguage: "TypeScript",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{Namespace: "react", ClassName: "React"},
			},
		},
	}
	raw := buildFileDepsJSON([]*pb.File{fileA}, nil, dict)
	if raw != "{}" {
		t.Fatalf("expected external bare-specifier import to stay unresolved, got %s", raw)
	}
}

func TestBuildFolderDepsJSON_HashedKeys(t *testing.T) {
	dict := NewStringDictionary()
	fileA := &pb.File{
		Path:                "/tmp/pkgA/a.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "Foo", Qualified: "pkgA\\Foo"}},
			},
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{ClassName: "Bar"},
			},
		},
	}
	fileB := &pb.File{
		Path:                "/tmp/pkgB/b.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{Name: &pb.Name{Short: "Bar", Qualified: "pkgB\\Bar"}},
			},
		},
	}
	raw := buildFolderDepsJSON([]*pb.File{fileA, fileB}, nil, dict)
	if raw == "" {
		t.Fatalf("expected non-empty json for cross-folder deps")
	}
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid json: %s", raw)
	}
	var decoded folderDepsPayload
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	for k, v := range decoded.Folders {
		if len(k) != 16 {
			t.Fatalf("expected 16-char hash key, got %q", k)
		}
		if v.Efferent == nil || v.Afferent == nil {
			t.Fatalf("expected non-nil slices for folder %s", k)
		}
	}
	// Dictionary should contain the original paths
	dictJSON := dict.ToJSON()
	if !json.Valid([]byte(dictJSON)) {
		t.Fatalf("invalid dict json: %s", dictJSON)
	}
}

func TestStringDictionary_AddAndResolve(t *testing.T) {
	dict := NewStringDictionary()
	h1 := dict.Add("/tmp/foo.go")
	h2 := dict.Add("/tmp/bar.go")
	if h1 == h2 {
		t.Fatalf("expected different hashes, got same: %s", h1)
	}
	if len(h1) != 16 {
		t.Fatalf("expected 16-char hash, got %d chars: %s", len(h1), h1)
	}
	// Same input produces same hash
	if dict.Add("/tmp/foo.go") != h1 {
		t.Fatalf("expected stable hash")
	}
	// ToJSON should contain both entries
	raw := dict.ToJSON()
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("invalid dict json: %v", err)
	}
	if m[h1] != "/tmp/foo.go" {
		t.Fatalf("expected /tmp/foo.go, got %s", m[h1])
	}
	if m[h2] != "/tmp/bar.go" {
		t.Fatalf("expected /tmp/bar.go, got %s", m[h2])
	}
}

func TestBuildFilesJSONPruned_PathHashStableAndDistinct(t *testing.T) {
	makeFile := func(path string) *pb.File {
		return &pb.File{
			Path:                path,
			ShortPath:           "internal/analyzer/community_aggregator.go",
			ProgrammingLanguage: "Golang",
			Stmts:               &pb.Stmts{},
		}
	}

	first := makeFile("/tmp/repo-a/internal/analyzer/community_aggregator.go")
	second := makeFile("/tmp/repo-b/internal/analyzer/community_aggregator.go")

	raw := buildFilesJSONPruned([]*pb.File{first, second}, nil)
	var decoded []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("expected valid json, got error: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 files in json, got %d", len(decoded))
	}

	hashA, okA := decoded[0]["pathHash"].(string)
	hashB, okB := decoded[1]["pathHash"].(string)
	if !okA || !okB || hashA == "" || hashB == "" {
		t.Fatalf("expected non-empty pathHash values, got %#v and %#v", decoded[0]["pathHash"], decoded[1]["pathHash"])
	}
	if hashA == hashB {
		t.Fatalf("expected different pathHash for different absolute paths, got same value %q", hashA)
	}

	raw2 := buildFilesJSONPruned([]*pb.File{first, second}, nil)
	var decoded2 []map[string]interface{}
	if err := json.Unmarshal([]byte(raw2), &decoded2); err != nil {
		t.Fatalf("expected valid json on second call, got error: %v", err)
	}
	hashA2, _ := decoded2[0]["pathHash"].(string)
	hashB2, _ := decoded2[1]["pathHash"].(string)
	if hashA != hashA2 || hashB != hashB2 {
		t.Fatalf("expected stable pathHash across calls, got (%q,%q) then (%q,%q)", hashA, hashB, hashA2, hashB2)
	}
}
