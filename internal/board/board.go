// Package board reads tickets from a source and assesses them against a
// workspace's policy. It does no terminal output and reads no flags, so a
// command line tool and a server can both use it.
package board

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"workfile/internal/workfile"
)

// Source is what board needs to read tickets and their records.
type Source interface {
	Tickets(ctx context.Context, keys []string, limit int) ([]workfile.Ticket, error)
	Records(ctx context.Context, tickets []workfile.Ticket) ([]workfile.Ticket, []string, error)
}

// Query says which tickets to read. Limit 0 reads every ticket in scope.
// Who, when set, keeps only tickets that involve those provider identities.
// Cut, when above 0, keeps at most that many tickets after filtering.
type Query struct {
	Keys  []string
	Limit int
	Who   map[string]string
	Cut   int
}

// Result is the assessed tickets and what was noticed while reading them.
type Result struct {
	Assessments []workfile.Assessment
	Warnings    []string // provider warnings, unprinted
	Missing     []string // named keys the source did not return
}

// Read reads the tickets in scope and assesses each against the policy. When
// it fails after reading some tickets, the Result still holds what it noted.
func Read(ctx context.Context, w *workfile.Workspace, src Source, q Query) (Result, error) {
	var r Result
	tickets, err := src.Tickets(ctx, q.Keys, q.Limit)
	if err != nil {
		return r, err
	}
	for _, key := range q.Keys {
		if !slices.ContainsFunc(tickets, func(t workfile.Ticket) bool { return t.Key == key }) {
			r.Missing = append(r.Missing, key)
		}
	}
	tickets, r.Warnings, err = src.Records(ctx, tickets)
	if err != nil {
		return r, err
	}
	if q.Who != nil {
		tickets = slices.DeleteFunc(tickets, func(t workfile.Ticket) bool { return !t.Involves(w.Policy.Tracker, q.Who) })
	}
	if q.Cut > 0 && len(tickets) > q.Cut {
		tickets = tickets[:q.Cut]
	}
	r.Assessments = make([]workfile.Assessment, len(tickets))
	for i, ticket := range tickets {
		r.Assessments[i] = w.Assess(ticket)
	}
	return r, nil
}

// Person is the provider identities for a name in people.yml.
func Person(w *workfile.Workspace, name string) (map[string]string, error) {
	person, ok := w.People[name]
	if !ok {
		return nil, fmt.Errorf("%s is not in people.yml", name)
	}
	who := map[string]string{}
	for provider := range w.Providers {
		if person[provider] != "" {
			who[provider] = person[provider]
		}
	}
	if len(who) == 0 {
		return nil, errors.New("that person has no provider identities in people.yml")
	}
	return who, nil
}
