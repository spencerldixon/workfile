package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"workfile/internal/workfile"
)

// readOnly fails the test unless the request only reads. Reads are GETs, a POST
// to a search or GraphQL endpoint, and never a GraphQL mutation. It leaves the
// request body readable.
func readOnly(t *testing.T, r *http.Request) {
	t.Helper()
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	switch {
	case r.Method == http.MethodGet:
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/search/jql"):
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/graphql"):
		text := strings.TrimSpace(string(body))
		var graphql struct{ Query string }
		if json.Unmarshal(body, &graphql) == nil && graphql.Query != "" {
			text = strings.TrimSpace(graphql.Query)
		}
		if strings.HasPrefix(text, "mutation") {
			t.Errorf("GraphQL mutation sent to %s", r.URL.Path)
		}
	default:
		t.Errorf("provider wrote or sent an unlisted request: %s %s", r.Method, r.URL.Path)
	}
}

// recorder wraps a fixture's responses. It checks every request reads only,
// remembers what was asked, and can fail requests on demand.
type recorder struct {
	mu       sync.Mutex
	requests []string
	failAll  bool
	failAt   int // fail the nth request, counting from 1; 0 means none
}

const sentinel = "secret-sentinel"

func (rec *recorder) wrap(t *testing.T, handle func(*http.Request) (int, string)) func(*http.Request) (int, string) {
	return func(r *http.Request) (int, string) {
		readOnly(t, r)
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		rec.mu.Lock()
		rec.requests = append(rec.requests, r.Method+" "+r.URL.String()+" "+string(body))
		n := len(rec.requests)
		fail := rec.failAll || n == rec.failAt
		rec.mu.Unlock()
		if fail {
			return 400, `{"message":"` + sentinel + `"}`
		}
		return handle(r)
	}
}

func (rec *recorder) count() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.requests)
}

// conformClient is a client whose connection is the fixture's provider. An empty
// name means the workspace's tracker.
func conformClient(t *testing.T, name string, p workfile.Provider, handle func(*http.Request) (int, string)) (*Client, *recorder) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rec := &recorder{}
	c := client(t, rec.wrap(t, handle))
	if name == "" {
		name = c.Workspace.Policy.Tracker
	}
	c.Workspace.Providers[name] = p
	return c, rec
}

func trackerOf(t *testing.T, c *Client) tracker {
	t.Helper()
	tr, err := c.tracker()
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func hostOf(t *testing.T, c *Client, name string) codeHost {
	t.Helper()
	conn, err := c.connection(name)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := conn.(codeHost)
	if !ok {
		t.Fatalf("%s is not a code host", name)
	}
	return h
}

// secrets are the credential values the connection sends.
func secrets(t *testing.T, c *Client, p workfile.Provider) []string {
	t.Helper()
	var out []string
	for _, reference := range p.Auth {
		value, err := c.Credentials.Get(reference)
		if err != nil || value == "" {
			t.Fatalf("fixture credential %s: %v", reference, err)
		}
		out = append(out, value)
	}
	return out
}

func noLeak(t *testing.T, what string, err error, secrets []string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: a failed request returned no error", what)
		return
	}
	for _, secret := range append([]string{sentinel}, secrets...) {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error exposes %q: %v", what, secret, err)
		}
	}
}

// known checks every standard fact is in facts or explained in unavailable.
func known(t *testing.T, what string, standard []string, facts workfile.Facts, unavailable map[string]string) {
	t.Helper()
	for _, fact := range standard {
		_, has := facts[fact]
		if !has && unavailable[fact] == "" {
			t.Errorf("%s: fact %q is neither known nor explained", what, fact)
		}
		if has && unavailable[fact] != "" {
			t.Errorf("%s: fact %q is both known and unavailable", what, fact)
		}
	}
}

type trackerFixture struct {
	Provider workfile.Provider
	Handle   func(*http.Request) (int, string)
	// InScope is a ticket in the scope with a mapped status. OutOfScope exists
	// but the scope hides it. Unmapped is in the scope with a status the policy
	// does not map. Missing does not exist.
	InScope, OutOfScope, Unmapped, Missing string
}

func conformTracker(t *testing.T, f trackerFixture) {
	ctx := context.Background()
	fresh := func(t *testing.T) (*Client, tracker, *recorder) {
		t.Helper()
		c, rec := conformClient(t, "", f.Provider, f.Handle)
		return c, trackerOf(t, c), rec
	}
	read := func(t *testing.T, tr tracker, keys ...string) map[string]workfile.Ticket {
		t.Helper()
		tickets, err := tr.Tickets(ctx, keys, 0)
		if err != nil {
			t.Fatal(err)
		}
		byKey := map[string]workfile.Ticket{}
		for _, ticket := range tickets {
			byKey[ticket.Key] = ticket
		}
		return byKey
	}

	t.Run("ReadOnly", func(t *testing.T) {
		// The recorder fails the test on any write, so use every read.
		_, tr, _ := fresh(t)
		if who, err := tr.Identity(ctx); err != nil || who == "" {
			t.Errorf("identity %q: %v", who, err)
		}
		if message, err := tr.Test(ctx); err != nil || message == "" {
			t.Errorf("test %q: %v", message, err)
		}
		read(t, tr)
		read(t, tr, f.InScope, f.OutOfScope)
		if _, err := tr.Listing(ctx, ""); err != nil {
			t.Error(err)
		}
	})

	t.Run("ErrorsHoldNoSecrets", func(t *testing.T) {
		c, tr, rec := fresh(t)
		rec.failAll = true
		tokens := secrets(t, c, f.Provider)
		_, err := tr.Tickets(ctx, nil, 0)
		noLeak(t, "Tickets", err, tokens)
		_, err = tr.Tickets(ctx, []string{f.InScope}, 0)
		noLeak(t, "Tickets by key", err, tokens)
		_, err = tr.Listing(ctx, "")
		noLeak(t, "Listing", err, tokens)
		_, err = tr.Identity(ctx)
		noLeak(t, "Identity", err, tokens)
		_, err = tr.Test(ctx)
		noLeak(t, "Test", err, tokens)
	})

	t.Run("FactsKnownOrExplained", func(t *testing.T) {
		_, tr, _ := fresh(t)
		tickets := read(t, tr, f.InScope, f.OutOfScope, f.Unmapped)
		for _, key := range []string{f.InScope, f.OutOfScope, f.Unmapped} {
			ticket, ok := tickets[key]
			if !ok {
				t.Fatalf("%s was not returned", key)
			}
			known(t, key, workfile.TrackerFacts, ticket.Facts, ticket.Unavailable)
		}
		for key, ticket := range read(t, tr) {
			known(t, key, workfile.TrackerFacts, ticket.Facts, ticket.Unavailable)
		}
	})

	t.Run("FactValues", func(t *testing.T) {
		_, tr, _ := fresh(t)
		for key, ticket := range read(t, tr, f.InScope, f.OutOfScope, f.Unmapped) {
			for _, fact := range []string{"summary", "description", "assignee", "type", "project"} {
				if value, ok := ticket.Facts[fact]; ok {
					if _, isString := value.(string); !isString {
						t.Errorf("%s: %s is %T, want string", key, fact, value)
					}
				}
			}
			if value, ok := ticket.Facts["headings"]; ok {
				if _, isList := value.([]string); !isList {
					t.Errorf("%s: headings is %T, want []string", key, value)
				}
			}
			if value, ok := ticket.Facts["labels"]; ok {
				if _, isList := value.([]workfile.Actor); !isList {
					t.Errorf("%s: labels is %T, want []workfile.Actor", key, value)
				}
			}
		}
	})

	t.Run("OutOfScope", func(t *testing.T) {
		_, tr, _ := fresh(t)
		tickets := read(t, tr, f.InScope, f.OutOfScope)
		in, hidden := tickets[f.InScope], tickets[f.OutOfScope]
		if in.Key == "" || hidden.Key == "" {
			t.Fatalf("a named ticket was not read: %v", tickets)
		}
		if in.OutOfScope {
			t.Errorf("%s is in scope", f.InScope)
		}
		if !hidden.OutOfScope {
			t.Errorf("%s is hidden by the scope but not marked", f.OutOfScope)
		}
	})

	t.Run("MissingKeys", func(t *testing.T) {
		_, tr, _ := fresh(t)
		tickets := read(t, tr, f.InScope, f.Missing)
		if _, ok := tickets[f.Missing]; ok {
			t.Errorf("returned %s, which does not exist", f.Missing)
		}
		if _, ok := tickets[f.InScope]; !ok {
			t.Errorf("a missing key hid %s", f.InScope)
		}
	})

	t.Run("UnmappedStatus", func(t *testing.T) {
		_, tr, _ := fresh(t)
		tickets := read(t, tr, f.InScope, f.Unmapped)
		unmapped, ok := tickets[f.Unmapped]
		if !ok {
			t.Fatalf("%s was left out", f.Unmapped)
		}
		if unmapped.State != "" || unmapped.Error == "" {
			t.Errorf("unmapped ticket has state %q and error %q", unmapped.State, unmapped.Error)
		}
		if mapped := tickets[f.InScope]; mapped.State == "" || mapped.Error != "" {
			t.Errorf("mapped ticket has state %q and error %q", mapped.State, mapped.Error)
		}
		listing, err := tr.Listing(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range listing {
			if item.Key == f.Unmapped {
				found = true
				if item.State != "" {
					t.Errorf("listing gave an unmapped status the state %q", item.State)
				}
			}
			if item.Key == f.InScope && item.State == "" {
				t.Errorf("listing left %s without a state", f.InScope)
			}
		}
		if !found {
			t.Errorf("listing left out %s", f.Unmapped)
		}
	})

	t.Run("OnlyMine", func(t *testing.T) {
		asked := func(onlyMine bool, run func(tr tracker)) []string {
			c, tr, rec := fresh(t)
			c.Workspace.Settings.OnlyMine = onlyMine
			run(tr)
			return rec.requests
		}
		scope := func(tr tracker) { read(t, tr) }
		if slices.Equal(asked(false, scope), asked(true, scope)) {
			t.Error("only_mine did not narrow a scope read")
		}
		listing := func(tr tracker) {
			if _, err := tr.Listing(ctx, ""); err != nil {
				t.Error(err)
			}
		}
		if !slices.Equal(asked(false, listing), asked(true, listing)) {
			t.Error("only_mine narrowed a listing")
		}
		c, tr, _ := fresh(t)
		c.Workspace.Settings.OnlyMine = true
		if _, ok := read(t, tr, f.InScope)[f.InScope]; !ok {
			t.Errorf("only_mine hid the named ticket %s", f.InScope)
		}
	})

	t.Run("ListingScope", func(t *testing.T) {
		const custom = "conformance-scope"
		c, tr, rec := fresh(t)
		if _, err := tr.Listing(ctx, custom); err != nil {
			t.Fatal(err)
		}
		configured, _ := json.Marshal(c.Workspace.Providers[c.Workspace.Policy.Tracker].Scope)
		configured = bytes.Trim(configured, `"`)
		for _, request := range rec.requests {
			if strings.Contains(request, custom) && !strings.Contains(request, string(configured)) {
				return
			}
		}
		t.Errorf("a given scope did not replace the configured one: %q", rec.requests)
	})

	t.Run("KeySurvivesParse", func(t *testing.T) {
		c, tr, _ := fresh(t)
		keys := c.Workspace.TrackerKeys()
		project := c.Workspace.Providers[c.Workspace.Policy.Tracker].DefaultProject
		tickets := read(t, tr, f.InScope, f.OutOfScope, f.Unmapped)
		for key := range read(t, tr) {
			tickets[key] = workfile.Ticket{Key: key}
		}
		for key := range tickets {
			if parsed, err := keys.Parse(key, project); err != nil || parsed != key {
				t.Errorf("Parse(%q) = %q, %v", key, parsed, err)
			}
		}
	})

	// Detail and Image are separate so a tracker that does not read them yet
	// can skip just these two.
	t.Run("Detail", func(t *testing.T) {
		c, tr, rec := fresh(t)
		d, err := tr.Detail(ctx, f.InScope)
		if err != nil || d.Key != f.InScope {
			t.Fatalf("detail %+v: %v", d, err)
		}
		rec.failAll = true
		_, err = tr.Detail(ctx, f.InScope)
		noLeak(t, "Detail", err, secrets(t, c, f.Provider))
	})

	t.Run("Image", func(t *testing.T) {
		_, tr, rec := fresh(t)
		for _, attachment := range []string{"", "../secret", "https://example.com/a.png", "1/../2", "a b"} {
			if _, err := tr.Image(ctx, attachment); err == nil {
				t.Errorf("Image(%q) was not refused", attachment)
			}
		}
		if rec.count() != 0 {
			t.Errorf("a bad attachment caused requests: %q", rec.requests)
		}
	})
}

type hostFixture struct {
	Provider workfile.Provider
	Handle   func(*http.Request) (int, string)
	// Tickets are asked about together. The fixture needs a ticket with
	// LinkedPRs, two tickets sharing a linked change, and a ticket with none
	// whose search also finds a change that does not mention it.
	Tickets []workfile.Ticket
}

const hostName = "github"

func conformCodeHost(t *testing.T, f hostFixture) {
	ctx := context.Background()
	fresh := func(t *testing.T) (*Client, codeHost, *recorder) {
		t.Helper()
		c, rec := conformClient(t, hostName, f.Provider, f.Handle)
		return c, hostOf(t, c, hostName), rec
	}
	keysOf := func(c *Client) workfile.Keys { return c.Workspace.TrackerKeys() }
	records := func(t *testing.T, tickets []workfile.Ticket) (map[string][]workfile.Record, *recorder) {
		t.Helper()
		c, h, rec := fresh(t)
		got, err := h.Records(ctx, tickets, keysOf(c))
		if err != nil {
			t.Fatal(err)
		}
		return got, rec
	}

	t.Run("ReadOnly", func(t *testing.T) {
		c, h, _ := fresh(t)
		if who, err := h.Identity(ctx); err != nil || who == "" {
			t.Errorf("identity %q: %v", who, err)
		}
		if message, err := h.Test(ctx); err != nil || message == "" {
			t.Errorf("test %q: %v", message, err)
		}
		if _, err := h.Records(ctx, f.Tickets, keysOf(c)); err != nil {
			t.Error(err)
		}
	})

	t.Run("ErrorsHoldNoSecrets", func(t *testing.T) {
		c, h, rec := fresh(t)
		rec.failAll = true
		tokens := secrets(t, c, f.Provider)
		_, err := h.Records(ctx, f.Tickets, keysOf(c))
		noLeak(t, "Records", err, tokens)
		_, err = h.Identity(ctx)
		noLeak(t, "Identity", err, tokens)
		_, err = h.Test(ctx)
		noLeak(t, "Test", err, tokens)
	})

	t.Run("FactsKnownOrExplained", func(t *testing.T) {
		got, _ := records(t, f.Tickets)
		n := 0
		for key, list := range got {
			for _, r := range list {
				n++
				known(t, key+" "+r.ID, workfile.CodeHostFacts, r.Facts, r.Unavailable)
			}
		}
		if n == 0 {
			t.Error("the fixture produced no records")
		}
	})

	t.Run("FactValues", func(t *testing.T) {
		allowed := map[string][]string{
			"state":       {"open", "merged"},
			"draft":       {"true", "false"},
			"checks":      {"success", "pending", "failure", "none"},
			"merge_queue": {"queued", "none"},
		}
		got, _ := records(t, f.Tickets)
		for key, list := range got {
			for _, r := range list {
				for fact, values := range allowed {
					value, ok := r.Facts[fact]
					if !ok {
						continue
					}
					if text, isString := value.(string); !isString || !slices.Contains(values, text) {
						t.Errorf("%s %s: %s is %v, want one of %v", key, r.ID, fact, value, values)
					}
				}
				for _, fact := range []string{"approvals", "fresh_approvals"} {
					if value, ok := r.Facts[fact]; ok {
						if _, isList := value.([]workfile.Actor); !isList {
							t.Errorf("%s %s: %s is %T, want []workfile.Actor", key, r.ID, fact, value)
						}
					}
				}
				for _, fact := range []string{"review_requests", "labels"} {
					if value, ok := r.Facts[fact]; ok {
						if _, isList := value.([]string); !isList {
							t.Errorf("%s %s: %s is %T, want []string", key, r.ID, fact, value)
						}
					}
				}
			}
		}
	})

	t.Run("LinkedPRsReadWithNoSearch", func(t *testing.T) {
		var linked []workfile.Ticket
		for _, ticket := range f.Tickets {
			if len(ticket.LinkedPRs) > 0 {
				linked = append(linked, ticket)
			}
		}
		if len(linked) == 0 {
			t.Fatal("the fixture needs a ticket with LinkedPRs")
		}
		c, h, rec := fresh(t)
		got, err := h.Records(ctx, linked, keysOf(c))
		if err != nil {
			t.Fatal(err)
		}
		for _, ticket := range linked {
			if len(got[ticket.Key]) == 0 {
				t.Errorf("%s: linked changes were not read", ticket.Key)
			}
			search := keysOf(c).Search(ticket.Key)
			for _, request := range rec.requests {
				if strings.Contains(request, search) {
					t.Errorf("%s: searched despite LinkedPRs: %s", ticket.Key, request)
				}
			}
		}
	})

	t.Run("LooseSearchResultsDropped", func(t *testing.T) {
		var searched []workfile.Ticket
		for _, ticket := range f.Tickets {
			if len(ticket.LinkedPRs) == 0 {
				searched = append(searched, ticket)
			}
		}
		if len(searched) == 0 {
			t.Fatal("the fixture needs a ticket with no LinkedPRs")
		}
		c, h, _ := fresh(t)
		keys := keysOf(c)
		got, err := h.Records(ctx, searched, keys)
		if err != nil {
			t.Fatal(err)
		}
		kept := 0
		for _, ticket := range searched {
			for _, r := range got[ticket.Key] {
				kept++
				if !slices.Contains(keys.Mentions(r.Title), ticket.Key) {
					t.Errorf("%s: kept %q, which does not mention it", ticket.Key, r.Title)
				}
			}
		}
		if kept == 0 {
			t.Error("the fixture's search found no change for any ticket")
		}
		// Every other result the search returned is loose, so none may appear.
		unique := map[string]bool{}
		for _, list := range got {
			for _, r := range list {
				unique[r.ID] = true
			}
		}
		if len(unique) != kept {
			t.Errorf("a loose result was kept: %d changes for %d links", len(unique), kept)
		}
	})

	t.Run("ChangeLinkedToTwoTicketsAppearsUnderBoth", func(t *testing.T) {
		byRef := map[string][]string{}
		for _, ticket := range f.Tickets {
			for _, ref := range ticket.LinkedPRs {
				byRef[ref] = append(byRef[ref], ticket.Key)
			}
		}
		shared := false
		got, _ := records(t, f.Tickets)
		for ref, keys := range byRef {
			if len(keys) < 2 {
				continue
			}
			shared = true
			ids := map[string]bool{}
			for _, key := range keys {
				if len(got[key]) == 0 {
					t.Fatalf("%s: no records for %s", ref, key)
				}
				for _, r := range got[key] {
					ids[r.ID] = true
				}
			}
			both := 0
			for id := range ids {
				found := 0
				for _, key := range keys {
					for _, r := range got[key] {
						if r.ID == id {
							found++
						}
					}
				}
				if found == len(keys) {
					both++
				}
			}
			if both == 0 {
				t.Errorf("%s is linked to %v but is not under each", ref, keys)
			}
		}
		if !shared {
			t.Fatal("the fixture needs two tickets with the same linked change")
		}
	})

	t.Run("FailedReadReturnsError", func(t *testing.T) {
		_, rec := records(t, f.Tickets)
		total := rec.count()
		if total == 0 {
			t.Fatal("the fixture made no requests")
		}
		for n := 1; n <= total; n++ {
			c, h, rec := fresh(t)
			rec.failAt = n
			_, err := h.Records(ctx, f.Tickets, keysOf(c))
			noLeak(t, fmt.Sprintf("request %d of %d failing", n, total), err, secrets(t, c, f.Provider))
		}
	})
}
