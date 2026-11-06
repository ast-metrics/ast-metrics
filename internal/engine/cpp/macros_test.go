package cpp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Libraries decorate most of what they export with a macro the parser cannot
// expand. The declaration must read the way the compiler reads it once the
// macro is gone.
func TestCppDeclarationsDecoratedWithAMacroAreKept(t *testing.T) {
	file := parseCpp(t, `
namespace leveldb {
class LEVELDB_EXPORT Slice {
 public:
  int size() const { return n_; }
 private:
  int n_;
};
struct FMT_API Spec : Base { int a; };
class Q_DECL_EXPORT Widget final : public QObject { int a; };
SPDLOG_INLINE void logger::log(int level) { if (level) flush(); }
FMT_API void vprint(int x) { emit(x); }
inline FMT_CONSTEXPR20 void reset() { clear(); }
}
`)
	var classes []string
	for _, class := range file.Stmts.StmtClass {
		classes = append(classes, class.Name.Qualified)
	}
	assert.Equal(t, []string{"leveldb::Slice", "leveldb::Spec", "leveldb::Widget"}, classes)
	require.Len(t, file.Stmts.StmtClass[0].Stmts.StmtFunction, 1)
	assert.Equal(t, "leveldb::Slice::size", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Qualified)
	assert.Equal(t, []string{"n_"}, operandNames(file.Stmts.StmtClass[0].Operands))

	var functions []string
	for _, fn := range file.Stmts.StmtFunction {
		functions = append(functions, fn.Name.Qualified)
	}
	assert.Equal(t, []string{"leveldb::logger::log", "leveldb::vprint", "leveldb::reset"}, functions)
	assert.Len(t, file.Stmts.StmtFunction[0].Stmts.StmtDecisionIf, 1)
}

func TestCppMaskingKeepsEveryOffsetAndLeavesValidCodeAlone(t *testing.T) {
	src := []byte("class LEVELDB_EXPORT Slice {\n  int n_;\n};\nDWORD get() { return 1; }\nHANDLE open();\n")
	masked := maskSpecifierMacros(src)
	assert.Equal(t, len(src), len(masked))
	assert.Equal(t, "class                Slice {\n  int n_;\n};\nDWORD get() { return 1; }\nHANDLE open();\n", string(masked))
	assert.Equal(t, "LEVELDB_EXPORT", string(src[6:20]), "the source itself is not modified")

	clean := []byte("namespace a { class B {}; }\n")
	assert.Equal(t, clean, maskSpecifierMacros(clean))
}

// A real type may be written in capitals too: only the macro goes.
func TestCppMaskingSparesTypesWrittenInCapitals(t *testing.T) {
	cases := map[string]string{
		"OPENSSL_EXPORT EVP_MD_CTX *EVP_MD_CTX_new(void) { return 0; }\n": "               EVP_MD_CTX *EVP_MD_CTX_new(void) { return 0; }\n",
		"DWORD WINAPI ThreadProc(int p) { return 0; }\n":                    "DWORD        ThreadProc(int p) { return 0; }\n",
	}
	for src, expected := range cases {
		assert.Equal(t, expected, string(maskSpecifierMacros([]byte(src))))
	}

	file := parseCpp(t, "LIB_API Matrix<ROWS, COLS> identity() { return make(); }\n")
	require.Len(t, file.Stmts.StmtFunction, 1)
	assert.Equal(t, "identity", file.Stmts.StmtFunction[0].Name.Short)
}
