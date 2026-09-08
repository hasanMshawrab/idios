package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hasanMshawrab/idios/internal/query/querytest"
	"github.com/hasanMshawrab/idios/internal/store"
)

// promptPath is the prompt of one incident in one mode.
func promptPath(id int64, mode string) string {
	return "/v1/incidents/" + strconv.FormatInt(id, 10) + "/prompt?mode=" + mode
}

// The seeded pod object, as a capture with an inline environment value would
// have left it.
const podJSONWithEnv = `{"kind":"Pod","spec":{"containers":[{"name":"api",` +
	`"env":[{"name":"TOKEN","value":"s3cret"}]}]}}`

// siblingLogPath is a capture that attached to the probe sibling, never to
// the crash incident the snapshot is asked for.
const (
	siblingLogPath = "prod/pod-crash/api-1.log"
	siblingLogBody = "the probe sibling's capture\n"
)

// Mode mcp is the whole short prompt: which incident this is, the uids its
// tools key on, the tool vocabulary, the order to work in, and the answer
// rules. It is asserted whole because a prompt is read as one text.
func TestIncidentPromptMCPIsTheShortPrompt(t *testing.T) {
	stack := newTestStack(t)
	code, header, body := get(t, stack.url, promptPath(1, "mcp"))
	if code != http.StatusOK {
		t.Fatalf("status %d, want %d: %s", code, http.StatusOK, body)
	}
	if got, want := header.Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
		t.Errorf("content type %q, want %q", got, want)
	}
	want := `You are investigating one Kubernetes incident recorded by idios, a
local incident recorder for pods and jobs in a few namespaces. The
idios MCP server is connected and read-only. Pull only what you
need; logs are read with tails, ranges or grep, never whole.

Incident 1: crash - idios-smoke/web-7d9f8c6b5-abcde container api - exit 1 (CrashLoopBackOff) - open.
Cluster prod, pod uid pod-crash, workload Deployment/web.

Tools: list_incidents, get_incident, get_pod, read_log, read_pod_json,
get_workload, get_status. Workload kinds: Deployment, StatefulSet,
DaemonSet, Job, CronJob, ReplicaSet or none, the spellings incident
rows print; a tool argument matches them case-insensitively. An absent
scalar in any response is proto3 dropping a zero value, not missing
data - a container with no resource fields has no requests and no
limits. list_incidents narrows by state, category, cluster, namespace,
workload, pod uid, job uid and node name; the node is how one machine's
storm of failures comes back as a single list.

Suggested order, abandon it when the evidence points elsewhere:
1. get_incident 1 for the detail, the timeline and the incidents
   sharing this pod or job.
2. get_pod for the whole event stream, containers, conditions and the
   pods of the same controller.
3. read_log tails of the subject container around the failure time.

Answer rules: separate what the data proves from what it only
suggests; when the trigger lies outside the recorded data (scaling
decisions, node history), say so rather than guess; finish with
"What happened" as at most five ordered timestamped steps and
"Follow-ups" listing only what a human should change.
`
	if got := string(body); got != want {
		t.Errorf("prompt:\n%s\nwant:\n%s", got, want)
	}
}

// Mode snapshot is the evidence itself: the sections in the order the legend
// names them, each delimited, with the log content whole, the truncation of a
// cut capture marked, the gap of a capture that left no file said in its own
// place, the pod object stripped of its environment values, and the pod's
// other incidents listed. The evidence is the pod's, not the subject row's: a
// capture hangs off whichever of the pod's incidents was open when it ran, so
// the pod.json and a log the probe sibling holds still travel.
func TestIncidentPromptSnapshotCarriesTheEvidence(t *testing.T) {
	stack := newTestStack(t)
	writeArtifactFile(t, stack.artifactsRoot, podJSONPath, podJSONWithEnv)
	writeArtifactFile(t, stack.artifactsRoot, logPath, logBody)
	writeArtifactFile(t, stack.artifactsRoot, siblingLogPath, siblingLogBody)
	sibling := int64(12)
	insertArtifact(t, stack, store.Artifact{
		PodUID: querytest.CrashPodUID, IncidentID: &sibling, Kind: store.ArtifactPodJSON,
		RestartCount: store.NoRestartIndex, FilePath: sp(podJSONPath), SizeBytes: 2048,
		CapturedAt: querytest.ArtifactPodJSONAt,
	})
	insertArtifact(t, stack, store.Artifact{
		PodUID: querytest.CrashPodUID, IncidentID: &sibling, ContainerName: "api",
		Kind: store.ArtifactLogPrevious, RestartCount: 1, FilePath: sp(siblingLogPath),
		SizeBytes: 1024, CapturedAt: "2026-08-27T11:59:20.000000Z",
	})
	code, _, raw := get(t, stack.url, promptPath(1, "snapshot"))
	if code != http.StatusOK {
		t.Fatalf("status %d, want %d: %s", code, http.StatusOK, raw)
	}
	body := string(raw)
	at := -1
	for _, section := range []string{
		"[incident]\ncategory: crash",
		"timeline:\n  2026-08-27T11:55:00.000000Z  lifecycle  lifecycle=opened",
		"[pod]\nname: web-7d9f8c6b5-abcde",
		"containers:\n  api  kind=app",
		"[events]\n  2026-08-27T11:59:00.000000Z  Warning Unhealthy x3 (kubelet) [incident 12]",
		"--- restart_000 container=api captured_at=2026-08-27T11:55:02.000000Z bytes=4096 ---\n" + logBody +
			"--- restart_000 was truncated at capture: the log was longer than the capture limit ---",
		"--- restart_001 container=api captured_at=2026-08-27T11:59:20.000000Z bytes=1024 ---\n" +
			"the probe sibling's capture",
		"--- current.log container=api captured_at=2026-08-27T11:55:03.000000Z bytes=0 ---\n" +
			"no log: no_output: container produced no output",
		"[pod.json]\n{\"kind\":\"Pod\",",
		"\"value\":\"(redacted)\"",
		"[related]\n  12  probe  open  container api",
		"Answer rules: separate what the data proves",
	} {
		i := strings.Index(body, section)
		if i < 0 {
			t.Errorf("prompt has no %q:\n%s", section, body)
			continue
		}
		if i < at {
			t.Errorf("%q comes out of order:\n%s", section, body)
		}
		at = i
	}
	if strings.Contains(body, "s3cret") {
		t.Errorf("prompt carries an environment value:\n%s", body)
	}
}

// The application says how large a prompt is before a person copies or saves
// it, so the size travels in the header and never has to be counted from the
// body.
func TestIncidentPromptCarriesItsSize(t *testing.T) {
	stack := newTestStack(t)
	writeArtifactFile(t, stack.artifactsRoot, podJSONPath, podJSONWithEnv)
	writeArtifactFile(t, stack.artifactsRoot, logPath, logBody)
	for _, mode := range []string{"mcp", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			_, header, body := get(t, stack.url, promptPath(1, mode))
			if got, want := header.Get("Content-Length"), strconv.Itoa(len(body)); got != want {
				t.Errorf("content length %q, want %q", got, want)
			}
		})
	}
}

// A request the daemon cannot render says which part of it was wrong.
func TestIncidentPromptRefusesWhatItCannotRender(t *testing.T) {
	stack := newTestStack(t)
	cases := []struct {
		name string
		path string
		code int
		want string
	}{
		{"an id that is not a number", "/v1/incidents/one/prompt?mode=mcp", http.StatusBadRequest,
			`"description":"incident id must be a number, got one"`},
		{"an unknown incident", promptPath(999, "mcp"), http.StatusNotFound,
			`{"message":"incident 999 not found"}`},
		{"a mode that is neither", promptPath(1, "brief"), http.StatusBadRequest,
			`"description":"mode must be mcp or snapshot, got \"brief\""`},
		{"no mode at all", "/v1/incidents/1/prompt", http.StatusBadRequest,
			`"description":"mode must be mcp or snapshot, got \"\""`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, body := get(t, stack.url, c.path)
			if code != c.code {
				t.Errorf("status %d, want %d: %s", code, c.code, body)
			}
			if !strings.Contains(string(body), c.want) {
				t.Errorf("body %s, want it to carry %s", body, c.want)
			}
		})
	}
}
