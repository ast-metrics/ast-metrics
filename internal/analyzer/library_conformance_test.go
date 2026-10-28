package analyzer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// The same situation in every language: a logger wraps a logging library,
// and a service uses the logger. Whatever the language, the library has to
// be recorded under the name the import gives it, on the logger only, and
// asking who uses it has to answer the logger first and the service one
// level further. This is what the who-uses command stands on.
type librarySample struct {
	language string
	engine   enginePkg.Engine
	setup    map[string]string
	// the logger, then the service: path and source
	files [2][2]string
	// the library as the logger's import names it
	module string
	// what a user would type to find it
	query string
}

func librarySamples() []librarySample {
	return []librarySample{
		{
			language: "PHP", engine: &php.PhpRunner{},
			files: [2][2]string{
				{"src/Log/Logger.php", "<?php\nnamespace App\\Log;\nuse Monolog\\Logger as Monolog;\nclass Logger { public function __construct(private Monolog $inner) {} }\n"},
				{"src/Billing/Service.php", "<?php\nnamespace App\\Billing;\nuse App\\Log\\Logger;\nclass Service { public function __construct(private Logger $logger) {} }\n"},
			},
			module: `Monolog\Logger`, query: "monolog",
		},
		{
			language: "Java", engine: &java.JavaRunner{},
			files: [2][2]string{
				{"src/com/acme/log/Logger.java", "package com.acme.log;\nimport org.apache.logging.log4j.LogManager;\npublic class Logger { private Object inner = LogManager.getLogger(); }\n"},
				{"src/com/acme/billing/Service.java", "package com.acme.billing;\nimport com.acme.log.Logger;\npublic class Service { private Logger logger; }\n"},
			},
			module: "org.apache.logging.log4j", query: "log4j",
		},
		{
			language: "C#", engine: &csharp.CSharpRunner{},
			files: [2][2]string{
				{"src/Log/Logger.cs", "using Serilog;\nnamespace Acme.Log { public class Logger { private Serilog.ILogger inner; } }\n"},
				{"src/Billing/Service.cs", "using Acme.Log;\nnamespace Acme.Billing { public class Service { private Logger logger; } }\n"},
			},
			module: "Serilog", query: "serilog",
		},
		{
			language: "Go", engine: &golang.GolangRunner{}, setup: map[string]string{"go.mod": "module example.com/demo\n"},
			files: [2][2]string{
				{"internal/log/logger.go", "package log\n\nimport \"github.com/sirupsen/logrus\"\n\ntype Logger struct{ inner *logrus.Logger }\n"},
				{"internal/billing/service.go", "package billing\n\nimport \"example.com/demo/internal/log\"\n\ntype Service struct{ logger log.Logger }\n"},
			},
			module: "github.com/sirupsen/logrus", query: "logrus",
		},
		{
			language: "Python", engine: &python.PythonRunner{}, setup: map[string]string{"pyproject.toml": ""},
			files: [2][2]string{
				{"acme/log/logger.py", "from loguru import logger as inner\n\nclass Logger:\n    def __init__(self):\n        self.inner = inner\n"},
				{"acme/billing/service.py", "from acme.log.logger import Logger\n\nclass Service:\n    def __init__(self, logger: Logger):\n        self.logger = logger\n"},
			},
			module: "loguru", query: "loguru",
		},
		{
			language: "TypeScript", engine: &typescript.TypeScriptRunner{}, setup: map[string]string{"package.json": "{}"},
			files: [2][2]string{
				{"src/log/logger.ts", "import pino from 'pino';\nexport class Logger { private inner = pino(); }\n"},
				{"src/billing/service.ts", "import { Logger } from '../log/logger';\nexport class Service { constructor(private logger: Logger) {} }\n"},
			},
			module: "pino", query: "pino",
		},
		{
			language: "Rust", engine: &rust.RustRunner{}, setup: map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n"},
			files: [2][2]string{
				{"src/log/mod.rs", "use tracing::info;\npub struct Logger;\nimpl Logger { pub fn log(&self) { info!(\"x\") } }\n"},
				{"src/billing/mod.rs", "use crate::log::Logger;\npub struct Service { logger: Logger }\n"},
			},
			module: "tracing", query: "tracing",
		},
	}
}

func TestALibraryIsFoundTheSameWayInEveryLanguage(t *testing.T) {
	for _, sample := range librarySamples() {
		t.Run(sample.language, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range sample.setup {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			paths := [2]string{}
			files := make([]*pb.File, 0, 2)
			for i, source := range sample.files {
				path := filepath.Join(root, source[0])
				paths[i] = path
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(source[1]), 0o644); err != nil {
					t.Fatal(err)
				}
				file, err := sample.engine.Parse(path)
				if err != nil {
					t.Fatalf("parse %s: %v", source[0], err)
				}
				AnalyzeFile(file)
				files = append(files, file)
			}
			aggregated := graphOfFiles(files...)
			logger, service := paths[0], paths[1]

			if got := aggregated.FileDependencies.Libraries[logger]; !reflect.DeepEqual(got, []string{sample.module}) {
				t.Errorf("the logger imports %q, expected only %q", got, sample.module)
			}
			if got := aggregated.FileDependencies.Libraries[service]; len(got) != 0 {
				t.Errorf("the service imports nothing foreign, got %q", got)
			}

			reach := aggregated.WhoUses(sample.query, 0)
			if want := [][]string{{logger}, {service}}; !reflect.DeepEqual(reach.Levels, want) {
				t.Errorf("who uses %q = %v, expected %v", sample.query, reach.Levels, want)
			}
			if reach.Scope != 2 {
				t.Errorf("scope = %d, expected 2", reach.Scope)
			}
		})
	}
}
