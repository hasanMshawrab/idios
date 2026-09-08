// Package prompt renders the text an AI agent is handed for one incident:
// a short prompt for an agent that pulls its own evidence over the MCP
// server, and a self-contained snapshot for one that cannot reach the
// machine.
package prompt

import (
	"sort"
	"strconv"
	"strings"

	"github.com/hasanMshawrab/idios/internal/query"
	"github.com/hasanMshawrab/idios/internal/store"
)

// Modes name the two prompts.
const (
	ModeMCP      = "mcp"
	ModeSnapshot = "snapshot"
)

// Log is one captured file as the snapshot carries it: the row that records
// the capture, and either the content or the reason there is none.
type Log struct {
	Artifact store.Artifact
	Content  string
	// Missing says why the section has no content, for a capture that left a
	// gap or a file the sweep took after the row.
	Missing string
}

// Input is everything the two prompts are rendered from. Only Detail and
// ClusterName are read in mode mcp: that agent fetches the rest itself.
type Input struct {
	Detail query.IncidentDetail
	// ClusterName is the cluster as the MCP tools take it; the numeric id the
	// incident row carries means nothing to them.
	ClusterName string
	Timeline    []query.TimelineEntry
	Logs        []Log
	// PodJSON is the captured pod object after sanitizing, or the reason
	// there is none.
	PodJSON string
}

// mcpIntro and snapshotIntro open their prompt: what the agent is looking at
// and what it may not assume is in the data.
const (
	mcpIntro = `You are investigating one Kubernetes incident recorded by idios, a
local incident recorder for pods and jobs in a few namespaces. The
idios MCP server is connected and read-only. Pull only what you
need; logs are read with tails, ranges or grep, never whole.`

	snapshotIntro = `You are investigating one Kubernetes incident from a snapshot
recorded by idios, a local incident recorder. The snapshot is
complete as recorded, but the recorder watches only pods, jobs and
their events in a few namespaces: scaling decisions, autoscalers
and node history are outside the data - say so rather than guess.`
)

// tools is the whole vocabulary of the MCP server, so the agent never has to
// guess at a tool name. The sentences after it head off two easy misreadings -
// a kind argument spelled differently than the incident rows print it, and an
// absent scalar mistaken for missing data rather than a dropped zero - and
// name the filters, which are the difference between one query and twenty.
const tools = `Tools: list_incidents, get_incident, get_pod, read_log, read_pod_json,
get_workload, get_status. Workload kinds: Deployment, StatefulSet,
DaemonSet, Job, CronJob, ReplicaSet or none, the spellings incident
rows print; a tool argument matches them case-insensitively. An absent
scalar in any response is proto3 dropping a zero value, not missing
data - a container with no resource fields has no requests and no
limits. list_incidents narrows by state, category, cluster, namespace,
workload, pod uid, job uid and node name; the node is how one machine's
storm of failures comes back as a single list.`

// sections is the snapshot's legend, in the order the sections follow it.
const sections = `[incident]   detail and timeline
[pod]        containers and conditions
[events]     every event of the pod, attached or not, time-ordered
[logs]       every capture of the pod, one section each, truncation marked
[pod.json]   sanitized: environment values are stripped
[related]    incidents sharing the pod or the job`

// answerRules end both prompts. They are one text on purpose: an answer must
// not depend on which way the evidence arrived.
const answerRules = `Answer rules: separate what the data proves from what it only
suggests; when the trigger lies outside the recorded data (scaling
decisions, node history), say so rather than guess; finish with
"What happened" as at most five ordered timestamped steps and
"Follow-ups" listing only what a human should change.`

// MCP renders the short prompt for an agent that has the MCP server.
func MCP(in Input) string {
	id := strconv.FormatInt(in.Detail.Incident.ID, 10)
	order := "Suggested order, abandon it when the evidence points elsewhere:\n" +
		"1. get_incident " + id + " for the detail, the timeline and the incidents\n" +
		"   sharing this pod or job.\n" +
		"2. get_pod for the whole event stream, containers, conditions and the\n" +
		"   pods of the same controller.\n" +
		"3. read_log tails of the subject container around the failure time."
	return join(mcpIntro, headline(in.Detail)+"\n"+identity(in), tools, order, answerRules)
}

// Snapshot renders the self-contained prompt: the same incident with every
// piece of evidence the recorder holds for it.
func Snapshot(in Input) string {
	return join(snapshotIntro, sections,
		headline(in.Detail)+"\n"+identity(in),
		incidentSection(in), podSection(in), eventsSection(in),
		logsSection(in), podJSONSection(in), relatedSection(in), answerRules)
}

// headline is the one line that says which incident this is and how it ended.
func headline(d query.IncidentDetail) string {
	i := d.Incident
	return "Incident " + strconv.FormatInt(i.ID, 10) + ": " + i.Category + " - " +
		subject(d) + " - " + outcome(d) + " - " + i.State + "."
}

// subject names what the incident is about, as a person reads it.
func subject(d query.IncidentDetail) string {
	i := d.Incident
	name := i.Namespace + "/"
	switch {
	case i.SubjectKind == store.SubjectJob && d.Job != nil:
		name += "job " + d.Job.Name
	case i.PodName != nil:
		name += *i.PodName
	case d.Pod != nil:
		name += d.Pod.Name
	default:
		name += "pod " + deref(i.PodUID)
	}
	if i.ContainerName != "" {
		name += " container " + i.ContainerName
	}
	return name
}

// outcome is how the subject container ended, or the reason the recorder last
// saw when nothing exited.
func outcome(d query.IncidentDetail) string {
	i := d.Incident
	if i.ExitCode == nil {
		return i.LastReason
	}
	out := "exit " + strconv.FormatInt(*i.ExitCode, 10)
	if i.Signal != nil && *i.Signal != 0 {
		out += " signal " + strconv.FormatInt(*i.Signal, 10)
	}
	if i.LastReason != "" {
		out += " (" + i.LastReason + ")"
	}
	return out
}

// identity carries the names and uids the tools and the sibling lookups key
// on. The cluster travels by name because that is what every tool takes; the
// id only stands in when the cluster row is gone.
func identity(in Input) string {
	i := in.Detail.Incident
	cluster := in.ClusterName
	if cluster == "" {
		cluster = strconv.FormatInt(i.ClusterID, 10)
	}
	out := []string{"Cluster " + cluster}
	if i.PodUID != nil {
		out = append(out, "pod uid "+*i.PodUID)
	}
	if i.JobUID != nil {
		out = append(out, "job uid "+*i.JobUID)
	}
	if i.WorkloadName != "" {
		out = append(out, "workload "+i.WorkloadKind+"/"+i.WorkloadName)
	}
	return strings.Join(out, ", ") + "."
}

func incidentSection(in Input) string {
	i := in.Detail.Incident
	f := fields{}
	f.add("category", i.Category)
	f.add("state", i.State)
	f.add("subject_kind", i.SubjectKind)
	f.add("occurrences", strconv.FormatInt(i.Occurrences, 10))
	f.add("first_reason", i.FirstReason)
	f.add("last_reason", i.LastReason)
	f.addPtr("last_message", i.LastMessage)
	f.addPtr("image", i.Image)
	f.addPtr("image_tag", i.ImageTag)
	f.add("opened_at", i.OpenedAt)
	f.add("last_seen_at", i.LastSeenAt)
	f.addPtr("closed_at", i.ClosedAt)
	f.addPtr("close_reason", i.CloseReason)
	f.addPtr("acknowledged_at", i.AcknowledgedAt)
	f.addPtr("dismissed_at", i.DismissedAt)
	f.addPtr("note", i.Note)
	return "[incident]\n" + f.block() + "\ntimeline:\n" + timeline(in.Timeline)
}

func timeline(entries []query.TimelineEntry) string {
	if len(entries) == 0 {
		return none
	}
	var b strings.Builder
	for _, e := range entries {
		at := e.ObservedAt
		if e.K8sAt != nil {
			at = *e.K8sAt
		}
		f := fields{}
		f.addPtr("container", e.ContainerName)
		f.addPtr("state", e.State)
		f.addPtr("reason", e.Reason)
		f.addInt("exit_code", e.ExitCode)
		f.addInt("signal", e.Signal)
		f.addInt("restarts", e.RestartCount)
		f.addBool("gap_reconstructed", e.GapReconstructed)
		f.addPtr("condition", e.ConditionType)
		f.addPtr("status", e.ConditionStatus)
		f.addPtr("event_type", e.EventType)
		f.addPtr("event_reason", e.EventReason)
		f.addInt("count", e.Count)
		f.addInt("artifact_id", e.ArtifactID)
		f.addPtr("artifact_kind", e.ArtifactKind)
		f.addPtr("capture_gap", e.CaptureGap)
		f.addPtr("replicaset", e.ReplicaSetName)
		f.addPtr("image_tag", e.ImageTag)
		f.addInt("revision", e.Revision)
		f.addPtr("lifecycle", e.Lifecycle)
		f.addPtr("close_reason", e.CloseReason)
		f.addPtr("message", e.Message)
		b.WriteString("  " + at + "  " + e.Kind)
		if len(f.pairs) > 0 {
			b.WriteString("  " + strings.Join(f.pairs, " "))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func podSection(in Input) string {
	d := in.Detail
	if d.Pod == nil {
		return "[pod]\n" + "The pod row is gone: the sweep took it while the incident outlived it."
	}
	p := *d.Pod
	f := fields{}
	f.add("name", p.Name)
	f.add("uid", p.UID)
	f.add("namespace", p.Namespace)
	f.addPtr("node", p.NodeName)
	f.add("phase", p.Phase)
	f.addPtr("status_reason", p.StatusReason)
	f.addPtr("status_message", p.StatusMessage)
	f.addPtr("qos_class", p.QOSClass)
	if p.ControllerKind != "" {
		f.add("controller", p.ControllerKind+"/"+p.ControllerName)
	}
	if p.WorkloadName != "" {
		f.add("workload", p.WorkloadKind+"/"+p.WorkloadName)
	}
	f.add("created_at", p.CreatedAt)
	f.addPtr("started_at", p.StartedAt)
	f.addPtr("deletion_requested_at", p.DeletionRequestedAt)
	f.addPtr("deleted_at", p.DeletedAt)
	f.addPtr("deletion_source", p.DeletionSource)
	f.addPtr("deletion_reason", p.DeletionReason)
	return "[pod]\n" + f.block() +
		"\ncontainers:\n" + containers(d.Containers) +
		"\nconditions:\n" + conditions(d.Conditions)
}

func containers(rows []store.Container) string {
	if len(rows) == 0 {
		return none
	}
	var b strings.Builder
	for _, c := range rows {
		f := fields{}
		f.add("kind", c.Kind)
		f.add("image", c.Image)
		f.add("state", c.State)
		f.addPtr("reason", c.Reason)
		f.addInt("exit_code", c.ExitCode)
		f.addInt("signal", c.Signal)
		f.add("ready", strconv.FormatBool(c.Ready))
		f.add("restarts", strconv.FormatInt(c.RestartCount, 10))
		f.addPtr("running_since", c.RunningSince)
		f.addPtr("last_terminated_reason", c.LastTerminatedReason)
		f.addInt("last_terminated_exit_code", c.LastTerminatedExitCode)
		f.addInt("last_terminated_signal", c.LastTerminatedSignal)
		f.addPtr("last_terminated_at", c.LastTerminatedAt)
		f.addPtr("cpu_request", c.CPURequest)
		f.addPtr("cpu_limit", c.CPULimit)
		f.addPtr("mem_request", c.MemRequest)
		f.addPtr("mem_limit", c.MemLimit)
		b.WriteString("  " + c.Name + "  " + strings.Join(f.pairs, " ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func conditions(rows []store.PodCondition) string {
	if len(rows) == 0 {
		return none
	}
	var b strings.Builder
	for _, c := range rows {
		at := c.ObservedAt
		if c.K8sTransitionAt != nil {
			at = *c.K8sTransitionAt
		}
		line := "  " + at + "  " + c.Type + "=" + c.Status
		if c.Reason != "" {
			line += " reason=" + c.Reason
		}
		if c.Message != nil && *c.Message != "" {
			line += " message=" + quote(*c.Message)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// eventsSection carries the pod's whole stream. An event that attached to
// another incident is marked rather than dropped: the incident that opened
// late holds none of the earlier evidence, and that stream is the story.
func eventsSection(in Input) string {
	rows := in.Detail.Events
	if len(rows) == 0 {
		return "[events]\n" + none
	}
	id := in.Detail.Incident.ID
	var b strings.Builder
	b.WriteString("[events]\n")
	for _, e := range rows {
		line := "  " + e.LastTS + "  " + e.Type + " " + e.Reason
		if e.Count > 1 {
			line += " x" + strconv.FormatInt(e.Count, 10)
		}
		if e.SourceComponent != "" {
			line += " (" + e.SourceComponent + ")"
		}
		switch {
		case e.IncidentID == nil:
			line += " [unattached]"
		case *e.IncidentID != id:
			line += " [incident " + strconv.FormatInt(*e.IncidentID, 10) + "]"
		}
		b.WriteString(line + "  " + quote(e.Message) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// logsSection delimits every captured file, so the model can tell one
// container's output from another's and a whole file from a cut one.
func logsSection(in Input) string {
	if len(in.Logs) == 0 {
		return "[logs]\nNo log was captured for " + scope(in) + "."
	}
	// The dead instances oldest first and the running container's log last, so
	// the sections read forward in time like everything above them.
	logs := append([]Log(nil), in.Logs...)
	sort.SliceStable(logs, func(a, b int) bool {
		x, y := logs[a].Artifact, logs[b].Artifact
		if x.ContainerName != y.ContainerName {
			return x.ContainerName < y.ContainerName
		}
		if x.Kind != y.Kind {
			return kindRank(x.Kind) < kindRank(y.Kind)
		}
		return x.RestartCount < y.RestartCount
	})
	var b strings.Builder
	b.WriteString("[logs]")
	for _, l := range logs {
		a := l.Artifact
		head := logName(a)
		f := fields{}
		if a.ContainerName != "" {
			f.add("container", a.ContainerName)
		}
		f.add("captured_at", a.CapturedAt)
		f.add("bytes", strconv.FormatInt(a.SizeBytes, 10))
		if a.CapturedEarly {
			f.add("captured_early", "true")
		}
		b.WriteString("\n--- " + head + " " + strings.Join(f.pairs, " ") + " ---\n")
		if l.Missing != "" {
			b.WriteString(l.Missing + "\n")
			continue
		}
		b.WriteString(strings.TrimRight(l.Content, "\n") + "\n")
		if a.Truncated {
			b.WriteString("--- " + head + " was truncated at capture: the log was longer than the capture limit ---\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// kindRank puts a dead instance's log before the running container's.
func kindRank(kind string) int {
	if kind == store.ArtifactLogPrevious {
		return 0
	}
	return 1
}

// logName names a captured file the way the file is named on disk.
func logName(a store.Artifact) string {
	switch a.Kind {
	case store.ArtifactLogPrevious:
		return "restart_" + pad(a.RestartCount)
	case store.ArtifactLogCurrent:
		return "current.log"
	default:
		return a.Kind
	}
}

// pad is the three-digit restart index the capture names a dead instance by.
func pad(n int64) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func podJSONSection(in Input) string {
	if in.PodJSON == "" {
		return "[pod.json]\nNo pod object was captured for " + scope(in) + "."
	}
	return "[pod.json]\n" + strings.TrimRight(in.PodJSON, "\n")
}

// scope names what the snapshot gathered evidence over: a pod incident
// carries its pod's whole record, anything else only what attached to it.
func scope(in Input) string {
	if in.Detail.Incident.PodUID != nil {
		return "this pod"
	}
	return "this incident"
}

func relatedSection(in Input) string {
	if len(in.Detail.RelatedIncidents) == 0 {
		return "[related]\nNo other incident shares this pod or job."
	}
	var b strings.Builder
	b.WriteString("[related]\n")
	for _, r := range in.Detail.RelatedIncidents {
		line := "  " + strconv.FormatInt(r.ID, 10) + "  " + r.Category + "  " + r.State
		if r.ContainerName != "" {
			line += "  container " + r.ContainerName
		}
		line += "  opened " + r.OpenedAt + "  last_seen " + r.LastSeenAt
		if r.LastReason != "" {
			line += "  " + r.LastReason
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// none stands where a section has nothing, so an empty stretch is read as
// nothing recorded and not as something the prompt forgot.
const none = "  (none recorded)"

// fields collects the key=value pairs of one line and the key: value lines of
// one block, dropping every absent value: a prompt full of empty keys spends
// the model's attention on nothing.
type fields struct {
	pairs []string
}

func (f *fields) add(key, value string) {
	if value == "" {
		return
	}
	f.pairs = append(f.pairs, key+"="+quoteIfNeeded(value))
}

func (f *fields) addPtr(key string, value *string) {
	if value == nil {
		return
	}
	f.add(key, *value)
}

func (f *fields) addInt(key string, value *int64) {
	if value == nil {
		return
	}
	f.add(key, strconv.FormatInt(*value, 10))
}

func (f *fields) addBool(key string, value *bool) {
	if value == nil || !*value {
		return
	}
	f.add(key, "true")
}

// block renders the pairs one per line, as a section header's own facts.
func (f *fields) block() string {
	out := make([]string, 0, len(f.pairs))
	for _, p := range f.pairs {
		key, value, _ := strings.Cut(p, "=")
		out = append(out, key+": "+value)
	}
	return strings.Join(out, "\n")
}

// quoteIfNeeded keeps a value that holds a space from running into the next
// key.
func quoteIfNeeded(value string) string {
	if !strings.ContainsAny(value, " \t\n") {
		return value
	}
	return quote(value)
}

// quote flattens a message onto its own line: a kubelet message carries
// newlines and would otherwise break the line-per-fact reading.
func quote(value string) string {
	return strconv.Quote(strings.TrimRight(value, "\n"))
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// join separates the parts by a blank line.
func join(parts ...string) string {
	return strings.Join(parts, "\n\n") + "\n"
}
