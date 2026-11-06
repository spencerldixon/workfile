package cli

import (
	"context"

	"workfile/internal/workfile"
)

var testCommand = command{flags: []string{"dir"}, run: testConnections}

func testConnections(ctx context.Context, e env) (int, error) {
	v, w, names := e.view, e.w, e.o.keys
	if len(names) == 0 {
		names = workfile.SortedKeys(w.Providers)
	}
	for _, name := range names {
		if _, ok := w.Providers[name]; !ok {
			v.line("  Unknown provider instance: " + name)
			return 2, nil
		}
	}
	v.heading("connections")
	code := 0
	for _, name := range names {
		detail, err := e.client.Test(ctx, name)
		if err != nil {
			v.line("  " + v.paint("31", "✗ "+name))
			v.wrap(err.Error(), "    ")
			code = 2
		} else {
			v.line("  " + v.paint("32", "✓ "+name))
			v.wrap(detail, "    ")
		}
		v.line("")
	}
	return code, nil
}
