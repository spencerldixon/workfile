package workfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Expression struct {
	Provider, Fact, Measure, Op string
	Value                       any
	kind                        string
}

var expressionPattern = regexp.MustCompile(`^\s*([a-z_][a-z0-9_]*)\.([a-z_][a-z0-9_]*)(?:\.(length|count))?\s+(>=|<=|==|!=|>|<|contains_any|contains|excludes|matches)\s+(.+?)\s*$`)
var bareValue = regexp.MustCompile(`^[\w.:*/?-]+$`)

func ParseExpression(source string) (Expression, error) {
	m := expressionPattern.FindStringSubmatch(source)
	if m == nil {
		return Expression{}, errors.New("expected provider.attribute operator value")
	}
	e := Expression{Provider: m[1], Fact: m[2], Measure: m[3], Op: m[4]}
	value, err := parseValue(m[5])
	if err != nil {
		return e, err
	}
	e.Value = value
	_, list := value.([]string)
	_, number := value.(int)
	if e.Op == "contains_any" && !list {
		return e, errors.New("contains_any needs a list such as [bug, feature]")
	}
	if e.Op != "contains_any" && list {
		return e, errors.New("this operator needs a single value")
	}
	if (slices.Contains([]string{">", ">=", "<", "<="}, e.Op) || e.Measure != "") && !number {
		return e, errors.New("size comparisons need a whole number")
	}
	if e.Measure != "" && !slices.Contains([]string{">", ">=", "<", "<=", "==", "!="}, e.Op) {
		return e, errors.New("length and count need a numeric comparison")
	}
	if e.Op == "matches" {
		pattern, ok := value.(string)
		if !ok {
			return e, errors.New("matches needs a text pattern")
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return e, errors.New("invalid matches pattern")
		}
	}
	return e, nil
}

func parseValue(raw string) (any, error) {
	if strings.HasPrefix(raw, "[") {
		if !strings.HasSuffix(raw, "]") {
			return nil, errors.New("unclosed list")
		}
		// Split outside quotes, so a value may itself contain a comma.
		var values []string
		start, quoted, escaped := 0, false, false
		body := raw[1 : len(raw)-1]
		if strings.TrimSpace(body) == "" {
			return []string{}, nil
		}
		for i, char := range body + "," {
			if char == '"' && !escaped {
				quoted = !quoted
			}
			if char == ',' && !quoted {
				v, err := parseScalar(strings.TrimSpace(body[start:i]))
				if err != nil {
					return nil, err
				}
				s, ok := v.(string)
				if !ok {
					return nil, errors.New("list items must be text")
				}
				values = append(values, s)
				start = i + 1
			}
			escaped = char == '\\' && !escaped
		}
		if quoted {
			return nil, errors.New("unclosed quoted value")
		}
		return values, nil
	}
	return parseScalar(raw)
}

func parseScalar(raw string) (any, error) {
	if n, err := strconv.Atoi(raw); err == nil {
		return n, nil
	}
	if strings.HasPrefix(raw, `"`) {
		var value string
		if json.Unmarshal([]byte(raw), &value) == nil {
			return value, nil
		}
	}
	if bareValue.MatchString(raw) {
		return raw, nil
	}
	return nil, errors.New("quote text containing spaces or punctuation with double quotes")
}

func (w *Workspace) parseExpression(source string) (Expression, error) {
	e, err := ParseExpression(source)
	if err != nil {
		return e, err
	}
	provider, ok := w.Providers[e.Provider]
	if !ok {
		return e, fmt.Errorf("unknown provider instance %s", e.Provider)
	}
	e.kind = provider.Kind
	facts := []string{"summary", "description", "type", "project", "headings", "assignee", "labels"}
	if provider.Kind == "github" {
		facts = []string{"prs", "labels", "approvals", "state", "author", "review_requests"}
	}
	if !slices.Contains(facts, e.Fact) {
		return e, fmt.Errorf("%s has no attribute %s", e.Provider, e.Fact)
	}
	return e, nil
}

func (e Expression) Record() bool { return e.kind == "github" && e.Fact != "prs" }

func size(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case string:
		return utf8.RuneCountInString(v)
	case []string:
		return len(v)
	case []Actor:
		return len(v)
	default:
		return 0
	}
}

func list(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case string:
		return []string{v}
	case []Actor:
		values := make([]string, len(v))
		for i, actor := range v {
			values[i] = actor.Value
		}
		return values
	default:
		return nil
	}
}

func (e Expression) Evaluate(value any) bool {
	if number, ok := e.Value.(int); ok {
		actual := size(value)
		switch e.Op {
		case ">":
			return actual > number
		case ">=":
			return actual >= number
		case "<":
			return actual < number
		case "<=":
			return actual <= number
		case "==":
			return actual == number
		case "!=":
			return actual != number
		}
	}
	switch e.Op {
	case "==":
		return reflect.DeepEqual(value, e.Value)
	case "!=":
		return !reflect.DeepEqual(value, e.Value)
	case "contains":
		return slices.Contains(list(value), fmt.Sprint(e.Value))
	case "excludes":
		return !slices.Contains(list(value), fmt.Sprint(e.Value))
	case "contains_any":
		for _, candidate := range e.Value.([]string) {
			if slices.Contains(list(value), candidate) {
				return true
			}
		}
	case "matches":
		for _, candidate := range list(value) {
			// A fact is text, not a filesystem path: '*' may span slashes.
			pattern := strings.ReplaceAll(strings.ToLower(e.Value.(string)), "/", "\x00")
			text := strings.ReplaceAll(strings.ToLower(candidate), "/", "\x00")
			if matched, _ := path.Match(pattern, text); matched {
				return true
			}
		}
	}
	return false
}
