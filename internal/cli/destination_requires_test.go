package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func TestDestinationRequirementsReachStatusJSONAndHealth(t *testing.T) {
	tk := fixtureTicket("APP-42", "todo", "short")
	f := &fakeSource{tickets: []workfile.Ticket{tk}}
	code, out, errOut := invoke(t, f, "status", "APP-42", "--json", "--dir", "../../example/destination-requires")
	var report machineReport
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &report) != nil || len(report.Tickets) != 1 {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	routes := report.Tickets[0].Routes
	if len(routes) != 2 || routes[0].To != "ready" || routes[0].Available || len(routes[0].Checks) != 1 || routes[1].To != "done" || !routes[1].Available || len(routes[1].Checks) != 0 {
		t.Fatalf("wrong conditional gates: %+v", routes)
	}
	_, out, _ = invoke(t, f, "status", "APP-42", "--dir", "../../example/destination-requires")
	if !strings.Contains(out, "Move APP-42 to done.") {
		t.Fatalf("shortcut not actionable: %s", out)
	}
	pipeline, _, _ := strings.Cut(out, "PULL REQUESTS")
	for _, state := range []string{"todo", "ready", "done"} {
		if strings.Count(pipeline, state) != 1 {
			t.Fatalf("%s repeated in pipeline: %s", state, pipeline)
		}
	}
	f.tickets[0].State = "done"
	code, out, errOut = invoke(t, f, "health", "--json", "--dir", "../../example/destination-requires")
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &report) != nil || report.Summary["in_policy"] != 1 {
		t.Fatalf("shortcut was marked out of policy: %d %s", code, out)
	}
}
