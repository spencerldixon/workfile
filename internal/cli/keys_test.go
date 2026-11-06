package cli

import (
	"slices"
	"strings"
	"testing"

	"workfile/internal/workfile"
)

func testKeys() workfile.Keys {
	w := &workfile.Workspace{
		Policy:    workfile.Policy{Tracker: "jira"},
		Providers: map[string]workfile.Provider{"jira": {Kind: "jira", Site: "example-team.atlassian.net"}},
	}
	return w.TrackerKeys()
}

func TestCompleteKeysExpandsBareNumbersWithDefaultProject(t *testing.T) {
	got, err := completeKeys([]string{"2713", "APP-5", "02713", "0", "app-5"}, testKeys(), "WEB")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"WEB-2713", "APP-5", "WEB-0"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCompleteKeysNeedsDefaultProjectForBareNumbers(t *testing.T) {
	if _, err := completeKeys([]string{"2713"}, testKeys(), ""); err == nil || !strings.Contains(err.Error(), "default_project") {
		t.Fatalf("want an error naming default_project, got %v", err)
	}
	if got, err := completeKeys([]string{"APP-1"}, testKeys(), ""); err != nil || !slices.Equal(got, []string{"APP-1"}) {
		t.Fatalf("full keys need no default project: %v %v", got, err)
	}
}

func TestCompleteKeysRejectsJunk(t *testing.T) {
	if _, err := completeKeys([]string{"27x13"}, testKeys(), "WEB"); err == nil || !strings.Contains(err.Error(), "is not a ticket key") {
		t.Fatalf("27x13 is not a key or a number: %v", err)
	}
}

func TestBadKeyIsRejectedBeforeAnyProviderCall(t *testing.T) {
	for _, args := range [][]string{{"status", "27x13"}, {"health", "27x13"}, {"check", "27x13", "--to", "done"}} {
		f := &fakeSource{}
		code, _, errOut := invoke(t, f, args...)
		if code != 2 || !strings.Contains(errOut, "is not a ticket key") || f.keys != nil {
			t.Fatalf("%v: %d %q %v", args, code, errOut, f.keys)
		}
	}
}
