package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

const exampleMediaID = "11111111-2222-4333-8444-555555555555"

func TestJiraDetailKeepsResolvableImageWithoutAltText(t *testing.T) {
	c := jiraClient(t, func(r *http.Request) (int, string) {
		return 200, `{"fields":{"description":{"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"type":"file","id":"11111111-2222-4333-8444-555555555555","collection":"","width":960,"height":154}}]}]},"comment":{"total":0,"comments":[]},"attachment":[{"id":"10042","filename":"example-screenshot.png","mimeType":"image/png"}]}}`
	})
	detail, err := c.Detail(context.Background(), "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Body) != 1 || detail.Body[0].Attachment == "" {
		t.Fatal("unnamed Jira media lost its image reference before the preview could load it")
	}
}

func TestJiraMediaResolutionIsLazyCachedAndNeverVisitsStorageHost(t *testing.T) {
	var heads, downloads atomic.Int32
	c := jiraClient(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "/attachment/content/") {
			if r.Method != "GET" || r.URL.Query().Get("redirect") != "false" || !strings.HasSuffix(r.URL.Path, "/10042") {
				t.Errorf("unexpected image download")
			}
			downloads.Add(1)
			return 200, "image bytes"
		}
		return 200, `{"fields":{"description":{"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"11111111-2222-4333-8444-555555555555","type":"file"}}]}]},"comment":{"total":0,"comments":[]},"attachment":[{"id":"10041","filename":"other.png","mimeType":"image/png"},{"id":"10042","filename":"example.png","mimeType":"image/png"}]}}`
	})
	inner := c.HTTP.Transport
	c.HTTP.CheckRedirect = nil // The identity probe must still refuse redirects.
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "media.example.com" {
			t.Error("credentials or a request reached the storage host")
		}
		if r.Method == "HEAD" {
			heads.Add(1)
			if r.Header.Get("Authorization") == "" {
				t.Error("Jira metadata lookup was not authenticated")
			}
			media := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
			if strings.HasSuffix(r.URL.Path, "/10042") {
				media = exampleMediaID
			}
			return &http.Response{StatusCode: 303, Header: http.Header{"Location": {"https://media.example.com/file/" + media + "/binary?token=SECRET_QUERY"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return inner.RoundTrip(r)
	})
	detail, err := c.Detail(context.Background(), "APP-7")
	if err != nil {
		t.Fatal(err)
	}
	if heads.Load() != 0 || downloads.Load() != 0 {
		t.Fatal("Detail eagerly fetched image metadata or content")
	}
	ref := detail.Body[0].Attachment
	// Concurrent previews must share each attachment's identity lookup.
	_, err = parallel(context.Background(), 8, func(ctx context.Context, _ int) ([]byte, error) { return c.Image(ctx, ref) })
	if err != nil {
		t.Fatal(err)
	}
	if heads.Load() != 2 || downloads.Load() != 8 {
		t.Fatalf("unexpected request counts: %d metadata, %d downloads", heads.Load(), downloads.Load())
	}
}

func TestJiraMediaResolutionDoesNotGuessOrLeakRedirectTokens(t *testing.T) {
	c := jiraClient(t, func(*http.Request) (int, string) { return 200, "" })
	inner := c.HTTP.Transport
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "HEAD" {
			return &http.Response{StatusCode: 303, Header: http.Header{"Location": {"https://media.example.com/file/aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee/binary?token=SECRET_QUERY"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if strings.Contains(r.URL.Path, "/attachment/content/") {
			t.Error("unmatched media downloaded an arbitrary attachment")
		}
		return inner.RoundTrip(r)
	})
	_, err := c.Image(context.Background(), "jira-media:"+exampleMediaID+":10042")
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "media.example.com") {
		t.Fatalf("unsafe or missing mismatch error: %v", err)
	}
}

func TestJiraMediaReferenceValidatesAndBoundsCandidates(t *testing.T) {
	files := []jiraFile{{id: "10000", name: "document.pdf", mimeType: "application/pdf"}}
	for i := 1; i < 20; i++ {
		files = append(files, jiraFile{id: fmt.Sprint(10000 + i), name: "example.png", mimeType: "image/png"})
	}
	ref := jiraMediaReference(exampleMediaID, files)
	_, ids, err := parseJiraMediaReference(ref)
	if err != nil || len(ids) != maxJiraMediaCandidates || ids[0] != "10001" {
		t.Fatalf("unbounded or non-image candidates: %v %v", ids, err)
	}
	for _, bad := range []string{"jira-media:not-a-uuid:10042", "jira-media:" + exampleMediaID + ":../../private", "jira-media:" + exampleMediaID + ":https://example.com/image", "jira-media:" + exampleMediaID + ":"} {
		if _, _, err := parseJiraMediaReference(bad); err == nil {
			t.Fatal("invalid locator was accepted")
		}
	}
}

func TestJiraMediaIdentityFailureCanBeRetried(t *testing.T) {
	var attempts int
	c := jiraClient(t, func(*http.Request) (int, string) { return 200, "image bytes" })
	inner := c.HTTP.Transport
	c.HTTP.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "HEAD" {
			attempts++
			status, header := 503, http.Header{}
			if attempts > 1 {
				status = 303
				header.Set("Location", "https://media.example.com/file/"+exampleMediaID+"/binary")
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return inner.RoundTrip(r)
	})
	ref := "jira-media:" + exampleMediaID + ":10042"
	if _, err := c.Image(context.Background(), ref); err == nil {
		t.Fatal("transient lookup failure was ignored")
	}
	if _, err := c.Image(context.Background(), ref); err != nil {
		t.Fatal("transient lookup failure was cached forever")
	}
}
