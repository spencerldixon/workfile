package run

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestSplit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"rex tab -- claude {prompt}", []string{"rex", "tab", "--", "claude", "{prompt}"}},
		{`"a b" c`, []string{"a b", "c"}},
		{`'it''s' x`, []string{"its", "x"}},
		{`a\ b`, []string{"a b"}},
		{"a && b", []string{"a", "&&", "b"}},
		{"echo $(id) ; ls", []string{"echo", "$(id)", ";", "ls"}},
	} {
		got, err := Split(tc.in)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("Split(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := Split(`"open`); err == nil {
		t.Error("an unclosed quote is an error")
	}
	if _, err := Split("   "); err == nil {
		t.Error("an empty command is an error")
	}
}

func TestFillNeverSplits(t *testing.T) {
	evil := "x; rm -rf ~ && echo $(id) \"quoted\" 'single'"
	got, err := Fill([]string{"rex", "tab", "--", "claude", "{prompt}"}, map[string]string{"prompt": evil})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[4] != evil {
		t.Fatalf("a value must stay one argument, byte for byte: %q", got)
	}
	got, err = Fill([]string{"--title={key}: {pr}"}, map[string]string{"key": "ABC-1", "pr": "#84"})
	if err != nil || got[0] != "--title=ABC-1: #84" {
		t.Fatalf("placeholders fill inside a word: %q %v", got, err)
	}
}

func TestFillMissing(t *testing.T) {
	_, err := Fill([]string{"open", "{pr}"}, map[string]string{"pr": ""})
	var missing *MissingError
	if !errors.As(err, &missing) || missing.Name != "pr" {
		t.Fatalf("want MissingError for pr, got %v", err)
	}
}

func TestStartExitCode(t *testing.T) {
	err := Start(context.Background(), []string{"sh", "-c", "exit 3"})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("want ExitError{3}, got %v", err)
	}
	if err := Start(context.Background(), []string{"true"}); err != nil {
		t.Fatalf("a clean exit is no error: %v", err)
	}
}

func TestStartNotFound(t *testing.T) {
	if err := Start(context.Background(), []string{"example-no-such-command"}); err == nil {
		t.Fatal("a missing command is an error")
	}
	if err := Start(context.Background(), nil); err == nil {
		t.Fatal("no command is an error")
	}
}
