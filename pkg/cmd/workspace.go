package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

// workspace is a private (0700) directory for plaintext secret files. On Linux it
// lives in /dev/shm so decoded values never hit disk. Editor swap/backup files
// land in the same directory and are wiped with it.
type workspace struct{ dir string }

func privateTempBase() string {
	if runtime.GOOS == "linux" {
		if info, err := os.Stat("/dev/shm"); err == nil && info.IsDir() {
			return "/dev/shm"
		}
	}
	return ""
}

// staleAfter is how old a leftover workspace (from a SIGKILLed run) must be before it is wiped.
const staleAfter = 12 * time.Hour

func sweepStale() {
	base := privateTempBase()
	if base == "" {
		base = os.TempDir()
	}
	matches, _ := filepath.Glob(filepath.Join(base, "kubectl-edit-secret-*"))
	for _, m := range matches {
		if info, err := os.Lstat(m); err == nil && info.IsDir() && time.Since(info.ModTime()) > staleAfter {
			(&workspace{dir: m}).Close()
		}
	}
}

func newWorkspace() (*workspace, error) {
	sweepStale()
	dir, err := os.MkdirTemp(privateTempBase(), "kubectl-edit-secret-")
	if err != nil && privateTempBase() != "" {
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

// cleanupOnSignal wipes the workspace and exits if the process is interrupted,
// terminated or hung up. Ctrl-C is left to the editor while it runs. SIGPIPE is
// caught so a closed stdout turns into write errors and normal cleanup runs.
func (w *workspace) cleanupOnSignal(editing *atomic.Bool) (stop func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				if s == syscall.SIGPIPE || (s == os.Interrupt && editing.Load()) {
					continue
				}
				w.Close()
				os.Exit(130)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sigs)
		close(done)
	}
}

func saveEdits(name string, content []byte) (string, error) {
	f, err := os.CreateTemp(privateTempBase(), "kubectl-edit-secret-"+filepath.Base(name)+"-*.yaml")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return "", err
	}
	return f.Name(), nil
}
