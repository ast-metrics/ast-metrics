package cribelo

import (
	"fmt"
	"strings"
)

// The engine words what it finds: the facts, the reasons, the gate. These
// helpers keep that wording consistent with the reports built on the plan.

// Plural appends an s when n is not one.
func Plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Duration words a reading time: "45 min", "2 h", "2 h 05".
func Duration(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%d h", minutes/60)
	}
	return fmt.Sprintf("%d h %02d", minutes/60, minutes%60)
}

// Percent words a share: "33%", "13%" (a half rounds up).
func Percent(x float64) string { return fmt.Sprintf("%d%%", int(x*100+0.5)) }

// ShortName drops the class qualifier of a method for display.
func ShortName(name string) string {
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		return name[idx+2:]
	}
	return name
}

// Phrases are written with their identifiers and paths between backticks.
// Markdown keeps the marks, a terminal drops them.
func code(s string) string { return "`" + s + "`" }

func codeAll(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = code(w)
	}
	return out
}

func plain(marked string) string { return strings.ReplaceAll(marked, "`", "") }
