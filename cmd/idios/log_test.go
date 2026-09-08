package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLogFileRotatesOnceAtTheCapAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), logFileName)
	f, err := openRotatingFile(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"first line\n", "second line\n", "third line\n"} {
		if _, err := f.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "third line\n" {
		t.Errorf("current = %q", got)
	}
	if got := readFile(t, path+".1"); got != "second line\n" {
		t.Errorf("previous = %q; the first generation must be gone", got)
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Error("a second generation was kept")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestRotatingFileRecoversWhenRenameFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission checks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, logFileName)
	f, err := openRotatingFile(path, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("first line\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := f.Write([]byte("second line\n")); err == nil {
		t.Fatal("want rotation to fail while the directory forbids rename")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("second line\n")); err != nil {
		t.Fatalf("retry after the directory is writable again: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "second line\n" {
		t.Errorf("current = %q", got)
	}
	if got := readFile(t, path+".1"); got != "first line\n" {
		t.Errorf("previous = %q", got)
	}
}

func TestTeeHandlerDeliversAttrsToEveryHandler(t *testing.T) {
	var a, b bytes.Buffer
	h := teeHandler{
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewTextHandler(&b, &slog.HandlerOptions{Level: slog.LevelWarn}),
	}
	log := slog.New(h).With("cluster", 1)
	log.Info("started")
	log.Warn("skew", "offset", "6m")
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled must be true when any handler wants the level")
	}
	if got := strings.Count(a.String(), "\n"); got != 2 {
		t.Errorf("json lines = %d, want 2", got)
	}
	if got := strings.Count(b.String(), "\n"); got != 1 {
		t.Errorf("text lines = %d, want 1 (info is below its level)", got)
	}
	for name, out := range map[string]string{"json": a.String(), "text": b.String()} {
		if !strings.Contains(out, "cluster") || (!strings.Contains(out, "offset=6m") && !strings.Contains(out, `"offset":"6m"`)) {
			t.Errorf("%s output lost attrs: %s", name, out)
		}
	}
}
