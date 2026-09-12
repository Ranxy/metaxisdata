//nolint:revive
package common

import (
	"testing"
	"time"
)

func TestEvalBindingConditionEmptyIsSatisfied(t *testing.T) {
	ok, err := EvalBindingCondition("", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("an empty condition should be satisfied")
	}
}

func TestEvalBindingConditionRequestTime(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	ok, err := EvalBindingCondition(`request.time > timestamp("2020-01-01T00:00:00Z")`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("condition that holds should evaluate to true")
	}

	ok, err = EvalBindingCondition(`request.time < timestamp("2020-01-01T00:00:00Z")`, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("condition that does not hold should evaluate to false")
	}
}

// A condition referencing a variable that is not bound cannot be decided.
// Returning true there would grant the binding globally, so it must be an error.
func TestEvalBindingConditionUnboundVariableFailsClosed(t *testing.T) {
	for _, expr := range []string{
		`resource.database == "hr"`,
		`resource.schema_name == "public"`,
		`resource.table_name.startsWith("secret")`,
	} {
		ok, err := EvalBindingCondition(expr, time.Now())
		if err == nil {
			t.Errorf("EvalBindingCondition(%q) returned (%v, nil), want an error", expr, ok)
		}
		if ok {
			t.Errorf("EvalBindingCondition(%q) must not report an undecidable condition as satisfied", expr)
		}
	}
}
