package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// workspace is a private (0700) directory for plaintext secret files. On Linux it
// lives in /dev/shm so decoded values never hit disk. Editor swap/backup files
// land in the same directory and are wiped with it.
type workspace struct{ dir string }

func newWorkspace() (*workspace, error) {
	var dir string
	var err error
	if runtime.GOOS == "linux" {
		dir, err = os.MkdirTemp("/dev/shm", "kubectl-edit-secret-")
	}
	if dir == "" || err != nil {
		dir, err = os.MkdirTemp("", "kubectl-edit-secret-")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	return &workspace{dir: dir}, nil
}

func (w *workspace) Path(name string) string {
	return filepath.Join(w.dir, filepath.Base(name))
}

func (w *workspace) Close() {
	_ = filepath.WalkDir(w.dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				_ = os.WriteFile(path, make([]byte, info.Size()), 0o600)
			}
		}
		return nil
	})
	_ = os.RemoveAll(w.dir)
}

func saveEdits(name string, content []byte) (string, error) {
	f, err := os.CreateTemp("", "kubectl-edit-secret-"+filepath.Base(name)+"-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}
