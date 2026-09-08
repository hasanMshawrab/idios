/// Category is the incident category the daemon derived from the Kubernetes
/// reason.
public enum Category: String, Sendable {
    case oom
    case crash
    case uncleanExit = "unclean_exit"
    case imagePull = "image_pull"
    case config
    case probe
    case scheduling
    case stuck
    case nodePressure = "node_pressure"
    case rescheduled
    case jobFailed = "job_failed"
    case other
}

/// IncidentState is the folder an incident belongs to.
public enum IncidentState: String, Sendable {
    case open
    case acknowledged
    case recovered
    case podDeleted = "pod_deleted"
    case jobFinished = "job_finished"
    case manual
    case dismissed
    /// attention is a filter and a count, never a row's own state.
    case attention
}

/// CloseReason is why an incident stopped being open.
public enum CloseReason: String, Sendable {
    case recovered
    case podDeleted = "pod_deleted"
    case jobFinished = "job_finished"
    case manual
}

/// CaptureGap is why a capture produced no file.
public enum CaptureGap: String, Sendable {
    case podDeleted = "pod_deleted"
    case noPreviousRun = "no_previous_run"
    case forbidden
    case noOutput = "no_output"
    case kubeletError = "kubelet_error"
    case unknown
    case unobservable
}

/// ArtifactKind is what a captured file holds.
public enum ArtifactKind: String, Sendable {
    case logPrevious = "log_previous"
    case logCurrent = "log_current"
    case podJSON = "pod_json"
}

/// ContainerKind is the position of a container in its pod.
public enum ContainerKind: String, Sendable {
    case `init`
    case sidecar
    case app
    case ephemeral
}

/// ContainerState is the kubelet's state for a container.
public enum ContainerState: String, Sendable {
    case waiting
    case running
    case terminated
}

/// DeletionSource is how idios learned a pod was gone.
public enum DeletionSource: String, Sendable {
    case watch
    case reconcile
    case unwatched
}

/// DeletionReason is the inferred cause of a pod deletion.
public enum DeletionReason: String, Sendable {
    case rollout
    case replaced
    case scaledDown = "scaled_down"
    case jobPruned = "job_pruned"
    case unknown
    case evicted
}

/// SubjectKind is what an incident is about.
public enum SubjectKind: String, Sendable {
    case pod
    case job
}

/// TimelineKind is the sort of entry a timeline row carries.
public enum TimelineKind: String, Sendable {
    case containerTransition = "container_transition"
    case condition
    case event
    case capture
    case rollout
    case lifecycle
    case cut
}

/// LifecycleStep is which end of an incident a lifecycle entry marks.
public enum LifecycleStep: String, Sendable {
    case opened
    case closed
}

/// Page is one list response: its rows, whether the daemon cut the list short,
/// and what the endpoint counted beyond the page.
public struct Page<Row: Hashable & Sendable>: Hashable, Sendable {
    public let rows: [Row]
    public let truncated: Bool
    public let total: Int32
    public let failedTotal: Int32

    // Only the job list counts past its own page, and Swift takes no stored
    // property in a constrained extension, so the counts sit on every page and
    // the lists that do not count leave them zero.
    init(rows: [Row], truncated: Bool, total: Int32 = 0, failedTotal: Int32 = 0) {
        self.rows = rows
        self.truncated = truncated
        self.total = total
        self.failedTotal = failedTotal
    }
}

// The generated enums carry protobuf's "unspecified" member, which is the wire
// zero value and never a value the daemon means; it maps to nil like an absent
// field.
func modelEnum<Model: RawRepresentable<String>, Wire: RawRepresentable<String>>(
    _ wire: Wire?
) -> Model? {
    guard let wire else { return nil }
    return Model(rawValue: wire.rawValue)
}

func parseInt64(_ raw: String?) -> Int64? {
    guard let raw else { return nil }
    return Int64(raw)
}

// The stored spelling of every closed vocabulary shown on a surface is already
// the reader's words once underscores become spaces, so the label is derived
// rather than a second table of prose kept true by hand.
extension Category {
    /// label is the reader's word for this category; the raw value stays on
    /// hover and on copy.
    public var label: String { rawValue.replacingOccurrences(of: "_", with: " ") }
}

extension DeletionReason {
    /// label is the reader's word for this reason; the raw value stays on
    /// hover and on copy.
    public var label: String { rawValue.replacingOccurrences(of: "_", with: " ") }
}

extension DeletionSource {
    /// label is the reader's word for this source; the raw value stays on
    /// hover and on copy.
    public var label: String { rawValue.replacingOccurrences(of: "_", with: " ") }
}

extension CaptureGap {
    /// label is the reader's word for this gap; the raw value stays on hover
    /// and on copy.
    public var label: String { rawValue.replacingOccurrences(of: "_", with: " ") }
}
