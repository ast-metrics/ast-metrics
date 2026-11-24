package treesitter_test

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/cpp"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// The Halstead operator set must mean the same thing in every language.
// Missing the keywords, the calls or the argument separators used to leave a
// method like "return array_keys($this->items);" with zero operators: its
// volume collapsed to zero, and since the maintainability index decreases with
// the logarithm of the volume, such a method scored as perfectly maintainable.
type halsteadCase struct {
	language string
	runner   engine.Engine
	// source declares a "keys" accessor made of a single "return call(attr);"
	// and an "add" method taking two parameters
	source string
	// access is the operator the language writes to read an attribute
	access string
}

func halsteadCases() []halsteadCase {
	return []halsteadCase{
		{
			language: "Golang",
			runner:   &golang.GolangRunner{},
			access:   ".",
			source: `package main

type Cart struct {
	items map[string]int
}

func (c *Cart) Keys() int {
	return len(c.items)
}

func (c *Cart) Add(name string, qty int) {
	c.items[name] = qty
}
`,
		},
		{
			language: "Python",
			runner:   &python.PythonRunner{},
			access:   ".",
			source: `class Cart:
    def keys(self):
        return len(self.items)

    def add(self, name, qty):
        self.items[name] = qty
`,
		},
		{
			language: "Rust",
			runner:   &rust.RustRunner{},
			access:   ".",
			source: `struct Cart {
    items: Vec<String>,
}

impl Cart {
    fn keys(&self) -> usize {
        return count(&self.items);
    }

    fn add(&mut self, name: String, qty: i32) {
        self.items.push(name);
    }
}
`,
		},
		{
			language: "TypeScript",
			runner:   &typescript.TypeScriptRunner{},
			access:   ".",
			source: `class Cart {
  private items: number[] = [];

  keys(): number {
    return count(this.items);
  }

  add(name: string, qty: number): void {
    this.items[qty] = qty;
  }
}
`,
		},
		{
			language: "Java",
			runner:   &java.JavaRunner{},
			access:   ".",
			source: `class Cart {
    private int[] items;

    public int keys() {
        return count(this.items);
    }

    public void add(String name, int qty) {
        this.items[qty] = qty;
    }
}
`,
		},
		{
			language: "C#",
			runner:   &csharp.CSharpRunner{},
			access:   ".",
			source: `class Cart {
    private int[] items;

    public int keys() {
        return Count(this.items);
    }

    public void add(string name, int qty) {
        this.items[qty] = qty;
    }
}
`,
		},
		{
			language: "C++",
			runner:   &cpp.CppRunner{},
			access:   "->",
			source: `class Cart {
    int* items;

public:
    int keys() {
        return count(this->items);
    }

    void add(int name, int qty) {
        this->items[qty] = qty;
    }
};
`,
		},
		{
			language: "PHP",
			runner:   &php.PhpRunner{},
			access:   "->",
			source: `<?php
class Cart {
    private array $items = [];

    public function keys(): array {
        return array_keys($this->items);
    }

    public function add(string $name, int $qty): void {
        $this->items[$name] = $qty;
    }
}
`,
		},
	}
}

func TestHalsteadOperatorsCoverKeywordsCallsAndSeparators(t *testing.T) {
	for _, c := range halsteadCases() {
		t.Run(c.language, func(t *testing.T) {
			file, err := engine.CreateTestFileWithCode(c.runner, c.source)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			analyzer.AnalyzeFile(file)
			functions := functionsByName(file.Stmts)

			keys := functions["keys"]
			if keys == nil {
				keys = functions["Keys"]
			}
			if keys == nil {
				t.Fatalf("accessor not found among %v", nameSet(functions))
			}
			operators := operatorNames(keys)

			for _, expected := range []string{"return", "()", c.access} {
				if !operators[expected] {
					t.Errorf("%q is missing from the operators of the accessor: %v", expected, operators)
				}
			}

			// the reported symptom: no operator at all meant no volume at all
			volume := keys.Stmts.Analyze.Volume
			if volume == nil || volume.HalsteadVolume == nil {
				t.Fatalf("no Halstead volume computed for the accessor")
			}
			if *volume.HalsteadVolume <= 0 {
				t.Errorf("expected a positive Halstead volume, got %v", *volume.HalsteadVolume)
			}

			add := functions["add"]
			if add == nil {
				add = functions["Add"]
			}
			if add == nil {
				t.Fatalf("two-parameter method not found among %v", nameSet(functions))
			}
			if !operatorNames(add)[","] {
				t.Errorf("the argument separator is missing from the operators of a two-parameter method: %v", operatorNames(add))
			}
		})
	}
}

// TestHalsteadVolumeStaysBelowTheMaintainabilityCeiling guards the other side
// of the fix: over-counting operators would push the volume up and the
// maintainability index down to an implausible score.
func TestHalsteadVolumeStaysBelowTheMaintainabilityCeiling(t *testing.T) {
	for _, c := range halsteadCases() {
		t.Run(c.language, func(t *testing.T) {
			file, err := engine.CreateTestFileWithCode(c.runner, c.source)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			analyzer.AnalyzeFile(file)
			for name, fn := range functionsByName(file.Stmts) {
				volume := fn.Stmts.Analyze.Volume
				if volume == nil || volume.HalsteadVolume == nil {
					continue
				}
				// a one-line accessor holds a handful of symbols: anything past
				// a hundred bits means the walk counted syntax as operators
				if *volume.HalsteadVolume > 100 {
					t.Errorf("%s: implausible Halstead volume %v for a one-liner (operators: %v)",
						name, *volume.HalsteadVolume, operatorNames(fn))
				}
			}
		})
	}
}

func functionsByName(stmts *pb.Stmts) map[string]*pb.StmtFunction {
	found := map[string]*pb.StmtFunction{}
	var walk func(*pb.Stmts)
	walk = func(s *pb.Stmts) {
		if s == nil {
			return
		}
		for _, fn := range s.StmtFunction {
			if fn.Name != nil && fn.Stmts != nil && fn.Stmts.Analyze != nil {
				found[fn.Name.Short] = fn
			}
			if fn.Stmts != nil {
				walk(fn.Stmts)
			}
		}
		for _, cls := range s.StmtClass {
			walk(cls.Stmts)
		}
		for _, ns := range s.StmtNamespace {
			walk(ns.Stmts)
		}
	}
	walk(stmts)
	return found
}

func nameSet(functions map[string]*pb.StmtFunction) []string {
	names := make([]string, 0, len(functions))
	for name := range functions {
		names = append(names, name)
	}
	return names
}

func operatorNames(fn *pb.StmtFunction) map[string]bool {
	names := map[string]bool{}
	for _, op := range fn.Operators {
		names[op.Name] = true
	}
	return names
}
