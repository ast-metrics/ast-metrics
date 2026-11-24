package cribelo

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Repo is a thin, read-only view of a git repository through the git CLI.
type Repo struct {
	Root string
}

// OpenRepo locates the repository holding path.
func OpenRepo(path string) (*Repo, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	out, err := runGit(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", path)
	}
	return &Repo{Root: strings.TrimSpace(out)}, nil
}

func runGit(dir string, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotepath=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.String(), nil
}

func (r *Repo) git(args ...string) (string, error) { return runGit(r.Root, args...) }

// ResolveRef turns a reference into a full commit sha.
func (r *Repo) ResolveRef(ref string) (string, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q (run git fetch?)", ref)
	}
	return strings.TrimSpace(out), nil
}

var defaultBaseCandidates = []string{"origin/main", "origin/master", "main", "master", "origin/develop", "develop"}

// DefaultBranch returns the reference changes are compared against: the
// remote HEAD when known, otherwise the first usual branch name that exists.
func (r *Repo) DefaultBranch() (string, error) {
	if out, err := r.git("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(out)
		if _, err := r.ResolveRef(ref); err == nil {
			return ref, nil
		}
	}
	for _, ref := range defaultBaseCandidates {
		if _, err := r.ResolveRef(ref); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("cannot find a base branch (tried %s); use --base", strings.Join(defaultBaseCandidates, ", "))
}

// MergeBase returns the fork point of two commits, or a when none exists.
func (r *Repo) MergeBase(a, b string) string {
	out, err := r.git("merge-base", a, b)
	if err != nil {
		return a
	}
	return strings.TrimSpace(out)
}

// Show returns the content of path at the given commit.
func (r *Repo) Show(sha, path string) ([]byte, error) {
	cmd := exec.Command("git", "-c", "core.quotepath=false", "show", sha+":"+path)
	cmd.Dir = r.Root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s:%s failed", shortSha(sha), path)
	}
	return out, nil
}

// CommitTime returns the committer time of a commit, in unix seconds.
func (r *Repo) CommitTime(sha string) (int64, error) {
	out, err := r.git("show", "-s", "--format=%ct", sha)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}

// Change is one file of a diff. Paths are relative to the repository root and
// use forward slashes. Hunks are line ranges in the new version of the file.
type Change struct {
	Path      string
	OldPath   string
	Status    byte // A, M, D, R
	Added     int
	Deleted   int
	Binary    bool
	Hunks     []Hunk
	Untracked bool
}

// Hunk is one block of a diff: an inclusive range of lines in the new file,
// the inclusive range it replaces in the old file, and the lines themselves.
// A pure addition has an old range of zero lines (OldStart == OldEnd, no
// Removed lines); a pure deletion has a new range of zero lines.
type Hunk struct {
	Start    int
	End      int
	OldStart int
	OldEnd   int
	Added    []string
	Removed  []string
}

// Changes lists the files that differ between base and head. An empty head
// means the working tree, including untracked files.
func (r *Repo) Changes(base, head string) ([]Change, error) {
	args := []string{"diff", "-M", "--name-status", "-z", base}
	if head != "" {
		args = append(args, head)
	}
	status, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	changes := parseNameStatus(status)

	args = []string{"diff", "-M", "--numstat", "-z", base}
	if head != "" {
		args = append(args, head)
	}
	numstat, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	applyNumstat(changes, parseNumstat(numstat))

	args = []string{"diff", "-M", "-U0", "--no-color", base}
	if head != "" {
		args = append(args, head)
	}
	unified, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	applyHunks(changes, parseHunks(unified))

	if head == "" {
		out, err := r.git("ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		for _, path := range splitNul(out) {
			lines := countLines(filepath.Join(r.Root, filepath.FromSlash(path)))
			changes = append(changes, Change{
				Path: path, Status: 'A', Added: lines, Untracked: true,
				Hunks: []Hunk{{Start: 1, End: maxInt(lines, 1)}},
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// TreeFiles lists the files of the working tree as git sees them: tracked
// files plus untracked ones that are not ignored. Deleted-but-tracked files
// are left out.
func (r *Repo) TreeFiles() ([]string, error) {
	out, err := r.git("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	deleted, err := r.git("ls-files", "-z", "--deleted")
	if err != nil {
		return nil, err
	}
	gone := map[string]bool{}
	for _, p := range splitNul(deleted) {
		gone[p] = true
	}
	var files []string
	for _, p := range splitNul(out) {
		if !gone[p] {
			files = append(files, p)
		}
	}
	return files, nil
}

// Event is one commit touching a file.
type Event struct {
	Time int64
	Fix  bool
}

// History indexes, per file, the commits that touched it.
type History struct {
	byPath map[string][]Event // sorted by time, ascending
}

// fixWords are the roots that say a commit repairs something, in the
// languages teams commonly write commits in, plus the conventional commit
// types and gitmoji that mean the same. Each is matched as a whole word
// (letters on either side break the match, so "fixture" and "bugle" do not
// count). The list is deliberately narrow: a false "fix" makes a file look
// riskier than it is.
var fixWords = []string{
	// English
	`fix(?:es|ed|ing)?`, `hotfix`, `bugfix`, `bug`, `revert(?:s|ed)?`, `regression`,
	// French
	`corrig(?:e|es|é|és|ée|ées|er|ez|eant)`, `corrections?`, `correctifs?`, `r[ée]par(?:e|é|és|ée|ées|er|ation|ations)`, `r[ée]gression`, `bogue`,
	// Spanish, Portuguese
	`arregl(?:a|o|ar|ado|ada|ados|adas)`, `corre(?:cci[óo]n|cciones|ção|ções|cao|coes)`, `corrigid[oa]s?`, `conserta(?:r|do|da|dos|das)?`, `regresi[óo]n`,
	// German
	`behebt`, `behoben`, `bugfixes`,
	// Gitmoji: bug, ambulance (hotfix), adhesive bandage (simple fix), rewind
	// (revert), as emoji and as shortcode. "bug" above already covers :bug:.
	`\x{1F41B}`, `\x{1F691}`, `\x{1FA79}`, `\x{23EA}`,
	`:ambulance:`, `:adhesive_bandage:`, `:rewind:`,
}

// fixPattern is built from fixWords, plus whatever the repository adds.
var fixPattern = compileFixPattern("")

func compileFixPattern(extra string) *regexp.Regexp {
	words := strings.Join(fixWords, "|")
	if extra != "" {
		words += "|" + extra
	}
	// RE2's \b is ASCII-only, so accented endings need an explicit
	// letter/non-letter boundary.
	return regexp.MustCompile(`(?i)(?:^|[^\pL])(?:` + words + `)(?:[^\pL]|$)`)
}

// SetFixPattern adds a repository-specific alternative to the fix words, for
// instance a tracker key such as `BUG-\d+`. Call it before reading history.
func SetFixPattern(extra string) error {
	if extra != "" {
		if _, err := regexp.Compile(extra); err != nil {
			return fmt.Errorf("fix_pattern: %w", err)
		}
	}
	fixPattern = compileFixPattern(extra)
	return nil
}

// IsFixMessage reports whether a commit subject describes a repair.
func IsFixMessage(subject string) bool { return fixPattern.MatchString(subject) }

// History reads the non-merge commits reachable from ref, with the files
// each one touched. One git call, whatever the size of the repository.
func (r *Repo) History(ref string) (*History, error) {
	return r.HistorySince(ref, 0)
}

// HistorySince is History limited to commits made at or after a unix time;
// zero means the whole history. Windows never look further back than that,
// so a large repository does not pay for commits it will not use.
func (r *Repo) HistorySince(ref string, since int64) (*History, error) {
	args := []string{"log", ref, "--no-merges", "--format=%x01%ct%x00%s", "--name-only"}
	if since > 0 {
		args = append(args, "--since="+time.Unix(since, 0).UTC().Format(time.RFC3339))
	}
	out, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	return parseHistory(out), nil
}

func parseHistory(out string) *History {
	h := &History{byPath: map[string][]Event{}}
	var current *Event
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "\x01") {
			parts := strings.SplitN(line[1:], "\x00", 2)
			t, _ := strconv.ParseInt(parts[0], 10, 64)
			subject := ""
			if len(parts) == 2 {
				subject = parts[1]
			}
			current = &Event{Time: t, Fix: IsFixMessage(subject)}
			continue
		}
		path := strings.TrimSpace(line)
		if path == "" || current == nil {
			continue
		}
		h.byPath[path] = append(h.byPath[path], *current)
	}
	for path := range h.byPath {
		events := h.byPath[path]
		sort.Slice(events, func(i, j int) bool { return events[i].Time < events[j].Time })
		h.byPath[path] = events
	}
	return h
}

// Before counts the commits and the fixes that touched path in the window
// (at-window, at].
func (h *History) Before(path string, at int64, window int64) (commits int, fixes int) {
	if h == nil {
		return 0, 0
	}
	for _, e := range h.byPath[path] {
		if e.Time > at-window && e.Time <= at {
			commits++
			if e.Fix {
				fixes++
			}
		}
	}
	return commits, fixes
}

// FixedAfter reports whether a fix touched path in the window (at, at+window].
func (h *History) FixedAfter(path string, at int64, window int64) bool {
	if h == nil {
		return false
	}
	for _, e := range h.byPath[path] {
		if e.Fix && e.Time > at && e.Time <= at+window {
			return true
		}
	}
	return false
}

// Landing is a change that reached the main line: a merge commit, or a
// commit made directly on the branch.
type Landing struct {
	Sha     string
	Parent  string
	Time    int64
	Subject string
	Merge   bool
}

// Landings lists the last max first-parent commits of ref, oldest first.
// The root commit, which has nothing to compare against, is left out.
// Landings lists the newest max commits of the first-parent line of ref,
// oldest first. A positive before keeps only the commits at least that old
// (unix time), so that a busy branch still yields landings old enough to
// judge.
func (r *Repo) Landings(ref string, max int, before int64) ([]Landing, error) {
	args := []string{"log", ref, "--first-parent", "--format=%H%x00%P%x00%ct%x00%s", "-n", strconv.Itoa(max)}
	if before > 0 {
		args = append(args, "--min-age="+strconv.FormatInt(before, 10))
	}
	out, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	var landings []Landing
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) < 4 {
			continue
		}
		parents := strings.Fields(parts[1])
		if len(parents) == 0 {
			continue
		}
		t, _ := strconv.ParseInt(parts[2], 10, 64)
		landings = append(landings, Landing{
			Sha: parts[0], Parent: parents[0], Time: t, Subject: parts[3], Merge: len(parents) > 1,
		})
	}
	sort.Slice(landings, func(i, j int) bool { return landings[i].Time < landings[j].Time })
	return landings, nil
}

func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func splitNul(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "\x00") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
