package engine

import (
	"fmt"
	"runtime"
	"sync"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/pterm/pterm"
)

type DumpOptions struct {
	Concurrency  int
	Label        string // ex: "PHP", "Go", "Python"
	BeforeParse  func(path string)
	AfterParse   func(file *pb.File)
	ProgressText func(done, total int, path string) string
}

type dumpJob struct {
	index int
	path  string
}

func DumpFiles(
	files []string,
	progress *pterm.SpinnerPrinter,
	parse func(path string) (*pb.File, error),
	opts DumpOptions,
) []*pb.File {
	if len(files) == 0 {
		return nil
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = runtime.NumCPU()
	}

	var wg sync.WaitGroup
	jobs := make(chan dumpJob, opts.Concurrency)
	total := len(files)
	done := 0
	var mu sync.Mutex

	// Workers finish in an arbitrary order. Keep one result slot per input file
	// so downstream aggregation always receives files in discovery order.
	results := make([]*pb.File, total)

	if opts.ProgressText == nil {
		opts.ProgressText = func(done, total int, _ string) string {
			if opts.Label == "" {
				return fmt.Sprintf("Parsing AST (%d/%d)", done, total)
			}
			return fmt.Sprintf("Parsing %s files (%d/%d)", opts.Label, done, total)
		}
	}

	worker := func() {
		for job := range jobs {
			if opts.ProgressText != nil && progress != nil {
				mu.Lock()
				done++
				progress.UpdateText(opts.ProgressText(done, total, job.path))
				mu.Unlock()
			}

			if opts.BeforeParse != nil {
				opts.BeforeParse(job.path)
			}

			if file, err := parse(job.path); err == nil && file != nil {
				if opts.AfterParse != nil {
					opts.AfterParse(file)
				}
				results[job.index] = file
			}
			wg.Done()
		}
	}

	for i := 0; i < opts.Concurrency; i++ {
		go worker()
	}
	for i, f := range files {
		wg.Add(1)
		jobs <- dumpJob{index: i, path: f}
	}
	close(jobs)
	wg.Wait()
	if progress != nil {
		progress.Info("AST parsed")
	}

	// Parsing errors leave nil slots. Compact them without disturbing the
	// relative order of successfully parsed files.
	parsed := results[:0]
	for _, file := range results {
		if file != nil {
			parsed = append(parsed, file)
		}
	}
	return parsed
}
