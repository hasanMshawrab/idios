package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hasanMshawrab/idios/internal/apigen/idiosv1"
	"github.com/hasanMshawrab/idios/internal/sanitize"
)

// listIncidentsArgs are the filters the incident list endpoint takes. An
// empty field is not applied.
type listIncidentsArgs struct {
	State        string `json:"state,omitempty" jsonschema:"keep only incidents in this state: open, acknowledged, recovered, pod_deleted, job_finished, manual, dismissed or attention (open, or closed within the daemon's attention window and never acknowledged or dismissed)"`
	Category     string `json:"category,omitempty" jsonschema:"keep only this failure category: oom, crash, image_pull, config, probe, scheduling, node_pressure, rescheduled, job_failed, other, stuck or unclean_exit"`
	Cluster      string `json:"cluster,omitempty" jsonschema:"keep only incidents in the cluster of this name, as get_status lists it"`
	Namespace    string `json:"namespace,omitempty" jsonschema:"keep only incidents in this Kubernetes namespace"`
	WorkloadKind string `json:"workload_kind,omitempty" jsonschema:"keep only incidents under this owner kind: Deployment, StatefulSet, DaemonSet, Job, CronJob, ReplicaSet or none"`
	WorkloadName string `json:"workload_name,omitempty" jsonschema:"keep only incidents under the owning workload of this name"`
	PodUID       string `json:"pod_uid,omitempty" jsonschema:"keep only incidents on this pod uid"`
	JobUID       string `json:"job_uid,omitempty" jsonschema:"keep only incidents on this job uid, which is how a job and the incidents of its retries are linked"`
	NodeName     string `json:"node_name,omitempty" jsonschema:"keep only incidents opened on this node, which is how a storm of failures on one machine reads as one list"`
	Limit        int32  `json:"limit,omitempty" jsonschema:"how many rows at most; zero means the daemon's default"`
}

// idArgs addresses one incident.
type idArgs struct {
	ID int64 `json:"id" jsonschema:"the incident id"`
}

// podArgs addresses one pod.
type podArgs struct {
	UID string `json:"uid" jsonschema:"the pod uid, as an incident or the pod list reports it"`
}

// readLogArgs addresses one captured log file and the part of it to read.
type readLogArgs struct {
	ArtifactID int64  `json:"artifact_id" jsonschema:"the artifact id, from the artifacts of get_incident or get_pod"`
	TailLines  int    `json:"tail_lines,omitempty" jsonschema:"return the last N lines; the default is the last 200"`
	RangeStart int64  `json:"range_start,omitempty" jsonschema:"return the bytes from this offset, counted from the start of the file"`
	RangeEnd   int64  `json:"range_end,omitempty" jsonschema:"stop the byte range at this offset, exclusive; zero reads to the end of the file"`
	Grep       string `json:"grep,omitempty" jsonschema:"return only the lines matching this regular expression, at most 200 of them"`
	Whole      bool   `json:"whole,omitempty" jsonschema:"return the entire file, however large it is; ask for this only when a narrower read has proved too small"`
}

// artifactArgs addresses one captured artifact.
type artifactArgs struct {
	ArtifactID int64 `json:"artifact_id" jsonschema:"the artifact id of the captured pod.json, from the artifacts of get_incident or get_pod"`
}

// workloadArgs addresses one workload, which is named rather than numbered.
type workloadArgs struct {
	Cluster   string `json:"cluster" jsonschema:"the cluster name, as get_status lists it"`
	Namespace string `json:"namespace" jsonschema:"the Kubernetes namespace of the workload"`
	Kind      string `json:"kind" jsonschema:"the owner kind: Deployment, StatefulSet, DaemonSet, Job, CronJob, ReplicaSet or none"`
	Name      string `json:"name" jsonschema:"the workload name"`
}

// emptyArgs is the input of a tool that takes nothing.
type emptyArgs struct{}

// addTools registers the read-only vocabulary. The descriptions are what an
// agent picks a tool from, so each says what the tool answers, not how.
func (s *Server) addTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_incidents",
		Description: "List the recorded incidents newest first, narrowed by any combination of state, category, cluster, namespace, workload, pod uid, job uid and node name.",
	}, s.listIncidents)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_incident",
		Description: "Get one incident whole: the failing container's kubelet facts, the owner chain, the captured artifacts and the time-ordered timeline of what happened around it.",
	}, s.getIncident)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_pod",
		Description: "Get one pod whole: its containers, the latest reading of every condition, its entire Kubernetes event stream, its state history, and the other pods of its controller, five at most with their total.",
	}, s.getPod)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "read_log",
		Description: "Read part of one captured container log: the last lines by default, or a byte range, or only the lines matching a pattern.",
	}, s.readLog)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "read_pod_json",
		Description: "Read the pod object captured at the failure, with environment values and other secret material redacted.",
	}, s.readPodJSON)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_workload",
		Description: "Get one workload: its rollout history, its pods live and deleted, and the incidents recorded against it.",
	}, s.getWorkload)
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_status",
		Description: "Report what the daemon is watching and how complete the recording is: the clusters, their readiness, the retention window and the capture counters.",
	}, s.getStatus)
}

// text answers a tool call with one block of text.
func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// jsonResult answers a tool call with the JSON of parts, whose fields are
// already-encoded responses.
func jsonResult(parts any) (*mcp.CallToolResult, any, error) {
	out, err := json.Marshal(parts)
	if err != nil {
		return nil, nil, err
	}
	return text(string(out)), nil, nil
}

func (s *Server) listIncidents(ctx context.Context, _ *mcp.CallToolRequest, in listIncidentsArgs) (*mcp.CallToolResult, any, error) {
	req := &idiosv1.ListIncidentsRequest{
		State: in.State, Category: in.Category, Namespace: in.Namespace,
		WorkloadKind: normalizeKind(in.WorkloadKind), WorkloadName: in.WorkloadName,
		PodUid: in.PodUID, JobUid: in.JobUID, NodeName: in.NodeName, Limit: in.Limit,
	}
	if in.Cluster != "" {
		id, err := s.d.clusterID(ctx, in.Cluster)
		if err != nil {
			return nil, nil, err
		}
		req.ClusterIds = []int64{id}
	}
	list, err := s.d.api.ListIncidents(ctx, req)
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	out, err := encode(list)
	if err != nil {
		return nil, nil, err
	}
	return text(string(out)), nil, nil
}

func (s *Server) getIncident(ctx context.Context, _ *mcp.CallToolRequest, in idArgs) (*mcp.CallToolResult, any, error) {
	detail, err := s.d.api.GetIncident(ctx, &idiosv1.GetIncidentRequest{Id: in.ID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	timeline, err := s.d.api.IncidentTimeline(ctx, &idiosv1.IncidentTimelineRequest{Id: in.ID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	var parts struct {
		Incident json.RawMessage `json:"incident"`
		Timeline json.RawMessage `json:"timeline"`
	}
	if parts.Incident, err = encode(detail); err != nil {
		return nil, nil, err
	}
	if parts.Timeline, err = encode(timeline); err != nil {
		return nil, nil, err
	}
	return jsonResult(parts)
}

func (s *Server) getPod(ctx context.Context, _ *mcp.CallToolRequest, in podArgs) (*mcp.CallToolResult, any, error) {
	detail, err := s.d.api.GetPod(ctx, &idiosv1.GetPodRequest{Uid: in.UID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	events, err := s.d.api.PodEvents(ctx, &idiosv1.PodEventsRequest{Uid: in.UID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	history, err := s.d.api.PodHistory(ctx, &idiosv1.PodHistoryRequest{Uid: in.UID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	var parts struct {
		Pod     json.RawMessage `json:"pod"`
		Events  json.RawMessage `json:"events"`
		History json.RawMessage `json:"history"`
	}
	if parts.Pod, err = encode(detail); err != nil {
		return nil, nil, err
	}
	if parts.Events, err = encode(events); err != nil {
		return nil, nil, err
	}
	if parts.History, err = encode(history); err != nil {
		return nil, nil, err
	}
	return jsonResult(parts)
}

func (s *Server) readLog(ctx context.Context, _ *mcp.CallToolRequest, in readLogArgs) (*mcp.CallToolResult, any, error) {
	sel := selection{tailLines: in.TailLines, rangeStart: in.RangeStart, rangeEnd: in.RangeEnd, grep: in.Grep, whole: in.Whole}
	// A call that names two cuts has no answer, so it is refused before the
	// file crosses the wire.
	if sel.named() > 1 {
		return nil, nil, errOneSelection
	}
	a, err := s.d.api.GetArtifact(ctx, &idiosv1.GetArtifactRequest{Id: in.ArtifactID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	body, err := s.d.content(ctx, in.ArtifactID)
	if err != nil {
		return nil, nil, err
	}
	cut, showing, err := trim(body, sel)
	if err != nil {
		return nil, nil, err
	}
	return text(logHeader(a, len(body), showing) + string(cut)), nil, nil
}

// kindName is the artifact kind as the wire spells it: the enum's Go name
// is not what the API, the database or the application call it.
func kindName(k idiosv1.ArtifactKind) string {
	data, err := k.MarshalJSON()
	if err != nil {
		return k.String()
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return k.String()
	}
	return s
}

// logHeader names the file the lines came from and the cut that was made,
// so an agent can ask for a different part without listing the artifacts
// again.
func logHeader(a *idiosv1.Artifact, size int, showing string) string {
	var b strings.Builder
	b.WriteString("artifact " + strconv.FormatInt(a.GetId(), 10))
	b.WriteString(" kind=" + kindName(a.GetKind()))
	if a.GetContainerName() != "" {
		b.WriteString(" container=" + a.GetContainerName())
	}
	if a.GetRestartCount() >= 0 {
		b.WriteString(" restart=" + strconv.FormatInt(int64(a.GetRestartCount()), 10))
	}
	b.WriteString(" captured_at=" + a.GetCapturedAt())
	b.WriteString(" bytes=" + strconv.Itoa(size))
	if a.GetTruncated() {
		b.WriteString(" truncated_by_capture=true")
	}
	b.WriteString("\nshowing: " + showing + "\n\n")
	return b.String()
}

func (s *Server) readPodJSON(ctx context.Context, _ *mcp.CallToolRequest, in artifactArgs) (*mcp.CallToolResult, any, error) {
	a, err := s.d.api.GetArtifact(ctx, &idiosv1.GetArtifactRequest{Id: in.ArtifactID})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	if a.GetKind() != idiosv1.ArtifactKind_ARTIFACT_KIND_POD_JSON {
		return nil, nil, fmt.Errorf("artifact %d is a %s, not a captured pod.json; read it with read_log", in.ArtifactID, kindName(a.GetKind()))
	}
	body, err := s.d.content(ctx, in.ArtifactID)
	if err != nil {
		return nil, nil, err
	}
	clean, err := sanitize.PodJSON(body)
	if err != nil {
		return nil, nil, err
	}
	return text(string(clean)), nil, nil
}

func (s *Server) getWorkload(ctx context.Context, _ *mcp.CallToolRequest, in workloadArgs) (*mcp.CallToolResult, any, error) {
	id, err := s.d.clusterID(ctx, in.Cluster)
	if err != nil {
		return nil, nil, err
	}
	detail, err := s.d.api.GetWorkload(ctx, &idiosv1.GetWorkloadRequest{ClusterId: id, Namespace: in.Namespace, Kind: normalizeKind(in.Kind), Name: in.Name})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	out, err := encode(detail)
	if err != nil {
		return nil, nil, err
	}
	return text(string(out)), nil, nil
}

func (s *Server) getStatus(ctx context.Context, _ *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
	st, err := s.d.api.GetStatus(ctx, &idiosv1.GetStatusRequest{})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	// The status message keys each cluster by id; the names every other tool
	// takes are in the cluster list, so the two travel together.
	clusters, err := s.d.api.ListClusters(ctx, &idiosv1.ListClustersRequest{})
	if err != nil {
		return nil, nil, s.d.unreachable(err)
	}
	var parts struct {
		Status   json.RawMessage `json:"status"`
		Clusters json.RawMessage `json:"clusters"`
	}
	if parts.Status, err = encode(st); err != nil {
		return nil, nil, err
	}
	if parts.Clusters, err = encode(clusters); err != nil {
		return nil, nil, err
	}
	return jsonResult(parts)
}
