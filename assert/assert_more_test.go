package assert

import "testing"

func TestFailureError(t *testing.T) {
	got := (Failure{Rule: "contains", Detail: "missing"}).Error()
	if got != "contains: missing" {
		t.Fatalf("Failure.Error() = %q", got)
	}
}
