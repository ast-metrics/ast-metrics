package java

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

func parseJava(t *testing.T, src string) *pb.File {
	t.Helper()
	result, err := engine.CreateTestFileWithCode(&JavaRunner{}, src)
	assert.Nil(t, err, "Expected no error, got %v", err)
	assert.NotNil(t, result)
	return result
}

func TestJavaBasicClassAndMethods(t *testing.T) {
	src := `
public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }

    public int sub(int a, int b) {
        return a - b;
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Incorrect number of classes")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Calculator", class.Name.Short)
	assert.Equal(t, "Calculator", class.Name.Qualified)
	assert.Equal(t, 2, len(class.Stmts.StmtFunction), "Incorrect number of methods")
	assert.Equal(t, "add", class.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "sub", class.Stmts.StmtFunction[1].Name.Short)
	assert.Equal(t, "Java", result.ProgrammingLanguage)
}

func TestJavaPackageQualifiedNames(t *testing.T) {
	src := `
package com.example.app;

public class Calculator {
    public int add(int a, int b) {
        return a + b;
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtNamespace))
	assert.Equal(t, "com.example.app", result.Stmts.StmtNamespace[0].Name.Qualified)
	assert.Equal(t, 1, len(result.Stmts.StmtClass))
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Calculator", class.Name.Short)
	assert.Equal(t, "com.example.app.Calculator", class.Name.Qualified)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
	assert.Equal(t, "com.example.app.Calculator.add", class.Stmts.StmtFunction[0].Name.Qualified)
}

func TestJavaNoPackage(t *testing.T) {
	src := `
class Foo {
    void bar() {}
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass))
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Foo", class.Name.Short)
	assert.Equal(t, "Foo", class.Name.Qualified, "Qualified name should equal short name in default package")
}

func TestJavaConstructorIsCountedAsFunction(t *testing.T) {
	src := `
public class Foo {
    private int x;

    public Foo(int x) {
        this.x = x;
    }

    public int get() {
        return x;
    }
}
`
	result := parseJava(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 2, len(class.Stmts.StmtFunction), "Constructor + method expected")
	assert.Equal(t, "Foo", class.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "get", class.Stmts.StmtFunction[1].Name.Short)
}

func TestJavaInterfaceDeclaration(t *testing.T) {
	src := `
package com.example;

public interface Shape {
    double area();
    double perimeter();
}
`
	result := parseJava(t, src)
	assert.Equal(t, 0, len(result.Stmts.StmtClass), "Interface should not be counted as class")
	assert.Equal(t, 1, len(result.Stmts.StmtInterface), "Incorrect number of interfaces")
	itf := result.Stmts.StmtInterface[0]
	assert.Equal(t, "Shape", itf.Name.Short)
	assert.Equal(t, "com.example.Shape", itf.Name.Qualified)
}

func TestJavaEnumDeclaration(t *testing.T) {
	src := `
public enum Color {
    RED, GREEN, BLUE;

    public String label() {
        return name().toLowerCase();
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Enum should be counted as a class")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Color", class.Name.Short)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Enum method should be counted")
	assert.Equal(t, "label", class.Stmts.StmtFunction[0].Name.Short)
}

func TestJavaRecordDeclaration(t *testing.T) {
	src := `
package com.example;

public record Point(int x, int y) {
    public int sum() {
        return x + y;
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Record should be counted as a class")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Point", class.Name.Short)
	assert.Equal(t, "com.example.Point", class.Name.Qualified)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
	assert.Equal(t, "sum", class.Stmts.StmtFunction[0].Name.Short)
}

func TestJavaSealedClass(t *testing.T) {
	src := `
public sealed class Shape permits Circle, Square {
    public void describe() {}
}

final class Circle extends Shape {}
final class Square extends Shape {}
`
	result := parseJava(t, src)
	assert.Equal(t, 3, len(result.Stmts.StmtClass), "Sealed class and its subclasses should be counted")
	assert.Equal(t, "Shape", result.Stmts.StmtClass[0].Name.Short)
	assert.Equal(t, 1, len(result.Stmts.StmtClass[0].Stmts.StmtFunction))
}

func TestJavaAnnotationTypeDeclaration(t *testing.T) {
	src := `
public @interface MyAnnotation {
    String value();
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Annotation type should be counted as a class-like container")
	assert.Equal(t, "MyAnnotation", result.Stmts.StmtClass[0].Name.Short)
}

func TestJavaNestedInnerClass(t *testing.T) {
	src := `
public class Outer {
    public void m() {}

    class Inner {
        public void n() {}
    }
}
`
	result := parseJava(t, src)
	// only the top-level class is attached to the file
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Only top-level class attached to file")
	outer := result.Stmts.StmtClass[0]
	assert.Equal(t, "Outer", outer.Name.Short)
	assert.Equal(t, 1, len(outer.Stmts.StmtClass), "Inner class should be nested in outer class")
	inner := outer.Stmts.StmtClass[0]
	assert.Equal(t, "Inner", inner.Name.Short)
	assert.Equal(t, 1, len(inner.Stmts.StmtFunction))
	assert.Equal(t, "n", inner.Stmts.StmtFunction[0].Name.Short)
	// namespace sees both classes
	assert.Equal(t, 2, len(result.Stmts.StmtNamespace[0].Stmts.StmtClass))
}

func TestJavaAnonymousClassNotCounted(t *testing.T) {
	src := `
public class Foo {
    public void start() {
        Runnable r = new Runnable() {
            public void run() {
                if (true) {
                    System.out.println("run");
                }
            }
        };
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Anonymous class must not be counted as a named class")
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Only the declared method belongs to the class")
	start := class.Stmts.StmtFunction[0]
	assert.Equal(t, "start", start.Name.Short)
	// the anonymous class' method is reached by recursion and nested under start()
	assert.Equal(t, 1, len(start.Stmts.StmtFunction), "Anonymous class method nested under enclosing method")
	run := start.Stmts.StmtFunction[0]
	assert.Equal(t, "run", run.Name.Short)
	assert.Equal(t, 1, len(run.Stmts.StmtDecisionIf), "Decisions inside anonymous class methods are counted")
}

func TestJavaImportsPlain(t *testing.T) {
	src := `
package com.example;

import java.util.List;
import java.util.Map;

public class Foo {
    List<String> items;
}
`
	result := parseJava(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 2, len(deps), "Incorrect number of dependencies")
	assert.Equal(t, "java.util", deps[0].Namespace)
	assert.Equal(t, "List", deps[0].ClassName)
	assert.Equal(t, "java.util", deps[1].Namespace)
	assert.Equal(t, "Map", deps[1].ClassName)
}

func TestJavaImportsWildcard(t *testing.T) {
	src := `
import java.util.*;

public class Foo {}
`
	result := parseJava(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 1, len(deps))
	assert.Equal(t, "java.util", deps[0].Namespace)
	assert.Equal(t, "", deps[0].ClassName, "Wildcard import has no class name")
}

func TestJavaImportsStatic(t *testing.T) {
	src := `
import static org.assertj.core.api.Assertions.assertThat;

public class Foo {}
`
	result := parseJava(t, src)
	deps := result.Stmts.StmtExternalDependencies
	assert.Equal(t, 1, len(deps))
	assert.Equal(t, "org.assertj.core.api.Assertions", deps[0].Namespace)
	assert.Equal(t, "assertThat", deps[0].ClassName)
}

func TestJavaIfElseIfElse(t *testing.T) {
	src := `
public class Foo {
    public String classify(int x) {
        if (x > 0) {
            return "positive";
        } else if (x == 0) {
            return "zero";
        } else if (x == -1) {
            return "minus one";
        } else {
            return "negative";
        }
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	// CountElseIfAsIf: each else-if is recorded as an if
	assert.Equal(t, 3, len(fn.Stmts.StmtDecisionIf), "if + 2 else-if recorded as ifs")
	assert.Equal(t, 0, len(fn.Stmts.StmtDecisionElseIf))
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionElse), "final else recorded")
}

func TestJavaDecisionsInsideElseIfBodiesAreCounted(t *testing.T) {
	src := `
public class Foo {
    public int deep(int x, int y) {
        if (x > 0) {
            return 1;
        } else if (x < 0) {
            if (y > 0) {
                return 2;
            }
            while (y < 0) {
                y++;
            }
        } else {
            for (int i = 0; i < y; i++) {
                y--;
            }
        }
        return 0;
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	// outer if + else-if as if + nested if inside the else-if body
	assert.Equal(t, 3, len(fn.Stmts.StmtDecisionIf), "Nested if inside else-if body must be counted")
	assert.Equal(t, 2, len(fn.Stmts.StmtLoop), "Loops inside else-if and else bodies must be counted")
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionElse))
}

func TestJavaSwitchColonStyle(t *testing.T) {
	src := `
public class Foo {
    public int sw(int x) {
        switch (x) {
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
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionSwitch))
	assert.Equal(t, 2, len(fn.Stmts.StmtDecisionCase), "2 cases; default is the fallback path")
}

func TestJavaSwitchExpressionArrowStyle(t *testing.T) {
	src := `
public class Foo {
    public int sw(int x) {
        return switch (x) {
            case 1 -> 10;
            case 2, 3 -> 20;
            default -> 0;
        };
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionSwitch))
	assert.Equal(t, 2, len(fn.Stmts.StmtDecisionCase), "2 case rules; the default rule is the fallback path")
}

func TestJavaForLoop(t *testing.T) {
	src := `
public class Foo {
    public void m() {
        for (int i = 0; i < 10; i++) {
            System.out.println(i);
        }
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestJavaEnhancedForLoop(t *testing.T) {
	src := `
import java.util.List;

public class Foo {
    public void m(List<String> items) {
        for (String s : items) {
            System.out.println(s);
        }
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestJavaWhileLoop(t *testing.T) {
	src := `
public class Foo {
    public void m(int x) {
        while (x > 0) {
            x--;
        }
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestJavaDoWhileLoop(t *testing.T) {
	src := `
public class Foo {
    public void m(int x) {
        do {
            x++;
        } while (x < 5);
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 1, len(fn.Stmts.StmtLoop))
}

func TestJavaNestedLoopsAndConditions(t *testing.T) {
	src := `
public class Foo {
    public void m(int n) {
        for (int i = 0; i < n; i++) {
            for (int j = 0; j < n; j++) {
                if (i == j) {
                    continue;
                }
            }
        }
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 2, len(fn.Stmts.StmtLoop), "Nested loops must both be counted")
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionIf), "If nested in loops must be counted")
}

func TestJavaGenericsParse(t *testing.T) {
	src := `
import java.util.List;
import java.util.Map;

public class Foo<T extends Comparable<T>> {
    public Map<String, List<Integer>> index(List<Map<String, Integer>> input) {
        return null;
    }
}
`
	result := parseJava(t, src)
	assert.Equal(t, 1, len(result.Stmts.StmtClass))
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, "Foo", class.Name.Short)
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
	assert.Equal(t, "index", class.Stmts.StmtFunction[0].Name.Short)
}

func TestJavaLambdaInsideMethod(t *testing.T) {
	src := `
import java.util.List;

public class Foo {
    public void m(List<String> items) {
        items.forEach(s -> {
            if (s.isEmpty()) {
                return;
            }
            System.out.println(s);
        });
    }
}
`
	result := parseJava(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction), "Lambda must not be counted as a named function")
	fn := class.Stmts.StmtFunction[0]
	assert.Equal(t, 0, len(fn.Stmts.StmtFunction), "Lambda is not a nested function")
	assert.Equal(t, 1, len(fn.Stmts.StmtDecisionIf), "Decisions inside lambda body count for the enclosing method")
}

func TestJavaTextBlock(t *testing.T) {
	src := `
public class Foo {
    public String m() {
        String json = """
            {
                "key": "value"
            }
            """;
        return json;
    }
}
`
	result := parseJava(t, src)
	class := result.Stmts.StmtClass[0]
	assert.Equal(t, 1, len(class.Stmts.StmtFunction))
	fn := class.Stmts.StmtFunction[0]
	assert.True(t, fn.LinesOfCode.LinesOfCode > 0, "LOC should be computed for methods with text blocks")
	assert.Equal(t, 0, len(fn.Stmts.StmtDecisionIf), "Text block content must not create decisions")
}

func TestJavaMethodParameters(t *testing.T) {
	src := `
import java.util.List;

public class Foo {
    public void m(String name, int count, List<String> items, int... rest) {
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, 4, len(fn.Parameters), "All parameters including varargs must be extracted")
	names := []string{}
	for _, p := range fn.Parameters {
		names = append(names, p.Name)
	}
	assert.ElementsMatch(t, []string{"name", "count", "items", "rest"}, names)
}

func TestJavaLocCloc(t *testing.T) {
	src := `package com.example;

/**
 * Documentation for the class.
 */
public class Foo {
    // counter
    private int count;

    public int bar() {
        int a = 1;
        // a full comment line
        int b = 2;

        /* block comment */
        return a + b;
    }
}
`
	result := parseJava(t, src)
	class := result.Stmts.StmtClass[0]
	fn := class.Stmts.StmtFunction[0]

	// method body spans from "public int bar() {" to "}" = 8 lines
	assert.Equal(t, int32(8), fn.LinesOfCode.LinesOfCode, "Method LOC")
	assert.Equal(t, int32(2), fn.LinesOfCode.CommentLinesOfCode, "Method CLOC: // line + /* */ line")

	// file-level comments: 3 docblock lines + // counter + // a full comment + /* block */ = 6
	assert.Equal(t, int32(6), result.LinesOfCode.CommentLinesOfCode, "File CLOC")
	assert.True(t, class.LinesOfCode.LinesOfCode > 0)
}

func TestJavaTestFileDetectionByAnnotation(t *testing.T) {
	src := `
package com.example;

import org.junit.jupiter.api.Test;

public class CalculatorCheck {
    @Test
    public void shouldAdd() {}
}
`
	result := parseJava(t, src)
	assert.True(t, result.IsTest, "File containing @Test/org.junit must be detected as test")
}

func TestJavaTestFileDetectionByName(t *testing.T) {
	r := JavaRunner{}
	assert.True(t, r.isTestFile("/project/src/CalculatorTest.java", []byte("class CalculatorTest {}")))
	assert.True(t, r.isTestFile("/project/src/CalculatorTests.java", []byte("class CalculatorTests {}")))
	assert.True(t, r.isTestFile("/project/src/TestCalculator.java", []byte("class TestCalculator {}")))
	assert.True(t, r.isTestFile("/project/src/test/java/com/example/Foo.java", []byte("class Foo {}")))
	assert.False(t, r.isTestFile("/project/src/main/java/Calculator.java", []byte("class Calculator {}")))
}

func TestJavaNotATestFile(t *testing.T) {
	src := `
public class Calculator {
    public int add(int a, int b) { return a + b; }
}
`
	result := parseJava(t, src)
	assert.False(t, result.IsTest, "Regular file must not be detected as test")
}

func TestJavaEmptyFile(t *testing.T) {
	result := parseJava(t, "")
	assert.Equal(t, 0, len(result.Stmts.StmtClass))
	assert.Equal(t, 0, len(result.Stmts.StmtFunction))
	assert.Equal(t, 1, len(result.Stmts.StmtNamespace), "A default namespace is always present")
}

func TestJavaInvalidSyntax(t *testing.T) {
	src := `
public class Foo {
    public int broken( {
        if (x > 0 {
}
`
	result := parseJava(t, src)
	// best-effort partial tree, no panic
	assert.NotNil(t, result.Stmts)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Class should still be detected in malformed source")
}

func TestJavaNonUTF8AndBOM(t *testing.T) {
	// UTF-8 BOM prefix
	bom := "\xEF\xBB\xBF" + `public class Foo { public void m() {} }`
	result := parseJava(t, bom)
	assert.NotNil(t, result.Stmts, "BOM file must not panic")

	// invalid UTF-8 bytes
	invalid := "public class Foo { void m() { String s = \"\xff\xfe\"; } }"
	result2 := parseJava(t, invalid)
	assert.NotNil(t, result2.Stmts, "Invalid UTF-8 must not panic")
}

func TestJavaOperatorsOperands(t *testing.T) {
	src := `
public class Foo {
    public int compute(int a, int b) {
        int c = a + b;
        int d = c * 2 - b / 4;
        boolean e = c >= d && a != b;
        return e ? c : d;
    }
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.True(t, len(fn.Operators) > 0, "Operators must be extracted")
	assert.True(t, len(fn.Operands) > 0, "Operands must be extracted")

	ops := map[string]bool{}
	for _, o := range fn.Operators {
		ops[o.Name] = true
	}
	for _, expected := range []string{"+", "*", "-", "/", ">=", "&&", "!="} {
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

func TestJavaMethodCallsExtracted(t *testing.T) {
	src := `
public class Foo {
    private int x;

    public void m() {
        this.helper();
        super.toString();
    }

    private void helper() {}
}
`
	result := parseJava(t, src)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	calls := []string{}
	for _, c := range fn.MethodCalls {
		calls = append(calls, c.Name)
	}
	assert.Contains(t, calls, "this.helper")
	assert.Contains(t, calls, "super.toString")
}

func TestJavaIntegrationAnalyzeFileCyclomatic(t *testing.T) {
	src := `
package com.example;

public class Foo {
    public int classify(int x) {
        if (x > 0) {
            return 1;
        } else if (x == 0) {
            return 0;
        } else {
            return -1;
        }
    }
}
`
	result := parseJava(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.NotNil(t, fn.Stmts.Analyze.Complexity.Cyclomatic)
	// 1 (function) + if + else-if (counted as if) = 3 ; empty else adds nothing
	assert.Equal(t, int32(3), *fn.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestJavaIntegrationAnalyzeFileSwitchAndLoops(t *testing.T) {
	src := `
public class Foo {
    public int m(int x) {
        for (int i = 0; i < x; i++) {
            x--;
        }
        switch (x) {
            case 1:
                return 1;
            default:
                return 0;
        }
    }
}
`
	result := parseJava(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	// 1 (function) + loop + case 1 = 3; the switch and its default are free
	assert.Equal(t, int32(3), *fn.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestJavaIntegrationHalsteadAndMaintainability(t *testing.T) {
	src := `
public class Foo {
    public int compute(int a, int b) {
        int c = a + b;
        if (c > 10) {
            c = c - b;
        }
        return c;
    }
}
`
	result := parseJava(t, src)
	analyzer.AnalyzeFile(result)
	fn := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.NotNil(t, fn.Stmts.Analyze.Volume.HalsteadVolume)
	assert.True(t, *fn.Stmts.Analyze.Volume.HalsteadVolume > 0, "Halstead volume must be > 0")
	class := result.Stmts.StmtClass[0]
	assert.NotNil(t, class.Stmts.Analyze.Maintainability.MaintainabilityIndex)
	assert.True(t, *class.Stmts.Analyze.Maintainability.MaintainabilityIndex > 0, "Maintainability index must be > 0")
}

func TestJavaRunnerInterface(t *testing.T) {
	r := JavaRunner{}
	assert.Equal(t, "Java", r.Name())
	assert.Nil(t, r.Ensure())
	assert.Nil(t, r.Finish())
}

func TestJavaRunnerIsRequiredWithoutFiles(t *testing.T) {
	r := JavaRunner{Configuration: &configuration.Configuration{}}
	assert.False(t, r.IsRequired(), "IsRequired must be false when no Java files found")
}

func TestJavaFileDiscoveryAndDumpAST(t *testing.T) {
	tmpDir := t.TempDir()
	src := `package com.example;

public class Discovered {
    public void m() {}
}
`
	assert.Nil(t, os.WriteFile(filepath.Join(tmpDir, "Discovered.java"), []byte(src), 0644))
	// a non-Java file must be ignored
	assert.Nil(t, os.WriteFile(filepath.Join(tmpDir, "ignored.txt"), []byte("nope"), 0644))

	config := configuration.NewConfiguration()
	assert.Nil(t, config.SetSourcesToAnalyzePath([]string{tmpDir}))

	r := JavaRunner{Configuration: config}
	assert.True(t, r.IsRequired(), "Java file must be discovered through the .java extension")

	files := r.DumpAST()
	assert.Equal(t, 1, len(files), "Exactly one Java file expected")
	assert.Equal(t, "Java", files[0].ProgrammingLanguage)
	assert.Equal(t, 1, len(files[0].Stmts.StmtClass))
	assert.Equal(t, "com.example.Discovered", files[0].Stmts.StmtClass[0].Name.Qualified)
}
