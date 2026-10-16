package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestCompleteKeysExpandsBareNumbersWithDefaultProject(t *testing.T) {
	got, err := completeKeys([]string{"2713", "APP-5", "02713", "0"}, "WEB")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"WEB-2713", "APP-5", "WEB-0"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCompleteKeysNeedsDefaultProjectForBareNumbers(t *testing.T) {
	if _, err := completeKeys([]string{"2713"}, ""); err == nil || !strings.Contains(err.Error(), "default_project") {
		t.Fatalf("want an error naming default_project, got %v", err)
	}
	if got, err := completeKeys([]string{"APP-1"}, ""); err != nil || !slices.Equal(got, []string{"APP-1"}) {
		t.Fatalf("full keys need no default project: %v %v", got, err)
	}
}

func TestParseAcceptsBareNumbersAndRejectsJunk(t *testing.T) {
	o, err := parse([]string{"status", "2713", "app-1"})
	if err != nil || !slices.Equal(o.keys, []string{"2713", "APP-1"}) {
		t.Fatalf("%v %v", o.keys, err)
	}
	if _, err := parse([]string{"status", "27x13"}); err == nil {
		t.Fatal("27x13 is not a key or a number")
	}
}
