package plugin

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SourceBuiltin is the Source of a plugin that ships inside Hive.
const SourceBuiltin = "builtin"

// ErrBuiltin refuses removing, or installing over, a plugin that ships
// inside Hive. It can only be enabled and disabled.
var ErrBuiltin = errors.New("plugin: ships with Hive; disable it instead")

// builtinIDs lists the top-level directories of fsys: one per bundled
// plugin, named by its id.
func builtinIDs(fsys fs.FS) ([]string, error) {
	if fsys == nil {
		return nil, nil
	}
	ents, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range ents {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// ensureBuiltins materializes every plugin in fsys into
// <stateDir>/plugins/<id>/ and makes sure plugins.json lists it. It
// runs on every daemon start, the way the Pi extension is written out:
// the bundled copy always matches the binary that is running.
//
//   - Files are rewritten only when they differ. The swap goes through a
//     temp dir and an aside rename, because renaming onto a non-empty
//     directory fails on every platform; a crash mid-swap leaves an
//     aside dir that the next start clears.
//   - A new builtin is added disabled. An existing record's enabled flag
//     is never touched: the user's choice survives upgrades.
func ensureBuiltins(stateDir string, fsys fs.FS) error {
	ids, err := builtinIDs(fsys)
	if err != nil || len(ids) == 0 {
		return err
	}
	root := filepath.Join(stateDir, installDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	for _, id := range ids {
		if err := materialize(root, id, fsys); err != nil {
			return fmt.Errorf("plugin: bundled %s: %w", id, err)
		}
	}
	recs, err := loadStore(stateDir)
	if err != nil {
		return err
	}
	changed := false
	for _, id := range ids {
		i := indexOf(recs, id)
		switch {
		case i < 0:
			recs = append(recs, record{ID: id, Source: SourceBuiltin})
			changed = true
		case recs[i].Source != SourceBuiltin || recs[i].Commit != "":
			// A user install under a bundled id: the bundled copy has
			// just replaced its files, so the record says so too.
			recs[i].Source, recs[i].Commit = SourceBuiltin, ""
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return saveStore(stateDir, recs)
}

func indexOf(recs []record, id string) int {
	for i, r := range recs {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func materialize(root, id string, fsys fs.FS) error {
	dst := filepath.Join(root, id)
	aside := filepath.Join(root, "."+id+".old")
	// Leftovers of a swap a crash interrupted.
	if err := os.RemoveAll(aside); err != nil {
		return err
	}
	stale, _ := filepath.Glob(filepath.Join(root, ".builtin-"+id+"-*"))
	for _, p := range stale {
		_ = os.RemoveAll(p)
	}
	sub, err := fs.Sub(fsys, id)
	if err != nil {
		return err
	}
	if same, err := treeMatches(sub, dst); err != nil || same {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".builtin-"+id+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) // no-op once renamed into place
	if err := writeTree(sub, tmp); err != nil {
		return err
	}
	if err := os.Rename(dst, aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.RemoveAll(aside)
}

// treeMatches reports whether dir holds exactly the files of fsys.
func treeMatches(fsys fs.FS, dir string) (bool, error) {
	want := map[string][]byte{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		want[p] = b
		return err
	})
	if err != nil {
		return false, err
	}
	n := 0
	same := true
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		exp, ok := want[filepath.ToSlash(rel)]
		if !ok {
			same = false
			return fs.SkipAll
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, exp) {
			same = false
			return fs.SkipAll
		}
		n++
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return same && err == nil && n == len(want), err
}

func writeTree(fsys fs.FS, dst string) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
}
