package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"workfile/internal/workfile"
)

func TestLinkedPRDetailsUseOneBoundedPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	active, peak, calls := 0, 0, 0
	release := make(chan struct{})
	c := client(t, func(r *http.Request) (int, string) {
		body := requestBody(t, r)
		query := body["query"].(string)
		if strings.Contains(query, "organization(login") {
			return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"api"}]}}}}`
		}
		if strings.Contains(query, "search(query") {
			nodes := make([]map[string]any, 20)
			for i := range nodes {
				nodes[i] = map[string]any{"id": fmt.Sprint(i + 1), "title": "APP-1 APP-2", "state": "OPEN"}
			}
			data, _ := json.Marshal(map[string]any{"data": map[string]any{"search": map[string]any{"issueCount": len(nodes), "nodes": nodes}}})
			return 200, string(data)
		}
		id := body["variables"].(map[string]any)["id"].(string)
		mu.Lock()
		active++
		calls++
		peak = max(peak, active)
		if active == 6 && calls == 6 {
			close(release)
		}
		mu.Unlock()
		// All six workers must reach detail requests before any can finish.
		select {
		case <-release:
		case <-ctx.Done():
			t.Error("detail requests were not concurrent")
		}
		mu.Lock()
		active--
		mu.Unlock()
		return 200, fmt.Sprintf(`{"data":{"node":{"id":%q,"number":%s,"title":"APP-1 APP-2","state":"OPEN","repository":{"name":"api"},"labels":{"nodes":[]},"latestOpinionatedReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}}}}`, id, id)
	})
	items, _, err := c.Records(ctx, []workfile.Ticket{{Key: "APP-1"}, {Key: "APP-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 20 || peak != 6 {
		t.Fatalf("detail calls %d, peak concurrency %d", calls, peak)
	}
	for _, item := range items {
		records := item.Records["github"]
		if len(records) != 20 {
			t.Fatalf("%s: got %d records", item.Key, len(records))
		}
		for i, record := range records {
			if record.ID != fmt.Sprintf("api#%d", i+1) {
				t.Fatal("records lost discovery order")
			}
		}
	}
}

func TestReadPRRejectsMissingFacts(t *testing.T) {
	for _, body := range []string{
		`{"data":{"node":null}}`,
		`{"data":{"node":{"id":"other","number":1,"repository":{"name":"api"}}}}`,
		`{"data":{"node":{"id":"pr1","number":1,"repository":{"name":"api"}}}}`,
	} {
		c := client(t, func(*http.Request) (int, string) { return 200, body })
		if _, err := c.readPR(context.Background(), "token", "pr1"); err == nil {
			t.Fatal("incomplete PR accepted")
		}
	}
}

func TestUnrelatedPRsNeedNoDetails(t *testing.T) {
	c := client(t, func(r *http.Request) (int, string) {
		query := requestBody(t, r)["query"].(string)
		if strings.Contains(query, "organization(login") {
			return 200, `{"data":{"organization":{"repositories":{"nodes":[{"name":"api"}]}}}}`
		}
		if strings.Contains(query, "search(query") {
			return 200, `{"data":{"search":{"issueCount":1,"nodes":[{"id":"unrelated","title":"APP-123","state":"OPEN"}]}}}`
		}
		t.Error("unrelated PR triggered a detail request")
		return 400, `{}`
	})
	items, _, err := c.Records(context.Background(), []workfile.Ticket{{Key: "APP-12"}})
	if err != nil || len(items[0].Records["github"]) != 0 {
		t.Fatalf("items %v, error %v", items, err)
	}
}
