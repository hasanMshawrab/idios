package api

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/notify"
	"github.com/hasanMshawrab/idios/internal/query/querytest"
)

// listedIncidentByID is listedIncident's counterpart for an incident the
// wire's PodUid filter would not reach: ListIncidentsRequest has no id
// field, so the whole seed is listed at a limit above its size and searched.
func listedIncidentByID(t *testing.T, client idiosv1.IdiosServiceClient, id int64) *idiosv1.IncidentRow {
	t.Helper()
	resp, err := client.ListIncidents(context.Background(), &idiosv1.ListIncidentsRequest{Limit: listLimitAboveSeed})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resp.GetIncidents() {
		if row.GetId() == id {
			return row
		}
	}
	t.Fatalf("incident %d is not in the list", id)
	return nil
}

// incidentActionCase is one row of TestIncidentActionsWriteOnceAndAnswerTheRow:
// an operation against one seeded incident, and how it changes the row the
// list already shows for it.
type incidentActionCase struct {
	name   string
	id     int64
	do     func(idiosv1.IdiosServiceClient, int64) (*idiosv1.IncidentRow, error)
	mutate func(*idiosv1.IncidentRow)
}

// Every human action is idempotent: calling it twice leaves the row where
// the first call put it, and answers the same row the triage list would.
func TestIncidentActionsWriteOnceAndAnswerTheRow(t *testing.T) {
	manual := idiosv1.CloseReason_CLOSE_REASON_MANUAL
	cases := []incidentActionCase{
		{
			name: "acknowledge marks the open incident",
			id:   1,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.AcknowledgeIncident(context.Background(), &idiosv1.AcknowledgeIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.AcknowledgedAt = sp(testNow)
				want.State = idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED
			},
		},
		{
			name: "unacknowledge returns the acknowledged incident to open",
			id:   2,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.UnacknowledgeIncident(context.Background(), &idiosv1.UnacknowledgeIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.AcknowledgedAt = nil
				want.State = idiosv1.IncidentState_INCIDENT_STATE_OPEN
			},
		},
		{
			name: "resolve closes the open incident as manual",
			id:   1,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.ResolveIncident(context.Background(), &idiosv1.ResolveIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.ClosedAt = sp(testNow)
				want.CloseReason = &manual
				want.State = idiosv1.IncidentState_INCIDENT_STATE_MANUAL
			},
		},
		{
			name: "resolve on a recovered incident keeps its close",
			id:   4,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.ResolveIncident(context.Background(), &idiosv1.ResolveIncidentRequest{Id: id})
			},
			mutate: func(*idiosv1.IncidentRow) {},
		},
		{
			name: "unresolve reopens the manually closed incident",
			id:   10,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.UnresolveIncident(context.Background(), &idiosv1.UnresolveIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.ClosedAt = nil
				want.CloseReason = nil
				want.State = idiosv1.IncidentState_INCIDENT_STATE_OPEN
			},
		},
		{
			name: "dismiss marks the manually closed incident",
			id:   10,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.DismissIncident(context.Background(), &idiosv1.DismissIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.DismissedAt = sp(testNow)
				want.State = idiosv1.IncidentState_INCIDENT_STATE_DISMISSED
			},
		},
		{
			name: "undismiss returns the dismissed-while-open incident to open",
			id:   3,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.UndismissIncident(context.Background(), &idiosv1.UndismissIncidentRequest{Id: id})
			},
			mutate: func(want *idiosv1.IncidentRow) {
				want.DismissedAt = nil
				want.State = idiosv1.IncidentState_INCIDENT_STATE_OPEN
			},
		},
		{
			name: "note replaces the incident's note",
			id:   1,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.SetIncidentNote(context.Background(), &idiosv1.SetIncidentNoteRequest{Id: id, Note: "fixed in PR 123"})
			},
			mutate: func(want *idiosv1.IncidentRow) { want.Note = sp("fixed in PR 123") },
		},
		{
			// Depends on the row above having left a note on incident 1.
			name: "an empty note clears it",
			id:   1,
			do: func(c idiosv1.IdiosServiceClient, id int64) (*idiosv1.IncidentRow, error) {
				return c.SetIncidentNote(context.Background(), &idiosv1.SetIncidentNoteRequest{Id: id, Note: ""})
			},
			mutate: func(want *idiosv1.IncidentRow) { want.Note = nil },
		},
	}

	client := newTestServer(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := listedIncidentByID(t, client, c.id)
			want, ok := proto.Clone(before).(*idiosv1.IncidentRow)
			if !ok {
				t.Fatal("proto.Clone did not return an IncidentRow")
			}
			c.mutate(want)
			for i := range 2 {
				got, err := c.do(client, c.id)
				if err != nil {
					t.Fatalf("call %d: %v", i, err)
				}
				if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
					t.Errorf("call %d: -want +got\n%s", i, diff)
				}
			}
			if diff := cmp.Diff(want, listedIncidentByID(t, client, c.id), protocmp.Transform()); diff != "" {
				t.Errorf("listed row: -want +got\n%s", diff)
			}
		})
	}
}

// An unknown id is a 404 for every incident action, the way it is for every
// other incident endpoint that names one.
func TestIncidentActionsSayWhenTheIdIsUnknown(t *testing.T) {
	stack := newTestStack(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/v1/incidents/999/acknowledge"},
		{http.MethodDelete, "/v1/incidents/999/acknowledge"},
		{http.MethodPost, "/v1/incidents/999/resolve"},
		{http.MethodDelete, "/v1/incidents/999/resolve"},
		{http.MethodPost, "/v1/incidents/999/dismiss"},
		{http.MethodDelete, "/v1/incidents/999/dismiss"},
		{http.MethodPut, "/v1/incidents/999/note"},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			code, _, body := call(t, stack.url, c.method, c.path, "{}")
			if code != http.StatusNotFound {
				t.Fatalf("status %d, want %d: %s", code, http.StatusNotFound, body)
			}
			want := `{"message":"incident 999 not found"}`
			if got := string(compact(t, body)); got != want {
				t.Errorf("body %s, want %s", got, want)
			}
		})
	}
}

// A human action is what Section 5 means by "changed it": the stream sends
// the same row the call answered, so a watching client never reloads by hand.
func TestIncidentActionsEmitOnTheStream(t *testing.T) {
	events := notify.New()
	stack := newTestStack(t, withNotifier(events), withThrottle(0))
	client := idiosv1.NewIdiosServiceClient(stack.url)
	rows := incidentStream(t, client, nil)

	resp, err := client.AcknowledgeIncident(context.Background(), &idiosv1.AcknowledgeIncidentRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(resp, recv(t, rows), protocmp.Transform()); diff != "" {
		t.Errorf("-answered +streamed\n%s", diff)
	}

	resp, err = client.UnresolveIncident(context.Background(), &idiosv1.UnresolveIncidentRequest{Id: 10})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(resp, recv(t, rows), protocmp.Transform()); diff != "" {
		t.Errorf("-answered +streamed\n%s", diff)
	}
}

// countRows answers a `SELECT COUNT(*) ...` query, failing the test on error.
func countRows(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// deleteIncidentFacts is what TestDeleteIncidentRemovesFilesAndDetachesHistory
// checks in one comparison: the row, its pod, its files and the rows it
// leaves behind, before and after the delete.
type deleteIncidentFacts struct {
	incidentListed        bool
	podListed             bool
	artifactTotal         int64
	artifactForIncident   int64
	eventTotal            int64
	eventForIncident      int64
	transitionTotal       int64
	transitionForIncident int64
	ownedFilesExist       bool
	unownedFileExists     bool
}

// Deleting an incident removes its files and its own row, but only detaches
// (never deletes) the history and event rows it had claimed, and leaves the
// pod and every other artifact alone.
func TestDeleteIncidentRemovesFilesAndDetachesHistory(t *testing.T) {
	stack := newTestStack(t)
	client := idiosv1.NewIdiosServiceClient(stack.url)
	db := stack.store.Reader.DB()

	writeArtifactFile(t, stack.artifactsRoot, "prod/pod-crash/pod.json", "{}")
	writeArtifactFile(t, stack.artifactsRoot, "prod/pod-crash/api-0.log", "log")
	writeArtifactFile(t, stack.artifactsRoot, "prod/pod-crash/orphan.log", "not owned by any row")

	artifactTotal := countRows(t, db, "SELECT COUNT(*) FROM artifacts")
	eventTotal := countRows(t, db, "SELECT COUNT(*) FROM k8s_events")
	transitionTotal := countRows(t, db, "SELECT COUNT(*) FROM container_state_history")

	for i := range 2 {
		resp, err := client.DeleteIncident(context.Background(), &idiosv1.DeleteIncidentRequest{Id: querytest.CrashIncidentID})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if diff := cmp.Diff(&idiosv1.DeleteIncidentResponse{}, resp, protocmp.Transform()); diff != "" {
			t.Errorf("call %d: -want +got\n%s", i, diff)
		}
	}

	incidentListed := false
	listResp, err := client.ListIncidents(context.Background(), &idiosv1.ListIncidentsRequest{Limit: listLimitAboveSeed})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listResp.GetIncidents() {
		if row.GetId() == querytest.CrashIncidentID {
			incidentListed = true
		}
	}
	_, err = client.GetPod(context.Background(), &idiosv1.GetPodRequest{Uid: querytest.CrashPodUID})
	podListed := err == nil

	_, errOwned1 := os.Stat(filepath.Join(stack.artifactsRoot, "prod/pod-crash/pod.json"))
	_, errOwned2 := os.Stat(filepath.Join(stack.artifactsRoot, "prod/pod-crash/api-0.log"))
	_, errUnowned := os.Stat(filepath.Join(stack.artifactsRoot, "prod/pod-crash/orphan.log"))

	got := deleteIncidentFacts{
		incidentListed:        incidentListed,
		podListed:             podListed,
		artifactTotal:         countRows(t, db, "SELECT COUNT(*) FROM artifacts"),
		artifactForIncident:   countRows(t, db, "SELECT COUNT(*) FROM artifacts WHERE incident_id = ?", querytest.CrashIncidentID),
		eventTotal:            countRows(t, db, "SELECT COUNT(*) FROM k8s_events"),
		eventForIncident:      countRows(t, db, "SELECT COUNT(*) FROM k8s_events WHERE incident_id = ?", querytest.CrashIncidentID),
		transitionTotal:       countRows(t, db, "SELECT COUNT(*) FROM container_state_history"),
		transitionForIncident: countRows(t, db, "SELECT COUNT(*) FROM container_state_history WHERE incident_id = ?", querytest.CrashIncidentID),
		ownedFilesExist:       errOwned1 == nil || errOwned2 == nil,
		unownedFileExists:     errUnowned == nil,
	}
	want := deleteIncidentFacts{
		incidentListed:        false,
		podListed:             true,
		artifactTotal:         artifactTotal - 3,
		artifactForIncident:   0,
		eventTotal:            eventTotal,
		eventForIncident:      0,
		transitionTotal:       transitionTotal,
		transitionForIncident: 0,
		ownedFilesExist:       false,
		unownedFileExists:     true,
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(deleteIncidentFacts{})); diff != "" {
		t.Errorf("-want +got\n%s", diff)
	}
}
