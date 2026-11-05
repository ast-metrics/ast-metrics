package cribelo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// parseNameStatus reads the -z output of git diff --name-status -M.
func parseNameStatus(out string) []Change {
	fields := splitNul(out)
	var changes []Change
	for i := 0; i < len(fields); {
		status := fields[i]
		if status == "" {
			i++
			continue
		}
		code := status[0]
		switch code {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return changes
			}
			changes = append(changes, Change{Status: 'R', OldPath: fields[i+1], Path: fields[i+2]})
			i += 3
		default:
			if i+1 >= len(fields) {
				return changes
			}
			if code != 'A' && code != 'D' {
				code = 'M'
			}
			changes = append(changes, Change{Status: code, Path: fields[i+1]})
			i += 2
		}
	}
	return changes
}

type numstat struct {
	added, deleted int
	binary         bool
}

// parseNumstat reads the -z output of git diff --numstat -M. A rename is
// written as "added\tdeleted\t\0old\0new\0", a plain file as
// "added\tdeleted\tpath\0".
func parseNumstat(out string) map[string]numstat {
	stats := map[string]numstat{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if field == "" {
			continue
		}
		parts := strings.SplitN(field, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		var st numstat
		if parts[0] == "-" {
			st.binary = true
		} else {
			st.added, _ = strconv.Atoi(parts[0])
			st.deleted, _ = strconv.Atoi(parts[1])
		}
		path := parts[2]
		if path == "" {
			// rename: the two following fields are old and new path
			if i+2 < len(fields) {
				path = fields[i+2]
				i += 2
			}
		}
		stats[path] = st
	}
	return stats
}

func applyNumstat(changes []Change, stats map[string]numstat) {
	for i := range changes {
		if st, ok := stats[changes[i].Path]; ok {
			changes[i].Added = st.added
			changes[i].Deleted = st.deleted
			changes[i].Binary = st.binary
		}
	}
}

// parseHunks reads a unified diff and returns, per new path, the ranges of
// changed lines in the new file. A pure deletion yields no range.
func parseHunks(out string) map[string][]Hunk {
	hunks := map[string][]Hunk{}
	current := ""
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			target := strings.TrimPrefix(line, "+++ ")
			if target == "/dev/null" {
				current = ""
				continue
			}
			target = strings.TrimPrefix(target, "b/")
			if tab := strings.IndexByte(target, '\t'); tab >= 0 {
				target = target[:tab]
			}
			current = target
			if _, ok := hunks[current]; !ok {
				hunks[current] = nil
			}
		case strings.HasPrefix(line, "@@ ") && current != "":
			if h, ok := parseHunkHeader(line); ok {
				hunks[current] = append(hunks[current], h)
			}
		}
	}
	return hunks
}

// parseHunkHeader reads "@@ -a,b +c,d @@" and returns the new range [c, c+d-1].
// A range of zero lines (pure deletion) is anchored on the line that follows.
func parseHunkHeader(line string) (Hunk, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if end < 0 {
		return Hunk{}, false
	}
	parts := strings.Fields(rest[:end])
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "+") {
		return Hunk{}, false
	}
	spec := strings.TrimPrefix(parts[1], "+")
	start, count := 0, 1
	if comma := strings.IndexByte(spec, ','); comma >= 0 {
		start, _ = strconv.Atoi(spec[:comma])
		count, _ = strconv.Atoi(spec[comma+1:])
	} else {
		start, _ = strconv.Atoi(spec)
	}
	if count == 0 {
		return Hunk{Start: start, End: start}, true
	}
	return Hunk{Start: start, End: start + count - 1}, true
}

func applyHunks(changes []Change, hunks map[string][]Hunk) {
	for i := range changes {
		changes[i].Hunks = hunks[changes[i].Path]
	}
}

// countLines counts the lines of a file on disk; 0 when unreadable.
func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	n := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// touches reports whether the inclusive line range [start,end] intersects
// one of the hunks.
func touches(hunks []Hunk, start, end int) bool {
	for _, h := range hunks {
		if h.Start <= end && h.End >= start {
			return true
		}
	}
	return false
}
