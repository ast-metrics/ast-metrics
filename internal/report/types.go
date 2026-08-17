package report

import "github.com/ast-metrics/ast-metrics/internal/analyzer"

// The report structure serves to maintain a standardized
// report format.
// This class should not be exposed to other packages, as
// it is specific to JSON formatting.
type report struct {
	ConcernedFiles []file `json:"concernedFiles,omitempty"`
	// hashmap of classes, just with the qualified name, used for afferent coupling calculation
	ClassesAfferentCoupling              map[string]int            `json:"classesAfferentCoupling,omitempty"`
	NbFiles                              int                       `json:"numberFiles,omitempty"`
	NbFunctions                          int                       `json:"numberFunctions,omitempty"`
	NbClasses                            int                       `json:"numberClasses,omitempty"`
	NbClassesWithCode                    int                       `json:"numberClassesWithCode,omitempty"`
	NbMethods                            int                       `json:"numberMethods,omitempty"`
	Loc                                  int                       `json:"loc,omitempty"`
	Cloc                                 int                       `json:"cloc,omitempty"`
	Lloc                                 int                       `json:"lloc,omitempty"`
	AverageMethodsPerClass               float64                   `json:"averageMethodsPerClass,omitempty"`
	AverageLocPerMethod                  float64                   `json:"averageLocPerMethod,omitempty"`
	AverageLlocPerMethod                 float64                   `json:"averageLlocPerMethod,omitempty"`
	AverageClocPerMethod                 float64                   `json:"averageClocPerMethod,omitempty"`
	AverageCyclomaticComplexityPerMethod float64                   `json:"averageCyclomaticComplexityPerMethod,omitempty"`
	AverageCyclomaticComplexityPerClass  float64                   `json:"averageCyclomaticComplexityPerClass,omitempty"`
	MinCyclomaticComplexity              int                       `json:"minCyclomaticComplexity,omitempty"`
	MaxCyclomaticComplexity              int                       `json:"maxCyclomaticComplexity,omitempty"`
	AverageHalsteadDifficulty            float64                   `json:"averageHalsteadDifficulty,omitempty"`
	AverageHalsteadEffort                float64                   `json:"averageHalsteadEffort,omitempty"`
	AverageHalsteadVolume                float64                   `json:"averageHalsteadVolume,omitempty"`
	AverageHalsteadTime                  float64                   `json:"averageHalsteadTime,omitempty"`
	AverageHalsteadBugs                  float64                   `json:"averageHalsteadBugs,omitempty"`
	SumHalsteadDifficulty                float64                   `json:"sumHalsteadDifficulty,omitempty"`
	SumHalsteadEffort                    float64                   `json:"sumHalsteadEffort,omitempty"`
	SumHalsteadVolume                    float64                   `json:"sumHalsteadVolume,omitempty"`
	SumHalsteadTime                      float64                   `json:"sumHalsteadTime,omitempty"`
	SumHalsteadBugs                      float64                   `json:"sumHalsteadBugs,omitempty"`
	AverageMI                            float64                   `json:"averageMI,omitempty"`
	AverageMIwoc                         float64                   `json:"averageMIwoc,omitempty"`
	AverageMIcw                          float64                   `json:"averageMIcw,omitempty"`
	AverageMIPerMethod                   float64                   `json:"averageMIPerMethod,omitempty"`
	AverageMIwocPerMethod                float64                   `json:"averageMIwocPerMethod,omitempty"`
	AverageMIcwPerMethod                 float64                   `json:"averageMIcwPerMethod,omitempty"`
	AverageAfferentCoupling              float64                   `json:"averageAfferentCoupling,omitempty"`
	AverageEfferentCoupling              float64                   `json:"averageEfferentCoupling,omitempty"`
	AverageInstability                   float64                   `json:"averageInstability,omitempty"`
	AverageLcom4                         float64                   `json:"averageLcom4,omitempty"`
	AverageCcn                           float64                   `json:"averageCcn,omitempty"`
	MaxLoc                               float64                   `json:"maxLoc,omitempty"`
	CommitCountForPeriod                 int                       `json:"commitCountForPeriod,omitempty"`
	CommittedFilesCountForPeriod         int                       `json:"committedFilesCountForPeriod,omitempty"` // for example if one commit concerns 10 files, it will be 10
	BusFactor                            int                       `json:"busFactor,omitempty"`
	TopCommitters                        []contributor             `json:"topCommitters,omitempty"`
	GitAnalysis                          []gitAnalysis             `json:"gitAnalysis,omitempty"`
	PackageRelations                     map[string]map[string]int `json:"packageRelations,omitempty"` // counter of dependencies. Ex: A -> B -> 2
	// Communities are the groups of classes that depend on each other more
	// than on the rest of the code, with the findings read from them.
	Communities *analyzer.CommunitiesExport `json:"communities,omitempty"`
}

type contributor struct {
	Name  string `json:"name,omitempty"`
	Count int    `json:"count,omitempty"`
}

type gitAnalysis struct {
	ProgrammingLanguage     string
	ReportRootDir           string
	CountCommits            int
	CountCommiters          int
	CountCommitsForLanguage int
	CountCommitsIgnored     int
}

type file struct {
	Path            string          `json:"path,omitempty"`
	Complexity      complexity      `json:"complexity,omitempty"`
	Volume          volume          `json:"volume,omitempty"`
	Maintainability maintainability `json:"maintainability,omitempty"`
	Risk            risk            `json:"risk,omitempty"`
	Coupling        coupling        `json:"coupling,omitempty"`
}

type complexity struct {
	Cyclomatic int32 `json:"cyclomatic,omitempty"`
}

type risk struct {
	Score float64 `json:"score,omitempty"` // score of risk. Lower is better
}

type coupling struct {
	Afferent    int32   `json:"afferent,omitempty"`    // number of classes that depends on this class
	Efferent    int32   `json:"efferent,omitempty"`    // number of classes that this class depends on
	Instability float64 `json:"instability,omitempty"` // instability of the class
}

type maintainability struct {
	MaintainabilityIndex                float64 `json:"maintainabilityIndex,omitempty"`
	MaintainabilityIndexWithoutComments float64 `json:"maintainabilityIndexWithoutComments,omitempty"`
	CommentWeight                       float64 `json:"commentWeight,omitempty"`
}

type volume struct {
	Loc                     int32   `json:"loc,omitempty"`
	Lloc                    int32   `json:"lloc,omitempty"`
	Cloc                    int32   `json:"cloc,omitempty"`
	HalsteadVocabulary      int32   `json:"halsteadVocabulary,omitempty"`
	HalsteadLength          int32   `json:"halsteadLength,omitempty"`
	HalsteadVolume          float64 `json:"halsteadVolume,omitempty"`
	HalsteadDifficulty      float64 `json:"halsteadDifficulty,omitempty"`
	HalsteadEffort          float64 `json:"halsteadEffort,omitempty"`
	HalsteadTime            float64 `json:"halsteadTime,omitempty"`
	HalsteadEstimatedLength float64 `json:"halsteadEstimatedLength,omitempty"`
}
