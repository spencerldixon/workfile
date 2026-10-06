package provider

import "encoding/json"

// Null descriptions and assignees are known empty values. An omitted field is
// different: Jira did not supply the evidence requested by the policy.
func (issue *jiraIssue) UnmarshalJSON(data []byte) error {
	type plain jiraIssue
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var raw struct{ Fields map[string]json.RawMessage }
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	value.Unavailable = map[string]string{}
	for field, facts := range map[string][]string{
		"summary": {"summary"}, "description": {"description", "headings"},
		"labels": {"labels"}, "assignee": {"assignee"}, "issuetype": {"type"},
	} {
		body, present := raw.Fields[field]
		if !present || string(body) == "null" && field != "description" && field != "assignee" {
			for _, fact := range facts {
				value.Unavailable[fact] = "Jira did not return " + fact
			}
		}
	}
	*issue = jiraIssue(value)
	return nil
}
