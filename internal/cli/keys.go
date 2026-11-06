package cli

import (
	"slices"

	"workfile/internal/workfile"
)

// completeKeys turns what people typed into the tracker's one true form, using
// its default_project for bare numbers, and drops repeats so a ticket is only
// fetched once. It rejects anything that is not a ticket key.
func completeKeys(input []string, keys workfile.Keys, project string) ([]string, error) {
	var result []string
	for _, in := range input {
		key, err := keys.Parse(in, project)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(result, key) {
			result = append(result, key)
		}
	}
	return result, nil
}
