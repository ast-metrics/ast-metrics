package cpp

import (
	sitter "github.com/smacker/go-tree-sitter"
	tsCpp "github.com/smacker/go-tree-sitter/cpp"
)

// maskSpecifierMacros blanks the macros a declaration is decorated with, so
// that the parser sees the declaration the compiler sees once they expand to
// an attribute or to nothing:
//
//	class LEVELDB_EXPORT Slice { ... };
//	SPDLOG_INLINE void logger::log(level lvl) { ... }
//	FMT_API void vprint(string_view fmt) { ... }
//
// Without a preprocessor, the first one reads as a function named Slice whose
// return type is a class named LEVELDB_EXPORT, and the other two hold an
// error before their name, which makes them indistinguishable from macro
// fallout. Libraries put such a macro on most of what they export, so leaving
// them in loses most of their code.
//
// Only what the parser could not place is touched: one SCREAMING_CASE name in
// the specifiers of a declaration holding a syntax error, and the name of a
// class directly followed by another name and a body. `DWORD x;` and
// `HANDLE open();` parse cleanly and are left alone. The macro is replaced by
// as many spaces, so that every line, column and byte offset stays where it
// was in the file.
func maskSpecifierMacros(src []byte) []byte {
	parser := sitter.NewParser()
	parser.SetLanguage(tsCpp.GetLanguage())
	masked := src
	// a pass blanks one macro per declaration, and masking one may reveal the
	// next: a declaration rarely carries more than two, and each pass parses
	// the whole file again
	for pass := 0; pass < 4; pass++ {
		tree := parser.Parse(nil, masked)
		if tree == nil || !tree.RootNode().HasError() {
			return masked
		}
		var ranges [][2]uint32
		collectSpecifierMacros(tree.RootNode(), masked, &ranges)
		if len(ranges) == 0 {
			return masked
		}
		if pass == 0 {
			masked = append([]byte(nil), src...)
		}
		for _, r := range ranges {
			for i := r[0]; i < r[1]; i++ {
				masked[i] = ' '
			}
		}
	}
	return masked
}

func collectSpecifierMacros(n *sitter.Node, src []byte, ranges *[][2]uint32) {
	switch n.Type() {
	case "function_definition", "declaration", "field_declaration":
		if specifier := n.ChildByFieldName("type"); specifier != nil && n.Type() == "function_definition" {
			// class MACRO Name { ... }: the class is named after the macro
			// and its real name reads as the name of a function
			switch specifier.Type() {
			case "class_specifier", "struct_specifier", "union_specifier":
				declarator := n.ChildByFieldName("declarator")
				if name := specifier.ChildByFieldName("name"); name != nil && declarator != nil &&
					declarator.Type() == "identifier" && specifier.ChildByFieldName("body") == nil &&
					isCppMacroName(nodeText(src, name)) {
					*ranges = append(*ranges, [2]uint32{name.StartByte(), name.EndByte()})
					return
				}
			}
		}
		if specifiersHoldAnError(n) {
			// one macro per declaration and per pass: the specifiers may
			// also hold a real type written in capitals, EVP_MD_CTX or
			// DWORD, which the next parse places once the macro is gone
			var candidates []*sitter.Node
			for i := 0; i < int(n.ChildCount()); i++ {
				switch n.FieldNameForChild(i) {
				case "declarator", "body", "default_value", "value":
					continue
				}
				collectMacroTokens(n.Child(i), src, &candidates)
			}
			if macro := pickMacro(candidates, src); macro != nil {
				*ranges = append(*ranges, [2]uint32{macro.StartByte(), macro.EndByte()})
			}
			return
		}
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		collectSpecifierMacros(n.NamedChild(i), src, ranges)
	}
}

// specifiersHoldAnError tells whether a declaration failed to parse in the
// place a decorating macro ends up in: a syntax error among its specifiers,
// or a declarator the parser had to complete with a missing token, which is
// how `LIB_API Matrix<R, C> identity()` comes out, as `Matrix<R, C>::identity`.
func specifiersHoldAnError(n *sitter.Node) bool {
	for i := 0; i < int(n.ChildCount()); i++ {
		switch n.FieldNameForChild(i) {
		case "body":
			continue
		case "declarator":
			if holdsAMissingToken(n.Child(i)) {
				return true
			}
			continue
		}
		if child := n.Child(i); child.IsError() {
			return true
		}
	}
	return false
}

func holdsAMissingToken(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if n.IsMissing() {
		return true
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if holdsAMissingToken(n.Child(i)) {
			return true
		}
	}
	return false
}

// callingConventions are the macros written between the return type and the
// name, `DWORD WINAPI ThreadProc(...)`: where one appears, it is the macro
// and the name before it is the type.
var callingConventions = map[string]bool{
	"WINAPI": true, "WINAPIV": true, "APIENTRY": true, "CALLBACK": true, "PASCAL": true,
	"CDECL": true, "STDCALL": true, "STDMETHODCALLTYPE": true, "STDAPICALLTYPE": true,
	"NTAPI": true, "JNICALL": true, "EGLAPIENTRY": true, "GLAPIENTRY": true, "SDLCALL": true,
}

// pickMacro chooses the capitalized name to blank among those of the
// specifiers: a calling convention if there is one, else the first one, the
// place an export macro is written.
func pickMacro(candidates []*sitter.Node, src []byte) *sitter.Node {
	for _, candidate := range candidates {
		if callingConventions[nodeText(src, candidate)] {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

func collectMacroTokens(n *sitter.Node, src []byte, candidates *[]*sitter.Node) {
	if n == nil || n.Type() == "template_argument_list" {
		// a template argument is a type or a value, never a decoration
		return
	}
	if n.ChildCount() == 0 {
		switch n.Type() {
		case "identifier", "type_identifier":
			if isCppMacroName(nodeText(src, n)) {
				*candidates = append(*candidates, n)
			}
		}
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		collectMacroTokens(n.Child(i), src, candidates)
	}
}
