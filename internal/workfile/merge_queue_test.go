package workfile

import "testing"

func TestMergeQueueRulesAndMergedStateStaySeparate(t *testing.T) {
	for _, membership := range []string{"queued", "none", "unknown"} {
		t.Run(membership, func(t *testing.T) {
			w := example(t)
			w.Policy.Gates["code_review"] = []Rule{{If: "github.merge_queue == queued"}, {If: "github.state == merged"}}
			if err := w.validate(); err != nil {
				t.Fatal(err)
			}
			r := pr("api#1", "example-alice", "low-risk")
			r.Facts["state"] = "open"
			if membership == "unknown" {
				r.Unavailable = map[string]string{"merge_queue": "GitHub did not return merge queue membership"}
			} else {
				r.Facts["merge_queue"] = membership
			}
			a := w.Assess(ticket("review", r))
			want := "fail"
			if membership == "queued" {
				want = "pass"
			}
			if membership == "unknown" {
				want = "unknown"
			}
			if a.Current[0].Outcome != want {
				t.Fatalf("membership outcome: %s", a.Current[0].Outcome)
			}
			if a.Current[1].Outcome != "fail" {
				t.Fatal("queue membership satisfied a merged-state requirement")
			}
		})
	}
}
