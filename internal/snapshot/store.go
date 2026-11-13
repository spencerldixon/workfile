package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Store keeps snapshots in Dir, at most one per hour, for Keep.
type Store struct {
	Dir  string
	Now  func() time.Time
	Keep time.Duration // 8 days
}

const hourLayout = "2006-01-02T15"

type file struct {
	name string
	hour time.Time
}

// files are the snapshot files, oldest first. Temporary and unknown files are
// ignored.
func (s Store) files() ([]file, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []file
	for _, e := range entries {
		name := e.Name()
		hour, err := time.Parse(hourLayout, strings.TrimSuffix(name, ".json"))
		if e.IsDir() || !strings.HasSuffix(name, ".json") || err != nil {
			continue
		}
		out = append(out, file{name, hour})
	}
	slices.SortFunc(out, func(a, b file) int { return a.hour.Compare(b.hour) })
	return out, nil
}

func (s Store) read(f file) (Snapshot, error) {
	var snap Snapshot
	data, err := os.ReadFile(filepath.Join(s.Dir, f.name))
	if err != nil {
		return snap, err
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", f.name, err)
	}
	return snap, nil
}

// Latest is the newest snapshot. ok is false when there is none.
func (s Store) Latest() (Snapshot, bool, error) {
	list, err := s.files()
	if err != nil || len(list) == 0 {
		return Snapshot{}, false, err
	}
	snap, err := s.read(list[len(list)-1])
	return snap, err == nil, err
}

// Before is the newest snapshot taken at or before t. When every snapshot is
// later, it is the oldest one and ok is false. With no snapshots it is empty
// and ok is false.
func (s Store) Before(t time.Time) (Snapshot, bool, error) {
	list, err := s.files()
	if err != nil || len(list) == 0 {
		return Snapshot{}, false, err
	}
	var found Snapshot
	for _, f := range slices.Backward(list) {
		snap, err := s.read(f)
		if err != nil {
			return Snapshot{}, false, err
		}
		if !snap.TakenAt.After(t) {
			return snap, true, nil
		}
		found = snap
	}
	return found, false, nil
}

// Save upserts: it keeps the latest snapshot's entries, replaces those for the
// tickets in seen, drops entries older than Keep, and writes the result as
// this hour's file. Files older than Keep are deleted.
func (s Store) Save(seen Snapshot) error {
	now := s.Now()
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	merged := Snapshot{Version: 1, TakenAt: now, PolicyHash: seen.PolicyHash, Entries: map[string]Entry{}}
	latest, ok, err := s.Latest()
	if err != nil {
		return err
	}
	if ok {
		for key, e := range latest.Entries {
			if now.Sub(e.SeenAt) <= s.Keep {
				merged.Entries[key] = e
			}
		}
	}
	for key, e := range seen.Entries {
		merged.Entries[key] = e
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(s.Dir, now.UTC().Format(hourLayout)+".json")); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	list, err := s.files()
	if err != nil {
		return err
	}
	for _, f := range list {
		if now.Sub(f.hour) > s.Keep {
			if err := os.Remove(filepath.Join(s.Dir, f.name)); err != nil {
				return err
			}
		}
	}
	return nil
}
