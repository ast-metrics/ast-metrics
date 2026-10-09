package engine

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	filefinder "github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"
	log "github.com/sirupsen/logrus"
)

// ParseFiles runs all engines against the given configuration and returns
// the parsed AST files. This is a standalone version of
// AnalyzeCommand.ExecuteRunnerAnalysis that has no UI dependencies.
func ParseFiles(config *configuration.Configuration, runners []Engine) ([]*pb.File, error) {
	// Precompute file discovery for all languages in a single directory walk
	if config.FileDiscovery == nil {
		discovery := &filefinder.FileDiscovery{}
		finder := filefinder.Finder{Configuration: *config}
		allExts := append(config.AllExtensions(), ".tsx")
		discovery.Precompute(finder, uniqueExts(allExts))
		config.FileDiscovery = discovery
	}

	var allParsed []*pb.File

	for _, runner := range runners {
		runner.SetConfiguration(config)

		if !runner.IsRequired() {
			continue
		}

		runner.SetProgressbar(nil)

		err := runner.Ensure()
		if err != nil {
			return nil, err
		}

		parsed := runner.DumpAST()
		allParsed = append(allParsed, parsed...)

		err = runner.Finish()
		if err != nil {
			log.Warn("Runner finish error: ", err)
		}
	}

	return allParsed, nil
}

func uniqueExts(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !strings.HasPrefix(v, ".") {
			v = "." + v
		}
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
