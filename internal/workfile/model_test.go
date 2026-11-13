package workfile

import "testing"

func TestStanding(t *testing.T) {
	fail := []Check{{Outcome: "fail"}}
	pass := []Check{{Outcome: "pass"}}
	for _, tc := range []struct {
		name string
		a    Assessment
		want Standing
	}{
		{"error", Assessment{Ticket: Ticket{Error: "boom"}}, CannotCheck},
		{"violation", Assessment{Earlier: fail, Routes: []RouteAssessment{{To: "a", Checks: pass}}}, OutOfPolicy},
		{"blocked", Assessment{Routes: []RouteAssessment{{To: "a", Checks: fail}}}, Blocked},
		{"needs work", Assessment{Routes: []RouteAssessment{{To: "a", Checks: pass}}}, NeedsWork},
		{"done", Assessment{}, Done},
	} {
		if got := tc.a.Standing(); got != tc.want {
			t.Errorf("%s: standing = %d, want %d", tc.name, got, tc.want)
		}
	}
}
