package php

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
)

// The dependencies that shape an architecture are read off the tree: what a
// class extends, implements, uses as a trait, tests with instanceof or
// catches, resolved against the imports and the namespace like any other.
func TestStructuralDependenciesAreExtracted(t *testing.T) {
	source := `<?php
namespace App\Billing;

use App\Shared\Model;
use App\Shared\Contract\Payable;
use App\Shared\Concern\HasTimestamps;
use App\Shared\Errors\BillingError;

final class Invoice extends Model implements Payable, \JsonSerializable
{
    use HasTimestamps;

    public function check(object $other): bool
    {
        try {
            return $other instanceof Invoice;
        } catch (BillingError | \RuntimeException $e) {
            return false;
        }
    }
}
`
	file, err := engine.CreateTestFileWithCode(&PhpRunner{}, source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	seen := map[string]bool{}
	for _, dep := range engine.GetDependenciesInFile(file) {
		seen[dep.Namespace] = true
	}
	for _, expected := range []string{
		`App\Shared\Model`,                 // extends
		`App\Shared\Contract\Payable`,      // implements
		`JsonSerializable`,                 // implements, global
		`App\Shared\Concern\HasTimestamps`, // trait
		`App\Shared\Errors\BillingError`,   // catch
		`RuntimeException`,                 // catch, global
	} {
		if !seen[expected] {
			t.Errorf("expected a dependency on %s, got %v", expected, keysOf(seen))
		}
	}
	// instanceof on the class itself is a self reference, filtered downstream,
	// but the name is resolved in the namespace
	if !seen[`App\Billing\Invoice`] {
		t.Errorf("instanceof should be read, got %v", keysOf(seen))
	}
}

// A use statement at the top of the file is an import, not a dependency by
// itself; the same keyword inside a class body brings a trait in.
func TestTopLevelUseIsNotADependencyOnItsOwn(t *testing.T) {
	source := `<?php
namespace App\Billing;

use App\Shared\Unused;

final class Invoice
{
}
`
	file, err := engine.CreateTestFileWithCode(&PhpRunner{}, source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, dep := range engine.GetDependenciesInFile(file) {
		if dep.Namespace == `App\Shared\Unused` {
			t.Errorf("an unused import is not a dependency")
		}
	}
}

func keysOf(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
