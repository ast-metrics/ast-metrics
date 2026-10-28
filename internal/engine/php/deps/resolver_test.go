package deps

import (
	"reflect"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

func phpFile(path string, classes, interfaces []string) *pb.File {
	stmts := &pb.Stmts{}
	for _, class := range classes {
		stmts.StmtClass = append(stmts.StmtClass, &pb.StmtClass{Name: &pb.Name{Qualified: class}})
	}
	for _, itf := range interfaces {
		stmts.StmtInterface = append(stmts.StmtInterface, &pb.StmtInterface{Name: &pb.Name{Qualified: itf}})
	}
	return &pb.File{Path: path, ProgrammingLanguage: Language, Stmts: stmts}
}

func TestFileDependencyResolverResolvesClassesAndInterfaces(t *testing.T) {
	client := phpFile("/src/Client.php", []string{`App\Client`}, nil)
	contract := phpFile("/src/ClientInterface.php", nil, []string{`App\ClientInterface`})
	scoped := NewFileDependencyResolver().ForFiles([]*pb.File{client, contract})

	tests := map[string][]string{
		`App\ClientInterface`:               {contract.Path},
		`\App\Client`:                       {client.Path},
		`Psr\Http\Message\RequestInterface`: nil,
		`RuntimeException`:                  nil,
	}
	for name, want := range tests {
		targets, handled := scoped.Resolve(client, &pb.StmtExternalDependency{Namespace: name, ClassName: name})
		if !handled {
			t.Errorf("%q: every dependency of a PHP file is claimed", name)
		}
		if !reflect.DeepEqual(targets, want) {
			t.Errorf("%q: got %v, want %v", name, targets, want)
		}
	}
	if _, handled := scoped.Resolve(&pb.File{ProgrammingLanguage: "Java"}, &pb.StmtExternalDependency{}); handled {
		t.Error("a file of another language is not claimed")
	}
}

func TestFileDependencyResolverTellsTheStandardLibrary(t *testing.T) {
	teller := NewFileDependencyResolver().ForFiles(nil).(dependency.StandardLibraryTeller)
	for name, standard := range map[string]bool{
		"RuntimeException": true, `\Closure`: true, "PDO": true,
		`Monolog\Logger`: false, `Psr\Log\LoggerInterface`: false,
	} {
		if got := teller.IsStandardLibrary(name); got != standard {
			t.Errorf("IsStandardLibrary(%q) = %v, expected %v", name, got, standard)
		}
	}
}
