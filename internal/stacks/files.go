package stacks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// file retains absence and permissions for optimistic concurrency checks and rollback.
type file struct {
	name          string
	before, after []byte
	mode          fs.FileMode
	exists        bool
}

// readFile rejects symlinks and special files before opening project-owned configuration.
func readFile(root, name string) (file, error) {
	f := file{name: name, mode: 0600}
	info, err := os.Lstat(filepath.Join(root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if !info.Mode().IsRegular() {
		return f, fmt.Errorf("%s must be a regular file", name)
	}
	f.exists = true
	f.mode = info.Mode().Perm()
	f.before, err = os.ReadFile(filepath.Join(root, name))
	return f, err
}

// stage writes a complete replacement on the destination filesystem before publishing it.
func stage(root string, f file, data []byte) (string, error) {
	tmp, err := os.CreateTemp(root, ".env.stack-tmp-*.local")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(f.mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	err = errors.Join(err, tmp.Close())
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// commit checks all original files before writing and rolls back a failed replacement.
func commit(root string, files []file, rename func(string, string) error) error {
	// Publish ignore rules and recovery files before the active environment so an interrupted process retains the previous configuration.
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].name == ".gitignore" {
			return files[j].name != ".gitignore"
		}
		if files[j].name == ".gitignore" {
			return false
		}
		return files[i].name != ".env" && files[j].name == ".env"
	})
	staged := make([]string, len(files))
	defer func() {
		for _, name := range staged {
			if name != "" {
				_ = os.Remove(name)
			}
		}
	}()
	for i, f := range files {
		current, err := readFile(root, f.name)
		if err != nil {
			return err
		}
		if current.exists != f.exists || !bytes.Equal(current.before, f.before) || current.mode != f.mode {
			return fmt.Errorf("%s changed while the wizard was open; run forj stack again", f.name)
		}
		if f.exists && f.mode&0222 == 0 {
			return fmt.Errorf("%s is read-only", f.name)
		}
		staged[i], err = stage(root, f, f.after)
		if err != nil {
			return err
		}
	}
	for i, f := range files {
		if err := rename(staged[i], filepath.Join(root, f.name)); err != nil {
			failure := fmt.Errorf("replace %s: %w", f.name, err)
			for j := i - 1; j >= 0; j-- {
				original := files[j]
				var restoreErr error
				if !original.exists {
					restoreErr = os.Remove(filepath.Join(root, original.name))
				} else {
					var backup string
					backup, restoreErr = stage(root, original, original.before)
					if restoreErr == nil {
						restoreErr = os.Rename(backup, filepath.Join(root, original.name))
						_ = os.Remove(backup)
					}
				}
				if restoreErr != nil {
					failure = errors.Join(failure, fmt.Errorf("restore %s: %w", original.name, restoreErr))
				}
			}
			return failure
		}
	}
	return nil
}
