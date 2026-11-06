package cribelo

import (
	"fmt"
	"strings"
)

// reasons turns the strongest contributions into at most three short phrases,
// each carrying a number. A feature that did not push the score up is never
// mentioned.
func reasons(r *FileReport) []string {
	var out []string
	seen := map[string]bool{}
	add := func(key, phrase string) {
		if phrase == "" || seen[key] || len(out) >= 3 {
			return
		}
		seen[key] = true
		out = append(out, phrase)
	}
	for _, c := range r.Contributions {
		if c.Logit <= 0 {
			break
		}
		switch c.Feature {
		case FeatureClone:
			if r.Clone != nil {
				names := strings.SplitN(r.Clone.Function, " ~ ", 2)
				from, to := names[0], names[len(names)-1]
				add("clone", fmt.Sprintf("%s() %d%% similar to %s:%s()", shortName(from), int(r.Clone.Similarity*100+0.5), r.Clone.Path, shortName(to)))
			}
		case FeatureComplexityAdded, FeatureMaxComplexity:
			add("complexity", complexityPhrase(r))
		case FeatureAfferent:
			add("afferent", fmt.Sprintf("imported by %d %s", int(c.Value), plural(int(c.Value), "file")))
		case FeatureFixes90d:
			add("fixes", fmt.Sprintf("fixed %d× in 90 days", int(c.Value)))
		case FeatureChurn90d:
			add("churn", fmt.Sprintf("%d %s in 90 days", int(c.Value), plural(int(c.Value), "commit")))
		case FeatureLinesAdded, FeatureLinesDeleted:
			add("lines", linesPhrase(r.Added, r.Deleted))
		case FeatureNewFile:
			add("new", "new file")
		case FeatureUntested:
			add("untested", "no test touched")
		}
	}
	if len(out) == 0 {
		out = append(out, linesPhrase(r.Added, r.Deleted))
	}
	return out
}

func linesPhrase(added, deleted int) string {
	switch {
	case deleted == 0:
		return fmt.Sprintf("+%d lines", added)
	case added == 0:
		return fmt.Sprintf("\u2212%d lines", deleted)
	}
	return fmt.Sprintf("+%d/\u2212%d lines", added, deleted)
}

func complexityPhrase(r *FileReport) string {
	if len(r.Functions) == 0 {
		return ""
	}
	top := r.Functions[0]
	if top.growth() <= 0 && top.After < 2 {
		return ""
	}
	var phrase string
	if top.New {
		phrase = fmt.Sprintf("new %s() complexity %d", shortName(top.Name), top.After)
	} else if top.After != top.Before {
		phrase = fmt.Sprintf("%s() complexity %d → %d", shortName(top.Name), top.Before, top.After)
	} else {
		phrase = fmt.Sprintf("%s() complexity %d", shortName(top.Name), top.After)
	}
	grown := 0
	for _, fn := range r.Functions[1:] {
		if fn.growth() > 0 {
			grown++
		}
	}
	if grown > 0 {
		phrase += fmt.Sprintf(", +%d more", grown)
	}
	return phrase
}

// shortName drops the class qualifier of a method for display.
func shortName(name string) string {
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		return name[idx+2:]
	}
	return name
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
