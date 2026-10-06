package workfile

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

func factName(fact string) string {
	switch fact {
	case "summary":
		return "title"
	case "type":
		return "ticket type"
	case "prs":
		return "linked PRs"
	case "draft":
		return "draft status"
	case "checks":
		return "check status"
	case "fresh_approvals":
		return "approvals of the current commit"
	default:
		return strings.ReplaceAll(fact, "_", " ")
	}
}

func textFact(fact string) bool {
	return slices.Contains([]string{"summary", "description", "type", "project", "assignee", "state", "author", "draft", "checks"}, fact)
}

func singular(fact string) string {
	if fact == "review_requests" {
		return "requested reviewer"
	}
	return strings.TrimSuffix(factName(fact), "s")
}

func comparison(op string) string {
	return map[string]string{">": "more than", ">=": "at least", "<": "fewer than", "<=": "at most", "==": "exactly", "!=": "anything except"}[op]
}

func capitalize(text string) string {
	runes := []rune(text)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

func (w *Workspace) reviewers(rule Rule) string {
	names := make([]string, len(rule.By))
	for i, person := range rule.By {
		names[i] = w.DisplayName(person)
	}
	return strings.Join(names, " or ")
}

func (w *Workspace) requirement(rule Rule) string {
	e := rule.Condition
	if passingCI(e) {
		return "Passing CI on the current commit"
	}
	noun := factName(e.Fact)
	var text string
	if number, ok := e.Value.(int); ok {
		if textFact(e.Fact) {
			text = fmt.Sprintf("%s: %s %d characters", noun, comparison(e.Op), number)
		} else {
			if number == 1 {
				noun = singular(e.Fact)
			}
			text = fmt.Sprintf("%s %d %s", comparison(e.Op), number, noun)
		}
	} else {
		switch e.Op {
		case "contains":
			text = fmt.Sprintf("%s include %q", noun, e.Value)
		case "contains_any":
			text = fmt.Sprintf("%s include one of: %s", noun, strings.Join(e.Value.([]string), ", "))
		case "excludes":
			text = fmt.Sprintf("No %q %s", e.Value, singular(e.Fact))
		case "matches":
			text = fmt.Sprintf("%s matching %q", noun, e.Value)
		case "==":
			text = fmt.Sprintf("%s is %q", noun, e.Value)
		case "!=":
			text = fmt.Sprintf("%s is not %q", noun, e.Value)
		}
	}
	if len(rule.By) > 0 {
		text += " from " + w.reviewers(rule)
	}
	return capitalize(text)
}

func (w *Workspace) nextAction(rule Rule, ticket Ticket, record Record) string {
	if rule.Message != "" {
		id := record.ID
		if id == "" {
			id = ticket.Key
		}
		return strings.NewReplacer("{record}", id, "{ticket}", ticket.Key).Replace(rule.Message)
	}
	e := rule.Condition
	if passingCI(e) {
		return ciNextAction(w.subject(e, ticket, record, rule.By))
	}
	noun := factName(e.Fact)
	var action string
	if number, ok := e.Value.(int); ok {
		amount := fmt.Sprintf("%s %d", comparison(e.Op), number)
		switch {
		case textFact(e.Fact):
			verb := "Set the length of"
			if e.Op == ">=" || e.Op == ">" {
				verb = "Expand"
			}
			if e.Op == "<=" || e.Op == "<" {
				verb = "Shorten"
			}
			action = fmt.Sprintf("%s the %s to %s characters", verb, noun, amount)
		case (e.Fact == "approvals" || e.Fact == "fresh_approvals" || e.Fact == "prs") && (e.Op == ">=" || e.Op == ">" || e.Op == "==" && number > 0):
			if number == 1 {
				noun = singular(e.Fact)
			}
			verb := "Get"
			if e.Fact == "prs" {
				verb = "Link"
				noun = strings.TrimPrefix(noun, "linked ")
			}
			action = fmt.Sprintf("%s %s %s", verb, amount, noun)
		default:
			action = fmt.Sprintf("Set the number of %s to %s", noun, amount)
		}
	} else {
		switch e.Op {
		case "contains":
			action = fmt.Sprintf("Add the %q %s", e.Value, singular(e.Fact))
		case "contains_any":
			action = fmt.Sprintf("Add one of these %s: %s", noun, strings.Join(e.Value.([]string), ", "))
		case "excludes":
			action = fmt.Sprintf("Remove the %q %s", e.Value, singular(e.Fact))
		case "matches":
			if textFact(e.Fact) {
				action = fmt.Sprintf("Update the %s to match %q", noun, e.Value)
			} else {
				action = fmt.Sprintf("Add a %s matching %q", singular(e.Fact), e.Value)
			}
		case "==":
			action = fmt.Sprintf("Set the %s to %q", noun, e.Value)
			if e.Fact == "checks" {
				action = fmt.Sprintf("Wait for or repair the checks until their status is %q", e.Value)
			}
			if e.Fact == "draft" && e.Value == "false" {
				action = "Mark the PR ready for review"
			}
		case "!=":
			action = fmt.Sprintf("Use a %s other than %q", noun, e.Value)
		}
	}
	if len(rule.By) > 0 {
		if e.Fact == "approvals" || e.Fact == "fresh_approvals" {
			action += " from " + w.reviewers(rule)
		} else {
			action = "Ask " + w.reviewers(rule) + " to " + strings.ToLower(action[:1]) + action[1:]
		}
	}
	if e.Fact == "prs" && strings.HasPrefix(action, "Link ") {
		action += "; include " + ticket.Key + " in the PR title or branch name"
	}
	return action + "."
}

func (w *Workspace) observation(rule Rule, ticket Ticket, record Record, value any) string {
	e := rule.Condition
	if passingCI(e) {
		return ciEvidence(value)
	}
	if textFact(e.Fact) {
		if _, number := e.Value.(int); number {
			return fmt.Sprintf("Currently %d characters", size(value))
		}
		return "Currently " + displayValue(value)
	}
	if e.Fact == "prs" {
		return fmt.Sprintf("%d linked", size(value))
	}
	// Show the unfiltered evidence as well as the qualifying count. An approval
	// from somebody else must not look like an absence of reviews.
	raw := w.subject(e, ticket, record, nil)
	values := list(raw)
	if len(values) == 0 {
		return "No " + factName(e.Fact)
	}
	text := strings.Join(values, ", ")
	if e.Fact == "approvals" || e.Fact == "fresh_approvals" {
		text = "Approved by " + text
		if len(rule.By) > 0 {
			noun := "approvals"
			if size(value) == 1 {
				noun = "approval"
			}
			text = fmt.Sprintf("%d qualifying %s · approved by %s", size(value), noun, strings.Join(values, ", "))
		}
	} else {
		text = capitalize(factName(e.Fact)) + ": " + text
	}
	return text
}

func conditionEvidence(e Expression, value any) string {
	if e.Fact == "labels" && slices.Contains([]string{"contains", "contains_any", "matches"}, e.Op) {
		var matched []string
		for _, label := range list(value) {
			if e.Evaluate([]string{label}) {
				matched = append(matched, label)
			}
		}
		noun := "label"
		if len(matched) != 1 {
			noun = "labels"
		}
		return "required for " + strings.Join(matched, ", ") + " " + noun
	}
	return "required because " + conditionText(e)
}
