package report

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
)

// communityFiles is what the folder explorer of the communities page needs
// to redraw the map of a folder: the files the units come from, which
// community each unit sits in, and the references crossing from one
// community to another. It is embedded in the page as JSON.
type communityFiles struct {
	// Dirs are the directories of the files, relative to the root every
	// path shares; Files are [directory index, file name].
	Dirs  []string         `json:"dirs"`
	Files [][2]interface{} `json:"f"`
	// Units maps a community id onto the index of the file of each of its
	// units; a file declaring several units appears as many times.
	Units map[string][]int `json:"c"`
	// Communities carries, per id, what the map needs to draw a box:
	// [short name, size, colour].
	Communities map[string][3]interface{} `json:"n"`
	// Exposed maps a community id onto the file indices of the members the
	// rest of the code reaches; Border onto the ones another resolution of
	// the detection places elsewhere. The members list marks them.
	Exposed map[string][]int `json:"x"`
	Border  map[string][]int `json:"m"`
	// Back lists the community edges closing a cycle, "from>to".
	Back []string `json:"b"`
	// Shared is the id of the shared kernel, "" without one.
	Shared string `json:"s"`
	// Unit is "class" or "package".
	Unit string `json:"u"`
	// Folders holds, per folder path, the analysis of the folder alone.
	Folders map[string]folderPayload `json:"d"`
}

// folderPayload is the analysis of one folder taken on its own.
type folderPayload struct {
	// UnitCount is the number of units the folder holds.
	UnitCount int `json:"u"`
	// Communities: [short name, size, shared, hint, [file indices as sorted deltas...]].
	Communities [][5]interface{} `json:"c"`
	// Edges: [from, to, references, back].
	Edges [][4]interface{} `json:"e"`
	// Verdict of the folder alone.
	Verdict string `json:"v"`
}

// communityFilesJSON serialises the files and cross references of the
// communities.
func communityFilesJSON(cm *analyzer.CommunityMetrics) string {
	empty := `{"dirs":[],"f":[],"c":{},"n":{},"x":{},"m":{},"b":[],"s":"","u":"class","d":{}}`
	if cm == nil || len(cm.UnitFiles) == 0 {
		return empty
	}
	// the root every path shares, to keep the paths short
	paths := make([]string, 0, len(cm.UnitFiles))
	seen := map[string]bool{}
	for _, path := range cm.UnitFiles {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, filepath.ToSlash(path))
		}
	}
	sort.Strings(paths)
	root := commonDirectory(paths)
	index := make(map[string]int, len(paths))
	dirs := []string{}
	dirIndex := map[string]int{}
	files := make([][2]interface{}, 0, len(paths))
	for _, path := range paths {
		short := strings.TrimPrefix(strings.TrimPrefix(path, root), "/")
		dir, name := "", short
		if i := strings.LastIndex(short, "/"); i >= 0 {
			dir, name = short[:i], short[i+1:]
		}
		di, known := dirIndex[dir]
		if !known {
			di = len(dirs)
			dirIndex[dir] = di
			dirs = append(dirs, dir)
		}
		index[path] = len(files)
		files = append(files, [2]interface{}{di, name})
	}
	fileOf := func(unit string) (int, bool) {
		path, ok := cm.UnitFiles[unit]
		if !ok {
			return 0, false
		}
		i, ok := index[filepath.ToSlash(path)]
		return i, ok
	}
	out := communityFiles{
		Dirs: dirs, Files: files, Units: map[string][]int{},
		Communities: map[string][3]interface{}{}, Back: []string{}, Unit: "class",
		Exposed: map[string][]int{}, Border: map[string][]int{},
		Folders: map[string]folderPayload{},
	}
	if cm.Granularity == analyzer.GranularityNamespace {
		out.Unit = "package"
	}
	if cm.Shared != nil {
		out.Shared = cm.Shared.ID
	}
	indicesOf := func(units []string) []int {
		list := make([]int, 0, len(units))
		for _, unit := range units {
			if fi, ok := fileOf(unit); ok {
				list = append(list, fi)
			}
		}
		return list
	}
	for i, c := range cm.Communities {
		out.Units[c.ID] = indicesOf(c.Units)
		if len(c.Exposed) > 0 {
			out.Exposed[c.ID] = indicesOf(c.Exposed)
		}
		if len(c.Border) > 0 {
			out.Border[c.ID] = indicesOf(c.Border)
		}
		out.Communities[c.ID] = [3]interface{}{c.ShortName, c.Size, communityColor(i, c.Shared)}
	}
	for _, e := range cm.Edges {
		if e.Back {
			out.Back = append(out.Back, e.From+">"+e.To)
		}
	}
	for _, folder := range cm.Folders {
		payload := folderPayload{UnitCount: folder.UnitCount, Communities: [][5]interface{}{}, Edges: [][4]interface{}{}, Verdict: folder.Verdict + " " + folder.VerdictNote}
		for _, c := range folder.Communities {
			payload.Communities = append(payload.Communities, [5]interface{}{c.ShortName, c.Size, c.Shared, c.Hint, deltas(indicesOf(c.Units))})
		}
		for _, e := range folder.Edges {
			if e.Shared {
				continue
			}
			payload.Edges = append(payload.Edges, [4]interface{}{e.From, e.To, e.Weight, e.Back})
		}
		out.Folders[folder.Path] = payload
	}
	data, err := json.Marshal(out)
	if err != nil {
		return empty
	}
	return string(data)
}

// deltas encodes a list of indices as sorted differences, which are small
// numbers: the file indices of a folder sit close to each other. The page
// adds them back up.
func deltas(indices []int) []int {
	sorted := append([]int{}, indices...)
	sort.Ints(sorted)
	out := make([]int, len(sorted))
	previous := 0
	for i, v := range sorted {
		out[i] = v - previous
		previous = v
	}
	return out
}

// commonDirectory returns the directory every path sits under, "" when there
// is none.
func commonDirectory(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := strings.Split(filepath.ToSlash(filepath.Dir(paths[0])), "/")
	for _, path := range paths[1:] {
		parts := strings.Split(filepath.ToSlash(filepath.Dir(path)), "/")
		n := 0
		for n < len(prefix) && n < len(parts) && prefix[n] == parts[n] {
			n++
		}
		prefix = prefix[:n]
		if n == 0 {
			return ""
		}
	}
	return strings.Join(prefix, "/")
}
