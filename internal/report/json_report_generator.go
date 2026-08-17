package report

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

type JsonReportGenerator struct {
	ReportPath string
}

// This factory creates a new JsonReportGenerator
func NewJsonReportGenerator(ReportPath string) Reporter {
	return &JsonReportGenerator{
		ReportPath: ReportPath,
	}
}

// Generate generates a JSON report
func (j *JsonReportGenerator) Generate(files []*pb.File, projectAggregated analyzer.ProjectAggregated) ([]GeneratedReport, error) {

	if j.ReportPath == "" {
		return nil, nil
	}

	report := j.buildReport(projectAggregated)

	err := engine.CleanVal(report)
	if err != nil {
		return nil, fmt.Errorf("can not clean report err: %s", err.Error())
	}

	// This code serializes the results to JSON
	jsonReport, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("can not serialize report to JSON err: %s", err.Error())
	}

	// This code writes the JSON report to a file
	err = ioutil.WriteFile(j.ReportPath, jsonReport, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("can not save report to path %s err: %s", j.ReportPath, err.Error())
	}

	reports := []GeneratedReport{
		{
			Path:        j.ReportPath,
			Type:        "file",
			Description: "The JSON report allows scripts to parse the results programmatically.",
			Icon:        "📄",
		},
	}
	return reports, nil
}

// The buildReport creates a JSON report using the
// `projectAggregated.Combined`. For concerned
// files, it only includes the `stmts.Analyze`.
func (j *JsonReportGenerator) buildReport(projectAggregated analyzer.ProjectAggregated) *report {
	r := &report{}
	combined := projectAggregated.Combined

	r.ConcernedFiles = make([]file, len(combined.ConcernedFiles))
	for i, f := range combined.ConcernedFiles {
		concernedFile := file{
			Path: f.Path,
		}

		if f.Stmts != nil && f.Stmts.Analyze != nil {
			if f.Stmts.Analyze.Complexity != nil {
				concernedFile.Complexity = complexity{
					Cyclomatic: *f.Stmts.Analyze.Complexity.Cyclomatic,
				}
			}

			if f.Stmts.Analyze.Volume != nil {
				concernedFile.Volume = volume{
					Loc:                     f.Stmts.Analyze.Volume.GetLoc(),
					Lloc:                    f.Stmts.Analyze.Volume.GetLloc(),
					Cloc:                    f.Stmts.Analyze.Volume.GetCloc(),
					HalsteadVolume:          f.Stmts.Analyze.Volume.GetHalsteadVolume(),
					HalsteadDifficulty:      f.Stmts.Analyze.Volume.GetHalsteadDifficulty(),
					HalsteadEffort:          f.Stmts.Analyze.Volume.GetHalsteadEffort(),
					HalsteadTime:            f.Stmts.Analyze.Volume.GetHalsteadTime(),
					HalsteadVocabulary:      f.Stmts.Analyze.Volume.GetHalsteadVocabulary(),
					HalsteadLength:          f.Stmts.Analyze.Volume.GetHalsteadLength(),
					HalsteadEstimatedLength: f.Stmts.Analyze.Volume.GetHalsteadEstimatedLength(),
				}
			}

			if f.Stmts.Analyze.Maintainability != nil {
				concernedFile.Maintainability = maintainability{
					MaintainabilityIndex:                f.Stmts.Analyze.Maintainability.GetMaintainabilityIndex(),
					MaintainabilityIndexWithoutComments: f.Stmts.Analyze.Maintainability.GetMaintainabilityIndexWithoutComments(),
					CommentWeight:                       f.Stmts.Analyze.Maintainability.GetCommentWeight(),
				}
			}

			if f.Stmts.Analyze.Risk != nil {
				concernedFile.Risk = risk{
					Score: f.Stmts.Analyze.Risk.GetScore(),
				}
			}

			if f.Stmts.Analyze.Coupling != nil {
				concernedFile.Coupling = coupling{
					Afferent:    f.Stmts.Analyze.Coupling.GetAfferent(),
					Efferent:    f.Stmts.Analyze.Coupling.GetEfferent(),
					Instability: f.Stmts.Analyze.Coupling.GetInstability(),
				}
			}
		}

		r.ConcernedFiles[i] = concernedFile
	}

	r.TopCommitters = make([]contributor, len(combined.TopCommitters))
	for i, committer := range combined.TopCommitters {
		r.TopCommitters[i] = contributor{Name: committer.Name, Count: committer.Count}
	}

	r.GitAnalysis = make([]gitAnalysis, len(combined.ResultOfGitAnalysis))
	for i, analysis := range combined.ResultOfGitAnalysis {
		r.GitAnalysis[i] = gitAnalysis{
			ProgrammingLanguage:     analysis.ProgrammingLanguage,
			ReportRootDir:           analysis.ReportRootDir,
			CountCommits:            analysis.CountCommits,
			CountCommiters:          analysis.CountCommiters,
			CountCommitsForLanguage: analysis.CountCommitsForLanguage,
			CountCommitsIgnored:     analysis.CountCommitsIgnored,
		}
	}

	if combined.Community != nil {
		r.Communities = analyzer.ExportCommunities(combined.Community, true)
	}

	// Other fields
	r.NbFiles = combined.NbFiles
	r.NbFunctions = combined.NbFunctions
	r.NbClasses = combined.NbClasses
	r.NbClassesWithCode = combined.NbClassesWithCode
	r.NbMethods = combined.NbMethods
	r.Loc = int(combined.Loc.Sum)
	r.Cloc = int(combined.Cloc.Sum)
	r.Lloc = int(combined.Lloc.Sum)
	r.AverageMethodsPerClass = combined.MethodsPerClass.Avg
	r.AverageLocPerMethod = combined.LocPerMethod.Avg
	r.AverageLlocPerMethod = combined.LlocPerMethod.Avg
	r.AverageClocPerMethod = combined.ClocPerMethod.Avg
	r.AverageCyclomaticComplexityPerMethod = combined.CyclomaticComplexityPerMethod.Avg
	r.AverageCyclomaticComplexityPerClass = combined.CyclomaticComplexityPerClass.Avg
	r.MinCyclomaticComplexity = int(combined.CyclomaticComplexityPerMethod.Min)
	r.MaxCyclomaticComplexity = int(combined.CyclomaticComplexityPerMethod.Max)
	r.AverageHalsteadDifficulty = combined.HalsteadDifficulty.Avg
	r.AverageHalsteadEffort = combined.HalsteadEffort.Avg
	r.AverageHalsteadVolume = combined.HalsteadVolume.Avg
	r.AverageHalsteadTime = combined.HalsteadTime.Avg
	r.AverageHalsteadBugs = combined.HalsteadBugs.Avg
	r.SumHalsteadDifficulty = combined.HalsteadDifficulty.Sum
	r.SumHalsteadEffort = combined.HalsteadEffort.Sum
	r.SumHalsteadVolume = combined.HalsteadVolume.Sum
	r.SumHalsteadTime = combined.HalsteadTime.Sum
	r.SumHalsteadBugs = combined.HalsteadBugs.Sum
	r.AverageMI = combined.MaintainabilityIndex.Avg
	r.AverageMIwoc = combined.MaintainabilityIndexWithoutComments.Avg
	r.AverageMIcw = combined.MaintainabilityCommentWeight.Avg
	r.AverageMIPerMethod = combined.MaintainabilityPerMethod.Avg
	r.AverageMIwocPerMethod = combined.MaintainabilityPerMethodWithoutComments.Avg
	r.AverageMIcwPerMethod = combined.MaintainabilityCommentWeightPerMethod.Avg
	r.AverageAfferentCoupling = combined.AfferentCoupling.Avg
	r.AverageEfferentCoupling = combined.EfferentCoupling.Avg
	r.AverageInstability = combined.Instability.Avg
	r.AverageLcom4 = combined.Lcom4PerClass.Avg
	r.AverageCcn = combined.CyclomaticComplexity.Avg
	r.MaxLoc = combined.Loc.Max
	r.CommitCountForPeriod = combined.CommitCountForPeriod
	r.CommittedFilesCountForPeriod = combined.CommittedFilesCountForPeriod
	r.BusFactor = combined.BusFactor
	r.PackageRelations = combined.PackageRelations

	return r
}
