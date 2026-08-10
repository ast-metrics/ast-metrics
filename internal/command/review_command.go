package command

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/report"
	"github.com/ast-metrics/ast-metrics/internal/review"
	"github.com/ast-metrics/ast-metrics/internal/scm"
	pb "github.com/ast-metrics/ast-metrics/pb"
	log "github.com/sirupsen/logrus"
)

// ReviewCommand compares the current working tree with a base git reference
// and reports only new or worsened findings, like a pull request check.
type ReviewCommand struct {
	Configuration *configuration.Configuration
	runners       []engine.Engine
	out           io.Writer

	// BaseRef is the git reference to compare against (e.g. origin/main).
	// Empty means: first of origin/main, origin/master, main, master.
	BaseRef string
	// Format of the stdout output: text, markdown or json
	Format string
	// FailOn makes the command exit with an error when a regression of at
	// least this severity exists: high, medium, any or never (default)
	FailOn string
	// MaxFindings caps text and markdown outputs (json and sarif are complete)
	MaxFindings int

	ReportMarkdown string
	ReportJson     string
	ReportSarif    string
	// ReportSarifMaxLevel caps the level of the SARIF results: error, warning or
	// note. Empty keeps the level derived from the severity of the regression.
	ReportSarifMaxLevel string
}

func NewReviewCommand(configuration *configuration.Configuration, out io.Writer, runners []engine.Engine) *ReviewCommand {
	return &ReviewCommand{
		Configuration: configuration,
		runners:       runners,
		out:           out,
		Format:        "text",
		FailOn:        "never",
		MaxFindings:   5,
	}
}

var defaultBaseRefs = []string{"origin/main", "origin/master", "main", "master"}

func (c *ReviewCommand) Execute() error {
	if len(c.Configuration.SourcesToAnalyzePath) == 0 {
		return fmt.Errorf("please provide a path to analyze")
	}

	repository, err := scm.NewGitRepositoryFromPath(c.Configuration.SourcesToAnalyzePath[0])
	if err != nil {
		return fmt.Errorf("the review command requires a git repository: %w", err)
	}

	baseRef, baseSha, err := c.resolveBase(&repository)
	if err != nil {
		return err
	}

	headSha, err := repository.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("cannot resolve HEAD: %w", err)
	}

	// Compare against the merge-base so that commits landed on the base
	// branch after the fork point are not attributed to this change.
	comparisonPoint := baseSha
	if mergeBase, err := repository.MergeBase(baseSha, headSha); err == nil {
		comparisonPoint = mergeBase
	} else {
		log.Debugf("merge-base unavailable, comparing directly with %s: %v", baseRef, err)
	}

	// Analyze the current working tree
	headFiles, err := c.analyze(c.Configuration)
	if err != nil {
		return err
	}

	// Analyze the base version in an isolated, detached worktree so the
	// user's working tree is never touched
	worktree, err := repository.AddWorktree(comparisonPoint)
	if err != nil {
		return err
	}
	defer repository.RemoveWorktree(worktree)

	baseConfig, err := c.configurationForBase(worktree, repository.Path)
	if err != nil {
		return err
	}
	var baseFiles []*pb.File
	if baseConfig != nil {
		baseFiles, err = c.analyze(baseConfig)
		if err != nil {
			return err
		}
	}

	// Compute findings
	result := review.Compare(headFiles, baseFiles, repository.Path, worktree, review.DefaultOptions())
	result.BaseRef = baseRef
	result.BaseSha = comparisonPoint
	result.HeadSha = headSha

	// New lint violations (only when requirements are configured)
	if c.Configuration.Requirements != nil {
		headOutcomes := c.evaluateRequirements(headFiles)
		baseOutcomes := c.evaluateRequirements(baseFiles)
		result.AppendFindings(review.DiffLint(headOutcomes, baseOutcomes, repository.Path, worktree))
	}

	result.Gate = result.EvaluateGate(c.FailOn)

	if err := c.render(&result); err != nil {
		return err
	}

	if result.Gate == "failed" {
		return fmt.Errorf("quality gate failed: %d high, %d medium, %d low regression(s)",
			result.Summary.High, result.Summary.Medium, result.Summary.Low)
	}
	return nil
}

func (c *ReviewCommand) resolveBase(repository *scm.GitRepository) (string, string, error) {
	if c.BaseRef != "" {
		sha, err := repository.ResolveRef(c.BaseRef)
		if err != nil {
			return "", "", fmt.Errorf("cannot resolve base %q; make sure it exists locally (e.g. run: git fetch origin)", c.BaseRef)
		}
		return c.BaseRef, sha, nil
	}
	for _, ref := range defaultBaseRefs {
		if sha, err := repository.ResolveRef(ref); err == nil {
			return ref, sha, nil
		}
	}
	return "", "", fmt.Errorf("cannot find a base branch (tried %s); use --base to specify one", strings.Join(defaultBaseRefs, ", "))
}

func (c *ReviewCommand) analyze(config *configuration.Configuration) ([]*pb.File, error) {
	parsed, err := engine.ParseFiles(config, c.runners)
	if err != nil {
		return nil, err
	}
	review.FillChecksums(parsed)
	return analyzer.AnalyzeFiles(parsed, nil), nil
}

// configurationForBase maps the analyzed sources into the base worktree.
// Returns nil when none of the source paths exist in the base version.
func (c *ReviewCommand) configurationForBase(worktree string, repositoryRoot string) (*configuration.Configuration, error) {
	baseConfig := *c.Configuration
	baseConfig.FileDiscovery = nil

	sources := []string{}
	for _, source := range c.Configuration.SourcesToAnalyzePath {
		rel, err := filepath.Rel(repositoryRoot, source)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("source %q is outside the git repository %q", source, repositoryRoot)
		}
		mapped := filepath.Join(worktree, rel)
		if _, err := os.Stat(mapped); err != nil {
			// the directory does not exist in the base version
			continue
		}
		sources = append(sources, mapped)
	}
	if len(sources) == 0 {
		return nil, nil
	}
	baseConfig.SourcesToAnalyzePath = sources
	return &baseConfig, nil
}

func (c *ReviewCommand) evaluateRequirements(files []*pb.File) []requirement.RuleOutcome {
	if c.Configuration.Requirements == nil || len(files) == 0 {
		return nil
	}
	aggregator := analyzer.NewAggregator(files, nil)
	projectAggregated := aggregator.Aggregates()
	evaluator := requirement.NewRequirementsEvaluator(*c.Configuration.Requirements)
	if baselinePath := requirement.ResolveBaselinePath(c.Configuration.Requirements.Baseline); baselinePath != "" {
		if baseline, err := requirement.LoadBaseline(baselinePath); err == nil {
			evaluator.Baseline = baseline
		}
	}
	evaluation := evaluator.Evaluate(files, requirement.ProjectAggregated{ProjectCtx: buildProjectContext(projectAggregated)})
	return evaluation.Errors
}

func (c *ReviewCommand) render(result *review.Result) error {
	switch strings.ToLower(strings.TrimSpace(c.Format)) {
	case "", "text", "terminal":
		fmt.Fprint(c.out, result.Text(c.MaxFindings))
	case "markdown":
		fmt.Fprint(c.out, result.Markdown(c.MaxFindings))
	case "json":
		out, err := result.JSON()
		if err != nil {
			return err
		}
		fmt.Fprint(c.out, out)
	default:
		return fmt.Errorf("unknown format %q (expected text, markdown or json)", c.Format)
	}

	if c.ReportMarkdown != "" {
		if err := os.WriteFile(c.ReportMarkdown, []byte(result.Markdown(c.MaxFindings)), 0644); err != nil {
			return err
		}
	}
	if c.ReportJson != "" {
		out, err := result.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(c.ReportJson, []byte(out), 0644); err != nil {
			return err
		}
	}
	if c.ReportSarif != "" {
		if _, err := report.GenerateSarifFromOutcomes(c.ReportSarif, result.ToRuleOutcomes(), c.ReportSarifMaxLevel); err != nil {
			return err
		}
	}
	return nil
}
