package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func graphFixture() (workfile.Policy, workfile.Assessment) {
	p := workfile.Policy{States: []string{"todo", "ready", "review", "done"}, Transitions: map[string]workfile.Transition{
		"todo":  {To: []string{"ready", "done"}, RequiresByDestination: map[string][]string{"ready": {"refinement"}}},
		"ready": {To: []string{"review"}}, "review": {Routes: map[string][]string{"ready": {}, "done": {}}}, "done": {End: true},
	}}
	a := workfile.Assessment{Ticket: workfile.Ticket{State: "todo"}, Routes: []workfile.RouteAssessment{{To: "ready", Checks: []workfile.Check{{Gate: "refinement", Outcome: "fail"}}}, {To: "done"}}}
	return p, a
}
func plainRow(r row) string {
	var b strings.Builder
	for _, c := range r {
		b.WriteString(c.text)
	}
	return b.String()
}
func pipelineText(v view, p workfile.Policy, a workfile.Assessment) string {
	var b strings.Builder
	for _, r := range v.pipeline(p, a) {
		b.WriteString(strings.TrimRight(v.render(r), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
func assertSingleStates(t *testing.T, p workfile.Policy, text string) {
	t.Helper()
	for _, state := range p.States {
		if got := len(regexp.MustCompile(`\b`+regexp.QuoteMeta(state)+`\b`).FindAllStringIndex(text, -1)); got != 1 {
			t.Fatalf("state %s rendered %d times:\n%s", state, got, text)
		}
	}
}
func TestApprovedHorizontalPipelineLayout(t *testing.T) {
	p, a := graphFixture()
	a.Ticket.State = ""
	got := pipelineText(view{width: 60}, p, a)
	want := " ┌──────────────→──────────────┐\n │                             ↓\ntodo ──→ ready ──→ review ──→ done\n           ↑         │\n           └────←────┘\n"
	if got != want {
		t.Fatalf("approved layout changed:\n%s\nwant:\n%s", got, want)
	}
}
func TestHorizontalPipelineFitsAndNeverRepeatsStates(t *testing.T) {
	p, a := graphFixture()
	for _, width := range []int{40, 60, 140} {
		v := view{width: width}
		text := pipelineText(v, p, a)
		assertSingleStates(t, p, text)
		t.Logf("%d columns:\n%s", width, text)
		if !strings.Contains(text, "←") || !strings.Contains(text, "↑") || !strings.Contains(text, "↓") || !strings.Contains(text, "todo*") {
			t.Fatalf("arrows or current marker missing: %s", text)
		}
		chainFound := false
		for _, r := range v.pipeline(p, a) {
			if rowWidth(r) > v.innerWidth() {
				t.Fatalf("overflow: %q", plainRow(r))
			}
			chainFound = chainFound || strings.Contains(plainRow(r), "todo*") && strings.Contains(plainRow(r), "done")
		}
		if !chainFound {
			t.Fatal("state labels are not on one horizontal chain")
		}
	}
}
func TestPipelineLanesAndArrowEndpointsMatchPolicy(t *testing.T) {
	p, a := graphFixture()
	edges := pipelineEdges(p, a)
	if len(edges) != 5 {
		t.Fatalf("wrong edge count: %d", len(edges))
	}
	for i, e := range edges {
		if e.to == e.from+1 {
			if e.lane != -1 {
				t.Fatal("adjacent move drawn as a shortcut")
			}
			continue
		}
		if e.lane < 0 {
			t.Fatal("shortcut/return has no lane")
		}
		for _, other := range edges[i+1:] {
			if other.lane == e.lane && other.backward() == e.backward() && e.low() <= other.high() && other.low() <= e.high() {
				t.Fatal("overlapping arcs share a lane")
			}
		}
	}
	anchors := []int{1, 11, 21, 31}
	for _, above := range []bool{true, false} {
		rows := pipelineArcs(edges, anchors, 34, above)
		baseline := 0
		arrow := '↑'
		if above {
			baseline = len(rows) - 1
			arrow = '↓'
		}
		chars := []rune(plainRow(rows[baseline]))
		for state, x := range anchors {
			expected := false
			for _, e := range edges {
				expected = expected || e.lane >= 0 && e.backward() != above && e.to == state
			}
			if (chars[x] == arrow) != expected {
				t.Fatalf("invented or missing arrow at stage %d", state)
			}
		}
	}
}
func TestOverlappingShortcutsAndReturnsKeepDirectionArrows(t *testing.T) {
	p, a := graphFixture()
	p.Transitions["todo"] = workfile.Transition{To: []string{"ready", "review", "done"}}
	p.Transitions["ready"] = workfile.Transition{To: []string{"review", "done"}}
	p.Transitions["done"] = workfile.Transition{To: []string{"todo", "ready"}}
	edges := pipelineEdges(p, a)
	anchors := []int{1, 11, 21, 31}
	for _, above := range []bool{true, false} {
		rows := pipelineArcs(edges, anchors, 34, above)
		for _, e := range edges {
			if e.lane < 0 || e.backward() == above {
				continue
			}
			y := 1 + e.lane*2
			arrow := "←"
			if above {
				y = len(rows) - 2 - e.lane*2
				arrow = "→"
			}
			chars := []rune(plainRow(rows[y]))
			if !strings.Contains(string(chars[anchors[e.low()]+1:anchors[e.high()]]), arrow) {
				t.Fatalf("direction lost for %d -> %d: %s", e.from, e.to, plainRow(rows[y]))
			}
		}
	}
}

func TestPipelineDoesNotInventAdjacentMoves(t *testing.T) {
	p := workfile.Policy{States: []string{"todo", "ready", "blocked", "review", "done"}, Transitions: map[string]workfile.Transition{
		"todo": {To: []string{"ready"}}, "ready": {To: []string{"blocked", "review"}}, "blocked": {To: []string{"ready"}}, "review": {To: []string{"done"}}, "done": {End: true},
	}}
	text := pipelineText(view{width: 140}, p, workfile.Assessment{})
	if !strings.Contains(text, "blocked     review") || strings.Contains(text, "blocked ──→ review") {
		t.Fatalf("workflow order invented an edge:\n%s", text)
	}
	assertSingleStates(t, p, text)
}
func TestWidePipelineUsesHorizontalStripsAndNumberedLinks(t *testing.T) {
	p, a := graphFixture()
	for i := 0; i < 12; i++ {
		p.States = append(p.States, fmt.Sprintf("stage_%d", i))
	}
	for _, state := range p.States {
		var targets []string
		for _, target := range p.States {
			if target != state {
				targets = append(targets, target)
			}
		}
		p.Transitions[state] = workfile.Transition{To: targets}
	}
	for _, width := range []int{40, 60} {
		v := view{width: width}
		text := pipelineText(v, p, a)
		assertSingleStates(t, p, text)
		if !strings.Contains(text, "Links use stage numbers.") || !strings.Contains(text, "↩") || !strings.Contains(text, "1.todo* → 2.ready") {
			t.Fatalf("horizontal fallback lost links:\n%s", text)
		}
		for _, r := range v.pipeline(p, a) {
			if rowWidth(r) > v.innerWidth() {
				t.Fatalf("overflow: %q", plainRow(r))
			}
		}
	}
}
func TestPipelinePlainAndStyledBlocksFit(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	p, a := graphFixture()
	for _, width := range []int{40, 60} {
		for _, color := range []bool{false, true} {
			var out bytes.Buffer
			v := view{out: &out, width: width, color: color}
			v.lane(muted, styled("PIPELINE", "1"), v.pipeline(p, a), "")
			text := out.String()
			if !color && strings.Contains(text, "\x1b") {
				t.Fatal("plain graph contains escapes")
			}
			if color && (!strings.Contains(text, "\x1b[1;31mready") || !strings.Contains(text, "\x1b[1;32mdone")) {
				t.Fatalf("readiness colours lost:\n%s", text)
			}
			assertSingleStates(t, p, terminalEscapes.ReplaceAllString(text, ""))
			for _, line := range strings.Split(terminalEscapes.ReplaceAllString(text, ""), "\n") {
				if rowWidth(row{plain(line)}) > width {
					t.Fatalf("block overflow: %q", line)
				}
			}
		}
	}
}
func TestUnicodePipelineAlignmentAndNarrowFallback(t *testing.T) {
	p, a := graphFixture()
	p.States[1] = "実装"
	p.Transitions["todo"] = workfile.Transition{To: []string{"実装", "done"}}
	p.Transitions["実装"] = workfile.Transition{To: []string{"review"}}
	p.Transitions["review"] = workfile.Transition{To: []string{"done", "実装"}}
	a.Ticket.State = "実装"
	for _, width := range []int{20, 40, 60, 140} {
		v := view{width: width}
		text := pipelineText(v, p, a)
		if strings.Count(text, "実装") != 1 {
			t.Fatalf("unicode label lost or repeated: %s", text)
		}
		for _, r := range v.pipeline(p, a) {
			if rowWidth(r) > v.innerWidth() {
				t.Fatalf("unicode overflow: %q", plainRow(r))
			}
		}
	}
}
func TestDestinationRequiresPipelineExample(t *testing.T) {
	w, err := workfile.Load("../../example/destination-requires")
	if err != nil {
		t.Fatal(err)
	}
	a := w.Assess(fixtureTicket("APP-42", "todo", "short"))
	text := pipelineText(view{width: 60}, w.Policy, a)
	assertSingleStates(t, w.Policy, text)
	t.Log("\n" + text)
	if a.Routes[0].Available() || !a.Routes[1].Available() {
		t.Fatal("destination-specific readiness lost")
	}
}
func TestLinearPipelineStaysCompact(t *testing.T) {
	p := workfile.Policy{States: []string{"todo", "ready", "done"}, Transitions: map[string]workfile.Transition{"todo": {To: []string{"ready"}}, "ready": {To: []string{"done"}}, "done": {End: true}}}
	a := workfile.Assessment{Ticket: workfile.Ticket{State: "ready"}}
	rows := (view{width: 100}).pipeline(p, a)
	if len(rows) != 2 || plainRow(rows[0]) != "todo ──→ ready* ──→ done" || plainRow(rows[1]) != "* current stage" {
		t.Fatalf("linear pipeline: %+v", rows)
	}
}
