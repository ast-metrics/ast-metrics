package dependency

import "strings"

// IsRelative tells whether a module is spelled from the importing file rather
// than from a root: "./x" and "../x" in TypeScript, ".x" in Python, "self::x",
// "super::x" and "crate::x" in Rust. Such a module is never a library: when it
// resolves to nothing, the import is broken or its target was left out of the
// analysis.
func IsRelative(module string) bool {
	if strings.HasPrefix(module, ".") {
		return true
	}
	for _, root := range [...]string{"crate", "self", "super"} {
		if module == root || strings.HasPrefix(module, root+"::") {
			return true
		}
	}
	return false
}
