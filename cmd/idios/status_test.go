package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hasanMshawrab/idios/api/testdata"
	"github.com/hasanMshawrab/idios/internal/api/mock"
	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/status"
	"github.com/hasanMshawrab/idios/internal/store"
)

// statusConfig is a data directory nothing has written to yet, on an address
// the test controls.
func statusConfig(t *testing.T, listen string) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	// The default artifacts root is under the real user data directory; an
	// installed daemon writing captures there must not leak into this count.
	cfg.ArtifactsRoot = filepath.Join(cfg.DataDir, "artifacts")
	cfg.APIListen = listen
	return cfg
}

// "The daemon is down, what is in the database" is a question status must
// still answer, so an address nothing listens on falls back to the database
// instead of failing.
func TestStatusFallsBackWhenDaemonUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := statusConfig(t, closed)
	var out bytes.Buffer
	if err := runStatus(context.Background(), cfg, &out); err != nil {
		t.Fatal(err)
	}
	want := "daemon not reachable at " + closed + "; reading the database directly\n" +
		"data_dir   " + cfg.DataDir + `
database   0 B, wal 0 B
artifacts  0 files, 0 B
daemon     not running (no status file)

clusters
  none

incidents
  open    none
  closed  none
rows
  pods 0 (0 live), transitions 0, events 0
  artifacts: none
sweep
  none
`
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

// What idios status prints comes from the daemon when it answers: the status
// message for the process facts and the cluster list for the stored rows the
// table names. An API with no recorder behind it, which is what the mock is,
// says so rather than claiming a process nobody reported.
func TestStatusRendersFromAPI(t *testing.T) {
	mux := http.NewServeMux()
	if err := idiosv1.RegisterIdiosServiceServer(mock.New(), idiosv1.WithMux(mux)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := statusConfig(t, strings.TrimPrefix(srv.URL, "http://"))
	var out bytes.Buffer
	if err := runStatus(context.Background(), cfg, &out); err != nil {
		t.Fatal(err)
	}
	want := "data_dir   " + cfg.DataDir + `
database   1.0 MiB, wal 32.0 KiB
artifacts  12 files, 256.0 KiB
daemon     api at ` + cfg.APIListen + ` answered, recorder not running

clusters
  id  name     context  ready  last_event  skew  last_error
  1   prod     prod     -      -           -     connection refused (2026-08-27T14:38:00.000000Z)
  2   staging  staging  -      -           -     -

incidents
  open    crash 3, oom 1
  closed  pod_deleted 1, recovered 2
rows
  pods 42 (38 live), transitions 310, events 88
  artifacts: file 12, no_output 1
sweep
  table                    ran_at                       rows  files  bytes   error
  container_state_history  2026-08-27T14:00:00.000000Z  3     1      262144  disk full
`
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

// The status message carries the snapshot the daemon assembled while
// answering, so its age is the round trip and the process block is printed
// from the counters the message holds.
func TestReportFromStatusAgesTheRunningSnapshot(t *testing.T) {
	s, err := testdata.Decode("status", &idiosv1.Status{})
	if err != nil {
		t.Fatal(err)
	}
	list, err := testdata.Decode("clusters", &idiosv1.ClustersResponse{})
	if err != nil {
		t.Fatal(err)
	}
	written, err := clock.Parse(s.GetWrittenAt())
	if err != nil {
		t.Fatal(err)
	}
	cfg := statusConfig(t, "127.0.0.1:7770")
	var out bytes.Buffer
	renderReport(&out, reportFromStatus(cfg, s, list.GetClusters(), written.Add(3*time.Second)))
	want := "data_dir   " + cfg.DataDir + `
database   1.0 MiB, wal 32.0 KiB
artifacts  12 files, 256.0 KiB
daemon     running, pid 4242, snapshot 3s old

clusters
  id  name     context  ready  last_event                   skew  last_error
  1   prod     prod     yes    2026-08-27T14:39:02.100000Z  2s    connection refused (2026-08-27T14:38:00.000000Z)
  2   staging  staging  no     -                            0s    -

incidents
  open    crash 3, oom 1
  closed  pod_deleted 1, recovered 2
rows
  pods 42 (38 live), transitions 310, events 88
  artifacts: file 12, no_output 1

writer    tx 1204, errors 2, p99 3.5ms
handlers  errors 1, panics 0
capture   queued 9, completed 7, dropped 1
closer    last tick 2026-08-27T14:38:00.000000Z, opened 1 (9 total), closed 5 (14 total), attached 3 (6 total)
sweep
  table                    ran_at                       rows  files  bytes   error
  container_state_history  2026-08-27T14:00:00.000000Z  3     1      262144  disk full
`
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestRenderReport(t *testing.T) {
	ts := "2026-08-27T12:00:00.000000Z"
	errAt := "2026-08-27T11:59:00.000000Z"
	full := report{
		DataDir: "/data", DBBytes: 12288, WALBytes: 0, ArtifactFiles: 2, ArtifactBytes: 1536,
		Snap: &status.Snapshot{
			PID: 42, Version: "0.0.1-dev",
			Clusters: []status.Cluster{{ID: 1, Ready: true, LastEventAt: ts, SkewSeconds: 0}, {ID: 2}},
			Writer:   status.Writer{Transactions: 1234, Errors: 0, P99Ms: 1.2},
			Handlers: status.Handlers{Errors: 0, Panics: 0},
			Capture:  status.Capture{Queued: 20, Completed: 20, Dropped: 0, Gaps: map[string]uint64{"file": 17, "no_output": 3}},
			Closer: status.Closer{LastTickAt: ts, Closed: 1, Attached: 0, Opened: 2,
				ClosedTotal: 9, AttachedTotal: 4, OpenedTotal: 6},
		},
		SnapAge: 3 * time.Second,
		Clusters: []store.Cluster{
			{ID: 1, Name: "orbstack", ContextName: "orbstack"},
			{ID: 2, Name: "prod", ContextName: "prod-admin", LastError: ptr("connect: dial tcp: connection refused"), LastErrorAt: &errAt},
		},
		OpenByCategory: map[string]int64{"image_pull": 1, "crash": 2},
		ClosedByReason: map[string]int64{"recovered": 3},
		Counts:         store.RowCounts{Pods: 12, LivePods: 10, Transitions: 45, Events: 120},
		Gaps:           map[string]int64{"file": 17, "no_output": 3},
		Sweeps: []store.SweepRun{
			{TableName: "pods", RanAt: ts, RowsRemoved: 2, FilesRemoved: 3, BytesRemoved: 4096},
			{TableName: "wal_checkpoint", RanAt: ts, Error: ptr("wal checkpoint: busy, WAL not truncated")},
		},
	}
	wantFull := `data_dir   /data
database   12.0 KiB, wal 0 B
artifacts  2 files, 1.5 KiB
daemon     running, pid 42, snapshot 3s old

clusters
  id  name      context     ready  last_event                   skew  last_error
  1   orbstack  orbstack    yes    2026-08-27T12:00:00.000000Z  0s    -
  2   prod      prod-admin  no     -                            0s    connect: dial tcp: connection refused (2026-08-27T11:59:00.000000Z)

incidents
  open    crash 2, image_pull 1
  closed  recovered 3
rows
  pods 12 (10 live), transitions 45, events 120
  artifacts: file 17, no_output 3

writer    tx 1234, errors 0, p99 1.2ms
handlers  errors 0, panics 0
capture   queued 20, completed 20, dropped 0
closer    last tick 2026-08-27T12:00:00.000000Z, opened 2 (6 total), closed 1 (9 total), attached 0 (4 total)
sweep
  table           ran_at                       rows  files  bytes  error
  pods            2026-08-27T12:00:00.000000Z  2     3      4096   -
  wal_checkpoint  2026-08-27T12:00:00.000000Z  0     0      0      wal checkpoint: busy, WAL not truncated
`
	empty := report{DataDir: "/data"}
	wantEmpty := `data_dir   /data
database   0 B, wal 0 B
artifacts  0 files, 0 B
daemon     not running (no status file)

clusters
  none

incidents
  open    none
  closed  none
rows
  pods 0 (0 live), transitions 0, events 0
  artifacts: none
sweep
  none
`
	// An API answering with no recorder behind it is not a dead data
	// directory, and the two must not print the same line.
	noRecorder := empty
	noRecorder.APIAddr = "127.0.0.1:7770"
	wantNoRecorder := replaceLine(wantEmpty,
		"daemon     not running (no status file)",
		"daemon     api at 127.0.0.1:7770 answered, recorder not running")
	stale := full
	stale.SnapAge = 5 * time.Minute
	unwatched := full
	unwatchedSnap := *full.Snap
	unwatchedSnap.Clusters = []status.Cluster{full.Snap.Clusters[0]}
	unwatched.Snap = &unwatchedSnap
	fractional := full
	fractional.SnapAge = 2026798 * time.Microsecond
	cases := []struct {
		name string
		in   report
		want string
	}{
		{"full", full, wantFull},
		{"empty data dir", empty, wantEmpty},
		{"api answered without a recorder", noRecorder, wantNoRecorder},
		{"stale snapshot", stale, replaceLine(wantFull, "daemon     running, pid 42, snapshot 3s old", "daemon     stale, pid 42, snapshot 5m0s old")},
		{"cluster row with no watcher", unwatched, replaceLine(wantFull,
			"  2   prod      prod-admin  no     -                            0s    connect:",
			"  2   prod      prod-admin  -      -                            -     connect:")},
		{"fractional snapshot age", fractional,
			replaceLine(wantFull, "snapshot 3s old", "snapshot 2s old")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			renderReport(&out, c.in)
			if out.String() != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", out.String(), c.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func replaceLine(s, old, new string) string {
	return bytes.NewBuffer(bytes.Replace([]byte(s), []byte(old), []byte(new), 1)).String()
}
