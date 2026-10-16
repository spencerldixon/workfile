//go:build !unix

package automation

// Lock is not available on this system; run one wf watch per workspace.
func (s Store) Lock() (release func(), err error) { return func() {}, nil }
