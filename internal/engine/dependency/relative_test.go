package dependency

import "testing"

func TestIsRelative(t *testing.T) {
	relative := []string{".", "..", "./util", "../shared/util", ".models", "..shared", "crate", "crate::shared", "self::child", "super::super::root"}
	for _, module := range relative {
		if !IsRelative(module) {
			t.Errorf("%q should be relative", module)
		}
	}
	absolute := []string{"react", "@types/node", "fmt", "github.com/x/y", "std::collections", "crates_io_api", "selfie::x", "org.apache.logging.log4j", `Monolog\Logger`}
	for _, module := range absolute {
		if IsRelative(module) {
			t.Errorf("%q should not be relative", module)
		}
	}
}
