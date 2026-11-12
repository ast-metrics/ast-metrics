package cribelo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"google.golang.org/protobuf/proto"
)

// A plan parses the whole tree, because hubs, cycles and clones are
// properties of the project, and nearly all of that tree is the same from
// one run to the next. The parsed form of a file is therefore kept under
// the git directory, keyed by what it was parsed from: its content, its
// path, the manifests that name its package, and the build of cribelo. A
// second run, the next push of a pull request, another pull request of the
// same repository parse what changed and read the rest.
//
// Nothing is kept outside a git repository, and CRIBELO_CACHE=off keeps
// nothing at all.

// parseCacheDays is how long an entry nobody wrote again is kept.
const parseCacheDays = 45

var (
	parseCacheOn = os.Getenv("CRIBELO_CACHE") != "off"
	buildOnce    sync.Once
	buildKey     string
)

// SetParseCache turns the cache of parsed files on or off.
func SetParseCache(on bool) { parseCacheOn = on }

// buildIdentity tells one build of cribelo from another: a parser that
// changed must not read what the previous one wrote. A released binary is
// named by the version of its engine; a local build by its own file.
func buildIdentity() string {
	buildOnce.Do(func() {
		const engine = "github.com/ast-metrics/ast-metrics"
		if info, ok := debug.ReadBuildInfo(); ok {
			candidates := append([]*debug.Module{&info.Main}, info.Deps...)
			for _, m := range candidates {
				if m.Path != engine {
					continue
				}
				if m.Replace != nil {
					break
				}
				if v := m.Version; v != "" && v != "(devel)" && !strings.Contains(v, "+dirty") {
					buildKey = v
					return
				}
			}
		}
		if exe, err := os.Executable(); err == nil {
			if stat, err := os.Stat(exe); err == nil {
				buildKey = fmt.Sprintf("local-%d-%d", stat.Size(), stat.ModTime().UnixNano())
				return
			}
		}
		buildKey = ""
	})
	return buildKey
}

// parseCache is the store of one repository for one build.
type parseCache struct {
	dir     string
	context []byte
}

// openParseCache finds the store of the repository a tree belongs to, nil
// when there is none to use. The context is the content of every manifest
// of the tree: a Go file is named after the module its go.mod declares.
func openParseCache(root string, relPaths []string) *parseCache {
	if !parseCacheOn || buildIdentity() == "" {
		return nil
	}
	common, err := runGit(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil
	}
	base := filepath.Join(strings.TrimSpace(common), "cribelo", "parsed")
	id := sha256.Sum256([]byte(buildIdentity()))
	dir := filepath.Join(base, hex.EncodeToString(id[:6]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	h := sha256.New()
	manifests := []string{}
	for _, rel := range relPaths {
		if isManifest(rel) || strings.HasPrefix(path.Base(rel), "tsconfig") {
			manifests = append(manifests, rel)
		}
	}
	sort.Strings(manifests)
	for _, rel := range manifests {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(data))
		h.Write(data)
	}
	c := &parseCache{dir: dir, context: h.Sum(nil)}
	c.sweep(base)
	return c
}

// sweep removes, once a day, the stores of other builds and the entries
// nobody wrote for a long time.
func (c *parseCache) sweep(base string) {
	marker := filepath.Join(base, "swept")
	if stat, err := os.Stat(marker); err == nil && time.Since(stat.ModTime()) < 24*time.Hour {
		return
	}
	os.WriteFile(marker, nil, 0o644)
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && filepath.Join(base, e.Name()) != c.dir {
			os.RemoveAll(filepath.Join(base, e.Name()))
		}
	}
	limit := time.Now().Add(-parseCacheDays * 24 * time.Hour)
	filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(limit) {
			os.Remove(p)
		}
		return nil
	})
}

func (c *parseCache) entry(rel string, content []byte) string {
	h := sha256.New()
	h.Write(c.context)
	fmt.Fprintf(h, "\x00%s\x00", rel)
	h.Write(content)
	sum := hex.EncodeToString(h.Sum(nil))
	return filepath.Join(c.dir, sum[:2], sum[2:40])
}

// parseTree parses the files of a working tree, reading from the cache the
// ones it already knows. The result is the same with and without it: the
// files come back in the order of their paths.
func parseTree(root string, relPaths []string) (*Parsed, error) {
	var analyzed []string
	for _, rel := range relPaths {
		if analyzedExtensions[strings.ToLower(filepath.Ext(rel))] {
			analyzed = append(analyzed, rel)
		}
	}
	sort.Strings(analyzed)
	cache := openParseCache(root, relPaths)
	if cache == nil {
		return parseFiles(root, analyzed)
	}

	type slot struct {
		rel  string
		key  string
		file *pb.File
	}
	slots := make([]slot, len(analyzed))
	var wg sync.WaitGroup
	jobs := make(chan int, 64)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rel := slots[i].rel
				content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if err != nil {
					continue
				}
				slots[i].key = cache.entry(rel, content)
				data, err := os.ReadFile(slots[i].key)
				if err != nil {
					continue
				}
				file := &pb.File{}
				if proto.Unmarshal(data, file) == nil {
					file.Path = filepath.Join(root, filepath.FromSlash(rel))
					slots[i].file = file
				}
			}
		}()
	}
	for i, rel := range analyzed {
		slots[i].rel = rel
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	var missing []string
	index := map[string]int{}
	for i, s := range slots {
		index[s.rel] = i
		if s.file == nil {
			missing = append(missing, s.rel)
		}
	}
	if len(missing) > 0 {
		fresh, err := parseFiles(root, missing)
		if err != nil {
			return nil, err
		}
		for rel, file := range fresh.Files {
			i, ok := index[rel]
			if !ok {
				continue
			}
			slots[i].file = file
			if slots[i].key == "" {
				continue
			}
			if data, err := proto.Marshal(file); err == nil {
				writeAtomic(slots[i].key, data)
			}
		}
	}
	parsed := &Parsed{Root: root, Files: make(map[string]*pb.File, len(slots))}
	for _, s := range slots {
		if s.file != nil {
			parsed.Files[s.rel] = s.file
			parsed.All = append(parsed.All, s.file)
		}
	}
	return parsed, nil
}

// writeAtomic writes a cache entry so that a reader never sees half of it.
func writeAtomic(target string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "tmp-")
	if err != nil {
		return
	}
	_, err = tmp.Write(data)
	tmp.Close()
	if err != nil || os.Rename(tmp.Name(), target) != nil {
		os.Remove(tmp.Name())
	}
}

// manifestNames are the files that declare what a project depends on and
// how it is built: a change in one of them can change what a file resolves
// to, so the cache is keyed on them.
var manifestNames = map[string]bool{
	"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "pnpm-workspace.yaml": true,
	"Cargo.toml": true, "Cargo.lock": true,
	"composer.json": true, "composer.lock": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "settings.gradle": true, "settings.gradle.kts": true, "gradle.properties": true,
	"Directory.Packages.props": true, "Directory.Build.props": true, "nuget.config": true,
	"pyproject.toml": true, "setup.py": true, "setup.cfg": true, "poetry.lock": true, "uv.lock": true, "Pipfile": true, "Pipfile.lock": true,
}

func isManifest(rel string) bool {
	base := path.Base(rel)
	if manifestNames[base] {
		return true
	}
	switch strings.ToLower(path.Ext(base)) {
	case ".csproj", ".sln", ".fsproj", ".vbproj":
		return true
	}
	return strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")
}
