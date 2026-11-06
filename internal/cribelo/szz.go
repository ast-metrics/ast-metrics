package cribelo

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// The backtest's second label traces bugs back to the change that wrote
// them, as SZZ does: a file of a landing is "induced" when a later fix
// commit, within the window, modifies or deletes a line that git blame
// attributes to one of the commits of that landing. The fix-touch label
// ("a fix touched the file later") mostly rewards files that stay busy; this
// one asks whether this change, in this file, wrote the lines that had to be
// repaired.
//
// The variant is the one the SZZ evaluations favour for its precision
// (Rosa et al., ICSE 2021): blame with whitespace ignored, blank and comment
// lines skipped, only the latest candidate commit kept per fix (R-SZZ), and a
// revert anchored on the commit it names. Sweeping fixes, which touch many
// files or blame many commits, are left out: they are rarely targeted
// repairs. Lines a fix only adds cannot be traced, as in every SZZ.
const (
	szzMaxFixFiles   = 20
	szzMaxCandidates = 10
)

var revertedSha = regexp.MustCompile(`This reverts commit ([0-9a-f]{40})`)

type fixCommit struct {
	Sha, Parent string
	Time        int64
	Reverts     string
}

type blamed struct {
	Commit string
	Time   int64
	Path   string
}

// InducedLabels returns, per landing sha, the paths of that landing which a
// later fix traced back to it.
func (r *Repo) InducedLabels(ref string, changes []ChangeEval, workers int) (map[string]map[string]bool, error) {
	labels := map[string]map[string]bool{}
	if len(changes) == 0 {
		return labels, nil
	}
	files := map[string]map[string]bool{}
	times := map[string]int64{}
	paths := map[string]bool{}
	from, to := changes[0].Time, changes[0].Time
	for _, c := range changes {
		files[c.Sha] = map[string]bool{}
		for _, s := range c.Files {
			files[c.Sha][s.Path] = true
			paths[s.Path] = true
		}
		times[c.Sha] = c.Time
		if c.Time < from {
			from = c.Time
		}
		if c.Time > to {
			to = c.Time
		}
	}

	// The commits each landing brought in: itself, and for a merge the
	// commits of the merged branch.
	owner := map[string]string{}
	for _, c := range changes {
		out, err := r.git("rev-list", c.Sha+"^1.."+c.Sha)
		if err != nil {
			return nil, err
		}
		for _, sha := range strings.Fields(out) {
			if _, taken := owner[sha]; !taken {
				owner[sha] = c.Sha
			}
		}
	}

	fixes, err := r.fixesBetween(ref, from, to+Window)
	if err != nil {
		return nil, err
	}

	if workers <= 0 {
		workers = defaultWorkers()
	}
	results := make([][]blamed, len(fixes))
	errs := make([]error, len(fixes))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i], errs[i] = r.traceFix(fixes[i], paths)
			}
		}()
	}
	for i := range fixes {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for i, fix := range fixes {
		if errs[i] != nil {
			return nil, errs[i]
		}
		for _, b := range pickInducing(fix, results[i]) {
			landing, ok := owner[b.Commit]
			if !ok {
				continue
			}
			t := times[landing]
			if fix.Time > t && fix.Time <= t+Window && files[landing][b.Path] {
				if labels[landing] == nil {
					labels[landing] = map[string]bool{}
				}
				labels[landing][b.Path] = true
			}
		}
	}
	return labels, nil
}

// pickInducing keeps, of the lines a fix blamed, those of the commit it most
// likely repairs: the reverted commit for a revert, the latest candidate
// otherwise. A fix that blames too many commits is not a targeted repair.
func pickInducing(fix fixCommit, lines []blamed) []blamed {
	if len(lines) == 0 {
		return nil
	}
	target := fix.Reverts
	if target == "" {
		candidates := map[string]int64{}
		for _, b := range lines {
			candidates[b.Commit] = b.Time
		}
		if len(candidates) > szzMaxCandidates {
			return nil
		}
		var latest int64 = -1
		for sha, t := range candidates {
			if t > latest || (t == latest && sha > target) {
				target, latest = sha, t
			}
		}
	}
	var kept []blamed
	for _, b := range lines {
		if b.Commit == target {
			kept = append(kept, b)
		}
	}
	return kept
}

// fixesBetween lists the non-merge fix commits of ref made in [from, to].
func (r *Repo) fixesBetween(ref string, from, to int64) ([]fixCommit, error) {
	out, err := r.git("log", ref, "--no-merges", "--format=%x01%H%x00%P%x00%ct%x00%s%x00%b",
		"--since=@"+strconv.FormatInt(from, 10), "--until=@"+strconv.FormatInt(to, 10))
	if err != nil {
		return nil, err
	}
	var fixes []fixCommit
	for _, record := range strings.Split(out, "\x01") {
		parts := strings.SplitN(record, "\x00", 5)
		if len(parts) < 5 {
			continue
		}
		parents := strings.Fields(parts[1])
		if len(parents) == 0 || !IsFixMessage(parts[3]) {
			continue
		}
		t, _ := strconv.ParseInt(parts[2], 10, 64)
		fix := fixCommit{Sha: parts[0], Parent: parents[0], Time: t}
		if m := revertedSha.FindStringSubmatch(parts[4]); m != nil {
			fix.Reverts = m[1]
		}
		fixes = append(fixes, fix)
	}
	return fixes, nil
}

// traceFix blames, on the fix's parent, the lines the fix modified or
// deleted in the given paths.
func (r *Repo) traceFix(fix fixCommit, paths map[string]bool) ([]blamed, error) {
	diff, err := r.git("diff", "-U0", "-w", "--no-renames", "--no-color", fix.Parent, fix.Sha)
	if err != nil {
		return nil, err
	}
	ranges := map[string][][2]int{}
	current := ""
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			current = ""
		case strings.HasPrefix(line, "--- "):
			current = ""
			if p := strings.TrimPrefix(line, "--- a/"); p != line && paths[p] {
				current = p
			}
		case current != "" && strings.HasPrefix(line, "@@ "):
			if start, count, ok := oldRange(line); ok && count > 0 {
				ranges[current] = append(ranges[current], [2]int{start, count})
			}
		}
	}
	if len(ranges) > szzMaxFixFiles {
		return nil, nil
	}
	var lines []blamed
	for path, rs := range ranges {
		args := []string{"blame", "--porcelain", "-w"}
		for _, rg := range rs {
			args = append(args, "-L", strconv.Itoa(rg[0])+",+"+strconv.Itoa(rg[1]))
		}
		args = append(args, fix.Parent, "--", path)
		out, err := r.git(args...)
		if err != nil {
			continue // the file or the range vanished: nothing to trace
		}
		lines = append(lines, parseBlame(out, path)...)
	}
	return lines, nil
}

// oldRange reads the old-side range of a unified diff hunk header.
func oldRange(header string) (start, count int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "-") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(fields[1], "-")
	count = 1
	if comma := strings.IndexByte(spec, ','); comma >= 0 {
		var err error
		if count, err = strconv.Atoi(spec[comma+1:]); err != nil {
			return 0, 0, false
		}
		spec = spec[:comma]
	}
	start, err := strconv.Atoi(spec)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}

// parseBlame reads git blame --porcelain: the commit of each line, with its
// committer time, leaving out blank and comment lines.
func parseBlame(out, path string) []blamed {
	var lines []blamed
	committed := map[string]int64{}
	commit := ""
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "\t"):
			text := strings.TrimSpace(line[1:])
			if commit != "" && text != "" && !isCommentLine(text) {
				lines = append(lines, blamed{Commit: commit, Time: committed[commit], Path: path})
			}
		case strings.HasPrefix(line, "committer-time "):
			t, _ := strconv.ParseInt(strings.TrimPrefix(line, "committer-time "), 10, 64)
			committed[commit] = t
		default:
			if fields := strings.Fields(line); len(fields) >= 3 && len(fields[0]) == 40 && isHex(fields[0]) {
				commit = fields[0]
			}
		}
	}
	return lines
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
