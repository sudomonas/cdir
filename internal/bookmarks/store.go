// SPDX-License-Identifier: GPL-3.0-or-later

package bookmarks

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// Store is the bookmark file on disk.
//
// Reads take no lock: writes replace the file atomically with rename(2), so
// a reader always sees either the old or the new version. Writers serialize
// on an flock(2) held on a separate lock file.
type Store struct {
	Path string
}

// Load reads the bookmark file. A missing file is an empty bookmark list.
func (s Store) Load() (*File, []Warning, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f, warns := Parse(data)
	return f, warns, nil
}

// Update loads the file under an exclusive lock, applies fn and atomically
// writes the result. If fn returns ErrNoChange nothing is written and Update
// returns nil; any other error aborts the update and is returned.
func (s Store) Update(fn func(*File) error) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	f, _, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(f); err != nil {
		if errors.Is(err, ErrNoChange) {
			return nil
		}
		return err
	}
	return writeAtomic(s.Path, f.Bytes())
}

// writeAtomic writes data to a temporary file next to path, syncs it and
// renames it over path. If path is a symlink (e.g. into a dotfiles repo),
// the link's target is replaced and the link itself is kept.
func writeAtomic(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bookmarks-*.tmp")
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	ok = true
	// Persist the rename itself.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}
