package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMergeQueueMembership(t *testing.T) {
	for _, tc := range []struct{ field, want string }{
		{`,"mergeQueueEntry":{"id":"example-queue-entry"}`, "queued"},
		{`,"mergeQueueEntry":null`, "none"},
		{``, ""},
		{`,"mergeQueueEntry":{}`, ""},
		{`,"mergeQueueEntry":{"id":null}`, ""},
		{`,"mergeQueueEntry":"unexpected"`, ""},
	} {
		t.Run(tc.field, func(t *testing.T) {
			p := decodePR(t, `{"state":"OPEN"`+tc.field+`}`)
			r := p.record()
			if tc.want == "" {
				if r.Unavailable["merge_queue"] == "" || r.Facts["merge_queue"] != nil {
					t.Fatal("missing membership treated as known")
				}
			} else if r.Facts["merge_queue"] != tc.want || r.Unavailable["merge_queue"] != "" {
				t.Fatalf("wrong membership: %+v", r)
			}
			if r.Facts["state"] != "open" {
				t.Fatal("queued PR must remain open")
			}
			if strings.Contains(r.Summary, "queued for merge") != (tc.want == "queued") {
				t.Fatalf("wrong summary: %s", r.Summary)
			}
		})
	}
}

func TestReadPRFetchesMergeQueueMembership(t *testing.T) {
	for _, entry := range []string{`null`, `{"id":"example-entry"}`} {
		c := client(t, func(r *http.Request) (int, string) {
			query := requestBody(t, r)["query"].(string)
			if !strings.Contains(query, "mergeQueueEntry { id }") {
				t.Fatal("query omitted merge queue membership")
			}
			return 200, fmt.Sprintf(`{"data":{"node":{"id":"pr1","number":1,"state":"OPEN","repository":{"name":"api"},"mergeQueueEntry":%s,"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}`, entry)
		})
		p, err := c.readPR(context.Background(), "example-token", "pr1")
		if err != nil {
			t.Fatal(err)
		}
		want := "queued"
		if entry == "null" {
			want = "none"
		}
		if p.record().Facts["merge_queue"] != want {
			t.Fatal("queue evidence lost during detail fetch")
		}
	}
}
