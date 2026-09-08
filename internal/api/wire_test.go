package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	sebufhttp "github.com/SebastienMelki/sebuf/http"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/clock"
	"github.com/hasanMshawrab/idios/internal/config"
	"github.com/hasanMshawrab/idios/internal/grafana"
)

var update = flag.Bool("update", false, "rewrite the wire fixtures under api/testdata")

// fixtureServer answers with one fixed message, so the fixtures are the bytes
// the generated marshaling produces and nothing else. Every endpoint it does
// not name is the embedded server's.
type fixtureServer struct {
	*Server
	row       *idiosv1.IncidentRow
	list      *idiosv1.IncidentsResponse
	counts    *idiosv1.IncidentCounts
	detail    *idiosv1.IncidentDetail
	timeline  *idiosv1.TimelineResponse
	pods      *idiosv1.PodsResponse
	podDetail *idiosv1.PodDetail
	events    *idiosv1.EventsResponse
	history   *idiosv1.HistoryResponse
	artifact  *idiosv1.Artifact

	workloads      *idiosv1.WorkloadsResponse
	workloadDetail *idiosv1.WorkloadDetail
	jobs           *idiosv1.JobsResponse
	clusters       *idiosv1.ClustersResponse
	kubeContexts   *idiosv1.KubeContextsResponse
	kubeNamespaces *idiosv1.KubeNamespacesResponse
	status         *idiosv1.Status

	err error
}

// ListWorkloads answers the fixed tree.
func (f *fixtureServer) ListWorkloads(context.Context, *idiosv1.ListWorkloadsRequest) (*idiosv1.WorkloadsResponse, error) {
	return f.workloads, nil
}

// GetWorkload answers the fixed workload page.
func (f *fixtureServer) GetWorkload(context.Context, *idiosv1.GetWorkloadRequest) (*idiosv1.WorkloadDetail, error) {
	return f.workloadDetail, nil
}

// ListJobs answers the fixed job list.
func (f *fixtureServer) ListJobs(context.Context, *idiosv1.ListJobsRequest) (*idiosv1.JobsResponse, error) {
	return f.jobs, nil
}

// ListClusters answers the fixed scope list.
func (f *fixtureServer) ListClusters(context.Context, *idiosv1.ListClustersRequest) (*idiosv1.ClustersResponse, error) {
	return f.clusters, nil
}

// ListKubeContexts answers the fixed contexts.
func (f *fixtureServer) ListKubeContexts(context.Context, *idiosv1.ListKubeContextsRequest) (*idiosv1.KubeContextsResponse, error) {
	return f.kubeContexts, nil
}

// ListKubeNamespaces answers the fixed namespaces.
func (f *fixtureServer) ListKubeNamespaces(context.Context, *idiosv1.ListKubeNamespacesRequest) (*idiosv1.KubeNamespacesResponse, error) {
	return f.kubeNamespaces, nil
}

// GetStatus answers the fixed status.
func (f *fixtureServer) GetStatus(context.Context, *idiosv1.GetStatusRequest) (*idiosv1.Status, error) {
	return f.status, nil
}

// GetIncident answers the fixed detail.
func (f *fixtureServer) GetIncident(context.Context, *idiosv1.GetIncidentRequest) (*idiosv1.IncidentDetail, error) {
	return f.detail, nil
}

// IncidentTimeline answers the fixed entries.
func (f *fixtureServer) IncidentTimeline(context.Context, *idiosv1.IncidentTimelineRequest) (*idiosv1.TimelineResponse, error) {
	return f.timeline, nil
}

// ListPods answers the fixed list.
func (f *fixtureServer) ListPods(context.Context, *idiosv1.ListPodsRequest) (*idiosv1.PodsResponse, error) {
	return f.pods, nil
}

// GetPod answers the fixed pod detail.
func (f *fixtureServer) GetPod(context.Context, *idiosv1.GetPodRequest) (*idiosv1.PodDetail, error) {
	return f.podDetail, nil
}

// PodEvents answers the fixed events.
func (f *fixtureServer) PodEvents(context.Context, *idiosv1.PodEventsRequest) (*idiosv1.EventsResponse, error) {
	return f.events, nil
}

// PodHistory answers the fixed history.
func (f *fixtureServer) PodHistory(context.Context, *idiosv1.PodHistoryRequest) (*idiosv1.HistoryResponse, error) {
	return f.history, nil
}

// GetArtifact answers the fixed artifact row.
func (f *fixtureServer) GetArtifact(context.Context, *idiosv1.GetArtifactRequest) (*idiosv1.Artifact, error) {
	return f.artifact, nil
}

// StreamIncidents sends the fixed row. It is the one operation whose message
// is an IncidentRow on its own rather than inside a list.
func (f *fixtureServer) StreamIncidents(_ context.Context, _ *idiosv1.StreamIncidentsRequest, sender idiosv1.SSESender) error {
	return sender.Send(f.row)
}

// GetIncidentCounts answers the fixed counts.
func (f *fixtureServer) GetIncidentCounts(context.Context, *idiosv1.GetIncidentCountsRequest) (*idiosv1.IncidentCounts, error) {
	return f.counts, nil
}

// ListIncidents answers the fixed list, or fails with the fixed error.
func (f *fixtureServer) ListIncidents(context.Context, *idiosv1.ListIncidentsRequest) (*idiosv1.IncidentsResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

func serveFixture(t *testing.T, f *fixtureServer, path string) (int, []byte) {
	t.Helper()
	code, _, body := serveFixtureWith(t, f, path, nil)
	return code, body
}

// serveFixtureWith serves f over loopback and returns the status, the content
// type and the body of a GET of path carrying headers.
func serveFixtureWith(t *testing.T, f *fixtureServer, path string, headers map[string]string) (int, string, []byte) {
	t.Helper()
	f.Server = New(nil, config.Default(), clock.Real{}, slog.New(slog.DiscardHandler))
	mux := http.NewServeMux()
	if err := idiosv1.RegisterIdiosServiceServer(f, idiosv1.WithMux(mux), idiosv1.WithErrorHandler(f.writeError)); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

// sseData is the message of the single event in an SSE body.
func sseData(t *testing.T, body []byte) []byte {
	t.Helper()
	line, ok := bytes.CutPrefix(bytes.TrimRight(body, "\n"), []byte("data: "))
	if !ok {
		t.Fatalf("no SSE data in %q", body)
	}
	return line
}

// compact removes the whitespace protojson sprinkles into a body to keep
// callers from depending on its exact spacing. It varies from build to build,
// so a fixture that kept it would break on an unrelated toolchain change;
// everything else about the bytes is compared as sent.
func compact(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := json.Compact(&out, body); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out.Bytes()
}

// fixtureIncidentRow is the triage row with every optional field set.
func fixtureIncidentRow() *idiosv1.IncidentRow {
	return &idiosv1.IncidentRow{
		Id: 412, ClusterId: 1, Namespace: "idios-smoke",
		SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp("pod-crash"), JobUid: sp("job-report-1"),
		ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "checkout-api",
		Category: idiosv1.Category_CATEGORY_CRASH, FirstReason: "CrashLoopBackOff", LastReason: "Error",
		LastMessage: sp("back-off 5m0s restarting failed container"),
		Image:       sp(web), ImageTag: sp(webTag), ImageId: sp(webID),
		Occurrences: 7, OpenedAt: "2026-08-27T14:03:11.482913Z", LastSeenAt: "2026-08-27T14:39:02.100000Z",
		ClosedAt:       sp("2026-08-27T14:40:00.000000Z"),
		CloseReason:    closeReason(idiosv1.CloseReason_CLOSE_REASON_POD_DELETED),
		AcknowledgedAt: sp("2026-08-27T14:20:00.000000Z"), DismissedAt: sp("2026-08-27T14:41:00.000000Z"),
		Note:    sp("known bad deploy"),
		State:   idiosv1.IncidentState_INCIDENT_STATE_DISMISSED,
		PodName: sp("checkout-api-7d9f8b6c4-x2kqp"), PodDeletedAt: sp("2026-08-27T14:39:30.000000Z"),
		PodDeletionReason: deletionReason(idiosv1.DeletionReason_DELETION_REASON_ROLLOUT),
		ContainerCount:    3, ExitCode: i32(137), Signal: i32(9),
		NodeName: sp("node-a"),
	}
}

// fixtureRelatedIncidentRow is another incident of the same pod, as the detail
// lists it: the application decodes the related rows with the same reader as
// the triage list, so one minimal row is enough to pin the shape.
func fixtureRelatedIncidentRow() *idiosv1.IncidentRow {
	return &idiosv1.IncidentRow{
		Id: 415, ClusterId: 1, Namespace: "idios-smoke",
		SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_POD, PodUid: sp("pod-crash"),
		ContainerName: "api", WorkloadKind: "Deployment", WorkloadName: "checkout-api",
		Category: idiosv1.Category_CATEGORY_PROBE, FirstReason: "Unhealthy", LastReason: "Unhealthy",
		Occurrences: 2, OpenedAt: "2026-08-27T14:30:00.000000Z", LastSeenAt: "2026-08-27T14:38:00.000000Z",
		State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, PodName: sp("checkout-api-7d9f8b6c4-x2kqp"),
		ContainerCount: 3, NodeName: sp("node-a"),
	}
}

// The bytes the daemon sends are the fixtures the application decodes, so
// they are compared exactly: int64 as a string, enum as the stored string,
// unset optional absent.
func TestWireContractIncidents(t *testing.T) {
	full := fixtureIncidentRow()
	minimal := &idiosv1.IncidentRow{
		Id: 413, ClusterId: 2, Namespace: "idios-smoke",
		SubjectKind:   idiosv1.SubjectKind_SUBJECT_KIND_JOB,
		ContainerName: "", WorkloadKind: "CronJob", WorkloadName: "report",
		Category: idiosv1.Category_CATEGORY_JOB_FAILED, FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded",
		Occurrences: 1, OpenedAt: "2026-08-27T14:03:11.482913Z", LastSeenAt: "2026-08-27T14:03:11.482913Z",
		State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, ContainerCount: 0,
	}
	counts := &idiosv1.IncidentCounts{
		ByState: []*idiosv1.StateCount{
			{State: idiosv1.IncidentState_INCIDENT_STATE_OPEN, Count: 4},
			{State: idiosv1.IncidentState_INCIDENT_STATE_ACKNOWLEDGED, Count: 1},
			{State: idiosv1.IncidentState_INCIDENT_STATE_POD_DELETED, Count: 2},
			{State: idiosv1.IncidentState_INCIDENT_STATE_ATTENTION, Count: 5},
		},
		ByCategory: []*idiosv1.CategoryCount{
			{Category: idiosv1.Category_CATEGORY_CRASH, Count: 3},
			{Category: idiosv1.Category_CATEGORY_OOM, Count: 2},
			{Category: idiosv1.Category_CATEGORY_UNCLEAN_EXIT, Count: 1},
		},
	}
	cases := []wireCase{
		{"incident_row", &fixtureServer{row: full}, "/v1/incidents/stream", http.StatusOK, true},
		{"incident_row_minimal", &fixtureServer{row: minimal}, "/v1/incidents/stream", http.StatusOK, true},
		{"incident_counts", &fixtureServer{counts: counts}, "/v1/incidents/counts", http.StatusOK, false},
		{
			"incidents",
			&fixtureServer{list: &idiosv1.IncidentsResponse{
				Incidents: []*idiosv1.IncidentRow{full, minimal}, Truncated: true,
			}},
			"/v1/incidents", http.StatusOK, false,
		},
		{
			"error",
			&fixtureServer{err: &notFoundError{what: "incident", id: "999999"}},
			"/v1/incidents", http.StatusNotFound, false,
		},
		{
			"validation_error",
			&fixtureServer{err: &sebufhttp.ValidationError{Violations: []*sebufhttp.FieldViolation{
				{Field: "state", Description: "unknown incident state bogus"},
				{Field: "limit", Description: "limit must not be negative"},
			}}},
			"/v1/incidents", http.StatusBadRequest, false,
		},
	}
	runWireCases(t, cases)
}

// wireCase is one message family served through the generated server and
// compared with the fixture of the same name.
type wireCase struct {
	name       string
	server     *fixtureServer
	path       string
	wantStatus int
	sse        bool
}

func runWireCases(t *testing.T, cases []wireCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := serveFixture(t, c.server, c.path)
			if code != c.wantStatus {
				t.Fatalf("status %d, want %d: %s", code, c.wantStatus, body)
			}
			if c.sse {
				body = sseData(t, body)
			}
			body = compact(t, body)
			path := filepath.Join("..", "..", "api", "testdata", c.name+".json")
			if *update {
				if err := os.WriteFile(path, body, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v; run go test ./internal/api -update to write it", err)
			}
			if !bytes.Equal(want, body) {
				t.Errorf("wire bytes changed\nwant %s\ngot  %s", want, body)
			}
		})
	}
}

// fixturePod is the pod snapshot with every optional field set.
func fixturePod() *idiosv1.Pod {
	return &idiosv1.Pod{
		Uid: "pod-crash", ClusterId: 1, Namespace: "idios-smoke", Name: "checkout-api-7d9f8b6c4-x2kqp",
		NodeName: sp("node-a"), Phase: "Running", StatusReason: sp("Evicted"),
		StatusMessage:       sp("The node was low on resource: memory."),
		DeletionRequestedAt: sp("2026-08-27T14:39:20.000000Z"), QosClass: sp("Burstable"),
		ControllerKind: "ReplicaSet", ControllerName: "checkout-api-7d9f8b6c4", ControllerUid: "rs-checkout-1",
		WorkloadKind: "Deployment", WorkloadName: "checkout-api",
		CreatedAt: "2026-08-27T13:00:00.000000Z", StartedAt: sp("2026-08-27T13:00:05.000000Z"),
		FirstSeenAt: "2026-08-27T13:00:10.000000Z", LastSeenAt: "2026-08-27T14:39:02.100000Z",
		DeletedAt:      sp("2026-08-27T14:39:30.000000Z"),
		DeletionSource: ep(idiosv1.DeletionSource_DELETION_SOURCE_WATCH),
		DeletionReason: ep(idiosv1.DeletionReason_DELETION_REASON_ROLLOUT),
	}
}

// fixtureContainer is the container snapshot with every optional field set.
func fixtureContainer() *idiosv1.Container {
	return &idiosv1.Container{
		Id: 51, PodUid: "pod-crash", Name: "api", Kind: idiosv1.ContainerKind_CONTAINER_KIND_APP,
		Image: web, ImageTag: sp(webTag), ImageId: sp(webID), ContainerId: sp("containerd://aaa"),
		CpuRequest: sp("250m"), CpuLimit: sp("1"), MemRequest: sp("256Mi"), MemLimit: sp("512Mi"),
		CpuRequestMillis: ip(250), CpuLimitMillis: ip(1000), MemRequestBytes: ip(268435456), MemLimitBytes: ip(536870912),
		State: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED, Reason: sp("Error"),
		ExitCode: i32(137), Signal: i32(9), Ready: false, RestartCount: 7,
		RunningSince: sp("2026-08-27T14:30:00.000000Z"), LastTerminatedReason: sp("OOMKilled"),
		LastTerminatedExitCode: i32(137), LastTerminatedSignal: i32(9),
		LastTerminatedAt: sp("2026-08-27T14:38:00.000000Z"), UpdatedAt: "2026-08-27T14:39:02.100000Z",
	}
}

// fixtureGrafanaConfig is the configuration the "clusters" fixture's first
// row carries, and what the incident and pod detail links are built from.
func fixtureGrafanaConfig() grafana.Config {
	return grafana.Config{BaseURL: "https://logs.example.grafana.net", DatasourceUID: "grafanacloud-logs", Selector: grafana.DefaultSelector}
}

// fixtureIncidentGrafanaURL is the Explore link fixtureIncidentRow's own
// span builds, scoped to fixturePod and its subject container.
func fixtureIncidentGrafanaURL(t *testing.T) string {
	t.Helper()
	opened, err := clock.Parse(fixtureIncidentRow().GetOpenedAt())
	if err != nil {
		t.Fatal(err)
	}
	closed, err := clock.Parse(fixtureIncidentRow().GetClosedAt())
	if err != nil {
		t.Fatal(err)
	}
	v := grafana.Values{
		Namespace: fixturePod().GetNamespace(), Pod: fixturePod().GetName(), Container: fixtureIncidentRow().GetContainerName(),
		Workload: fixtureIncidentRow().GetWorkloadName(), Node: fixturePod().GetNodeName(), Cluster: "prod",
	}
	return grafana.ExploreURL(fixtureGrafanaConfig(), v, grafana.Window{From: opened.Add(-grafana.Pad), To: closed.Add(grafana.Pad)})
}

// fixtureContainerGrafanaURL is the Explore link fixtureContainer's own run
// builds, scoped to fixturePod for the pod detail page.
func fixtureContainerGrafanaURL(t *testing.T) string {
	t.Helper()
	c := fixtureContainer()
	since, err := clock.Parse(c.GetRunningSince())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := clock.Parse(c.GetUpdatedAt())
	if err != nil {
		t.Fatal(err)
	}
	v := grafana.Values{
		Namespace: fixturePod().GetNamespace(), Pod: fixturePod().GetName(), Container: c.GetName(),
		Workload: fixturePod().GetWorkloadName(), Node: fixturePod().GetNodeName(), Cluster: "prod",
	}
	return grafana.ExploreURL(fixtureGrafanaConfig(), v, grafana.Window{From: since.Add(-grafana.Pad), To: updated.Add(grafana.Pad)})
}

// fixtureCondition is the pod condition with every optional field set.
func fixtureCondition() *idiosv1.PodCondition {
	return &idiosv1.PodCondition{
		Id: 61, PodUid: "pod-crash", Type: "Ready", Status: "False", Reason: "ContainersNotReady",
		Message:         sp("containers with unready status: [api]"),
		K8STransitionAt: sp("2026-08-27T14:38:01.000000Z"), ObservedAt: "2026-08-27T14:39:02.100000Z",
	}
}

// fixtureEvent is the Kubernetes event with every optional field set.
func fixtureEvent() *idiosv1.K8SEvent {
	return &idiosv1.K8SEvent{
		Id: 71, ClusterId: 1, EventUid: "ev-unhealthy-1", Namespace: "idios-smoke", Type: "Warning",
		InvolvedKind: "Pod", InvolvedName: "checkout-api-7d9f8b6c4-x2kqp", InvolvedUid: "pod-crash",
		FieldPath: "spec.containers{api}", Reason: "Unhealthy",
		Message:         "Readiness probe failed: HTTP probe failed with statuscode: 503",
		SourceComponent: "kubelet", Count: 3,
		FirstTs: "2026-08-27T14:35:00.000000Z", LastTs: "2026-08-27T14:39:00.000000Z",
		Category: ep(idiosv1.Category_CATEGORY_PROBE), IncidentId: ip(412),
	}
}

// fixtureUnattachedEvent is an event of the same pod that attached to no
// incident: an incident detail carries its pod's stream whole, so the
// application must decode a row with no incident id beside one that has it.
func fixtureUnattachedEvent() *idiosv1.K8SEvent {
	return &idiosv1.K8SEvent{
		Id: 72, ClusterId: 1, EventUid: "ev-killing-1", Namespace: "idios-smoke", Type: "Normal",
		InvolvedKind: "Pod", InvolvedName: "checkout-api-7d9f8b6c4-x2kqp", InvolvedUid: "pod-crash",
		FieldPath: "spec.containers{api}", Reason: "Killing", Message: "Stopping container api",
		SourceComponent: "kubelet", Count: 1,
		FirstTs: "2026-08-27T14:39:01.000000Z", LastTs: "2026-08-27T14:39:01.000000Z",
	}
}

// fixtureArtifact is the artifact row with every optional field set.
func fixtureArtifact() *idiosv1.Artifact {
	return &idiosv1.Artifact{
		Id: 81, PodUid: "pod-crash", IncidentId: ip(412), ContainerName: "api",
		Kind: idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS, RestartCount: 6,
		FilePath: sp("prod/pod-crash/api-6.log"), SizeBytes: 262144, Truncated: true, CapturedEarly: true,
		CaptureGap:  ep(idiosv1.CaptureGap_CAPTURE_GAP_KUBELET_ERROR),
		CaptureNote: sp("the container is not available"), CapturedAt: "2026-08-27T14:38:05.000000Z",
	}
}

// The detail and timeline bytes are the fixtures the application decodes for
// the incident page.
func TestWireContractIncidentDetail(t *testing.T) {
	incidentGrafanaURL := fixtureIncidentGrafanaURL(t)
	detail := &idiosv1.IncidentDetail{
		Incident:   fixtureIncidentRow(),
		Pod:        fixturePod(),
		Containers: []*idiosv1.Container{fixtureContainer()},
		Artifacts:  []*idiosv1.Artifact{fixtureArtifact()},
		Events:     []*idiosv1.K8SEvent{fixtureEvent(), fixtureUnattachedEvent()},
		Conditions: []*idiosv1.PodCondition{fixtureCondition()},
		GrafanaUrl: &incidentGrafanaURL,
		// The failing pod is a Job's, so the pod incident carries the run whose
		// condition says whether the retry finally succeeded, and the other
		// incidents of that pod and that Job.
		Job:              fixtureJobRow(),
		RelatedIncidents: []*idiosv1.IncidentRow{fixtureRelatedIncidentRow()},
	}
	timeline := &idiosv1.TimelineResponse{Entries: []*idiosv1.TimelineEntry{
		{
			Kind:  idiosv1.TimelineKind_TIMELINE_KIND_CONTAINER_TRANSITION,
			K8SAt: sp("2026-08-27T14:38:00.000000Z"), ObservedAt: "2026-08-27T14:38:02.000000Z",
			ContainerName: sp("api"), State: ep(idiosv1.ContainerState_CONTAINER_STATE_TERMINATED),
			Reason: sp("Error"), ExitCode: i32(137), Signal: i32(9), RestartCount: i32(7),
			GapReconstructed: bp(true), ConditionType: sp("Ready"), ConditionStatus: sp("False"),
			Message: sp("back-off 5m0s restarting failed container"), EventType: sp("Warning"),
			EventReason: sp("Unhealthy"), Count: i32(3), ArtifactId: ip(81),
			ArtifactKind:   ep(idiosv1.ArtifactKind_ARTIFACT_KIND_LOG_PREVIOUS),
			CaptureGap:     ep(idiosv1.CaptureGap_CAPTURE_GAP_KUBELET_ERROR),
			ReplicasetName: sp("checkout-api-7d9f8b6c4"), ImageTag: sp(webTag), Revision: ip(8),
			Lifecycle:   ep(idiosv1.LifecycleStep_LIFECYCLE_STEP_CLOSED),
			CloseReason: ep(idiosv1.CloseReason_CLOSE_REASON_POD_DELETED),
		},
		{
			Kind: idiosv1.TimelineKind_TIMELINE_KIND_LIFECYCLE, ObservedAt: "2026-08-27T14:03:11.482913Z",
			Lifecycle: ep(idiosv1.LifecycleStep_LIFECYCLE_STEP_OPENED),
		},
	}}
	// A Job is the subject of its own incident: no pod is the subject, so the
	// kubelet rows are empty and the Job row carries the failure instead.
	jobDetail := &idiosv1.IncidentDetail{
		Incident: &idiosv1.IncidentRow{
			Id: 414, ClusterId: 1, Namespace: "idios-smoke",
			SubjectKind: idiosv1.SubjectKind_SUBJECT_KIND_JOB, JobUid: sp("job-report-1"),
			WorkloadKind: "CronJob", WorkloadName: "report",
			Category:    idiosv1.Category_CATEGORY_JOB_FAILED,
			FirstReason: "BackoffLimitExceeded", LastReason: "BackoffLimitExceeded",
			LastMessage: sp("Job has reached the specified backoff limit"),
			Occurrences: 1, OpenedAt: "2026-08-27T14:00:22.000000Z",
			LastSeenAt: "2026-08-27T14:00:22.000000Z",
			State:      idiosv1.IncidentState_INCIDENT_STATE_OPEN,
		},
		Job:         fixtureJobRow(),
		LastPodName: sp("report-28812345-b7t2m"),
	}
	runWireCases(t, []wireCase{
		{"incident_detail", &fixtureServer{detail: detail}, "/v1/incidents/412", http.StatusOK, false},
		{"incident_detail_job", &fixtureServer{detail: jobDetail}, "/v1/incidents/414", http.StatusOK, false},
		{"timeline", &fixtureServer{timeline: timeline}, "/v1/incidents/412/timeline", http.StatusOK, false},
	})
}

// fixturePodRow is the pod list line with every optional field set.
func fixturePodRow() *idiosv1.PodRow {
	p := fixturePod()
	return &idiosv1.PodRow{
		Uid: p.GetUid(), ClusterId: p.GetClusterId(), Namespace: p.GetNamespace(), Name: p.GetName(),
		NodeName: p.NodeName, Phase: p.GetPhase(), StatusReason: p.StatusReason, StatusMessage: p.StatusMessage,
		DeletionRequestedAt: p.DeletionRequestedAt, QosClass: p.QosClass,
		ControllerKind: p.GetControllerKind(), ControllerName: p.GetControllerName(), ControllerUid: p.GetControllerUid(),
		WorkloadKind: p.GetWorkloadKind(), WorkloadName: p.GetWorkloadName(),
		CreatedAt: p.GetCreatedAt(), StartedAt: p.StartedAt,
		FirstSeenAt: p.GetFirstSeenAt(), LastSeenAt: p.GetLastSeenAt(), DeletedAt: p.DeletedAt,
		DeletionSource: p.DeletionSource, DeletionReason: p.DeletionReason,
		OpenIncidents: 2, ContainerCount: 3,
		WorstState: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED,
	}
}

// The pod bytes are the fixtures the application decodes for the pod screens.
func TestWireContractPods(t *testing.T) {
	row := fixturePodRow()
	minimal := &idiosv1.PodRow{
		Uid: "pod-side", ClusterId: 2, Namespace: "idios-smoke", Name: "checkout-worker-6b8d9c5f7-q4nlz",
		Phase: "Pending", ControllerKind: "ReplicaSet", ControllerName: "checkout-worker-6b8d9c5f7",
		ControllerUid: "rs-worker-1", WorkloadKind: "Deployment", WorkloadName: "checkout-worker",
		CreatedAt: "2026-08-27T14:00:00.000000Z", FirstSeenAt: "2026-08-27T14:00:01.000000Z",
		LastSeenAt: "2026-08-27T14:00:01.000000Z",
	}
	evicted := &idiosv1.PodRow{
		Uid: "pod-evicted", ClusterId: 1, Namespace: "idios-smoke", Name: "checkout-api-7d9f8b6c4-m8vqr",
		NodeName: sp("node-b"), Phase: "Failed", StatusReason: sp("Evicted"),
		StatusMessage:  sp("The node was low on resource: ephemeral-storage."),
		QosClass:       sp("BestEffort"),
		ControllerKind: "ReplicaSet", ControllerName: "checkout-api-7d9f8b6c4", ControllerUid: "rs-checkout-1",
		WorkloadKind: "Deployment", WorkloadName: "checkout-api",
		CreatedAt: "2026-08-27T12:00:00.000000Z", StartedAt: sp("2026-08-27T12:00:04.000000Z"),
		FirstSeenAt: "2026-08-27T12:00:06.000000Z", LastSeenAt: "2026-08-27T12:58:00.000000Z",
		DeletedAt:      sp("2026-08-27T12:58:10.000000Z"),
		DeletionSource: ep(idiosv1.DeletionSource_DELETION_SOURCE_RECONCILE),
		DeletionReason: ep(idiosv1.DeletionReason_DELETION_REASON_EVICTED),
		OpenIncidents:  1, ContainerCount: 1,
		WorstState: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED,
	}
	containerGrafanaURL := fixtureContainerGrafanaURL(t)
	podContainer := fixtureContainer()
	podContainer.GrafanaUrl = &containerGrafanaURL
	detail := &idiosv1.PodDetail{
		Pod:        row,
		Containers: []*idiosv1.Container{podContainer},
		Conditions: []*idiosv1.PodCondition{fixtureCondition()},
		Incidents:  []*idiosv1.IncidentRow{fixtureIncidentRow()},
		Artifacts:  []*idiosv1.Artifact{fixtureArtifact()},
		Siblings: []*idiosv1.SiblingPod{
			{Uid: "pod-sib-1", Name: "checkout-api-7d9f8b6c4-k9tld", Phase: "Running", RestartCount: 3, Ready: false,
				WorstOpenCategory: ep(idiosv1.Category_CATEGORY_IMAGE_PULL)},
			{Uid: "pod-sib-2", Name: "checkout-api-7d9f8b6c4-p8cg4", Phase: "Running", Ready: true},
			{Uid: "pod-sib-3", Name: "checkout-api-7d9f8b6c4-ngq6w", Phase: "Running", Ready: true},
			{Uid: "pod-sib-4", Name: "checkout-api-7d9f8b6c4-tmf48", Phase: "Running", Ready: true},
			{Uid: "pod-sib-5", Name: "checkout-api-7d9f8b6c4-b7t2m", Phase: "Failed",
				DeletedAt: sp("2026-08-27T14:39:30.000000Z"), RestartCount: 2},
		},
		SiblingTotal: 8,
	}
	history := &idiosv1.HistoryResponse{
		Transitions: []*idiosv1.ContainerStateHistory{{
			Id: 91, PodUid: "pod-crash", ContainerName: "api", IncidentId: ip(412), Image: web,
			ImageId: sp(webID), ContainerId: sp("containerd://aaa"),
			State: idiosv1.ContainerState_CONTAINER_STATE_TERMINATED, Reason: sp("Error"),
			ExitCode: i32(137), Signal: i32(9), RestartCount: 7,
			Category:     ep(idiosv1.Category_CATEGORY_OOM),
			K8SStartedAt: sp("2026-08-27T14:30:00.000000Z"), K8SFinishedAt: sp("2026-08-27T14:38:00.000000Z"),
			ObservedAt: "2026-08-27T14:38:02.000000Z", GapReconstructed: true,
		}},
		Conditions: []*idiosv1.PodCondition{fixtureCondition()},
	}
	runWireCases(t, []wireCase{
		{
			"pods", &fixtureServer{pods: &idiosv1.PodsResponse{
				Pods: []*idiosv1.PodRow{row, minimal, evicted}, Truncated: true,
			}}, "/v1/pods", http.StatusOK, false,
		},
		{"pod_detail", &fixtureServer{podDetail: detail}, "/v1/pods/pod-crash", http.StatusOK, false},
		{
			"events", &fixtureServer{events: &idiosv1.EventsResponse{
				Events: []*idiosv1.K8SEvent{fixtureEvent()}, Truncated: true,
			}}, "/v1/pods/pod-crash/events", http.StatusOK, false,
		},
		{"history", &fixtureServer{history: history}, "/v1/pods/pod-crash/history", http.StatusOK, false},
	})
}

// The artifact bytes are the fixture the application decodes beside the file
// it fetches from the content endpoint.
func TestWireContractArtifact(t *testing.T) {
	runWireCases(t, []wireCase{
		{"artifact", &fixtureServer{artifact: fixtureArtifact()}, "/v1/artifacts/81", http.StatusOK, false},
	})
}

// fixtureJobRow is the failed Job with every optional field set.
func fixtureJobRow() *idiosv1.JobRow {
	return &idiosv1.JobRow{
		Uid: "job-report-1", ClusterId: 1, Namespace: "idios-smoke", Name: "report-28812345",
		CronjobUid: sp("cj-report"), CronjobName: sp("report"), Active: 0, Succeeded: 0, Failed: 3,
		BackoffLimit: i32(2), Completions: i32(1), Parallelism: i32(1),
		ActiveDeadlineSeconds: i64(900), RestartPolicy: "Never",
		ConditionType: sp("Failed"), ConditionReason: sp("BackoffLimitExceeded"),
		ConditionMessage: sp("Job has reached the specified backoff limit"),
		CreatedAt:        "2026-08-27T13:45:00.000000Z", StartedAt: sp("2026-08-27T13:45:02.000000Z"),
		FinishedAt:  sp("2026-08-27T13:57:00.000000Z"),
		FirstSeenAt: "2026-08-27T14:00:21.000000Z", LastSeenAt: "2026-08-27T14:00:22.000000Z",
		DeletedAt: sp("2026-08-27T14:41:00.000000Z"),
	}
}

// fixtureCompleteJobRow is the run that finished its work, which only the
// condition reports.
func fixtureCompleteJobRow() *idiosv1.JobRow {
	return &idiosv1.JobRow{
		Uid: "job-report-2", ClusterId: 1, Namespace: "idios-smoke", Name: "report-28812350",
		Succeeded: 1, RestartPolicy: "Never", ConditionType: sp("Complete"),
		CreatedAt:   "2026-08-27T13:45:00.000000Z",
		FirstSeenAt: "2026-08-27T14:00:23.000000Z", LastSeenAt: "2026-08-27T14:00:24.000000Z",
		Complete: true,
	}
}

// The workload and job bytes are the fixtures the application decodes for
// the tree and the job list.
func TestWireContractWorkloads(t *testing.T) {
	row := &idiosv1.WorkloadRow{
		ClusterId: 1, Namespace: "idios-smoke", WorkloadKind: "Deployment", WorkloadName: "checkout-api",
		IncidentsByCategory: []*idiosv1.CategoryCount{
			{Category: idiosv1.Category_CATEGORY_CRASH, Count: 3},
			{Category: idiosv1.Category_CATEGORY_OOM, Count: 1},
		},
		OpenIncidents: 2, Occurrences: 7,
		ImageTags: []*idiosv1.TagCount{{Tag: webTag, Count: 3}, {Count: 1}},
		LivePods:  4, DeletedPods: 2,
	}
	minimal := &idiosv1.WorkloadRow{
		ClusterId: 2, Namespace: "idios-smoke", WorkloadKind: "ReplicaSet", WorkloadName: "checkout-worker-6b8d9c5f7",
		LivePods: 1,
	}
	// A pod no controller owns is its own row: kind "none", no name, and the
	// pod it stands for named instead.
	lone := &idiosv1.WorkloadRow{
		ClusterId: 1, Namespace: "idios-smoke", WorkloadKind: "none",
		OpenIncidents: 1, Occurrences: 4, LivePods: 1,
		PodUid: "pod-debug", PodName: "debug-shell",
	}
	detail := &idiosv1.WorkloadDetail{
		Workload: row,
		Rollouts: []*idiosv1.Rollout{{
			ReplicasetUid: "rs-checkout-1", ReplicasetName: "checkout-api-7d9f8b6c4", Revision: ip(8),
			Images:      []string{web},
			FirstSeenAt: "2026-08-27T13:00:00.000000Z", LastSeenAt: "2026-08-27T14:39:02.100000Z",
			DeletedAt: sp("2026-08-27T14:40:00.000000Z"), Incidents: 3,
			CreatedAt: "2026-08-27T12:59:50.000000Z",
			Replicas:  i32(5), ReadyReplicas: i32(4), AvailableReplicas: i32(2),
		}, {
			ReplicasetUid: "rs-checkout-0", ReplicasetName: "checkout-api-6c8e7a5b3", Revision: ip(7),
			Images:      []string{web},
			FirstSeenAt: "2026-08-27T11:00:00.000000Z", LastSeenAt: "2026-08-27T12:59:49.000000Z",
			DeletedAt: sp("2026-08-27T13:05:00.000000Z"), Incidents: 1,
			CreatedAt: "2026-08-27T10:59:40.000000Z",
		}},
		RestartsByHour: []*idiosv1.HourBucket{
			{Hour: "2026-08-27T14:00:00.000000Z", Restarts: 7, Reconstructed: true},
		},
		Pods: []*idiosv1.PodRow{fixturePodRow()}, PodsTruncated: true,
		Runs: []*idiosv1.JobRow{fixtureJobRow(), fixtureCompleteJobRow()},
	}
	job := fixtureJobRow()
	complete := fixtureCompleteJobRow()
	runWireCases(t, []wireCase{
		{
			"workloads", &fixtureServer{workloads: &idiosv1.WorkloadsResponse{
				Workloads: []*idiosv1.WorkloadRow{row, minimal, lone}, Truncated: true,
			}}, "/v1/workloads", http.StatusOK, false,
		},
		{
			"workload_detail", &fixtureServer{workloadDetail: detail},
			"/v1/workloads/1/idios-smoke/Deployment/checkout-api", http.StatusOK, false,
		},
		{
			"jobs", &fixtureServer{jobs: &idiosv1.JobsResponse{
				Jobs: []*idiosv1.JobRow{job, complete}, Truncated: true, Total: 22, FailedTotal: 4,
			}}, "/v1/jobs", http.StatusOK, false,
		},
	})
}

// The scope and status bytes are the fixtures the application decodes for
// the cluster picker, the add-cluster flow and the status window.
func TestWireContractClustersAndStatus(t *testing.T) {
	clusters := &idiosv1.ClustersResponse{Clusters: []*idiosv1.Cluster{
		{
			Id: 1, Identity: sp("id-prod"), Name: "prod", ContextName: "prod",
			ApiServerUrl: "https://127.0.0.1:26443", FirstSeenAt: "2026-08-27T13:00:00.000000Z",
			LastConnectedAt: sp("2026-08-27T14:39:02.100000Z"),
			LastError:       sp("connection refused"), LastErrorAt: sp("2026-08-27T14:38:00.000000Z"),
			Namespaces: []string{"default", "idios-smoke"}, Ready: true,
			LastEventAt: sp("2026-08-27T14:39:02.100000Z"), SkewSeconds: 1.5,
			GrafanaUrl: "https://logs.example.grafana.net", LokiDatasourceUid: "grafanacloud-logs",
			LogSelector: grafana.DefaultSelector,
		},
		{
			Id: 2, Name: "staging", ContextName: "staging", ApiServerUrl: "https://127.0.0.1:26444",
			FirstSeenAt: "2026-08-27T13:00:00.000000Z",
		},
	}}
	status := &idiosv1.Status{
		DaemonRunning: true, WrittenAt: "2026-08-27T14:39:02.100000Z", Pid: 4242, Version: "0.1.0",
		Clusters: []*idiosv1.StatusCluster{
			{Id: 1, Ready: true, LastEventAt: sp("2026-08-27T14:39:02.100000Z"), SkewSeconds: 1.5},
			{Id: 2},
		},
		Writer:   &idiosv1.WriterStats{Transactions: 1204, Errors: 2, P99Ms: 3.5},
		Handlers: &idiosv1.HandlerStats{Errors: 1, Panics: 0},
		Capture: &idiosv1.CaptureStats{Queued: 9, Completed: 7, Dropped: 1, Gaps: []*idiosv1.GapCount{
			{Gap: idiosv1.CaptureGap_CAPTURE_GAP_POD_DELETED, Count: 1},
			{Gap: idiosv1.CaptureGap_CAPTURE_GAP_NO_OUTPUT, Count: 2},
		}},
		Closer: &idiosv1.CloserStats{
			LastTickAt: sp("2026-08-27T14:38:00.000000Z"), Closed: 5, Attached: 3,
			ClosedTotal: 14, AttachedTotal: 6, Opened: 1, OpenedTotal: 9,
		},
		OpenByCategory: []*idiosv1.CategoryCount{
			{Category: idiosv1.Category_CATEGORY_OOM, Count: 1},
			{Category: idiosv1.Category_CATEGORY_CRASH, Count: 3},
		},
		ClosedByReason: []*idiosv1.CloseReasonCount{
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_RECOVERED, Count: 2},
			{CloseReason: idiosv1.CloseReason_CLOSE_REASON_POD_DELETED, Count: 1},
		},
		ArtifactsByOutcome: []*idiosv1.OutcomeCount{
			{Outcome: "file", Count: 12}, {Outcome: "no_output", Count: 1},
		},
		RowCounts: &idiosv1.RowCounts{Pods: 42, LivePods: 38, Transitions: 310, Events: 88},
		LatestSweepRuns: []*idiosv1.SweepRun{{
			Id: 7, RanAt: "2026-08-27T14:00:00.000000Z", Cutoff: "2026-08-24T14:00:00.000000Z",
			TableName: "container_state_history", RowsRemoved: 3, FilesRemoved: 1, BytesRemoved: 262144,
			DurationMs: 4, Error: sp("disk full"),
		}},
		DbBytes: 1048576, WalBytes: 32768, ArtifactFiles: 12, ArtifactBytes: 262144,
		RetentionDays: 3, SweepIntervalSeconds: 3600, StabilizationWindowSeconds: 600,
		StabilizationCheckIntervalSeconds: 30, EarlyCaptureDebounceSeconds: 60, ApiStreamThrottleSeconds: 1,
		SchedulingGraceSeconds: 60, ProbeGraceSeconds: 60, StuckAfterSeconds: 900, AttentionWindowSeconds: 86400,
	}
	runWireCases(t, []wireCase{
		{"clusters", &fixtureServer{clusters: clusters}, "/v1/clusters", http.StatusOK, false},
		{
			"kube_contexts", &fixtureServer{kubeContexts: &idiosv1.KubeContextsResponse{
				Contexts: []*idiosv1.KubeContext{
					{Name: "orbstack", Cluster: "orbstack", Server: "https://127.0.0.1:26443"},
				},
			}}, "/v1/kube/contexts", http.StatusOK, false,
		},
		{
			"kube_namespaces", &fixtureServer{kubeNamespaces: &idiosv1.KubeNamespacesResponse{
				Names: []string{"default", "idios-smoke"}, Forbidden: false,
			}}, "/v1/kube/contexts/orbstack/namespaces", http.StatusOK, false,
		},
		// A Role that cannot list namespaces sends the marker and no names,
		// which the application has to tell from a cluster that has none.
		{
			"kube_namespaces_forbidden", &fixtureServer{kubeNamespaces: &idiosv1.KubeNamespacesResponse{
				Forbidden: true,
			}}, "/v1/kube/contexts/orbstack/namespaces", http.StatusOK, false,
		},
		{"status", &fixtureServer{status: status}, "/v1/status", http.StatusOK, false},
	})
}
