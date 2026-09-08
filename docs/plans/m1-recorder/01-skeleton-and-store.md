# Phase 1: Skeleton + Store Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Read `.ai/*.md` before writing any code; those rules override habits.

**Goal:** A Go module whose `idios` binary opens `.storage/idios.db`, applies the complete schema from the storage design doc, and exits cleanly on SIGINT; with the `clock`, `config` and `store` packages every later phase compiles against, covered by tests on a real temp-file SQLite.

**Architecture:** One binary. `internal/store` owns the SQLite file through two `*sql.DB` handles: a single-connection `Writer` (one method, `Tx`) and a read-only `Reader` pool. Schema lives in embedded SQL migration files applied in order and recorded in `schema_migrations`. `internal/clock` is the only source of process time and the only place the timestamp layout is spelled. `internal/config` holds process settings (TOML + flags) with development defaults pointing at `./.storage`.

**Tech Stack:** Go 1.24, `modernc.org/sqlite`, `github.com/BurntSushi/toml`, `log/slog`, standard `testing`.

**Spec:** `docs/design/data-storage.md` (Sections 5, 10, 11), `docs/design/process-architecture.md` (Sections 2, 3, 9, 13, 14.2, 14.3). Roadmap: `docs/plans/m1-recorder/roadmap.md`. The plan may cite these; the code must not (`.ai/code-is-truth.md`).

## Global Constraints

- `.ai/ascii-only.md`, `.ai/tests.md`, `.ai/comments.md`, `.ai/code-is-truth.md`, `.ai/scope.md`, `.ai/commits.md` apply to every line written. The ASCII check is `hack/ascii-check` (perl-based; macOS grep has no `-P`). The code blocks below already follow them; do not add comments, tests, or helpers beyond what a task lists.
- Every task ends with the checkpoint `go build ./... && go test ./... && make ascii` green, then one commit per `.ai/commits.md` (subject `area: what`, no trailers). Each task's checkpoint step names the commit subject.
- Module path `idios`. `go.mod` says `go 1.24`.
- Timestamp layout, verbatim: `2006-01-02T15:04:05.000000Z`.
- Development storage: `data_dir` default `.storage`; DB at `<data_dir>/idios.db`; artifacts at `<data_dir>/artifacts`; config at `<data_dir>/idios.toml`. Directories `0700`, files `0600`.
- Kubeconfig default for development: `kube/config`. Not read by any code in this phase.
- SQLite pragmas on every connection: `journal_mode = WAL`, `synchronous = NORMAL`, `foreign_keys = ON`, `busy_timeout = 5000`; reader connections additionally `query_only = ON`. Writer `SetMaxOpenConns(1)`.
- All tables `STRICT`. Booleans `INTEGER` 0/1. Columns in a UNIQUE rule that can be "absent" store `''` and are `NOT NULL DEFAULT ''`.

## File structure

```
go.mod, go.sum
.gitignore
Makefile
cmd/idios/main.go
internal/clock/clock.go, clock_test.go
internal/config/config.go, config_test.go
internal/store/open.go              Open, Store, Close, DSN
internal/store/writer.go            Writer, Tx
internal/store/reader.go            Reader
internal/store/migrate.go           embedded FS, Migrate, SchemaVersion
internal/store/migrations/0001_init.sql
internal/store/rows.go              row structs, enum constants, scanPod
internal/store/testhelpers_test.go  shared fixtures
internal/store/open_test.go
internal/store/migrate_test.go
internal/store/writer_test.go
internal/store/schema_test.go
internal/store/rows_test.go
internal/archtest/deps_test.go
```

Every test below traces to a spec line; the trace is given next to it. If an executor thinks a test is missing, they add it with its trace; if they cannot name a trace, they do not add it.

---

### Task 1: Module skeleton, gitignore, Makefile, stub binary

**Files:**
- Create: `go.mod`, `Makefile`, `cmd/idios/main.go`

**Interfaces:**
- Produces: module `idios`; `make build` -> `bin/idios`; `make test`, `make lint`, `make ascii`.

- [ ] **Step 1: Create the module**

```bash
go mod init idios
```

Edit `go.mod` so the directive reads exactly `go 1.24`.

- [ ] **Step 2: Check `.gitignore`**

`.gitignore` already exists at the repo root and covers `.storage/`, `kube/config`, `bin/`. Run `git check-ignore kube/config .storage bin` and confirm all three print. Do not edit it.

- [ ] **Step 3: Write the stub binary**

`cmd/idios/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "idios:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fmt.Println("idios: skeleton")
	return nil
}
```

- [ ] **Step 4: Write the Makefile**

Recipe lines use tabs.

```make
.PHONY: build test lint ascii hooks run clean

build:
	mkdir -p bin
	go build -o bin/idios ./cmd/idios

test:
	go test ./...

lint:
	go vet ./...
	golangci-lint run ./...

ascii:
	hack/ascii-check

hooks:
	install -m 0755 hack/pre-commit .git/hooks/pre-commit

run: build
	./bin/idios

clean:
	rm -rf bin
```

- [ ] **Step 5: Verify**

Run: `make build && ./bin/idios && make ascii && make hooks`
Expected: `idios: skeleton`; `make ascii` silent, exit 0; `.git/hooks/pre-commit` present and executable.

Commit: `git add go.mod Makefile cmd/idios/main.go && git commit -m "build: add module, makefile and stub binary"`

---

### Task 2: `internal/clock`

**Files:**
- Create: `internal/clock/clock.go`
- Test: `internal/clock/clock_test.go`

**Interfaces:**
- Produces: `const Layout`, `type Clock interface { Now() time.Time }`, `type Real struct{}`, `type Fake struct` with `NewFake(t) *Fake`, `Now()`, `Set(t)`, `Advance(d)`, `func Format(t time.Time) string`, `func Parse(s string) (time.Time, error)`.

Tests and their trace:
- `TestFormatIsFixedWidthUTC`: storage doc Section 5 conventions -- six fractional digits always, whole seconds padded, UTC, lexicographic order equals chronological.
- `TestParseAcceptsOnlyLayout`: same section, "one fixed-width layout"; a value in any other layout in the DB is a bug and must not parse.

`Fake` and `Real` are not tested: they hold a value and return it.

- [ ] **Step 1: Write the failing tests**

`internal/clock/clock_test.go`:

```go
package clock

import (
	"testing"
	"time"
)

func TestFormatIsFixedWidthUTC(t *testing.T) {
	plus4 := time.FixedZone("plus4", 4*3600)
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"microseconds", time.Date(2026, 8, 26, 14, 3, 11, 482913000, time.UTC), "2026-08-26T14:03:11.482913Z"},
		{"whole seconds padded", time.Date(2026, 8, 26, 14, 3, 11, 0, time.UTC), "2026-08-26T14:03:11.000000Z"},
		{"non-UTC converted", time.Date(2026, 8, 26, 18, 3, 11, 0, plus4), "2026-08-26T14:03:11.000000Z"},
		{"nanoseconds truncated", time.Date(2026, 8, 26, 14, 3, 11, 482913999, time.UTC), "2026-08-26T14:03:11.482913Z"},
	}
	for _, c := range cases {
		if got := Format(c.in); got != c.want {
			t.Errorf("%s: Format = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseAcceptsOnlyLayout(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"2026-08-26T14:03:11.482913Z", time.Date(2026, 8, 26, 14, 3, 11, 482913000, time.UTC), true},
		{"2026-08-26T14:03:11.000000Z", time.Date(2026, 8, 26, 14, 3, 11, 0, time.UTC), true},
		{"2026-08-26T14:03:11Z", time.Time{}, false},
		{"2026-08-26T14:03:11.482Z", time.Time{}, false},
		{"2026-08-26T14:03:11.482913+00:00", time.Time{}, false},
		{"2026-08-26 14:03:11.482913Z", time.Time{}, false},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if (err == nil) != c.ok {
			t.Errorf("Parse(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && (!got.Equal(c.want) || got.Location() != time.UTC) {
			t.Errorf("Parse(%q) = %v, want %v in UTC", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/clock/`
Expected: FAIL, undefined `Format`, `Parse`.

- [ ] **Step 3: Implement**

`internal/clock/clock.go`:

```go
// Package clock is the only source of process time and the only place the
// on-disk timestamp layout is spelled.
package clock

import (
	"sync"
	"time"
)

// Layout is fixed width so text order equals time order; a column mixing
// "...11Z" and "...11.482913Z" would sort the later value first.
const Layout = "2006-01-02T15:04:05.000000Z"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Fake is a settable clock for tests.
type Fake struct {
	mu sync.Mutex
	t  time.Time
}

func NewFake(t time.Time) *Fake { return &Fake{t: t} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// Format renders t in Layout.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Parse reads a value written by Format; any other layout is an error.
func Parse(s string) (time.Time, error) {
	t, err := time.Parse(Layout, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/clock/ -v`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go test ./... && make ascii`

Commit: `git add internal/clock && git commit -m "clock: add Layout, Format, Parse and Fake"`

---

### Task 3: `internal/config`

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type Config struct` (fields below), `const DefaultDataDir`, `DefaultKubeconfig`, `DefaultConfigFile`, `func Default() Config`, `func Load(path string) (Config, error)`, `func (c Config) Validate() error`, `func (c Config) DBPath() string`.

Tests and their trace:
- `TestDefaultsMatchDesign`: process doc Section 13 table of defaults.
- `TestLoad`: same section, "one file, or flags overriding it"; `artifacts_root` derives from `data_dir` unless set; a typo key must fail rather than silently keep a default.
- `TestValidateRejectsZeroOrNegative`: every duration and count is used as a ticker period, channel size or worker count; zero would hang or panic at runtime.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/BurntSushi/toml@latest
```

- [ ] **Step 2: Write the failing tests**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultsMatchDesign(t *testing.T) {
	want := Config{
		DataDir:                    ".storage",
		ArtifactsRoot:              filepath.Join(".storage", "artifacts"),
		Kubeconfig:                 "kube/config",
		RetentionDays:              3,
		SweepInterval:              time.Hour,
		StabilizationWindow:        10 * time.Minute,
		StabilizationCheckInterval: 30 * time.Second,
		LogTailLines:               50,
		LogMaxBytes:                262144,
		CaptureWorkersPerCluster:   4,
		CaptureQueueSize:           1024,
		EarlyCaptureDebounce:       60 * time.Second,
	}
	got := Default()
	if got != want {
		t.Fatalf("Default() = %+v\nwant        %+v", got, want)
	}
	if p := got.DBPath(); p != filepath.Join(".storage", "idios.db") {
		t.Fatalf("DBPath = %q", p)
	}
}

func TestLoad(t *testing.T) {
	write := func(t *testing.T, body string) string {
		p := filepath.Join(t.TempDir(), "idios.toml")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	overlay := Default()
	overlay.DataDir = "/var/lib/idios"
	overlay.ArtifactsRoot = filepath.Join("/var/lib/idios", "artifacts")
	overlay.RetentionDays = 7
	overlay.SweepInterval = 30 * time.Minute
	overlay.Kubeconfig = "/tmp/kc"

	explicitRoot := Default()
	explicitRoot.DataDir = "/d"
	explicitRoot.ArtifactsRoot = "/elsewhere"

	cases := []struct {
		name string
		path string
		want Config
		ok   bool
	}{
		{"missing file gives defaults", filepath.Join(t.TempDir(), "absent.toml"), Default(), true},
		{"file overlays and derives artifacts_root", write(t, `
data_dir = "/var/lib/idios"
retention_days = 7
sweep_interval = "30m"
kubeconfig = "/tmp/kc"
`), overlay, true},
		{"explicit artifacts_root wins", write(t, "data_dir = \"/d\"\nartifacts_root = \"/elsewhere\"\n"), explicitRoot, true},
		{"unknown key rejected", write(t, "retention_dayz = 3\n"), Config{}, false},
	}
	for _, c := range cases {
		got, err := Load(c.path)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("%s: Load = %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestValidateRejectsZeroOrNegative(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty data_dir", func(c *Config) { c.DataDir = "" }},
		{"retention 0", func(c *Config) { c.RetentionDays = 0 }},
		{"sweep interval 0", func(c *Config) { c.SweepInterval = 0 }},
		{"stabilization window 0", func(c *Config) { c.StabilizationWindow = 0 }},
		{"check interval 0", func(c *Config) { c.StabilizationCheckInterval = 0 }},
		{"tail lines 0", func(c *Config) { c.LogTailLines = 0 }},
		{"max bytes 0", func(c *Config) { c.LogMaxBytes = 0 }},
		{"workers 0", func(c *Config) { c.CaptureWorkersPerCluster = 0 }},
		{"queue 0", func(c *Config) { c.CaptureQueueSize = 0 }},
		{"debounce negative", func(c *Config) { c.EarlyCaptureDebounce = -time.Second }},
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	for _, c := range cases {
		cfg := Default()
		c.mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: Validate accepted", c.name)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL, compile errors.

- [ ] **Step 4: Implement**

`internal/config/config.go`:

```go
// Package config holds process-level settings. Clusters and watched
// namespaces are rows in the database, not settings.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Development defaults. Before a release DataDir moves to the OS user data
// directory and Kubeconfig to clientcmd's normal resolution.
const (
	DefaultDataDir    = ".storage"
	DefaultKubeconfig = "kube/config"
	DefaultConfigFile = ".storage/idios.toml"
)

type Config struct {
	DataDir                    string        `toml:"data_dir"`
	ArtifactsRoot              string        `toml:"artifacts_root"`
	Kubeconfig                 string        `toml:"kubeconfig"`
	RetentionDays              int           `toml:"retention_days"`
	SweepInterval              time.Duration `toml:"sweep_interval"`
	StabilizationWindow        time.Duration `toml:"stabilization_window"`
	StabilizationCheckInterval time.Duration `toml:"stabilization_check_interval"`
	LogTailLines               int           `toml:"log_tail_lines"`
	LogMaxBytes                int64         `toml:"log_max_bytes"`
	CaptureWorkersPerCluster   int           `toml:"capture_workers_per_cluster"`
	CaptureQueueSize           int           `toml:"capture_queue_size"`
	EarlyCaptureDebounce       time.Duration `toml:"early_capture_debounce"`
}

func Default() Config {
	c := Config{
		DataDir:                    DefaultDataDir,
		Kubeconfig:                 DefaultKubeconfig,
		RetentionDays:              3,
		SweepInterval:              time.Hour,
		StabilizationWindow:        10 * time.Minute,
		StabilizationCheckInterval: 30 * time.Second,
		LogTailLines:               50,
		LogMaxBytes:                262144,
		CaptureWorkersPerCluster:   4,
		CaptureQueueSize:           1024,
		EarlyCaptureDebounce:       60 * time.Second,
	}
	c.deriveArtifactsRoot()
	return c
}

// Load returns Default overlaid with the TOML file at path. A missing file
// is not an error; an unknown key is, so a typo cannot silently keep a
// default.
func Load(path string) (Config, error) {
	c := Default()
	c.ArtifactsRoot = ""
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c.deriveArtifactsRoot()
		return c, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("config %s: unknown keys %v", path, undecoded)
	}
	c.deriveArtifactsRoot()
	return c, nil
}

func (c *Config) deriveArtifactsRoot() {
	if c.ArtifactsRoot == "" && c.DataDir != "" {
		c.ArtifactsRoot = filepath.Join(c.DataDir, "artifacts")
	}
}

func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "idios.db")
}

func (c Config) Validate() error {
	var errs []error
	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir must not be empty"))
	}
	if c.RetentionDays < 1 {
		errs = append(errs, errors.New("retention_days must be >= 1"))
	}
	if c.SweepInterval <= 0 {
		errs = append(errs, errors.New("sweep_interval must be > 0"))
	}
	if c.StabilizationWindow <= 0 {
		errs = append(errs, errors.New("stabilization_window must be > 0"))
	}
	if c.StabilizationCheckInterval <= 0 {
		errs = append(errs, errors.New("stabilization_check_interval must be > 0"))
	}
	if c.LogTailLines < 1 {
		errs = append(errs, errors.New("log_tail_lines must be >= 1"))
	}
	if c.LogMaxBytes < 1 {
		errs = append(errs, errors.New("log_max_bytes must be >= 1"))
	}
	if c.CaptureWorkersPerCluster < 1 {
		errs = append(errs, errors.New("capture_workers_per_cluster must be >= 1"))
	}
	if c.CaptureQueueSize < 1 {
		errs = append(errs, errors.New("capture_queue_size must be >= 1"))
	}
	if c.EarlyCaptureDebounce < 0 {
		errs = append(errs, errors.New("early_capture_debounce must be >= 0"))
	}
	return errors.Join(errs...)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS. If `SweepInterval` does not decode from `"30m"`, confirm `BurntSushi/toml` is v1.x.

- [ ] **Step 6: Checkpoint**

Run: `go mod tidy && go build ./... && go test ./... && make ascii`

Commit: `git add go.mod go.sum internal/config && git commit -m "config: add settings with development defaults"`

---

### Task 4: `store.Open` with per-connection pragmas

**Files:**
- Create: `internal/store/open.go`, `internal/store/writer.go` (struct only; `Tx` is Task 6), `internal/store/reader.go`, `internal/store/testhelpers_test.go`
- Test: `internal/store/open_test.go`

**Interfaces:**
- Produces: `type Store struct { Writer *Writer; Reader *Reader }`, `func Open(path string) (*Store, error)`, `func (s *Store) Close() error`, `type Writer struct` (unexported `db`, `mu`), `type Reader struct` with `DB() *sql.DB`. Test helper `openTestStore(t) *Store`.

`modernc.org/sqlite` applies `_pragma=` DSN parameters to every new connection, which is how "set on every connection, not once" is satisfied.

Tests and their trace:
- `TestConnectionsCarryPragmas`: storage doc Section 10 pragma list; process doc Section 3 "pragmas are set on every new connection". Checks writer and reader values, and that the reader refuses a write.
- `TestDBFileIsPrivate`: process doc Section 12, "files 0600".

- [ ] **Step 1: Add the driver**

```bash
go get modernc.org/sqlite@latest
```

- [ ] **Step 2: Write the failing tests**

`internal/store/testhelpers_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "idios.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
```

`internal/store/open_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
)

func pragma(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return strings.ToLower(v)
}

func TestConnectionsCarryPragmas(t *testing.T) {
	s := openTestStore(t)
	cases := []struct {
		db     *sql.DB
		pragma string
		want   string
	}{
		{s.Writer.db, "journal_mode", "wal"},
		{s.Writer.db, "synchronous", "1"},
		{s.Writer.db, "foreign_keys", "1"},
		{s.Writer.db, "busy_timeout", "5000"},
		{s.Writer.db, "query_only", "0"},
		{s.Reader.DB(), "foreign_keys", "1"},
		{s.Reader.DB(), "query_only", "1"},
	}
	for _, c := range cases {
		if got := pragma(t, c.db, c.pragma); got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
	if _, err := s.Reader.DB().ExecContext(context.Background(), "CREATE TABLE t (x INTEGER) STRICT"); err == nil {
		t.Error("reader executed a write")
	}
}

func TestDBFileIsPrivate(t *testing.T) {
	s := openTestStore(t)
	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("db file mode = %o, want 600", perm)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL, undefined `Open`.

- [ ] **Step 4: Implement**

`internal/store/writer.go`:

```go
package store

import (
	"database/sql"
	"sync"
)

// Writer owns the single write connection; every mutation goes through it.
type Writer struct {
	db *sql.DB
	mu sync.Mutex
}
```

`internal/store/reader.go`:

```go
package store

import "database/sql"

// Reader is the read-only pool on the same file.
type Reader struct {
	db *sql.DB
}

func (r *Reader) DB() *sql.DB { return r.db }
```

`internal/store/open.go`:

```go
// Package store owns the SQLite file: opening, migrations, the single
// Writer, the read-only Reader, and the row types other packages speak in.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	Writer *Writer
	Reader *Reader
	path   string
}

// busy_timeout first: journal_mode fails on a briefly locked file.
var basePragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"foreign_keys(1)",
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	for _, p := range basePragmas {
		q.Add("_pragma", p)
	}
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	return "file:" + path + "?" + q.Encode()
}

// Open opens both handles, creating the directory (0700) and file (0600)
// if needed. It does not migrate.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// Created here rather than by the driver so the mode is 0600 regardless of umask.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create db file: %w", err)
	}
	_ = f.Close()

	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("ping writer: %w", err)
	}

	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	if err := r.Ping(); err != nil {
		_ = w.Close()
		_ = r.Close()
		return nil, fmt.Errorf("ping reader: %w", err)
	}

	return &Store{Writer: &Writer{db: w}, Reader: &Reader{db: r}, path: path}, nil
}

func (s *Store) Close() error {
	return errors.Join(s.Reader.db.Close(), s.Writer.db.Close())
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS. `journal_mode = "delete"` means the `_pragma` parameters were not applied; check the DSN begins with `file:`.

- [ ] **Step 6: Checkpoint**

Run: `go mod tidy && go build ./... && go test ./... && make ascii`

Commit: `git add go.mod go.sum internal/store && git commit -m "store: open writer and reader with per-connection pragmas"`

---

### Task 5: Migrations and `0001_init.sql`

**Files:**
- Create: `internal/store/migrate.go`, `internal/store/migrations/0001_init.sql`
- Modify: `internal/store/testhelpers_test.go` (add `testEpoch`, `openMigratedStore`)
- Test: `internal/store/migrate_test.go`

**Interfaces:**
- Consumes: `Store`, `Writer.db`, `clock.Clock`, `clock.Format`.
- Produces: `func (s *Store) Migrate(ctx, clk clock.Clock) error`, `func (s *Store) SchemaVersion(ctx) (int, error)`, helper `openMigratedStore(t) (*Store, *clock.Fake)`.

Tests and their trace:
- `TestMigrateFromEmpty`: process doc Section 14.2 "migrations apply cleanly from empty"; asserts the exact table set, version 1, and `applied_at` equal to the injected clock in the fixed layout (Section 9: process time from the injected clock, one layout).
- `TestMigrateTwiceIsNoop`: Section 3, "recorded in schema_migrations"; a rerun must not reapply.
- `TestAllTablesAreStrict`: storage doc Section 5, "Tables are STRICT".

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/testhelpers_test.go`:

```go
import (
	"context"
	"time"

	"idios/internal/clock"
)

var testEpoch = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

func openMigratedStore(t *testing.T) (*Store, *clock.Fake) {
	t.Helper()
	s := openTestStore(t)
	clk := clock.NewFake(testEpoch)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return s, clk
}
```

(Merge into the existing import block.)

`internal/store/migrate_test.go`:

```go
package store

import (
	"context"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func TestMigrateFromEmpty(t *testing.T) {
	s, _ := openMigratedStore(t)
	ctx := context.Background()

	rows, err := s.Reader.DB().QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	want := []string{
		"artifacts", "clusters", "container_state_history", "containers", "incidents", "jobs",
		"k8s_events", "pod_condition_history", "pods", "rollout_history", "schema_migrations",
		"sweep_runs", "watched_namespaces",
	}
	if d := cmp.Diff(want, tables); d != "" {
		t.Fatalf("tables mismatch:\n%s", d)
	}

	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", v)
	}
	var applied string
	if err := s.Reader.DB().QueryRowContext(ctx, "SELECT applied_at FROM schema_migrations WHERE version = 1").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if want := clock.Format(testEpoch); applied != want {
		t.Fatalf("applied_at = %q, want %q", applied, want)
	}
}

func TestMigrateTwiceIsNoop(t *testing.T) {
	s, clk := openMigratedStore(t)
	if err := s.Migrate(context.Background(), clk); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
}

func TestAllTablesAreStrict(t *testing.T) {
	s, _ := openMigratedStore(t)
	rows, err := s.Reader.DB().QueryContext(context.Background(),
		"SELECT name, sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	strict := regexp.MustCompile(`(?i)\)\s*STRICT\s*$`)
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		if !strict.MatchString(ddl) {
			t.Errorf("%s is not STRICT", name)
		}
	}
}
```

Run `go get github.com/google/go-cmp/cmp` for the diff.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/`
Expected: FAIL, `Migrate` undefined.

- [ ] **Step 3: Write the schema**

`internal/store/migrations/0001_init.sql`. Table order follows foreign keys: `clusters`, `watched_namespaces`, `pods`, `jobs`, `incidents`, then everything that references `incidents`.

```sql
CREATE TABLE clusters (
    id                INTEGER PRIMARY KEY,
    identity          TEXT UNIQUE,
    name              TEXT NOT NULL DEFAULT '',
    context_name      TEXT NOT NULL,
    api_server_url    TEXT NOT NULL,
    first_seen_at     TEXT NOT NULL,
    last_connected_at TEXT,
    last_error        TEXT,
    last_error_at     TEXT
) STRICT;

CREATE TABLE watched_namespaces (
    id         INTEGER PRIMARY KEY,
    cluster_id INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    added_at   TEXT NOT NULL,
    UNIQUE (cluster_id, name)
) STRICT;

CREATE TABLE pods (
    uid                   TEXT PRIMARY KEY,
    cluster_id            INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace             TEXT NOT NULL,
    name                  TEXT NOT NULL,
    node_name             TEXT,
    phase                 TEXT NOT NULL,
    status_reason         TEXT,
    status_message        TEXT,
    deletion_requested_at TEXT,
    qos_class             TEXT,
    controller_kind       TEXT NOT NULL DEFAULT 'none',
    controller_name       TEXT NOT NULL DEFAULT '',
    controller_uid        TEXT NOT NULL DEFAULT '',
    workload_kind         TEXT NOT NULL DEFAULT 'none',
    workload_name         TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    started_at            TEXT,
    first_seen_at         TEXT NOT NULL,
    last_seen_at          TEXT NOT NULL,
    deleted_at            TEXT,
    deletion_source       TEXT CHECK (deletion_source IN ('watch', 'reconcile', 'unwatched')),
    deletion_reason       TEXT CHECK (deletion_reason IN ('rollout', 'replaced', 'scaled_down', 'job_pruned', 'unknown'))
) STRICT;
CREATE INDEX pods_cluster_deleted ON pods (cluster_id, deleted_at);
CREATE INDEX pods_cluster_ns_name ON pods (cluster_id, namespace, name);
CREATE INDEX pods_workload        ON pods (cluster_id, workload_kind, workload_name);
CREATE INDEX pods_deleted         ON pods (deleted_at);

CREATE TABLE jobs (
    uid               TEXT PRIMARY KEY,
    cluster_id        INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace         TEXT NOT NULL,
    name              TEXT NOT NULL,
    cronjob_uid       TEXT,
    cronjob_name      TEXT,
    active            INTEGER NOT NULL DEFAULT 0,
    succeeded         INTEGER NOT NULL DEFAULT 0,
    failed            INTEGER NOT NULL DEFAULT 0,
    backoff_limit     INTEGER,
    completions       INTEGER,
    parallelism       INTEGER,
    restart_policy    TEXT NOT NULL DEFAULT '',
    condition_type    TEXT CHECK (condition_type IN ('Complete', 'Failed', 'Suspended')),
    condition_reason  TEXT,
    condition_message TEXT,
    created_at        TEXT NOT NULL,
    started_at        TEXT,
    finished_at       TEXT,
    first_seen_at     TEXT NOT NULL,
    last_seen_at      TEXT NOT NULL,
    deleted_at        TEXT
) STRICT;
CREATE INDEX jobs_cronjob ON jobs (cluster_id, namespace, cronjob_uid, started_at);
CREATE INDEX jobs_sweep   ON jobs (deleted_at, finished_at);

CREATE TABLE incidents (
    id              INTEGER PRIMARY KEY,
    cluster_id      INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace       TEXT NOT NULL,
    subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('pod', 'job')),
    pod_uid         TEXT REFERENCES pods(uid) ON DELETE CASCADE,
    job_uid         TEXT REFERENCES jobs(uid) ON DELETE CASCADE,
    container_name  TEXT NOT NULL DEFAULT '',
    workload_kind   TEXT NOT NULL DEFAULT 'none',
    workload_name   TEXT NOT NULL DEFAULT '',
    category        TEXT NOT NULL CHECK (category IN (
                        'oom', 'crash', 'image_pull', 'config', 'probe', 'scheduling',
                        'node_pressure', 'rescheduled', 'job_failed', 'other')),
    first_reason    TEXT NOT NULL,
    last_reason     TEXT NOT NULL,
    last_message    TEXT,
    image           TEXT,
    image_tag       TEXT,
    image_id        TEXT,
    occurrences     INTEGER NOT NULL DEFAULT 1,
    opened_at       TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    closed_at       TEXT,
    close_reason    TEXT CHECK (close_reason IN ('recovered', 'pod_deleted', 'job_finished', 'manual')),
    acknowledged_at TEXT,
    dismissed_at    TEXT,
    note            TEXT,
    CHECK ((subject_kind = 'pod') = (pod_uid IS NOT NULL)),
    CHECK ((subject_kind = 'job') = (job_uid IS NOT NULL)),
    CHECK ((closed_at IS NULL) = (close_reason IS NULL))
) STRICT;
-- Only OPEN rows are unique per key; closed rows never conflict, so a plain
-- insert after a close cannot fail and reopen is a separate path.
CREATE UNIQUE INDEX incidents_open_pod ON incidents (pod_uid, container_name, category)
    WHERE closed_at IS NULL AND subject_kind = 'pod';
CREATE UNIQUE INDEX incidents_open_job ON incidents (job_uid, category)
    WHERE closed_at IS NULL AND subject_kind = 'job';
CREATE INDEX incidents_cluster_closed ON incidents (cluster_id, closed_at);
CREATE INDEX incidents_workload       ON incidents (workload_kind, workload_name, opened_at);
CREATE INDEX incidents_closed         ON incidents (closed_at);
CREATE INDEX incidents_pod_closed     ON incidents (pod_uid, closed_at);
CREATE INDEX incidents_job_closed     ON incidents (job_uid, closed_at);

CREATE TABLE pod_condition_history (
    id                INTEGER PRIMARY KEY,
    pod_uid           TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    type              TEXT NOT NULL,
    status            TEXT NOT NULL,
    reason            TEXT NOT NULL DEFAULT '',
    message           TEXT,
    k8s_transition_at TEXT,
    observed_at       TEXT NOT NULL
) STRICT;
CREATE INDEX pod_condition_history_pod      ON pod_condition_history (pod_uid, observed_at);
CREATE INDEX pod_condition_history_observed ON pod_condition_history (observed_at);

CREATE TABLE containers (
    id                        INTEGER PRIMARY KEY,
    pod_uid                   TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    name                      TEXT NOT NULL,
    kind                      TEXT NOT NULL CHECK (kind IN ('init', 'sidecar', 'app', 'ephemeral')),
    image                     TEXT NOT NULL,
    image_tag                 TEXT,
    image_id                  TEXT,
    container_id              TEXT,
    cpu_request               TEXT,
    cpu_limit                 TEXT,
    mem_request               TEXT,
    mem_limit                 TEXT,
    cpu_request_millis        INTEGER,
    cpu_limit_millis          INTEGER,
    mem_request_bytes         INTEGER,
    mem_limit_bytes           INTEGER,
    state                     TEXT NOT NULL CHECK (state IN ('waiting', 'running', 'terminated')),
    reason                    TEXT,
    exit_code                 INTEGER,
    signal                    INTEGER,
    ready                     INTEGER NOT NULL DEFAULT 0,
    restart_count             INTEGER NOT NULL DEFAULT 0,
    running_since             TEXT,
    last_terminated_reason    TEXT,
    last_terminated_exit_code INTEGER,
    last_terminated_signal    INTEGER,
    last_terminated_at        TEXT,
    updated_at                TEXT NOT NULL,
    UNIQUE (pod_uid, name)
) STRICT;

CREATE TABLE container_state_history (
    id                INTEGER PRIMARY KEY,
    pod_uid           TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    container_name    TEXT NOT NULL,
    incident_id       INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    image             TEXT NOT NULL,
    image_id          TEXT,
    container_id      TEXT,
    state             TEXT NOT NULL CHECK (state IN ('waiting', 'running', 'terminated')),
    reason            TEXT,
    exit_code         INTEGER,
    signal            INTEGER,
    restart_count     INTEGER NOT NULL,
    category          TEXT,
    k8s_started_at    TEXT,
    k8s_finished_at   TEXT,
    observed_at       TEXT NOT NULL,
    gap_reconstructed INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE INDEX container_state_history_pod      ON container_state_history (pod_uid, observed_at);
CREATE INDEX container_state_history_incident ON container_state_history (incident_id);
CREATE INDEX container_state_history_observed ON container_state_history (observed_at);

CREATE TABLE rollout_history (
    id              INTEGER PRIMARY KEY,
    cluster_id      INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace       TEXT NOT NULL,
    deployment_name TEXT NOT NULL DEFAULT '',
    deployment_uid  TEXT NOT NULL DEFAULT '',
    replicaset_uid  TEXT NOT NULL,
    replicaset_name TEXT NOT NULL,
    container_name  TEXT NOT NULL,
    image           TEXT NOT NULL,
    image_tag       TEXT,
    revision        INTEGER,
    first_seen_at   TEXT NOT NULL,
    last_seen_at    TEXT NOT NULL,
    deleted_at      TEXT,
    UNIQUE (replicaset_uid, container_name)
) STRICT;
CREATE INDEX rollout_history_deployment ON rollout_history (cluster_id, namespace, deployment_name, first_seen_at);
CREATE INDEX rollout_history_sweep      ON rollout_history (deleted_at, last_seen_at);

CREATE TABLE k8s_events (
    id               INTEGER PRIMARY KEY,
    cluster_id       INTEGER NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    event_uid        TEXT NOT NULL,
    namespace        TEXT NOT NULL,
    type             TEXT NOT NULL,
    involved_kind    TEXT NOT NULL,
    involved_name    TEXT NOT NULL,
    involved_uid     TEXT NOT NULL,
    field_path       TEXT NOT NULL DEFAULT '',
    reason           TEXT NOT NULL,
    message          TEXT NOT NULL DEFAULT '',
    source_component TEXT NOT NULL DEFAULT '',
    count            INTEGER NOT NULL DEFAULT 1,
    first_ts         TEXT NOT NULL,
    last_ts          TEXT NOT NULL,
    category         TEXT,
    incident_id      INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    raw_json         TEXT NOT NULL,
    UNIQUE (cluster_id, event_uid)
) STRICT;
CREATE INDEX k8s_events_involved   ON k8s_events (involved_uid, last_ts);
CREATE INDEX k8s_events_cluster_ns ON k8s_events (cluster_id, namespace, last_ts);
CREATE INDEX k8s_events_last_ts    ON k8s_events (last_ts);
CREATE INDEX k8s_events_incident   ON k8s_events (incident_id);

CREATE TABLE artifacts (
    id             INTEGER PRIMARY KEY,
    pod_uid        TEXT NOT NULL REFERENCES pods(uid) ON DELETE CASCADE,
    incident_id    INTEGER REFERENCES incidents(id) ON DELETE SET NULL,
    container_name TEXT NOT NULL DEFAULT '',
    kind           TEXT NOT NULL CHECK (kind IN ('log_previous', 'log_current', 'pod_json')),
    restart_count  INTEGER NOT NULL,
    file_path      TEXT,
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    truncated      INTEGER NOT NULL DEFAULT 0,
    captured_early INTEGER NOT NULL DEFAULT 0,
    capture_gap    TEXT CHECK (capture_gap IN (
                       'pod_deleted', 'no_previous_run', 'forbidden', 'no_output',
                       'kubelet_error', 'unknown', 'unobservable')),
    capture_note   TEXT,
    captured_at    TEXT NOT NULL,
    UNIQUE (pod_uid, container_name, kind, restart_count),
    CHECK ((file_path IS NULL) = (capture_gap IS NOT NULL))
) STRICT;
CREATE INDEX artifacts_incident ON artifacts (incident_id);

CREATE TABLE sweep_runs (
    id            INTEGER PRIMARY KEY,
    ran_at        TEXT NOT NULL,
    cutoff        TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    rows_removed  INTEGER NOT NULL DEFAULT 0,
    files_removed INTEGER NOT NULL DEFAULT 0,
    bytes_removed INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    error         TEXT
) STRICT;
```

Decisions that go one step past the spec's prose, for the reviewer (not for the code):
- `pods.controller_*` / `workload_*` are `NOT NULL` with `'none'` / `''` defaults; the spec lists `none` as a value and `controller_uid` is a join key.
- `CHECK ((closed_at IS NULL) = (close_reason IS NULL))` on incidents: close and reopen always set or clear both.
- `CHECK ((file_path IS NULL) = (capture_gap IS NOT NULL))` on artifacts: "no file" and "gap set" are one fact.
- `k8s_events_incident` index: same `SET NULL` cascade reasoning the spec gives for `artifacts`.

- [ ] **Step 4: Write the migrator**

`internal/store/migrate.go`:

```go
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"idios/internal/clock"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %q: name must be NNNN_description.sql", name)
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %q: bad version prefix: %w", name, err)
		}
		body, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// Migrate applies every migration above the recorded version, each in its
// own transaction. There are no down migrations; the file is a cache.
func (s *Store) Migrate(ctx context.Context, clk clock.Clock) error {
	db := s.Writer.db
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
) STRICT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyOne(ctx, db, m, clock.Format(clk.Now())); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration, appliedAt string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", m.version, appliedAt); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// SchemaVersion is the highest applied migration, 0 if none.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	if err := s.Writer.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -v -run 'Migrate|Strict'`
Expected: PASS. A DDL error names the statement; fix the SQL, not the test.

- [ ] **Step 6: Checkpoint**

Run: `go mod tidy && go build ./... && go test ./... && make ascii`

Commit: `git add go.mod go.sum internal/store && git commit -m "store: add schema migration 0001 and migrator"`

---

### Task 6: `Writer.Tx`

**Files:**
- Modify: `internal/store/writer.go`
- Test: `internal/store/writer_test.go`

**Interfaces:**
- Produces: `func (w *Writer) Tx(ctx context.Context, fn func(*sql.Tx) error) error`. Commit on nil; roll back on error; roll back and re-panic on panic. Test helpers `insertCluster`, `countRows`.

Tests and their trace:
- `TestTxCommitsOrRollsBack`: process doc Section 3, "Begin, run fn, commit or roll back". Table: nil -> row present; error -> row absent; panic -> row absent and panic propagates (Section 10: panics are recovered at the handler boundary, so `Tx` must leave the DB clean and let the panic reach it).
- `TestReaderIsNotBlockedByOpenTx`: Section 3, "WAL mode means readers never block the writer and the writer never blocks readers".

Serialisation by mutex is not tested separately: `SetMaxOpenConns(1)` plus the mutex make concurrent `Tx` calls queue, and a test that measures "max in flight" tests the mutex, not a behaviour the spec states beyond "contention is the mutex".

- [ ] **Step 1: Write the failing tests**

`internal/store/writer_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"idios/internal/clock"
)

func insertCluster(t *testing.T, s *Store, identity string) int64 {
	t.Helper()
	var id int64
	err := s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		res, err := tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES (?, 'c', 'ctx', 'https://127.0.0.1:26443', ?)`, identity, clock.Format(testEpoch))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("insertCluster: %v", err)
	}
	return id
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.Reader.DB().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTxCommitsOrRollsBack(t *testing.T) {
	sentinel := errors.New("handler error")
	insert := func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES ('x', 'c', 'ctx', 'u', ?)`, clock.Format(testEpoch))
		return err
	}
	cases := []struct {
		name      string
		fn        func(*sql.Tx) error
		wantErr   error
		wantPanic bool
		wantRows  int
	}{
		{"nil commits", insert, nil, false, 1},
		{"error rolls back", func(tx *sql.Tx) error { _ = insert(tx); return sentinel }, sentinel, false, 0},
		{"panic rolls back and propagates", func(tx *sql.Tx) error { _ = insert(tx); panic("bug") }, nil, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openMigratedStore(t)
			var err error
			panicked := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						panicked = true
					}
				}()
				err = s.Writer.Tx(context.Background(), c.fn)
			}()
			if panicked != c.wantPanic {
				t.Fatalf("panicked = %v, want %v", panicked, c.wantPanic)
			}
			if !c.wantPanic && !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if got := countRows(t, s, "clusters"); got != c.wantRows {
				t.Fatalf("rows = %d, want %d", got, c.wantRows)
			}
		})
	}
}

func TestReaderIsNotBlockedByOpenTx(t *testing.T) {
	s, _ := openMigratedStore(t)
	insertCluster(t, s, "committed")
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
			_, _ = tx.ExecContext(context.Background(), `
INSERT INTO clusters (identity, name, context_name, api_server_url, first_seen_at)
VALUES ('uncommitted', 'c', 'ctx', 'u', ?)`, clock.Format(testEpoch))
			<-release
			return nil
		})
	}()
	got := make(chan int, 1)
	go func() { got <- countRows(t, s, "clusters") }()
	select {
	case n := <-got:
		if n != 1 {
			t.Errorf("reader saw %d rows, want 1 (only committed)", n)
		}
	case <-time.After(2 * time.Second):
		t.Error("reader blocked behind open write transaction")
	}
	close(release)
	<-done
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Tx|Reader'`
Expected: FAIL, `Tx` undefined.

- [ ] **Step 3: Implement**

Replace `internal/store/writer.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

// Writer owns the single write connection; every mutation goes through it.
type Writer struct {
	db *sql.DB
	mu sync.Mutex
}

// Tx runs fn in one transaction. Commit on nil, roll back on error. A panic
// in fn rolls back and is re-raised so the handler boundary can log it.
func (w *Writer) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -v -run 'Tx|Reader'`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go test ./... -race && make ascii`

Commit: `git add internal/store && git commit -m "store: add Writer.Tx"`

---

### Task 7: Schema behaviour tests

**Files:**
- Test: `internal/store/schema_test.go`

No production code; this proves the DDL from Task 5. A failure means fix `0001_init.sql`.

Tests and their trace (all process doc Section 14.2 unless noted):
- `TestStrictRejectsWrongType`: "STRICT rejects a wrong type".
- `TestCascades`: "every cascade removes what the storage doc says". Table: delete pod removes containers, both histories, incidents, artifacts; delete cluster removes namespaces, pods, jobs, events, rollouts; delete incident sets `incident_id` NULL on history, events, artifacts and keeps those rows.
- `TestOpenIncidentUniqueness`: "both partial unique indexes reject a duplicate open incident and accept one after close, including the `container_name = ''` and job cases".
- `TestArtifactUniqueness`: storage doc 5.11, `restart_count = -1` and `container_name = ''` so the index has no NULL; two `pod_json` rows must conflict.
- `TestIncidentChecks`: storage doc 5.7 CHECK constraints plus the closed_at/close_reason pairing added in Task 5.

- [ ] **Step 1: Write the tests**

`internal/store/schema_test.go`:

```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"idios/internal/clock"
)

func exec(t *testing.T, s *Store, query string, args ...any) (sql.Result, error) {
	t.Helper()
	var res sql.Result
	err := s.Writer.Tx(context.Background(), func(tx *sql.Tx) error {
		var err error
		res, err = tx.ExecContext(context.Background(), query, args...)
		return err
	})
	return res, err
}

func mustExec(t *testing.T, s *Store, query string, args ...any) sql.Result {
	t.Helper()
	res, err := exec(t, s, query, args...)
	if err != nil {
		t.Fatalf("exec: %v\n%s", err, query)
	}
	return res
}

func insertPod(t *testing.T, s *Store, clusterID int64, uid string) {
	t.Helper()
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO pods (uid, cluster_id, namespace, name, phase, created_at, first_seen_at, last_seen_at)
VALUES (?, ?, 'idios-smoke', ?, 'Running', ?, ?, ?)`, uid, clusterID, "pod-"+uid, ts, ts, ts)
}

func insertJob(t *testing.T, s *Store, clusterID int64, uid string) {
	t.Helper()
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO jobs (uid, cluster_id, namespace, name, restart_policy, created_at, first_seen_at, last_seen_at)
VALUES (?, ?, 'idios-smoke', ?, 'Never', ?, ?, ?)`, uid, clusterID, "job-"+uid, ts, ts, ts)
}

func insertPodIncident(t *testing.T, s *Store, clusterID int64, podUID, container, category string) (int64, error) {
	t.Helper()
	ts := clock.Format(testEpoch)
	res, err := exec(t, s, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, pod_uid, container_name, category,
                       first_reason, last_reason, opened_at, last_seen_at)
VALUES (?, 'idios-smoke', 'pod', ?, ?, ?, 'r', 'r', ?, ?)`, clusterID, podUID, container, category, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func insertJobIncident(t *testing.T, s *Store, clusterID int64, jobUID, category string) (int64, error) {
	t.Helper()
	ts := clock.Format(testEpoch)
	res, err := exec(t, s, `
INSERT INTO incidents (cluster_id, namespace, subject_kind, job_uid, category,
                       first_reason, last_reason, opened_at, last_seen_at)
VALUES (?, 'idios-smoke', 'job', ?, ?, 'r', 'r', ?, ?)`, clusterID, jobUID, category, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func closeIncident(t *testing.T, s *Store, id int64, reason string) {
	t.Helper()
	mustExec(t, s, "UPDATE incidents SET closed_at = ?, close_reason = ? WHERE id = ?", clock.Format(testEpoch), reason, id)
}

func insertArtifact(t *testing.T, s *Store, podUID, container, kind string, restart int, incidentID *int64) error {
	t.Helper()
	_, err := exec(t, s, `
INSERT INTO artifacts (pod_uid, incident_id, container_name, kind, restart_count, file_path, captured_at)
VALUES (?, ?, ?, ?, ?, 'f', ?)`, podUID, incidentID, container, kind, restart, clock.Format(testEpoch))
	return err
}

func nullInt(t *testing.T, s *Store, query string, args ...any) sql.NullInt64 {
	t.Helper()
	var v sql.NullInt64
	if err := s.Reader.DB().QueryRowContext(context.Background(), query, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestStrictRejectsWrongType(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	_, err := exec(t, s, `
INSERT INTO containers (pod_uid, name, kind, image, state, restart_count, updated_at)
VALUES ('p1', 'api', 'app', 'img', 'running', 'three', ?)`, clock.Format(testEpoch))
	if err == nil {
		t.Fatal("TEXT accepted in INTEGER column")
	}
}

func TestCascades(t *testing.T) {
	ts := clock.Format(testEpoch)
	seed := func(t *testing.T) (*Store, int64, int64) {
		s, _ := openMigratedStore(t)
		cid := insertCluster(t, s, "c")
		mustExec(t, s, "INSERT INTO watched_namespaces (cluster_id, name, added_at) VALUES (?, 'idios-smoke', ?)", cid, ts)
		insertPod(t, s, cid, "p1")
		insertJob(t, s, cid, "j1")
		mustExec(t, s, `INSERT INTO containers (pod_uid, name, kind, image, state, updated_at) VALUES ('p1', 'api', 'app', 'img', 'running', ?)`, ts)
		inc, err := insertPodIncident(t, s, cid, "p1", "api", "crash")
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, s, `INSERT INTO container_state_history (pod_uid, container_name, incident_id, image, state, restart_count, observed_at)
			VALUES ('p1', 'api', ?, 'img', 'waiting', 1, ?)`, inc, ts)
		mustExec(t, s, `INSERT INTO pod_condition_history (pod_uid, type, status, observed_at) VALUES ('p1', 'Ready', 'False', ?)`, ts)
		mustExec(t, s, `INSERT INTO k8s_events (cluster_id, event_uid, namespace, type, involved_kind, involved_name, involved_uid, reason, first_ts, last_ts, incident_id, raw_json)
			VALUES (?, 'e1', 'idios-smoke', 'Warning', 'Pod', 'p', 'p1', 'BackOff', ?, ?, ?, '{}')`, cid, ts, ts, inc)
		if err := insertArtifact(t, s, "p1", "api", "log_previous", 0, &inc); err != nil {
			t.Fatal(err)
		}
		mustExec(t, s, `INSERT INTO rollout_history (cluster_id, namespace, replicaset_uid, replicaset_name, container_name, image, first_seen_at, last_seen_at)
			VALUES (?, 'idios-smoke', 'rs1', 'web-abc', 'web', 'web:1', ?, ?)`, cid, ts, ts)
		return s, cid, inc
	}

	t.Run("delete pod removes everything under it", func(t *testing.T) {
		s, _, _ := seed(t)
		mustExec(t, s, "DELETE FROM pods WHERE uid = 'p1'")
		for _, table := range []string{"containers", "incidents", "container_state_history", "pod_condition_history", "artifacts"} {
			if n := countRows(t, s, table); n != 0 {
				t.Errorf("%s = %d rows, want 0", table, n)
			}
		}
		if n := countRows(t, s, "k8s_events"); n != 1 {
			t.Errorf("k8s_events = %d, want 1 (swept by age, not cascade)", n)
		}
	})

	t.Run("delete cluster removes everything", func(t *testing.T) {
		s, cid, _ := seed(t)
		mustExec(t, s, "DELETE FROM clusters WHERE id = ?", cid)
		for _, table := range []string{"watched_namespaces", "pods", "jobs", "incidents", "k8s_events", "rollout_history", "artifacts"} {
			if n := countRows(t, s, table); n != 0 {
				t.Errorf("%s = %d rows, want 0", table, n)
			}
		}
	})

	t.Run("delete incident detaches but keeps evidence", func(t *testing.T) {
		s, _, inc := seed(t)
		mustExec(t, s, "DELETE FROM incidents WHERE id = ?", inc)
		for _, q := range []string{
			"SELECT incident_id FROM container_state_history",
			"SELECT incident_id FROM k8s_events",
			"SELECT incident_id FROM artifacts",
		} {
			if v := nullInt(t, s, q); v.Valid {
				t.Errorf("%s: incident_id = %d, want NULL", q, v.Int64)
			}
		}
		for _, table := range []string{"container_state_history", "k8s_events", "artifacts"} {
			if n := countRows(t, s, table); n != 1 {
				t.Errorf("%s = %d rows, want 1", table, n)
			}
		}
	})
}

func TestOpenIncidentUniqueness(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")

	podCase := func(container string) func() (int64, error) {
		return func() (int64, error) { return insertPodIncident(t, s, cid, "p1", container, "crash") }
	}
	jobCase := func() (int64, error) { return insertJobIncident(t, s, cid, "j1", "job_failed") }

	cases := []struct {
		name string
		open func() (int64, error)
	}{
		{"container-level pod incident", podCase("api")},
		{"pod-level incident with container_name ''", podCase("")},
		{"job incident", jobCase},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first, err := c.open()
			if err != nil {
				t.Fatalf("first open: %v", err)
			}
			if _, err := c.open(); err == nil {
				t.Fatal("second open with same key accepted")
			}
			closeIncident(t, s, first, "recovered")
			if _, err := c.open(); err != nil {
				t.Fatalf("open after close rejected: %v", err)
			}
		})
	}
	if _, err := insertPodIncident(t, s, cid, "p1", "api", "oom"); err != nil {
		t.Fatalf("different category rejected: %v", err)
	}
}

func TestArtifactUniqueness(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	cases := []struct {
		name      string
		container string
		kind      string
		restart   int
	}{
		{"log_previous per dead instance", "api", "log_previous", 0},
		{"pod_json uses '' and -1", "", "pod_json", -1},
		{"log_current uses -1", "api", "log_current", -1},
	}
	for _, c := range cases {
		if err := insertArtifact(t, s, "p1", c.container, c.kind, c.restart, nil); err != nil {
			t.Fatalf("%s: first insert: %v", c.name, err)
		}
		if err := insertArtifact(t, s, "p1", c.container, c.kind, c.restart, nil); err == nil {
			t.Errorf("%s: duplicate accepted", c.name)
		}
	}
	if err := insertArtifact(t, s, "p1", "api", "log_previous", 1, nil); err != nil {
		t.Fatalf("next restart index rejected: %v", err)
	}
}

func TestIncidentChecks(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	insertPod(t, s, cid, "p1")
	insertJob(t, s, cid, "j1")
	ts := clock.Format(testEpoch)
	cases := []struct {
		name string
		cols string
		vals []any
	}{
		{"pod kind without pod_uid",
			"cluster_id, namespace, subject_kind, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "pod", "crash", "r", "r", ts, ts}},
		{"job kind with pod_uid too",
			"cluster_id, namespace, subject_kind, pod_uid, job_uid, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "job", "p1", "j1", "job_failed", "r", "r", ts, ts}},
		{"unknown category",
			"cluster_id, namespace, subject_kind, pod_uid, category, first_reason, last_reason, opened_at, last_seen_at",
			[]any{cid, "n", "pod", "p1", "weird", "r", "r", ts, ts}},
		{"closed_at without close_reason",
			"cluster_id, namespace, subject_kind, pod_uid, category, first_reason, last_reason, opened_at, last_seen_at, closed_at",
			[]any{cid, "n", "pod", "p1", "crash", "r", "r", ts, ts, ts}},
	}
	for _, c := range cases {
		placeholders := "?"
		for i := 1; i < len(c.vals); i++ {
			placeholders += ", ?"
		}
		if _, err := exec(t, s, "INSERT INTO incidents ("+c.cols+") VALUES ("+placeholders+")", c.vals...); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/store/ -v -run 'Strict|Cascades|Uniqueness|Checks'`
Expected: PASS on first run. Any failure is a DDL bug.

- [ ] **Step 3: Checkpoint**

Run: `go build ./... && go test ./... && make ascii`

Commit: `git add internal/store && git commit -m "store: test cascades, uniqueness and checks"`

---

### Task 8: Row types

**Files:**
- Create: `internal/store/rows.go`
- Test: `internal/store/rows_test.go`

**Interfaces:**
- Produces: one struct per table; constants for every enumerated column; `podColumns` and `scanPod(rowScanner) (Pod, error)`. Nullable columns are pointers; timestamps are `string` in `clock.Layout`; booleans are `bool`.

Only `scanPod` gets a scanner in this phase because only it has a caller planned (Phase 3 loads the pod snapshot). Other scanners are added by the phase that needs them.

Test and trace:
- `TestScanPodRoundTrip`: the column list and the struct must agree, including NULL to nil pointer; a mismatch here would surface as a scan error deep in Phase 3.

- [ ] **Step 1: Write the failing test**

`internal/store/rows_test.go`:

```go
package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"idios/internal/clock"
)

func ptr[T any](v T) *T { return &v }

func TestScanPodRoundTrip(t *testing.T) {
	s, _ := openMigratedStore(t)
	cid := insertCluster(t, s, "c")
	ts := clock.Format(testEpoch)
	mustExec(t, s, `
INSERT INTO pods (uid, cluster_id, namespace, name, node_name, phase, status_reason, qos_class,
                  controller_kind, controller_name, controller_uid, workload_kind, workload_name,
                  created_at, first_seen_at, last_seen_at, deleted_at, deletion_source, deletion_reason)
VALUES ('p1', ?, 'idios-smoke', 'web-abc-xyz', 'node-a', 'Failed', 'Evicted', 'Burstable',
        'ReplicaSet', 'web-abc', 'rs-1', 'Deployment', 'web', ?, ?, ?, ?, 'watch', 'rollout')`,
		cid, ts, ts, ts, ts)

	got, err := scanPod(s.Reader.DB().QueryRowContext(context.Background(), "SELECT "+podColumns+" FROM pods WHERE uid = 'p1'"))
	if err != nil {
		t.Fatal(err)
	}
	want := Pod{
		UID: "p1", ClusterID: cid, Namespace: "idios-smoke", Name: "web-abc-xyz",
		NodeName: ptr("node-a"), Phase: "Failed", StatusReason: ptr("Evicted"), QOSClass: ptr("Burstable"),
		ControllerKind: "ReplicaSet", ControllerName: "web-abc", ControllerUID: "rs-1",
		WorkloadKind: "Deployment", WorkloadName: "web",
		CreatedAt: ts, FirstSeenAt: ts, LastSeenAt: ts,
		DeletedAt: ptr(ts), DeletionSource: ptr(DeletionSourceWatch), DeletionReason: ptr(DeletionReasonRollout),
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Fatal(d)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run ScanPod`
Expected: FAIL, undefined `podColumns`, `scanPod`.

- [ ] **Step 3: Implement**

`internal/store/rows.go`:

```go
package store

import "database/sql"

const (
	DeletionSourceWatch     = "watch"
	DeletionSourceReconcile = "reconcile"
	DeletionSourceUnwatched = "unwatched"
)

const (
	DeletionReasonRollout    = "rollout"
	DeletionReasonReplaced   = "replaced"
	DeletionReasonScaledDown = "scaled_down"
	DeletionReasonJobPruned  = "job_pruned"
	DeletionReasonUnknown    = "unknown"
)

const (
	ContainerKindInit      = "init"
	ContainerKindSidecar   = "sidecar"
	ContainerKindApp       = "app"
	ContainerKindEphemeral = "ephemeral"
)

const (
	StateWaiting    = "waiting"
	StateRunning    = "running"
	StateTerminated = "terminated"
)

const (
	SubjectPod = "pod"
	SubjectJob = "job"
)

const (
	CategoryOOM          = "oom"
	CategoryCrash        = "crash"
	CategoryImagePull    = "image_pull"
	CategoryConfig       = "config"
	CategoryProbe        = "probe"
	CategoryScheduling   = "scheduling"
	CategoryNodePressure = "node_pressure"
	CategoryRescheduled  = "rescheduled"
	CategoryJobFailed    = "job_failed"
	CategoryOther        = "other"
)

const (
	CloseRecovered   = "recovered"
	ClosePodDeleted  = "pod_deleted"
	CloseJobFinished = "job_finished"
	CloseManual      = "manual"
)

const (
	ArtifactLogPrevious = "log_previous"
	ArtifactLogCurrent  = "log_current"
	ArtifactPodJSON     = "pod_json"
)

const (
	GapPodDeleted    = "pod_deleted"
	GapNoPreviousRun = "no_previous_run"
	GapForbidden     = "forbidden"
	GapNoOutput      = "no_output"
	GapKubeletError  = "kubelet_error"
	GapUnknown       = "unknown"
	GapUnobservable  = "unobservable"
)

// NoRestartIndex is artifacts.restart_count for pod_json and log_current,
// so the unique index never sees a NULL.
const NoRestartIndex = -1

type Cluster struct {
	ID              int64
	Identity        *string
	Name            string
	ContextName     string
	APIServerURL    string
	FirstSeenAt     string
	LastConnectedAt *string
	LastError       *string
	LastErrorAt     *string
}

type WatchedNamespace struct {
	ID        int64
	ClusterID int64
	Name      string
	AddedAt   string
}

type Pod struct {
	UID                 string
	ClusterID           int64
	Namespace           string
	Name                string
	NodeName            *string
	Phase               string
	StatusReason        *string
	StatusMessage       *string
	DeletionRequestedAt *string
	QOSClass            *string
	ControllerKind      string
	ControllerName      string
	ControllerUID       string
	WorkloadKind        string
	WorkloadName        string
	CreatedAt           string
	StartedAt           *string
	FirstSeenAt         string
	LastSeenAt          string
	DeletedAt           *string
	DeletionSource      *string
	DeletionReason      *string
}

type PodCondition struct {
	ID              int64
	PodUID          string
	Type            string
	Status          string
	Reason          string
	Message         *string
	K8sTransitionAt *string
	ObservedAt      string
}

type Container struct {
	ID                     int64
	PodUID                 string
	Name                   string
	Kind                   string
	Image                  string
	ImageTag               *string
	ImageID                *string
	ContainerID            *string
	CPURequest             *string
	CPULimit               *string
	MemRequest             *string
	MemLimit               *string
	CPURequestMillis       *int64
	CPULimitMillis         *int64
	MemRequestBytes        *int64
	MemLimitBytes          *int64
	State                  string
	Reason                 *string
	ExitCode               *int64
	Signal                 *int64
	Ready                  bool
	RestartCount           int64
	RunningSince           *string
	LastTerminatedReason   *string
	LastTerminatedExitCode *int64
	LastTerminatedSignal   *int64
	LastTerminatedAt       *string
	UpdatedAt              string
}

type ContainerStateHistory struct {
	ID               int64
	PodUID           string
	ContainerName    string
	IncidentID       *int64
	Image            string
	ImageID          *string
	ContainerID      *string
	State            string
	Reason           *string
	ExitCode         *int64
	Signal           *int64
	RestartCount     int64
	Category         *string
	K8sStartedAt     *string
	K8sFinishedAt    *string
	ObservedAt       string
	GapReconstructed bool
}

type Incident struct {
	ID             int64
	ClusterID      int64
	Namespace      string
	SubjectKind    string
	PodUID         *string
	JobUID         *string
	ContainerName  string
	WorkloadKind   string
	WorkloadName   string
	Category       string
	FirstReason    string
	LastReason     string
	LastMessage    *string
	Image          *string
	ImageTag       *string
	ImageID        *string
	Occurrences    int64
	OpenedAt       string
	LastSeenAt     string
	ClosedAt       *string
	CloseReason    *string
	AcknowledgedAt *string
	DismissedAt    *string
	Note           *string
}

type Job struct {
	UID              string
	ClusterID        int64
	Namespace        string
	Name             string
	CronJobUID       *string
	CronJobName      *string
	Active           int64
	Succeeded        int64
	Failed           int64
	BackoffLimit     *int64
	Completions      *int64
	Parallelism      *int64
	RestartPolicy    string
	ConditionType    *string
	ConditionReason  *string
	ConditionMessage *string
	CreatedAt        string
	StartedAt        *string
	FinishedAt       *string
	FirstSeenAt      string
	LastSeenAt       string
	DeletedAt        *string
}

type RolloutHistory struct {
	ID             int64
	ClusterID      int64
	Namespace      string
	DeploymentName string
	DeploymentUID  string
	ReplicaSetUID  string
	ReplicaSetName string
	ContainerName  string
	Image          string
	ImageTag       *string
	Revision       *int64
	FirstSeenAt    string
	LastSeenAt     string
	DeletedAt      *string
}

type K8sEvent struct {
	ID              int64
	ClusterID       int64
	EventUID        string
	Namespace       string
	Type            string
	InvolvedKind    string
	InvolvedName    string
	InvolvedUID     string
	FieldPath       string
	Reason          string
	Message         string
	SourceComponent string
	Count           int64
	FirstTS         string
	LastTS          string
	Category        *string
	IncidentID      *int64
	RawJSON         string
}

type Artifact struct {
	ID            int64
	PodUID        string
	IncidentID    *int64
	ContainerName string
	Kind          string
	RestartCount  int64
	FilePath      *string
	SizeBytes     int64
	Truncated     bool
	CapturedEarly bool
	CaptureGap    *string
	CaptureNote   *string
	CapturedAt    string
}

type SweepRun struct {
	ID           int64
	RanAt        string
	Cutoff       string
	TableName    string
	RowsRemoved  int64
	FilesRemoved int64
	BytesRemoved int64
	DurationMs   int64
	Error        *string
}

const podColumns = `uid, cluster_id, namespace, name, node_name, phase, status_reason, status_message,
deletion_requested_at, qos_class, controller_kind, controller_name, controller_uid,
workload_kind, workload_name, created_at, started_at, first_seen_at, last_seen_at,
deleted_at, deletion_source, deletion_reason`

type rowScanner interface {
	Scan(dest ...any) error
}

var (
	_ rowScanner = (*sql.Row)(nil)
	_ rowScanner = (*sql.Rows)(nil)
)

func scanPod(r rowScanner) (Pod, error) {
	var p Pod
	err := r.Scan(
		&p.UID, &p.ClusterID, &p.Namespace, &p.Name, &p.NodeName, &p.Phase, &p.StatusReason, &p.StatusMessage,
		&p.DeletionRequestedAt, &p.QOSClass, &p.ControllerKind, &p.ControllerName, &p.ControllerUID,
		&p.WorkloadKind, &p.WorkloadName, &p.CreatedAt, &p.StartedAt, &p.FirstSeenAt, &p.LastSeenAt,
		&p.DeletedAt, &p.DeletionSource, &p.DeletionReason,
	)
	return p, err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/store/ -v -run ScanPod`
Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `go build ./... && go vet ./... && go test ./... && make ascii`

Commit: `git add internal/store && git commit -m "store: add row types and scanPod"`

---

### Task 9: Package dependency rule test

**Files:**
- Create: `internal/archtest/deps_test.go`

**Interfaces:**
- Produces: a test every later phase must keep green. Rules (process doc Section 2): `ingest`, `incident` never import client-go, `capture`, `k8s`; `k8s` never imports `store`; `capture` never imports `ingest`; `sweep` and `query` import only `store` and `clock` from this module; nothing under `internal/` imports `query` or `cmd`; `store` never imports `k8s.io/`.

Packages that do not exist yet pass trivially.

Test and trace:
- `TestPackageDependencyRules`: process doc Section 14.3, "a second test asserts the package dependency rules of Section 2".

- [ ] **Step 1: Write the test**

`internal/archtest/deps_test.go`:

```go
// Package archtest checks import rules by parsing import declarations.
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "idios"

var forbidden = map[string][]string{
	"internal/ingest":   {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
	"internal/incident": {"k8s.io/client-go", module + "/internal/capture", module + "/internal/k8s"},
	"internal/k8s":      {module + "/internal/store"},
	"internal/capture":  {module + "/internal/ingest"},
	"internal/store":    {"k8s.io/"},
}

var onlyInternal = map[string][]string{
	"internal/sweep": {module + "/internal/store", module + "/internal/clock"},
	"internal/query": {module + "/internal/store", module + "/internal/clock"},
}

func importsByPackage(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == "bin" || strings.HasPrefix(n, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(rel)
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			out[pkg][p] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hasPrefix(imp, prefix string) bool {
	return imp == prefix || strings.HasPrefix(imp, prefix+"/") || strings.HasSuffix(prefix, "/") && strings.HasPrefix(imp, prefix)
}

func TestPackageDependencyRules(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	imports := importsByPackage(t, filepath.Join(wd, "..", ".."))

	for pkg, bad := range forbidden {
		for imp := range imports[pkg] {
			for _, prefix := range bad {
				if hasPrefix(imp, prefix) {
					t.Errorf("%s imports %s", pkg, imp)
				}
			}
		}
	}
	for pkg, allowed := range onlyInternal {
		for imp := range imports[pkg] {
			if !strings.HasPrefix(imp, module+"/") {
				continue
			}
			ok := false
			for _, a := range allowed {
				ok = ok || imp == a
			}
			if !ok {
				t.Errorf("%s imports %s; allowed: %v", pkg, imp, allowed)
			}
		}
	}
	for pkg, set := range imports {
		if !strings.HasPrefix(pkg, "internal/") {
			continue
		}
		for imp := range set {
			if hasPrefix(imp, module+"/internal/query") || hasPrefix(imp, module+"/cmd") {
				t.Errorf("%s imports %s", pkg, imp)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 3: Prove the test bites**

Create `internal/store/tmp.go` containing `package store` and `import _ "k8s.io/client-go/kubernetes"` (the parser does not resolve imports; no `go get`). Run `go test ./internal/archtest/`; it must FAIL naming `internal/store`. Delete `tmp.go`; run again; PASS.

- [ ] **Step 4: Checkpoint**

Run: `go build ./... && go test ./... && make ascii`

Commit: `git add internal/archtest && git commit -m "archtest: enforce package dependency rules"`

---

### Task 10: Wire `cmd/idios`

**Files:**
- Modify: `cmd/idios/main.go`

**Interfaces:**
- Consumes: `config.Load`, `config.DefaultConfigFile`, `Config.Validate`, `Config.DBPath`, `store.Open`, `Store.Migrate`, `Store.SchemaVersion`, `clock.Real`.
- Produces: `idios` opens the store, migrates, logs one line, blocks until SIGINT/SIGTERM, closes. Flags `-config`, `-data-dir`, `-kubeconfig`. `idios version` prints the version. Later phases start watchers, pool, closer and sweeper between the "started" log and the wait.

No unit test: this is wiring; it is verified by running it.

- [ ] **Step 1: Replace `cmd/idios/main.go`**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"idios/internal/clock"
	"idios/internal/config"
	"idios/internal/store"
)

const version = "0.0.1-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "idios:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "version" {
		fmt.Println("idios", version)
		return nil
	}

	fs := flag.NewFlagSet("idios", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultConfigFile, "path to idios.toml")
	dataDir := fs.String("data-dir", "", "override data_dir")
	kubeconfig := fs.String("kubeconfig", "", "override kubeconfig")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
		cfg.ArtifactsRoot = filepath.Join(*dataDir, "artifacts")
	}
	if *kubeconfig != "" {
		cfg.Kubeconfig = *kubeconfig
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data_dir: %w", err)
	}
	if err := os.MkdirAll(cfg.ArtifactsRoot, 0o700); err != nil {
		return fmt.Errorf("create artifacts_root: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := st.Close(); cerr != nil {
			logger.Error("close store", "err", cerr)
		}
	}()
	if err := st.Migrate(ctx, clock.Real{}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	ver, err := st.SchemaVersion(ctx)
	if err != nil {
		return err
	}

	logger.Info("idios started",
		"version", version,
		"data_dir", cfg.DataDir,
		"db", cfg.DBPath(),
		"artifacts_root", cfg.ArtifactsRoot,
		"schema_version", ver,
		"kubeconfig", cfg.Kubeconfig,
	)

	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}
	logger.Info("idios stopping")
	return nil
}
```

- [ ] **Step 2: Build and run against `.storage`**

```bash
make build && (./bin/idios & pid=$!; sleep 2; kill -INT $pid; wait $pid)
```

Expected: `msg="idios started" ... data_dir=.storage db=.storage/idios.db artifacts_root=.storage/artifacts schema_version=1 kubeconfig=kube/config`, then `msg="idios stopping"`, exit 0.

- [ ] **Step 3: Inspect what the binary made**

```bash
stat -f '%Lp %N' .storage .storage/idios.db && sqlite3 .storage/idios.db '.tables' && sqlite3 .storage/idios.db 'PRAGMA journal_mode; SELECT * FROM schema_migrations;'
```

Expected: `700 .storage`, `600 .storage/idios.db`, thirteen tables, `wal`, `1|2026-...Z` with six fractional digits.

- [ ] **Step 4: Run twice**

Repeat Step 2. Expected: same output, `schema_version=1`, `SELECT COUNT(*) FROM schema_migrations` still 1.

- [ ] **Step 5: Override flag**

```bash
(./bin/idios -data-dir /tmp/idios-alt & pid=$!; sleep 1; kill -INT $pid; wait $pid); ls /tmp/idios-alt; rm -rf /tmp/idios-alt
```

Expected: `artifacts` and `idios.db` present.

- [ ] **Step 6: Final checkpoint**

```bash
go mod tidy && go build ./... && go vet ./... && go test ./... -race && make ascii && golangci-lint run ./...
```

Expected: clean.

Commit: `git add cmd/idios/main.go && git commit -m "idios: open store, migrate and wait for signal"`

Phase 1 is done when this passes, `.storage/idios.db` has schema version 1, and `git status` is clean.

---

## Self-review

**Spec coverage.** Storage doc Section 5: all thirteen tables and columns, listed indexes, both partial unique indexes, `''` rule, STRICT, cascades and SET NULL -> Tasks 5, 7. Section 10 -> Task 4. Section 11 / process doc Section 13 -> Task 3. Process doc Section 2 -> file layout and Task 9. Section 3 -> Tasks 4, 5, 6. Section 9 layout and injected clock -> Task 2, asserted in Task 5. Section 12 file modes -> Tasks 4, 10. Section 14.2 store tests -> Tasks 5, 6, 7 (sweep-rule tests wait for the sweeper, Phase 6). Section 14.3 -> Task 9 here; the reason-only mapping test waits for the mapping functions, Phase 2.

**`.ai/tests.md`.** Nineteen test functions, each with a named trace above it. Not tested, on purpose: `Fake` and `Real` (hold and return a value), `MaxOpenConns` (a constant), lexicographic sort (implied by fixed width), FK enforcement alone (proven by the cascade tests), index names (proven by the uniqueness tests), mutex contention (the spec states no behaviour beyond serialisation).

**`.ai/comments.md` and `.ai/code-is-truth.md`.** No code block references a document or section. Remaining comments state a reason (`busy_timeout` ordering, why the file is created before the driver opens it, why closed rows are excluded from the unique index, why `NoRestartIndex` exists, panic re-raise) or are the one-line doc comment on an exported name that needs it.

**`.ai/scope.md`.** `Writer` without `Tx` in Task 4 and `scanPod` alone in Task 8 are named seams. Nothing else is created for a later phase.

**Type consistency.** `Writer.Tx(ctx, func(*sql.Tx) error) error` in Tasks 6, 7, 8, 10. `Store.Migrate(ctx, clock.Clock)` in Tasks 5, 10. `config.Config` fields in Tasks 3, 10. Constants in Task 8 match the CHECK lists in Task 5.
