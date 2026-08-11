package csharp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func parseCSharp(t *testing.T, src string) *pb.File {
	t.Helper()
	result, err := engine.CreateTestFileWithCode(&CSharpRunner{}, src)
	assert.Nil(t, err, "Expected no error, got %v", err)
	assert.NotNil(t, result)
	return result
}

func TestCSharpBasicClassAndMethods(t *testing.T) {
	src := `
public class Calculator
{
    public int Add(int a, int b)
    {
        return a + b;
    }

    public int Sub(int a, int b)
    {
        return a - b;
    }
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Incorrect number of classes")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Calculator", class.Name.Short)
	assert.Equal(t, "Calculator", class.Name.Qualified)
	assert.Equal(t, 2, len(class.Stmts.StmtFunction), "Incorrect number of methods")
	assert.Equal(t, "Add", class.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "Sub", class.Stmts.StmtFunction[1].Name.Short)
	assert.Equal(t, "C#", result.ProgrammingLanguage)
}

func TestCSharpBlockNamespaceQualified(t *testing.T) {
	src := `
namespace App.Services
{
    public class UserService
    {
        public void Save() {}
    }
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtNamespace))
	assert.Equal(t, "App.Services", result.Stmts.StmtNamespace[0].Name.Qualified)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "UserService", class.Name.Short)
	assert.Equal(t, "App.Services.UserService", class.Name.Qualified)
	assert.Equal(t, "App.Services.UserService.Save", class.Stmts.StmtFunction[0].Name.Qualified)
}

func TestCSharpFileScopedNamespace(t *testing.T) {
	src := `
namespace App.Models;

public class Item
{
    public void Touch() {}
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, "App.Models", result.Stmts.StmtNamespace[0].Name.Qualified)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "App.Models.Item", class.Name.Qualified)
	assert.Equal(t, "App.Models.Item.Touch", class.Stmts.StmtFunction[0].Name.Qualified)
}

func TestCSharpNoNamespace(t *testing.T) {
	src := `
public class Foo
{
    public void Bar() {}
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Foo", class.Name.Short)
	assert.Equal(t, "Foo", class.Name.Qualified, "Qualified name should equal short name without namespace")
}

func TestCSharpConstructorCounted(t *testing.T) {
	src := `
public class Foo
{
    private int x;

    public Foo(int x)
    {
        this.x = x;
    }

    public int Get()
    {
        return x;
    }
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 2, len(class.Stmts.StmtFunction), "Constructor + method expected")
	assert.Equal(t, "Foo", class.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "Get", class.Stmts.StmtFunction[1].Name.Short)
}

func TestCSharpInterfaceDeclaration(t *testing.T) {
	src := `
namespace App;

public interface IShape
{
    double Area();
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 0, len(result.Stmts.StmtClass), "Interface should not be counted as class")
	assert.Equal(t, 1, len(result.Stmts.StmtInterface))
	itf := result.Stmts.StmtInterface[0]
	assert.Equal(t, "IShape", itf.Name.Short)
	assert.Equal(t, "App.IShape", itf.Name.Qualified)
}

func TestCSharpStructDeclaration(t *testing.T) {
	src := `
public struct Vec
{
    public int X;

    public int Sum()
    {
        return X;
    }
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Struct should be counted as a class")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Vec", class.Name.Short)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
}

func TestCSharpRecordDeclaration(t *testing.T) {
	src := `
namespace App;

public record Person(string Name);

public record struct Point(int X, int Y);

public record Employee(string Name)
{
    public string Display()
    {
        return Name;
    }
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 3, len(result.Stmts.StmtClass), "Records and record structs should be counted as classes")
	assert.Equal(t, "Person", result.Stmts.StmtClass[0].Name.Short)
	assert.Equal(t, "Point", result.Stmts.StmtClass[1].Name.Short)
	emp := result.Stmts.StmtClass[2]
	assert.Equal(t, "App.Employee", emp.Name.Qualified)
	assert.Equal(t, 1, len(emp.Stmts.StmtFunction), "Record body methods should be counted")
}

func TestCSharpEnumDeclaration(t *testing.T) {
	src := `
public enum Color
{
    Red,
    Green,
    Blue
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Enum should be counted as a class")
	assert.Equal(t, "Color", result.Stmts.StmtClass[0].Name.Short)
}

func TestCSharpPropertyAccessorsNotCountedAsMethods(t *testing.T) {
	src := `
public class Foo
{
    private int count;

    public int Auto { get; set; }

    public int WithBody
    {
        get { return count; }
        set { count = value; }
    }

    public void Method() {}
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Property accessors must not be counted as methods")
	assert.Equal(t, "Method", class.Stmts.StmtFunction[0].Name.Short)
}

func TestCSharpLocalFunctionCounted(t *testing.T) {
	src := `
public class Foo
{
    public int Outer(int x)
    {
        int Local(int a)
        {
            return a + 1;
        }
        return Local(x);
    }
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Only Outer belongs directly to the class")
	outer := class.Stmts.StmtFunction[0]
	assert.Equal(t, "Outer", outer.Name.Short)
	assert.Equal(t, 1, len(outer.Stmts.StmtFunction), "Local function nested under enclosing method")
	assert.Equal(t, "Local", outer.Stmts.StmtFunction[0].Name.Short)
}

func TestCSharpExpressionBodiedMethod(t *testing.T) {
	src := `
public class Foo
{
    private int count;

    public int Doubled() => count * 2;
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Expression-bodied method must be counted")
	fn := class.Stmts.StmtFunction[0]
	assert.Equal(t, "Doubled", fn.Name.Short)
	assert.True(t, fn.LinesOfCode.LinesOfCode > 0)
}

func TestCSharpUsingPlain(t *testing.T) {
	src := `
using System;
using System.Collections.Generic;

public class Foo {}
`
	result := parseCSharp(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 2, len(deps))
	assert.Equal(t, "System", deps[0].Namespace)
	assert.Equal(t, "", deps[0].ClassName)
	assert.Equal(t, "System.Collections.Generic", deps[1].Namespace)
}

func TestCSharpUsingStatic(t *testing.T) {
	src := `
using static System.Math;

public class Foo {}
`
	result := parseCSharp(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 1, len(deps))
	assert.Equal(t, "System.Math", deps[0].Namespace)
}

func TestCSharpGlobalUsing(t *testing.T) {
	src := `
global using System.Linq;

public class Foo {}
`
	result := parseCSharp(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 1, len(deps))
	assert.Equal(t, "System.Linq", deps[0].Namespace)
}

func TestCSharpUsingAlias(t *testing.T) {
	src := `
using Builder = System.Text.StringBuilder;

public class Foo {}
`
	result := parseCSharp(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 1, len(deps))
	assert.Equal(t, "System.Text.StringBuilder", deps[0].Namespace)
	assert.Equal(t, "Builder", deps[0].ClassName, "Alias name must be kept")
}

func TestCSharpIfElseIfElse(t *testing.T) {
	src := `
public class Foo
{
    public string Classify(int x)
    {
        if (x > 0)
        {
            return "positive";
        }
        else if (x == 0)
        {
            return "zero";
        }
        else if (x == -1)
        {
            return "minus one";
        }
        else
        {
            return "negative";
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	// CountElseIfAsIf: each else-if is recorded as an if
	assert.Equal(t, 3, len(fn.Stmts.StmtDecisionIf), "if + 2 else-if recorded as ifs")
	assert.Equal(t, 0, len(fn.Stmts.StmtDecisionElseIf))
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionElse), "final else recorded")
}

func TestCSharpDecisionsInsideElseIfBodiesAreCounted(t *testing.T) {
	src := `
public class Foo
{
    public int Deep(int x, int y)
    {
        if (x > 0)
        {
            return 1;
        }
        else if (x < 0)
        {
            if (y > 0)
            {
                return 2;
            }
            while (y < 0)
            {
                y++;
            }
        }
        else
        {
            for (int i = 0; i < y; i++)
            {
                y--;
            }
        }
        return 0;
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 3, len(fn.Stmts.StmtDecisionIf), "Nested if inside else-if body must be counted")
	assert.Equal(t, 2, len(fn.Stmts.StmtLoop), "Loops inside else-if and else bodies must be counted")
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionElse))
}

func TestCSharpSwitchStatement(t *testing.T) {
	src := `
public class Foo
{
    public int Sw(int x)
    {
        switch (x)
        {
            case 1:
                return 1;
            case 2:
                return 2;
            default:
                return 0;
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionSwitch))
	assert.Equal(t, 2, len(fn.Stmts.StmtDecisionCase), "2 cases; default is the fallback path")
}

func TestCSharpSwitchExpression(t *testing.T) {
	src := `
public class Foo
{
    public int Sw(int x) => x switch
    {
        1 => 10,
        2 or 3 => 20,
        _ => 0,
    };
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionSwitch), "switch expression counted as switch")
	assert.Equal(t, 2, len(fn.Stmts.StmtDecisionCase), "2 arms; the `_` arm is the fallback path")
}

func TestCSharpForLoop(t *testing.T) {
	src := `
public class Foo
{
    public void M()
    {
        for (int i = 0; i < 10; i++)
        {
            System.Console.WriteLine(i);
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestCSharpForeachLoop(t *testing.T) {
	src := `
public class Foo
{
    public void M(int[] items)
    {
        foreach (var s in items)
        {
            System.Console.WriteLine(s);
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestCSharpWhileLoop(t *testing.T) {
	src := `
public class Foo
{
    public void M(int x)
    {
        while (x > 0)
        {
            x--;
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestCSharpDoWhileLoop(t *testing.T) {
	src := `
public class Foo
{
    public void M(int x)
    {
        do
        {
            x++;
        } while (x < 5);
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestCSharpNestedLoopsAndConditions(t *testing.T) {
	src := `
public class Foo
{
    public void M(int n)
    {
        for (int i = 0; i < n; i++)
        {
            foreach (var x in new int[] { 1, 2 })
            {
                if (i == x)
                {
                    continue;
                }
            }
        }
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 2, len(fn.Stmts.StmtLoop), "Nested loops must both be counted")
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionIf), "If nested in loops must be counted")
}

func TestCSharpGenericsParse(t *testing.T) {
	src := `
using System.Collections.Generic;

public class Repository<T> where T : class
{
    public Dictionary<string, List<T>> Index(List<Dictionary<string, T>> input)
    {
        return null;
    }
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass))
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Repository", class.Name.Short)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
	assert.Equal(t, "Index", class.Stmts.StmtFunction[0].Name.Short)
}

func TestCSharpTopLevelStatements(t *testing.T) {
	src := `
using System;

Console.WriteLine("hello");
if (args.Length > 0)
{
    Console.WriteLine(args[0]);
}
`
	result := parseCSharp(t, src)
	assert.Equal(t, 0, len(result.Stmts.StmtClass))
	assert.Equal(t, 1, len(result.Stmts.StmtDecisionIf), "Top-level decisions are recorded at file level")
}

func TestCSharpMethodParameters(t *testing.T) {
	src := `
public class Foo
{
    public void M(string name, ref int x, out int y, int count = 3)
    {
        y = 0;
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 4, len(fn.Parameters))
	names := []string{}
	for _, p := range fn.Parameters {
		names = append(names, p.Name)
	}
	assert.ElementsMatch(t, []string{"name", "x", "y", "count"}, names, "ref/out modifiers and types must be skipped")
}

func TestCSharpLocCloc(t *testing.T) {
	src := `namespace App;

/// <summary>
/// Documentation.
/// </summary>
public class Foo
{
    public int Bar()
    {
        int a = 1;
        // a full comment line
        int b = 2;

        /* block comment */
        return a + b;
    }
}
`
	result := parseCSharp(t, src)
	class := result.Stmts.StmtClass[0]
	fn := class.Stmts.StmtFunction[0]

	// method body spans from "{" to "}" = 8 lines
	assert.Equal(t, int32(8), fn.LinesOfCode.LinesOfCode, "Method LOC")
	assert.Equal(t, int32(2), fn.LinesOfCode.CommentLinesOfCode, "Method CLOC: // line + /* */ line")

	// file-level comments: 3 xml-doc lines + // line + /* */ line = 5
	assert.Equal(t, int32(5), result.LinesOfCode.CommentLinesOfCode, "File CLOC")
	assert.True(t, class.LinesOfCode.LinesOfCode > 0)
}

func TestCSharpTestFileDetectionByAttribute(t *testing.T) {
	srcXunit := `
using Xunit;

public class CalculatorCheck
{
    [Fact]
    public void ShouldAdd() {}
}
`
	result := parseCSharp(t, srcXunit)
	assert.True(t, result.IsTest, "File containing [Fact]/using Xunit must be detected as test")

	srcNunit := `
using NUnit.Framework;

public class CalculatorCheck
{
    [Test]
    public void ShouldAdd() {}
}
`
	result2 := parseCSharp(t, srcNunit)
	assert.True(t, result2.IsTest, "File containing [Test]/using NUnit must be detected as test")
}

func TestCSharpTestFileDetectionByName(t *testing.T) {
	r := CSharpRunner{}
	assert.True(t, r.isTestFile("/project/CalculatorTest.cs", []byte("class CalculatorTest {}")))
	assert.True(t, r.isTestFile("/project/CalculatorTests.cs", []byte("class CalculatorTests {}")))
	assert.False(t, r.isTestFile("/project/Calculator.cs", []byte("class Calculator {}")))
}

func TestCSharpNotATestFile(t *testing.T) {
	src := `
public class Calculator
{
    public int Add(int a, int b) { return a + b; }
}
`
	result := parseCSharp(t, src)
	assert.False(t, result.IsTest)
}

func TestCSharpEmptyFile(t *testing.T) {
	result := parseCSharp(t, "")
	assert.Equal(t, 0, len(result.Stmts.StmtClass))
	assert.Equal(t, 0, len(result.Stmts.StmtFunction))
	assert.Equal(t, 1, len(result.Stmts.StmtNamespace), "A default namespace is always present")
}

func TestCSharpInvalidSyntax(t *testing.T) {
	// severely malformed source: tree-sitter-c-sharp error recovery yields an
	// empty result; the engine must not panic and must return a usable file
	src := `
public class Foo
{
    public int Broken( {
        if (x > 0 {
}
`
	result := parseCSharp(t, src)
	assert.NotNil(t, result.Stmts)
	assert.Equal(t, 1, len(result.Stmts.StmtNamespace))

	// mildly malformed source (missing semicolon): the class and its method
	// are still recovered
	src2 := `
public class Foo
{
    public int Ok() { return 1 }
}
`
	result2 := parseCSharp(t, src2)
	assert.Equal(t, 1, len(result2.Stmts.StmtClass), "Class should still be detected with a missing semicolon")
	assert.Equal(t, 1, len(result2.Stmts.StmtClass[0].Stmts.StmtFunction))
}

func TestCSharpNonUTF8AndBOM(t *testing.T) {
	bom := "\xEF\xBB\xBF" + `public class Foo { public void M() {} }`
	result := parseCSharp(t, bom)
	assert.NotNil(t, result.Stmts, "BOM file must not panic")

	invalid := "public class Foo { void M() { string s = \"\xff\xfe\"; } }"
	result2 := parseCSharp(t, invalid)
	assert.NotNil(t, result2.Stmts, "Invalid UTF-8 must not panic")
}

func TestCSharpOperatorsOperands(t *testing.T) {
	src := `
public class Foo
{
    public int Compute(int a, int b)
    {
        int c = a + b;
        int d = c * 2 - b / 4;
        bool e = c >= d && a != b;
        int f = a ?? b;
        return e ? c : d;
    }
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.True(t, len(fn.Operators) > 0, "Operators must be extracted")
	assert.True(t, len(fn.Operands) > 0, "Operands must be extracted")

	ops := map[string]bool{}
	for _, o := range fn.Operators {
		ops[o.Name] = true
	}
	for _, expected := range []string{"+", "*", "-", "/", ">=", "&&", "!=", "??"} {
		assert.True(t, ops[expected], "Operator %q must be detected", expected)
	}

	operands := map[string]bool{}
	for _, o := range fn.Operands {
		operands[o.Name] = true
	}
	for _, expected := range []string{"a", "b", "c", "d", "e"} {
		assert.True(t, operands[expected], "Operand %q must be detected", expected)
	}
}

func TestCSharpMethodCallsExtracted(t *testing.T) {
	src := `
public class Foo
{
    public void M()
    {
        this.Helper();
        base.ToString();
    }

    private void Helper() {}
}
`
	result := parseCSharp(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	calls := []string{}
	for _, c := range fn.MethodCalls {
		calls = append(calls, c.Name)
	}
	assert.Contains(t, calls, "this.Helper")
	assert.Contains(t, calls, "base.ToString")
}

func TestCSharpIntegrationAnalyzeFileCyclomatic(t *testing.T) {
	src := `
namespace App;

public class Foo
{
    public int Classify(int x)
    {
        if (x > 0)
        {
            return 1;
        }
        else if (x == 0)
        {
            return 0;
        }
        else
        {
            return -1;
        }
    }
}
`
	result := parseCSharp(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.NotNil(t, fn.Stmts.Analyze.Complexity.Cyclomatic)
	// 1 (function) + if + else-if (counted as if) = 3 ; empty else adds nothing
	assert.Equal(t, int32(3), *fn.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestCSharpIntegrationAnalyzeFileSwitchAndLoops(t *testing.T) {
	src := `
public class Foo
{
    public int M(int x)
    {
        for (int i = 0; i < x; i++)
        {
            x--;
        }
        switch (x)
        {
            case 1:
                return 1;
            default:
                return 0;
        }
    }
}
`
	result := parseCSharp(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	// 1 (function) + loop + case 1 = 3; the switch and its default are free
	assert.Equal(t, int32(3), *fn.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestCSharpIntegrationHalsteadAndMaintainability(t *testing.T) {
	src := `
public class Foo
{
    public int Compute(int a, int b)
    {
        int c = a + b;
        if (c > 10)
        {
            c = c - b;
        }
        return c;
    }
}
`
	result := parseCSharp(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.NotNil(t, fn.Stmts.Analyze.Volume.HalsteadVolume)
	assert.True(t, *fn.Stmts.Analyze.Volume.HalsteadVolume > 0, "Halstead volume must be > 0")
	class := result.Stmts.StmtClass[0]
	assert.NotNil(t, class.Stmts.Analyze.Maintainability.MaintainabilityIndex)
	assert.True(t, *class.Stmts.Analyze.Maintainability.MaintainabilityIndex > 0, "Maintainability index must be > 0")
}

func TestCSharpRunnerInterface(t *testing.T) {
	r := CSharpRunner{}
	assert.Equal(t, "C#", r.Name())
	assert.Nil(t, r.Ensure())
	assert.Nil(t, r.Finish())
}

func TestCSharpRunnerIsRequiredWithoutFiles(t *testing.T) {
	r := CSharpRunner{Configuration: &configuration.Configuration{}}
	assert.False(t, r.IsRequired(), "IsRequired must be false when no C# files found")
}

func TestCSharpFileDiscoveryAndDumpAST(t *testing.T) {
	tmpDir := t.TempDir()
	src := `namespace App;

public class Discovered
{
    public void M() {}
}
`
	assert.Nil(t, os.WriteFile(filepath.Join(tmpDir, "Discovered.cs"), []byte(src), 0644))
	// a non-C# file must be ignored
	assert.Nil(t, os.WriteFile(filepath.Join(tmpDir, "ignored.txt"), []byte("nope"), 0644))

	config := configuration.NewConfiguration()
	assert.Nil(t, config.SetSourcesToAnalyzePath([]string{tmpDir}))

	r := CSharpRunner{Configuration: config}
	assert.True(t, r.IsRequired(), "C# file must be discovered through the .cs extension")

	files := r.DumpAST()
	assert.Equal(t, 1, len(files), "Exactly one C# file expected")
	assert.Equal(t, "C#", files[0].ProgrammingLanguage)
	assert.Equal(t, 1, len(files[0].Stmts.StmtClass))
	assert.Equal(t, "App.Discovered", files[0].Stmts.StmtClass[0].Name.Qualified)
}
