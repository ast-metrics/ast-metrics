package issue

// Severity represents the criticité of a rule outcome
// Values are indicative; when not detected, Unknown is used.
type Severity string

const (
	SeverityUnknown Severity = "unknown"
	SeverityLow     Severity = "low"
	SeverityMedium  Severity = "medium"
	SeverityHigh    Severity = "high"
)

// RequirementError is a structured error produced by rules
// Message: human-readable description (without severity tag prefix)
// Code: a stable code identifying error category (usually rule name)
// Severity: criticité
type RequirementError struct {
	Message  string
	Code     string
	Severity Severity
	// Line is the 1-based line in the concerned file where the violation
	// occurs. Zero means the rule is file-level (no specific line).
	Line int
	// File names the concerned file when the rule is a project rule: a
	// per-file rule leaves it empty, the evaluator already knows the file.
	// Project rules give a path relative to the working directory so the
	// baseline recognizes the entry from one machine to the next.
	File string
}
