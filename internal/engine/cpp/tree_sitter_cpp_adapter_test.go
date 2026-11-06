package cpp

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCppControlFlowIncludesAndHalstead(t *testing.T) {
	file := parseCpp(t, `
#include <vector>
#include "relay.hpp"
int classify(const std::vector<int>& values, bool ready) {
    int total = 0;
    for (int value : values) {
        if (ready && value > 0 || value == 7) total += value;
        else if (!ready) total--;
    }
    int i = 0;
    while (i++ < 2) {}
    do { total++; } while (total < 3);
    switch (total) { case 1: break; case 2: break; default: break; }
    try { return ready ? total : 0; } catch (...) { return -1; }
}
`)
	require.Len(t, file.Stmts.StmtFunction, 1)
	fn := file.Stmts.StmtFunction[0]
	assert.Len(t, fn.Stmts.StmtDecisionIf, 2)
	assert.Len(t, fn.Stmts.StmtLoop, 3)
	assert.Len(t, fn.Stmts.StmtDecisionSwitch, 1)
	assert.Len(t, fn.Stmts.StmtDecisionCase, 2)
	assert.Len(t, fn.Stmts.StmtDecisionCatch, 1)
	assert.Len(t, fn.Stmts.StmtDecisionTernary, 1)
	assert.Len(t, fn.Stmts.StmtDecisionLogical, 2)
	require.Len(t, file.Stmts.StmtExternalDependencies, 2)
	assert.Equal(t, "vector", file.Stmts.StmtExternalDependencies[0].Namespace)
	assert.Equal(t, "relay.hpp", file.Stmts.StmtExternalDependencies[1].Namespace)
	ops := operatorNames(fn.Operators)
	for _, expected := range []string{"+=", ">", "&&", "||", "=", "return"} {
		assert.Contains(t, ops, expected)
	}
	for _, expected := range []string{"values", "ready", "total", "value"} {
		assert.Contains(t, operandNames(fn.Operands), expected)
	}
}

func TestCppEmbeddedModernSyntaxFixture(t *testing.T) {
	file := parseCpp(t, `
#include <Arduino.h>

class RelayController {
public:
    explicit RelayController(uint8_t pin) : pin_(pin) {}

    void update(bool enabled) {
        if (enabled && !active_) {
            digitalWrite(pin_, HIGH);
            active_ = true;
        } else if (!enabled && active_) {
            digitalWrite(pin_, LOW);
            active_ = false;
        }
    }

private:
    uint8_t pin_;
    bool active_{false};
};
`)
	require.Len(t, file.Stmts.StmtClass, 1)
	class := file.Stmts.StmtClass[0]
	assert.Equal(t, []string{"pin_", "active_"}, operandNames(class.Operands))
	require.Len(t, class.Stmts.StmtFunction, 2)
	update := class.Stmts.StmtFunction[1]
	assert.Equal(t, "update", update.Name.Short)
	assert.Len(t, update.Stmts.StmtDecisionIf, 2)
	assert.Len(t, update.Stmts.StmtDecisionLogical, 2)
	assert.Contains(t, operandNames(update.Operands), "active_")
}

func TestCppDeclaratorsAndModernSyntax(t *testing.T) {
	file := parseCpp(t, `
auto trailing(int value) -> int { return value; }
template <typename T> T identity(T&& value) { return static_cast<T&&>(value); }
struct Callable { int operator()(int x) const { return x; } };
`)
	assert.Contains(t, functionNames(file.Stmts.StmtFunction), "trailing")
	assert.Contains(t, functionNames(file.Stmts.StmtFunction), "identity")
	require.Len(t, file.Stmts.StmtClass, 1)
	require.Len(t, file.Stmts.StmtClass[0].Stmts.StmtFunction, 1)
	assert.Equal(t, "operator()", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Short)
}

func TestCppClassDependenciesFromTypesConstructionAndStaticUse(t *testing.T) {
	file := parseCpp(t, `
class Transport {};
class BaseController {};
class Logger { public: static Logger& instance(); };

class Controller : public BaseController {
public:
    Controller(Transport& transport) : transport_(transport) {}
    Transport* replacement() {
        auto next = new Transport();
        Logger::instance();
        return next;
    }
private:
    Transport& transport_;
};
`)
	require.Len(t, file.Stmts.StmtClass, 4)
	controller := file.Stmts.StmtClass[3]
	deps := controller.Stmts.StmtExternalDependencies
	depNames := make([]string, 0, len(deps))
	for _, dep := range deps {
		depNames = append(depNames, dep.ClassName)
		assert.Equal(t, controller.Name.Qualified, dep.From)
	}
	assert.ElementsMatch(t, []string{"BaseController", "Transport", "Logger"}, depNames)
}

func TestCppClassDependencyNoiseIsSkipped(t *testing.T) {
	file := parseCpp(t, `
template <typename T, typename Alloc>
class Holder {
public:
    void fill(const std::vector<T, Alloc>& items) { count_ = items.size(); }
    std::size_t size() const { return count_; }
private:
    std::size_t count_ = 0;
    FMT_CONSTEXPR int unused_ = 0;
};
`)
	holder := file.Stmts.StmtClass[0]
	for _, dep := range holder.Stmts.StmtExternalDependencies {
		assert.NotEqual(t, "T", dep.ClassName, "template parameters are not dependencies")
		assert.NotEqual(t, "Alloc", dep.ClassName, "template parameters are not dependencies")
		assert.NotEqual(t, "std", dep.ClassName, "std is not a dependency")
		assert.NotContains(t, dep.Namespace, "std::", "std-rooted references are not dependencies")
		assert.NotEqual(t, "FMT_CONSTEXPR", dep.ClassName, "macros are not dependencies")
	}
	assert.Empty(t, holder.Stmts.StmtExternalDependencies)
}

func TestCppClassMemberTemplateParametersAreNotDependencies(t *testing.T) {
	file := parseCpp(t, `
namespace app {
class Store {};
class Widget {
public:
    template <typename T> void load(const std::vector<T>& items) { target_.load(items); }
    template <typename... Args> void log(Args&&... args) { target_.log(args...); }
    void bind(Store* store) { target_ = store; }
private:
    Store* target_ = nullptr;
};
}
`)
	require.Len(t, file.Stmts.StmtClass, 2)
	widget := file.Stmts.StmtClass[1]
	for _, dep := range widget.Stmts.StmtExternalDependencies {
		assert.NotEqual(t, "T", dep.ClassName, "member template parameters are not dependencies")
		assert.NotEqual(t, "app::T", dep.Namespace, "member template parameters are not dependencies")
		assert.NotEqual(t, "Args", dep.ClassName, "member template parameter packs are not dependencies")
		assert.NotEqual(t, "app::Args", dep.Namespace, "member template parameter packs are not dependencies")
	}
	// The positive control: real dependencies from the same class still appear.
	depNames := make([]string, 0, len(widget.Stmts.StmtExternalDependencies))
	for _, dep := range widget.Stmts.StmtExternalDependencies {
		depNames = append(depNames, dep.Namespace)
	}
	assert.ElementsMatch(t, []string{"Store"}, depNames)
}

func TestCppClassDependenciesUseTemplateNameWithoutArguments(t *testing.T) {
	file := parseCpp(t, `
namespace other { template <typename Y> class Thing {}; }
class Consumer {
public:
    void store(other::Thing<int>& thing) { kept_ = &thing; }
private:
    other::Thing<int>* kept_;
};
`)
	consumer := file.Stmts.StmtClass[1]
	require.Len(t, consumer.Stmts.StmtExternalDependencies, 1)
	dep := consumer.Stmts.StmtExternalDependencies[0]
	assert.Equal(t, "other::Thing", dep.Namespace)
	assert.Equal(t, "Thing", dep.ClassName)
}

func TestCppReferencesAreKeptAsWritten(t *testing.T) {
	file := parseCpp(t, `
namespace fmt {
class formatter { public: int format() { return 1; } };
class context {
public:
    formatter& get() { return fmt_; }
private:
    formatter fmt_;
    detail::buffer<char> buffer_;
};
}
`)
	require.Len(t, file.Stmts.StmtClass, 2)
	context := file.Stmts.StmtClass[1]
	assert.Equal(t, "fmt::context", context.Name.Qualified)
	// C++ resolves a name from the scope it is written in outward, and only
	// the whole project knows which candidate exists: the engine keeps the
	// name as written, the analyzer looks it up
	var names []string
	for _, dep := range context.Stmts.StmtExternalDependencies {
		names = append(names, dep.Namespace)
		assert.Equal(t, "fmt::context", dep.From)
	}
	assert.Equal(t, []string{"formatter", "detail::buffer"}, names)
}

// The names a class uses are looked up from its scope outward: the enclosing
// namespace first, then its parents, the way the compiler does it.
func TestCppReferencesAreLookedUpFromTheEnclosingScopes(t *testing.T) {
	file := parseCpp(t, `
namespace app {
class Clock {};
namespace io {
class Clock {};
class Port { Clock* clock_; };
}
namespace net {
class Socket { Clock* clock_; io::Port* port_; };
}
}
`)
	analyzer.AnalyzeFile(file)
	analyzer.NewAggregator([]*pb.File{file}, nil).Aggregates()
	classes := map[string]*pb.StmtClass{}
	for _, class := range file.Stmts.StmtClass {
		classes[class.Name.Qualified] = class
	}
	require.Len(t, classes, 4)
	// io::Port sees io::Clock, net::Socket sees app::Clock and app::io::Port
	assert.Equal(t, int32(1), classes["app::io::Port"].Stmts.Analyze.Coupling.Efferent)
	assert.Equal(t, int32(2), classes["app::net::Socket"].Stmts.Analyze.Coupling.Efferent)
	assert.Equal(t, int32(1), classes["app::io::Clock"].Stmts.Analyze.Coupling.Afferent)
	assert.Equal(t, int32(1), classes["app::Clock"].Stmts.Analyze.Coupling.Afferent)
	assert.Equal(t, int32(1), classes["app::io::Port"].Stmts.Analyze.Coupling.Afferent)
}

// A .cpp defines the methods of a class declared in its header: what their
// bodies use belongs to that class.
func TestCppOutOfClassMethodBodiesCarryTheirDependencies(t *testing.T) {
	file := parseCpp(t, `
#include "engine.hpp"
namespace car {
void Engine::start() {
    Starter starter;
    Engine::warmUp();
    detail::log("start");
    starter.crank(new Battery());
}
}
`)
	var deps []string
	for _, dep := range engineDependencies(file) {
		if dep.ClassName != "" {
			deps = append(deps, dep.Namespace)
			assert.Equal(t, "car::Engine::start", dep.From)
		}
	}
	// the class itself and the namespace function are not dependencies
	assert.ElementsMatch(t, []string{"Starter", "Battery"}, deps)
}

func TestCppClassDependenciesDriveCouplingMetrics(t *testing.T) {
	file := parseCpp(t, `
namespace devices {
class Motor {};
class Controller {
    Motor* motor_;
public:
    explicit Controller(Motor* motor) : motor_(motor) {}
};
}
`)
	analyzer.AnalyzeFile(file)
	project := analyzer.NewAggregator([]*pb.File{file}, nil).Aggregates()
	controller := file.Stmts.StmtClass[1]
	motor := file.Stmts.StmtClass[0]
	require.NotNil(t, controller.Stmts.Analyze.Coupling)
	require.NotNil(t, motor.Stmts.Analyze.Coupling)
	assert.Equal(t, int32(1), controller.Stmts.Analyze.Coupling.Efferent)
	assert.Equal(t, int32(1), motor.Stmts.Analyze.Coupling.Afferent)
	assert.Greater(t, project.ByClass.EfferentCoupling.Sum, float64(0))
}

func engineDependencies(file *pb.File) []*pb.StmtExternalDependency {
	return engine.GetDependenciesInFile(file)
}

// A constructor and a destructor touch every attribute: kept in the graph,
// they would connect two responsibilities that share nothing else.
func TestCppConstructorAndDestructorAreLeftOutOfCohesion(t *testing.T) {
	file := parseCpp(t, `
class Example {
public:
    Example() : a_(0), b_(0) { a_ = 1; b_ = 2; }
    ~Example() { a_ = 0; b_ = 0; }
    int useA() { return a_ + 1; }
    int useB() { return b_ + 1; }
private:
    int a_;
    int b_;
};
`)
	analyzer.AnalyzeFile(file)
	class := file.Stmts.StmtClass[0]
	require.NotNil(t, class.Stmts.Analyze.ClassCohesion)
	assert.Equal(t, int32(2), *class.Stmts.Analyze.ClassCohesion.Lcom4)
}

// A function written on one line from the first column used to be measured
// from its first token, the return type, and came out with no Halstead
// symbol at all, so a maintainability index of a perfect one-liner.
func TestCppOneLineFunctionAtTheFirstColumnHasHalsteadSymbols(t *testing.T) {
	for _, source := range []string{
		"int g() { return value; }\n",
		"static int g() { return value; }\n",
		"  int g() { return value; }\n",
	} {
		file := parseCpp(t, source)
		analyzer.AnalyzeFile(file)
		g := file.Stmts.StmtFunction[len(file.Stmts.StmtFunction)-1]
		assert.Equal(t, "g", g.Name.Short, source)
		assert.Contains(t, operatorNames(g.Operators), "return", source)
		assert.Contains(t, operandNames(g.Operands), "value", source)
	}
}
