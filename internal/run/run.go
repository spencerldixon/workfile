// Package run starts the person's own commands, such as an agent launcher. It
// never uses a shell: a command is split into words once, then placeholders
// are filled inside those words, so text from a provider can never become a
// second command. No terminal code, no network.
package run

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// MissingError is a placeholder the command uses but has no value for.
type MissingError struct{ Name string }

func (e *MissingError) Error() string { return fmt.Sprintf("needs {%s}, and there is none", e.Name) }

// ExitError is a command that ran and exited with a non-zero code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exited with code %d", e.Code) }

// Split breaks a command into words the way a shell would quote them, with
// single quotes, double quotes and backslashes. Nothing else is special:
// &&, |, ; and $(...) are plain text.
func Split(command string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord, quote, escaped := false, rune(0), false
	for _, c := range command {
		switch {
		case escaped:
			word.WriteRune(c)
			escaped = false
		case c == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(c)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("the command has an unclosed quote")
	}
	if inWord {
		words = append(words, word.String())
	}
	if len(words) == 0 {
		return nil, errors.New("the command is empty")
	}
	return words, nil
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// Fill replaces {name} inside each word. A value never splits a word. A
// placeholder that is used but empty is a MissingError.
func Fill(words []string, values map[string]string) ([]string, error) {
	out := make([]string, len(words))
	var missing error
	for i, w := range words {
		out[i] = placeholder.ReplaceAllStringFunc(w, func(m string) string {
			name := m[1 : len(m)-1]
			v := values[name]
			if v == "" && missing == nil {
				missing = &MissingError{Name: name}
			}
			return v
		})
	}
	if missing != nil {
		return nil, missing
	}
	return out, nil
}

// Start runs argv and waits for it. It reads no input and keeps no output, so
// it never takes over the terminal; a launcher such as `rex tab` opens its own.
func Start(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("no command to run")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{Code: exit.ExitCode()}
	}
	return err
}
