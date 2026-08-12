package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/elliotchance/orderedmap/v2"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"google.golang.org/protobuf/proto"
)

// stripQuotes removes content inside single or double quotes (non-escaped) to avoid counting comment tokens inside strings
func stripQuotes(s string) string {
	inSingle := false
	inDouble := false
	res := make([]rune, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' { // escape next
			if i+1 < len(s) {
				res = append(res, ' ')
				i++
			}
			continue
		}
		if !inDouble && c == '\'' {
			inSingle = !inSingle
			res = append(res, ' ')
			continue
		}
		if !inSingle && c == '"' {
			inDouble = !inDouble
			res = append(res, ' ')
			continue
		}
		if inSingle || inDouble {
			res = append(res, ' ')
			continue
		}
		res = append(res, rune(c))
	}
	return string(res)
}

func DumpProtobuf(file *pb.File, binPath string) error {
	out, err := proto.Marshal(file)
	if err != nil {
		return err
	}

	f, err := os.Create(binPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(out)
	if err != nil {
		return err
	}

	return nil
}

// FactoryStmts returns a new instance of Stmts
func FactoryStmts() *pb.Stmts {

	stmts := &pb.Stmts{}
	stmts.StmtDecisionIf = []*pb.StmtDecisionIf{}
	stmts.StmtDecisionSwitch = []*pb.StmtDecisionSwitch{}
	stmts.StmtDecisionCase = []*pb.StmtDecisionCase{}
	stmts.StmtLoop = []*pb.StmtLoop{}
	stmts.StmtFunction = []*pb.StmtFunction{}
	stmts.StmtClass = []*pb.StmtClass{}

	return stmts
}

func GetClassesInFile(file *pb.File) []*pb.StmtClass {
	var classes []*pb.StmtClass
	if file.Stmts == nil {
		return classes
	}

	seen := make(map[string]struct{})
	addClass := func(class *pb.StmtClass) {
		if class == nil {
			return
		}
		key := classDedupKey(class)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		classes = append(classes, class)
	}

	if file.Stmts.StmtNamespace != nil {
		for _, namespace := range file.Stmts.StmtNamespace {
			if namespace == nil || namespace.Stmts == nil {
				continue
			}
			for _, class := range namespace.Stmts.StmtClass {
				addClass(class)
			}
		}
	}
	for _, class := range file.Stmts.StmtClass {
		addClass(class)
	}
	return classes
}

func GetFunctionsInFile(file *pb.File) []*pb.StmtFunction {
	var functions []*pb.StmtFunction
	if file.Stmts == nil {
		return functions
	}

	seen := make(map[string]struct{})
	addFunction := func(function *pb.StmtFunction) {
		if function == nil {
			return
		}
		key := functionDedupKey(function)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		functions = append(functions, function)
	}

	if file.Stmts.StmtNamespace != nil {
		for _, namespace := range file.Stmts.StmtNamespace {
			if namespace == nil || namespace.Stmts == nil {
				continue
			}
			for _, function := range namespace.Stmts.StmtFunction {
				addFunction(function)
			}
		}
	}
	classes := GetClassesInFile(file)
	for _, class := range classes {
		if class.Stmts == nil {
			continue
		}

		for _, function := range class.Stmts.StmtFunction {
			addFunction(function)
		}
	}
	for _, function := range file.Stmts.StmtFunction {
		addFunction(function)
	}
	return functions
}

// GetFunctionsOutsideClassesInFile returns functions found at file/namespace level
// excluding functions already attached to classes.
func GetFunctionsOutsideClassesInFile(file *pb.File) []*pb.StmtFunction {
	var functions []*pb.StmtFunction
	if file == nil || file.Stmts == nil {
		return functions
	}

	classFunctions := make(map[string]struct{})
	classes := GetClassesInFile(file)
	for _, class := range classes {
		if class == nil || class.Stmts == nil {
			continue
		}
		for _, function := range class.Stmts.StmtFunction {
			if function == nil {
				continue
			}
			classFunctions[functionDedupKey(function)] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	addFunction := func(function *pb.StmtFunction) {
		if function == nil {
			return
		}
		key := functionDedupKey(function)
		if _, inClass := classFunctions[key]; inClass {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		functions = append(functions, function)
	}

	if file.Stmts.StmtNamespace != nil {
		for _, namespace := range file.Stmts.StmtNamespace {
			if namespace == nil || namespace.Stmts == nil {
				continue
			}
			for _, function := range namespace.Stmts.StmtFunction {
				addFunction(function)
			}
		}
	}

	for _, function := range file.Stmts.StmtFunction {
		addFunction(function)
	}

	return functions
}

func classDedupKey(class *pb.StmtClass) string {
	if class == nil {
		return ""
	}
	if class.Name != nil {
		if q := strings.TrimSpace(class.Name.Qualified); q != "" {
			return "q:" + q
		}
		if s := strings.TrimSpace(class.Name.Short); s != "" {
			return "s:" + s
		}
	}
	return fmt.Sprintf("ptr:%p", class)
}

func functionDedupKey(function *pb.StmtFunction) string {
	if function == nil {
		return ""
	}
	if function.Name != nil {
		if q := strings.TrimSpace(function.Name.Qualified); q != "" {
			return "q:" + q
		}
		if s := strings.TrimSpace(function.Name.Short); s != "" {
			return "s:" + s
		}
	}
	return fmt.Sprintf("ptr:%p", function)
}

// render as HTML
func HtmlChartLine(data *orderedmap.OrderedMap[string, float64], label string, id string) string {
	series := "["
	for _, key := range data.Keys() {
		value, _ := data.Get(key)
		series += "{ x: \"" + key + "\", y: " + fmt.Sprintf("%f", value) + "},"
	}
	series += "]"
	html := `
	<div id="` + id + `"></div>
	<script type="text/javascript">
var options = {
  colors: ["#1A56DB"],
  series: [
    {
      name: "` + label + `",
      color: "#1A56DB",
      data: ` + series + `,
    },
  ],
  chart: {
    type: "bar",
    height: "120px",
    fontFamily: "Inter, sans-serif",
    toolbar: {
      show: false,
    },
  },
  plotOptions: {
    bar: {
      horizontal: false,
      columnWidth: "70%",
      borderRadiusApplication: "end",
      borderRadius: 8,
    },
  },
  tooltip: {
    shared: true,
    intersect: false,
    style: {
      fontFamily: "Inter, sans-serif",
    },
  },
  states: {
    hover: {
      filter: {
        type: "darken",
        value: 1,
      },
    },
  },
  stroke: {
    show: true,
    width: 0,
    colors: ["transparent"],
  },
  grid: {
    show: false,
    strokeDashArray: 4,
    padding: {
      left: 2,
      right: 2,
      top: -14
    },
  },
  dataLabels: {
    enabled: false,
  },
  legend: {
    show: false,
  },
  xaxis: {
    floating: false,
    labels: {
      show: true,
      style: {
        fontFamily: "Inter, sans-serif",
        cssClass: 'text-xs font-normal fill-gray-500 dark:fill-gray-400'
      }
    },
    axisBorder: {
      show: false,
    },
    axisTicks: {
      show: false,
    },
  },
  yaxis: {
    show: false,
  },
  fill: {
    opacity: 1,
  },
}


if (document.getElementById("` + id + `") && typeof ApexCharts !== 'undefined') {
  const chart = new ApexCharts(document.getElementById("` + id + `"), options);
  chart.render();
}
</script>`
	return html
}

// render as HTML
func HtmlChartArea(data *orderedmap.OrderedMap[string, float64], label string, id string) string {

	values := "["
	keys := "["
	for _, key := range data.Keys() {
		value, _ := data.Get(key)
		values += fmt.Sprintf("%f", value) + ","
		keys += "\"" + key + "\","
	}
	values += "]"
	keys += "]"

	html := `
	<div id="` + id + `"></div>
	<script type="text/javascript">
	var options = {
		chart: {
		  height: "100%",
		  maxWidth: "100%",
		  type: "area",
		  fontFamily: "Inter, sans-serif",
		  dropShadow: {
			enabled: false,
		  },
		  toolbar: {
			show: false,
		  },
		},
		tooltip: {
		  enabled: true,
		  x: {
			show: false,
		  },
		},
		fill: {
		  type: "gradient",
		  gradient: {
			opacityFrom: 0.55,
			opacityTo: 0,
			shade: "#1C64F2",
			gradientToColors: ["#1C64F2"],
		  },
		},
		dataLabels: {
		  enabled: false,
		},
		stroke: {
		  width: 6,
		},
		grid: {
		  show: false,
		  strokeDashArray: 4,
		  padding: {
			left: 2,
			right: 2,
			top: 0
		  },
		},
		series: [
		  {
			name: "` + label + `",
			data: ` + values + `,
			color: "#1A56DB",
		  },
		],
		xaxis: {
		  categories: ` + keys + `,
		  labels: {
			show: false,
		  },
		  axisBorder: {
			show: false,
		  },
		  axisTicks: {
			show: false,
		  },
		},
		yaxis: {
		  show: false,
		},
	  }


if (document.getElementById("` + id + `") && typeof ApexCharts !== 'undefined') {
  const chart = new ApexCharts(document.getElementById("` + id + `"), options);
  chart.render();
}
</script>`
	return html
}

func CreateTestFileWithCode(parser Engine, fileContent string) (*pb.File, error) {
	tmpDir := os.TempDir()
	f, err := os.CreateTemp(tmpDir, "test-*.src")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write([]byte(fileContent)); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return parser.Parse(f.Name())
}

var regForNamespacePart = regexp.MustCompile("[^A-Za-z0-9]+")

// Keep only n levels in namespace
func ReduceDepthOfNamespace(namespace string, depth int) string {

	// if namespace starts with github.com, avoid using the dot separator
	if strings.HasPrefix(namespace, "github.com") {
		namespace = strings.Replace(namespace, "github.com", "githubcom", -1)
		depth += 1
	}

	separator := regForNamespacePart.FindString(namespace)
	parts := regForNamespacePart.Split(namespace, -1)

	if depth >= len(parts) {
		return strings.Replace(namespace, "githubcom", "github.com", -1)
	}

	result := ""
	for i := 0; i < depth; i++ {
		if i <= len(parts) {
			result += parts[i] + separator
		}
	}

	// revert the github.com replacement
	if strings.HasPrefix(namespace, "githubcom") {
		result = strings.Replace(result, "githubcom", "github.com", -1)
	}

	return strings.Trim(result, separator)
}

func SearchFilesByExtension(dirs []string, ext string) ([]string, error) {
	var files []string
	for _, dir := range dirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ext) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// in-memory cache for dependencies per file path, invalidated by file mtime
var depCache = struct {
	items map[string]struct {
		mtime time.Time
		deps  []*pb.StmtExternalDependency
	}
}{items: make(map[string]struct {
	mtime time.Time
	deps  []*pb.StmtExternalDependency
})}

// GetDependenciesInFile aggregates all dependencies found in a file across namespaces,
// classes (including extends/implements/uses), traits, and functions. It deduplicates
// the list and caches results per file path using modification time for invalidation.
func GetDependenciesInFile(file *pb.File) []*pb.StmtExternalDependency {
	if file == nil || file.Stmts == nil {
		return []*pb.StmtExternalDependency{}
	}

	// Try cache based on file path mtime if path is set
	var mtime time.Time
	if file.Path != "" {
		if fi, err := os.Stat(file.Path); err == nil {
			mtime = fi.ModTime()
			if c, ok := depCache.items[file.Path]; ok && c.mtime.Equal(mtime) && c.deps != nil {
				return c.deps
			}
		}
	}

	uniq := make(map[string]*pb.StmtExternalDependency)
	add := func(dep *pb.StmtExternalDependency) {
		if dep == nil {
			return
		}
		k := dep.Namespace + "|" + dep.ClassName + "|" + dep.FunctionName + "|" + dep.From
		if k == "|||" { // empty
			return
		}
		if _, ok := uniq[k]; ok {
			return
		}
		// Make a shallow copy to avoid accidental mutation
		cpy := *dep
		uniq[k] = &cpy
	}

	// 1) file-level externals
	for _, d := range file.Stmts.StmtExternalDependencies {
		add(d)
	}
	// 2) namespace-level externals
	for _, ns := range file.Stmts.StmtNamespace {
		if ns == nil || ns.Stmts == nil {
			continue
		}
		for _, d := range ns.Stmts.StmtExternalDependencies {
			add(d)
		}
	}
	// 3) classes/interfaces/traits and their externals
	classes := GetClassesInFile(file)
	for _, c := range classes {
		if c == nil {
			continue
		}
		// explicit externals attached to class stmts
		if c.Stmts != nil {
			for _, d := range c.Stmts.StmtExternalDependencies {
				add(d)
			}
		}
		from := ""
		if c.Name != nil {
			from = c.Name.Qualified
			if from == "" {
				from = c.Name.Short
			}
		}
		// extends / implements / uses as dependencies
		for _, p := range c.Extends {
			if p == nil {
				continue
			}
			add(&pb.StmtExternalDependency{Namespace: p.Qualified, From: from, ClassName: p.Short})
		}
		for _, p := range c.Implements {
			if p == nil {
				continue
			}
			add(&pb.StmtExternalDependency{Namespace: p.Qualified, From: from, ClassName: p.Short})
		}
		for _, p := range c.Uses {
			if p == nil {
				continue
			}
			add(&pb.StmtExternalDependency{Namespace: p.Qualified, From: from, ClassName: p.Short})
		}
	}
	// 4) function-level externals (top-level and in classes)
	funcs := GetFunctionsInFile(file)
	for _, f := range funcs {
		if f == nil {
			continue
		}
		from := ""
		if f.Name != nil {
			from = f.Name.Qualified
			if from == "" {
				from = f.Name.Short
			}
		}
		for _, n := range f.Externals {
			if n == nil {
				continue
			}
			ns := n.Qualified
			if ns == "" {
				ns = n.Short
			}
			add(&pb.StmtExternalDependency{Namespace: ns, From: from, ClassName: n.Short})
		}
		// Also account for explicit StmtExternalDependencies attached to function stmts if any
		if f.Stmts != nil {
			for _, d := range f.Stmts.StmtExternalDependencies {
				add(d)
			}
		}
	}

	// Build final list
	res := make([]*pb.StmtExternalDependency, 0, len(uniq))
	for _, v := range uniq {
		res = append(res, v)
	}

	// Save in cache
	if file.Path != "" && !mtime.IsZero() {
		depCache.items[file.Path] = struct {
			mtime time.Time
			deps  []*pb.StmtExternalDependency
		}{mtime: mtime, deps: res}
	}

	return res
}

func GetFirstStatementName(file *pb.File) string {
	if file.Stmts == nil {
		return ""
	}

	// we dome the same thing than previousliy (k := dep.Namespace + "|" + dep.ClassName + "|" + dep.FunctionName + ...)
	namespace := ""
	classname := ""
	functionname := ""

	var explorer func(*pb.Stmts) (string, string, string)

	explorer = func(stmts *pb.Stmts) (string, string, string) {
		if stmts == nil {
			return "", "", ""
		}

		// namespaces
		if stmts.StmtNamespace != nil && len(stmts.StmtNamespace) > 0 {
			firstNs := stmts.StmtNamespace[0]
			if firstNs != nil && firstNs.Stmts != nil {
				return explorer(firstNs.Stmts)
			}
		}

		// classes
		if stmts.StmtClass != nil && len(stmts.StmtClass) > 0 {
			firstClass := stmts.StmtClass[0]
			if firstClass != nil && firstClass.Name != nil {
				return "", firstClass.Name.Qualified, ""
			}
		}

		// functions
		if stmts.StmtFunction != nil && len(stmts.StmtFunction) > 0 {
			firstFunction := stmts.StmtFunction[0]
			if firstFunction != nil && firstFunction.Name != nil {
				return "", "", firstFunction.Name.Qualified
			}
		}

		return "", "", ""
	}

	if classname != "" {
		return classname // -> we use the Qualified name of the class if any
	}

	namespace, classname, functionname = explorer(file.Stmts)
	fullName := namespace + "|" + classname + "|" + functionname

	// trim leading and trailing pipes
	fullName = strings.Trim(fullName, "|")

	return fullName
}
