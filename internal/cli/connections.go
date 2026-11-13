package cli

import (
	"context"

	"workfile/internal/workfile"
)

// testConnections is wf setup --test: try each connection, or the named ones.
// Exit 0 when every connection works, 1 when one fails, and 2 when the
// workspace cannot be loaded or a name is unknown.
func testConnections(ctx context.Context, e env) (int, error) {
	v, names := e.view, e.o.keys
	w, err := workfile.Load(e.o.dir)
	if err != nil {
		return 2, err
	}
	client := e.connect(w)
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
		detail, err := client.Test(ctx, name)
		if err != nil {
			v.line("  " + v.paint("31", "✗ "+name))
			v.wrap(err.Error(), "    ")
			code = 1
		} else {
			v.line("  " + v.paint("32", "✓ "+name))
			v.wrap(detail, "    ")
		}
		v.line("")
	}
	return code, nil
}
