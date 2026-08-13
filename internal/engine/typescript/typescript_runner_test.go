package typescript

import (
	"os"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

const sampleTS = `import { EventEmitter } from 'events';

class Calculator {
    add(a: number, b: number): number {
        if (a > 0) {
            console.log("positive");
        } else if (a === 0) {
            console.log("zero");
        } else {
            console.log("negative");
        }
        for (let i = 0; i < 3; i++) {
            // loop body
        }
        while (b > 0) {
            b--;
        }
        return a + b;
    }
}

async function processData(data: string[]): Promise<void> {
    switch (data.length) {
        case 0:
            return;
        case 1:
            console.log("single");
            break;
        default:
            console.log("multiple");
    }
}

const multiply = (x: number, y: number): number => {
    return x * y;
};

function simple(z: number): number {
    return z * 2;
}
`

func TestTypeScriptParser_TreeSitter_Decisions(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleTS)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file == nil || file.Stmts == nil {
		t.Fatalf("nil file or stmts")
	}
	if file.ProgrammingLanguage != "TypeScript" {
		t.Fatalf("expected TypeScript, got %q", file.ProgrammingLanguage)
	}
	if len(file.Stmts.StmtNamespace) != 1 {
		t.Fatalf("expected 1 namespace, got %d", len(file.Stmts.StmtNamespace))
	}
	ns := file.Stmts.StmtNamespace[0]
	if ns == nil || ns.Stmts == nil {
		t.Fatalf("nil namespace stmts")
	}

	// Class Calculator
	if len(ns.Stmts.StmtClass) != 1 {
		t.Fatalf("expected 1 class, got %d", len(ns.Stmts.StmtClass))
	}
	cls := ns.Stmts.StmtClass[0]
	if cls.Name == nil || cls.Name.Short != "Calculator" {
		t.Fatalf("expected class Calculator, got %+v", cls.Name)
	}

	// Method add
	var add *pb.StmtFunction
	for _, fn := range cls.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "add" {
			add = fn
			break
		}
	}
	if add == nil {
		t.Fatalf("method add not found in Calculator")
	}
	if got := len(add.Stmts.StmtDecisionIf); got < 1 {
		t.Fatalf("expected at least 1 if in add, got %d", got)
	}
	if got := len(add.Stmts.StmtLoop); got != 2 {
		t.Fatalf("expected 2 loops (for, while) in add, got %d", got)
	}
	if add.LinesOfCode == nil || add.LinesOfCode.LinesOfCode == 0 {
		t.Fatalf("expected LOC on add")
	}

	// Functions in namespace: add, processData, multiply, simple (at minimum)
	if len(ns.Stmts.StmtFunction) < 3 {
		t.Fatalf("expected at least 3 functions in namespace, got %d", len(ns.Stmts.StmtFunction))
	}

	// processData: switch + cases
	var fnProcess *pb.StmtFunction
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "processData" {
			fnProcess = fn
			break
		}
	}
	if fnProcess == nil {
		t.Fatalf("function processData not found")
	}
	if got := len(fnProcess.Stmts.StmtDecisionSwitch); got != 1 {
		t.Fatalf("expected 1 switch in processData, got %d", got)
	}
	if got := len(fnProcess.Stmts.StmtDecisionCase); got < 2 {
		t.Fatalf("expected at least 2 cases in processData, got %d", got)
	}
	if fnProcess.LinesOfCode == nil || fnProcess.LinesOfCode.LinesOfCode == 0 {
		t.Fatalf("expected LOC on processData")
	}

	// simple function
	var fnSimple *pb.StmtFunction
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "simple" {
			fnSimple = fn
			break
		}
	}
	if fnSimple == nil {
		t.Fatalf("function simple not found")
	}
	if fnSimple.LinesOfCode == nil || fnSimple.LinesOfCode.LinesOfCode == 0 {
		t.Fatalf("expected LOC on simple")
	}
}

const sampleImports = `
import fs from 'fs';
import { readFile, writeFile } from 'fs/promises';
import * as path from 'path';
`

func TestTypeScriptParser_TreeSitter_Imports(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleImports)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file == nil || file.Stmts == nil || len(file.Stmts.StmtNamespace) != 1 {
		t.Fatalf("invalid file/namespace")
	}
	ns := file.Stmts.StmtNamespace[0]
	if ns == nil || ns.Stmts == nil {
		t.Fatalf("nil ns stmts")
	}

	got := len(ns.Stmts.StmtExternalDependencies)
	if got < 4 {
		t.Fatalf("expected at least 4 external deps, got %d", got)
	}

	has := func(module, name string) bool {
		for _, d := range ns.Stmts.StmtExternalDependencies {
			if d.Namespace == module && d.ClassName == name {
				return true
			}
			if name == "" && d.Namespace == module {
				return true
			}
		}
		return false
	}

	if !has("fs", "fs") {
		t.Fatalf("missing dep: import fs from 'fs'")
	}
	if !has("fs/promises", "readFile") {
		t.Fatalf("missing dep: import { readFile } from 'fs/promises'")
	}
	if !has("fs/promises", "writeFile") {
		t.Fatalf("missing dep: import { writeFile } from 'fs/promises'")
	}
	if !has("path", "path") {
		t.Fatalf("missing dep: import * as path from 'path'")
	}
}

const sampleReExports = `
export * from './components';
export * as ns from './bar';
export { a, b as c } from './foo';
export type { X } from './types';
export { local };
`

func TestTypeScriptParser_TreeSitter_ReExports(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleReExports)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file == nil || file.Stmts == nil || len(file.Stmts.StmtNamespace) != 1 {
		t.Fatalf("invalid file/namespace")
	}
	ns := file.Stmts.StmtNamespace[0]
	if ns == nil || ns.Stmts == nil {
		t.Fatalf("nil ns stmts")
	}

	has := func(module, name string) bool {
		for _, d := range ns.Stmts.StmtExternalDependencies {
			if d.Namespace == module && d.ClassName == name {
				return true
			}
			if name == "" && d.Namespace == module {
				return true
			}
		}
		return false
	}

	if !has("./components", "") {
		t.Fatalf("missing dep: export * from './components'")
	}
	if !has("./bar", "ns") {
		t.Fatalf("missing dep: export * as ns from './bar'")
	}
	if !has("./foo", "a") {
		t.Fatalf("missing dep: export { a } from './foo'")
	}
	if !has("./foo", "b") {
		t.Fatalf("missing dep: export { b as c } from './foo'")
	}
	if !has("./types", "X") {
		t.Fatalf("missing dep: export type { X } from './types'")
	}
	for _, d := range ns.Stmts.StmtExternalDependencies {
		if d.Namespace == "" {
			t.Fatalf("local export `export { local }` must not be recorded as a dependency, got %+v", d)
		}
	}
}

const sampleInterface = `
interface Shape {
    area(): number;
    perimeter(): number;
}

class Circle implements Shape {
    constructor(private radius: number) {}

    area(): number {
        return Math.PI * this.radius ** 2;
    }

    perimeter(): number {
        return 2 * Math.PI * this.radius;
    }
}
`

func TestTypeScriptParser_TreeSitter_Interface(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleInterface)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	// Interface
	if len(file.Stmts.StmtInterface) < 1 {
		t.Fatalf("expected at least 1 interface, got %d", len(file.Stmts.StmtInterface))
	}
	found := false
	for _, itf := range file.Stmts.StmtInterface {
		if itf.Name != nil && itf.Name.Short == "Shape" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("interface Shape not found")
	}

	// Class Circle
	if len(ns.Stmts.StmtClass) < 1 {
		t.Fatalf("expected at least 1 class, got %d", len(ns.Stmts.StmtClass))
	}
	var circle *pb.StmtClass
	for _, c := range ns.Stmts.StmtClass {
		if c.Name != nil && c.Name.Short == "Circle" {
			circle = c
			break
		}
	}
	if circle == nil {
		t.Fatalf("class Circle not found")
	}

	// Methods on Circle: constructor, area, perimeter
	methodNames := map[string]bool{}
	for _, fn := range circle.Stmts.StmtFunction {
		if fn.Name != nil {
			methodNames[fn.Name.Short] = true
		}
	}
	for _, want := range []string{"area", "perimeter"} {
		if !methodNames[want] {
			t.Fatalf("method %q not found in Circle", want)
		}
	}
}

const sampleArrow = `
const greet = (name: string): string => {
    return "Hello, " + name + "!";
};

const double = (n: number) => n * 2;

class Service {
    handler = (req: Request) => {
        return new Response();
    };
}
`

func TestTypeScriptParser_TreeSitter_ArrowFunctions(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleArrow)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	// Look for greet function
	fnNames := map[string]bool{}
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short != "" {
			fnNames[fn.Name.Short] = true
		}
	}
	if !fnNames["greet"] {
		t.Fatalf("arrow function 'greet' not found in namespace functions, found: %v", fnNames)
	}

	// double might or might not be detected (expression body arrow)
	// Service class
	if len(ns.Stmts.StmtClass) < 1 {
		t.Fatalf("expected at least 1 class (Service), got %d", len(ns.Stmts.StmtClass))
	}
}

const sampleEnum = `
enum Direction {
    Up = "UP",
    Down = "DOWN",
    Left = "LEFT",
    Right = "RIGHT",
}
`

func TestTypeScriptParser_TreeSitter_Enum(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleEnum)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	if len(ns.Stmts.StmtClass) < 1 {
		t.Fatalf("expected enum as class, got %d classes", len(ns.Stmts.StmtClass))
	}
	found := false
	for _, c := range ns.Stmts.StmtClass {
		if c.Name != nil && c.Name.Short == "Direction" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("enum Direction not found as class")
	}
}

const sampleAbstract = `
abstract class Animal {
    abstract makeSound(): void;

    move(distance: number): void {
        console.log("Moving " + distance + "m");
    }
}
`

func TestTypeScriptParser_TreeSitter_AbstractClass(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleAbstract)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	if len(ns.Stmts.StmtClass) < 1 {
		t.Fatalf("expected 1 class (Animal), got %d", len(ns.Stmts.StmtClass))
	}
	cls := ns.Stmts.StmtClass[0]
	if cls.Name == nil || cls.Name.Short != "Animal" {
		t.Fatalf("expected Animal, got %+v", cls.Name)
	}

	// Method move should be detected
	found := false
	for _, fn := range cls.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "move" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("method move not found in Animal")
	}
}

const sampleTSX = `
import React from 'react';

interface Props {
    name: string;
}

const Greeting = ({ name }: Props) => {
    return <div>Hello, {name}!</div>;
};

export default Greeting;
`

func TestTypeScriptParser_TreeSitter_TSX(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleTSX)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.ProgrammingLanguage != "TypeScript" {
		t.Fatalf("expected TypeScript, got %q", file.ProgrammingLanguage)
	}

	// Interface Props
	if len(file.Stmts.StmtInterface) < 1 {
		t.Fatalf("expected at least 1 interface (Props)")
	}

	// Import React
	ns := file.Stmts.StmtNamespace[0]
	if len(ns.Stmts.StmtExternalDependencies) < 1 {
		t.Fatalf("expected at least 1 import (React)")
	}
}

const sampleNested = `
function outer(x: number): number {
    function inner(y: number): number {
        return y + 1;
    }
    return inner(x);
}
`

func TestTypeScriptParser_TreeSitter_NestedFunctions(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleNested)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	// outer should be found
	var outer *pb.StmtFunction
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "outer" {
			outer = fn
			break
		}
	}
	if outer == nil {
		t.Fatalf("function outer not found")
	}

	// inner should be nested inside outer
	found := false
	for _, fn := range outer.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "inner" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("nested function inner not found in outer")
	}
}

const sampleForLoops = `
function processItems(items: string[]): void {
    for (const item of items) {
        console.log(item);
    }
    for (const key in items) {
        console.log(key);
    }
}
`

func TestTypeScriptParser_TreeSitter_ForOfForIn(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleForLoops)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var fn *pb.StmtFunction
	for _, f := range ns.Stmts.StmtFunction {
		if f.Name != nil && f.Name.Short == "processItems" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatalf("function processItems not found")
	}
	if got := len(fn.Stmts.StmtLoop); got != 2 {
		t.Fatalf("expected 2 loops (for-of, for-in), got %d", got)
	}
}

const sampleDoWhile = `
function countdown(n: number): void {
    do {
        n--;
    } while (n > 0);
}
`

func TestTypeScriptParser_TreeSitter_DoWhile(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleDoWhile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var fn *pb.StmtFunction
	for _, f := range ns.Stmts.StmtFunction {
		if f.Name != nil && f.Name.Short == "countdown" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatalf("function countdown not found")
	}
	if got := len(fn.Stmts.StmtLoop); got != 1 {
		t.Fatalf("expected 1 loop (do-while), got %d", got)
	}
}

const sampleGenerics = `
function identity<T>(arg: T): T {
    return arg;
}

class Container<T> {
    private value: T;

    constructor(val: T) {
        this.value = val;
    }

    get(): T {
        return this.value;
    }
}
`

func TestTypeScriptParser_TreeSitter_Generics(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleGenerics)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	// identity function
	found := false
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "identity" {
			found = true
			// T should not appear as a parameter (it's a type parameter)
			for _, p := range fn.Parameters {
				if p.Name == "T" {
					t.Fatalf("type parameter T should not appear in function parameters")
				}
			}
			break
		}
	}
	if !found {
		t.Fatalf("function identity not found")
	}

	// Container class
	if len(ns.Stmts.StmtClass) < 1 {
		t.Fatalf("expected at least 1 class (Container)")
	}
}

const sampleDecorators = `
function Log(target: any, key: string, descriptor: PropertyDescriptor) {
    return descriptor;
}

class MyService {
    @Log
    greet(name: string): string {
        return "Hello, " + name;
    }
}
`

func TestTypeScriptParser_TreeSitter_Decorators(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleDecorators)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	// MyService class with greet method
	var svc *pb.StmtClass
	for _, c := range ns.Stmts.StmtClass {
		if c.Name != nil && c.Name.Short == "MyService" {
			svc = c
			break
		}
	}
	if svc == nil {
		t.Fatalf("class MyService not found")
	}
	found := false
	for _, fn := range svc.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("method greet not found in MyService")
	}
}

const sampleComments = `
// Single line comment
/* Multi-line
   comment */
/**
 * JSDoc comment
 */
function commented(): void {
    // inline comment
    const x = 1;
}
`

func TestTypeScriptParser_TreeSitter_Comments(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.LinesOfCode == nil {
		t.Fatalf("expected LinesOfCode to be set")
	}
	if file.LinesOfCode.CommentLinesOfCode < 5 {
		t.Fatalf("expected at least 5 comment lines, got %d", file.LinesOfCode.CommentLinesOfCode)
	}
}

func TestTypeScriptParser_TreeSitter_Operators(t *testing.T) {
	code := `
function calc(a: number, b: number): number {
    const sum = a + b;
    const diff = a - b;
    const isEqual = a === b;
    return sum * diff;
}
`
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, code)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var fn *pb.StmtFunction
	for _, f := range ns.Stmts.StmtFunction {
		if f.Name != nil && f.Name.Short == "calc" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatalf("function calc not found")
	}
	if len(fn.Operators) == 0 {
		t.Fatalf("expected operators to be extracted")
	}
	if len(fn.Operands) == 0 {
		t.Fatalf("expected operands to be extracted")
	}
}

// --- Runner tests ---

func TestTypeScriptRunner_Name(t *testing.T) {
	r := &TypeScriptRunner{}
	if r.Name() != "TypeScript" {
		t.Fatalf("expected TypeScript, got %q", r.Name())
	}
}

func TestTypeScriptRunner_Ensure(t *testing.T) {
	r := &TypeScriptRunner{}
	if err := r.Ensure(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestTypeScriptRunner_Finish(t *testing.T) {
	r := &TypeScriptRunner{}
	if err := r.Finish(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestTypeScriptRunner_IsTest_TestTs(t *testing.T) {
	code := `export function add(a: number, b: number): number { return a + b; }`
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/calculator.test.ts"
	if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	r := &TypeScriptRunner{}
	file, err := r.Parse(tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !file.IsTest {
		t.Fatalf("expected .test.ts to be detected as test")
	}
}

func TestTypeScriptRunner_IsTest_SpecTs(t *testing.T) {
	code := `export function add(a: number, b: number): number { return a + b; }`
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/calculator.spec.ts"
	if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	r := &TypeScriptRunner{}
	file, err := r.Parse(tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !file.IsTest {
		t.Fatalf("expected .spec.ts to be detected as test")
	}
}

func TestTypeScriptRunner_IsTest_TestTsx(t *testing.T) {
	code := `export const App = () => <div>test</div>;`
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/app.test.tsx"
	if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	r := &TypeScriptRunner{}
	file, err := r.Parse(tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !file.IsTest {
		t.Fatalf("expected .test.tsx to be detected as test")
	}
}

func TestTypeScriptRunner_IsTest_TestsDir(t *testing.T) {
	code := `export function add(a: number, b: number): number { return a + b; }`
	tmpDir := t.TempDir()
	testsDir := tmpDir + "/__tests__"
	if err := os.MkdirAll(testsDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	tmpFile := testsDir + "/calculator.ts"
	if err := os.WriteFile(tmpFile, []byte(code), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	r := &TypeScriptRunner{}
	file, err := r.Parse(tmpFile)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !file.IsTest {
		t.Fatalf("expected __tests__/ file to be detected as test")
	}
}

func TestTypeScriptRunner_IsTest_NormalFile(t *testing.T) {
	code := `export function add(a: number, b: number): number { return a + b; }`
	file, err := enginePkg.CreateTestFileWithCode(&TypeScriptRunner{}, code)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if file.IsTest {
		t.Fatalf("expected normal file NOT to be detected as test")
	}
}

func TestTypeScriptRunner_Parse_NonExistentFile(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := r.Parse("/nonexistent/file.ts")
	if err == nil {
		t.Fatalf("expected error for non-existent file")
	}
	if file == nil {
		t.Fatalf("expected non-nil file even on error")
	}
	if file.ProgrammingLanguage != "TypeScript" {
		t.Fatalf("expected TypeScript language on error file")
	}
}

const sampleMethodCalls = `
class Counter {
    private count: number = 0;

    increment(): void {
        this.count++;
        this.log("incremented");
        super.validate();
    }

    log(msg: string): void {
        console.log(msg);
    }
}
`

func TestTypeScriptParser_TreeSitter_MethodCalls(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleMethodCalls)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var counter *pb.StmtClass
	for _, c := range ns.Stmts.StmtClass {
		if c.Name != nil && c.Name.Short == "Counter" {
			counter = c
			break
		}
	}
	if counter == nil {
		t.Fatalf("class Counter not found")
	}

	var increment *pb.StmtFunction
	for _, fn := range counter.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "increment" {
			increment = fn
			break
		}
	}
	if increment == nil {
		t.Fatalf("method increment not found")
	}
	if len(increment.MethodCalls) == 0 {
		t.Fatalf("expected method calls to be extracted from increment")
	}
}

const sampleComplexSwitch = `
function handleStatus(status: number): string {
    switch (status) {
        case 200:
            return "OK";
        case 301:
        case 302:
            return "Redirect";
        case 404:
            return "Not Found";
        case 500:
            return "Server Error";
        default:
            return "Unknown";
    }
}
`

func TestTypeScriptParser_TreeSitter_ComplexSwitch(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleComplexSwitch)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var fn *pb.StmtFunction
	for _, f := range ns.Stmts.StmtFunction {
		if f.Name != nil && f.Name.Short == "handleStatus" {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatalf("function handleStatus not found")
	}
	if got := len(fn.Stmts.StmtDecisionSwitch); got != 1 {
		t.Fatalf("expected 1 switch, got %d", got)
	}
	// At least 5 cases (200, 301, 302, 404, 500) + default
	if got := len(fn.Stmts.StmtDecisionCase); got < 5 {
		t.Fatalf("expected at least 5 cases, got %d", got)
	}
}

const sampleDestructuring = `
function processOptions({ timeout, retries }: { timeout: number; retries: number }): void {
    console.log(timeout, retries);
}

function withRest(...args: number[]): number {
    return args.reduce((a, b) => a + b, 0);
}
`

func TestTypeScriptParser_TreeSitter_DestructuringAndRest(t *testing.T) {
	r := &TypeScriptRunner{}
	file, err := enginePkg.CreateTestFileWithCode(r, sampleDestructuring)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	ns := file.Stmts.StmtNamespace[0]

	var processOptions *pb.StmtFunction
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "processOptions" {
			processOptions = fn
			break
		}
	}
	if processOptions == nil {
		t.Fatalf("function processOptions not found")
	}
	// Should have parameters (destructured names)
	if len(processOptions.Parameters) < 1 {
		t.Fatalf("expected at least 1 parameter from destructuring, got %d", len(processOptions.Parameters))
	}

	var withRest *pb.StmtFunction
	for _, fn := range ns.Stmts.StmtFunction {
		if fn.Name != nil && fn.Name.Short == "withRest" {
			withRest = fn
			break
		}
	}
	if withRest == nil {
		t.Fatalf("function withRest not found")
	}
}
