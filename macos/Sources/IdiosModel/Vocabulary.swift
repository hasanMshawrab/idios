// A word a person hovers must give back the value the daemon stores, because
// that is what a query or a bug report is written in; the parenthesis carries
// it wherever the reader's word is not the stored one.
private func tooltipText(word: String, raw: String, sentence: String) -> String {
    word.lowercased() == raw ? "\(word): \(sentence)" : "\(word) (\(raw)): \(sentence)"
}

extension IncidentState {
    /// title is the reader's word for this state as a heading: the sidebar
    /// row, the list title, the empty view.
    public var title: String {
        switch self {
        case .open: "Open"
        case .acknowledged: "Acknowledged"
        case .recovered: "Recovered"
        case .podDeleted: "Pod deleted"
        case .jobFinished: "Job finished"
        case .manual: "Resolved"
        case .dismissed: "Dismissed"
        case .attention: "Attention"
        }
    }

    /// label is the reader's word for this state in a badge; the raw value
    /// stays on hover and on copy.
    public var label: String { title.lowercased() }

    /// key is the letter that puts an incident into this state, when one does.
    public var key: String? {
        switch self {
        case .acknowledged: "a"
        case .manual: "r"
        case .dismissed: "d"
        default: nil
        }
    }

    /// tooltip is the one sentence every mention of this state carries.
    public var tooltip: String { tooltipText(word: title, raw: rawValue, sentence: sentence) }

    private var sentence: String {
        switch self {
        case .open:
            "Still happening, or still sitting broken. Nobody has acknowledged it."
        case .acknowledged:
            "Still open, but a person has marked it seen (a). Kept if the incident reopens."
        case .recovered:
            """
            Closed by the system: the container ran and stayed ready for 10 minutes, or the \
            pod got scheduled. Can reopen.
            """
        case .podDeleted:
            """
            Closed because the pod is gone. Final. Shown with the inferred reason: rollout, \
            scaled down, job pruned, evicted, unknown.
            """
        case .jobFinished:
            """
            The Job reached its final condition, was deleted, or a later run of the same \
            CronJob completed. The run may have failed; finished is about the Job's \
            lifecycle, not its outcome. Final.
            """
        case .manual:
            """
            A person closed it (r). Final: a recurrence opens a new incident. Can be undone \
            while still resolved.
            """
        case .dismissed:
            """
            Hidden by a person (d). Outranks every other state. Cleared if the incident \
            reopens.
            """
        case .attention:
            """
            A view, never a row's state: everything open, plus anything that closed in the \
            last N hours without being acknowledged or dismissed, where N is the daemon's \
            attention window.
            """
        }
    }
}

extension CloseReason {
    /// label is the reader's word for this reason; a manual close reads
    /// resolved, as its state does, with the raw value on hover.
    public var label: String {
        self == .manual ? "resolved" : rawValue.replacingOccurrences(of: "_", with: " ")
    }
}

extension Category {
    /// tooltip is the one sentence every mention of this category carries.
    public var tooltip: String { tooltipText(word: label, raw: rawValue, sentence: sentence) }

    private var sentence: String {
        switch self {
        case .crash:
            """
            The container exits with a non-zero code and the kubelet restarts it \
            (CrashLoopBackOff, Error).
            """
        case .oom:
            """
            The kubelet killed the container for exceeding its memory limit (OOMKilled, \
            OOMKilling).
            """
        case .uncleanExit:
            """
            The container died badly while its pod was terminating: a non-zero exit or a \
            signal on the way out (Error).
            """
        case .imagePull:
            """
            The image cannot be pulled (ImagePullBackOff, ErrImagePull, InvalidImageName).
            """
        case .config:
            """
            The container cannot be created or started from its spec: a missing ConfigMap, \
            Secret or volume, or a command that cannot run (CreateContainerConfigError, \
            CreateContainerError, ContainerCannotRun, StartError).
            """
        case .probe:
            "A liveness or readiness probe fails while the container runs (Unhealthy)."
        case .scheduling:
            """
            The pod has had no node for longer than the grace window (FailedScheduling, \
            Unschedulable).
            """
        case .stuck:
            """
            The pod has been pending or terminating for longer than the stuck threshold with \
            no reason of its own.
            """
        case .nodePressure:
            """
            The kubelet evicted the pod for node pressure (Evicted from the kubelet, \
            TerminationByKubelet).
            """
        case .rescheduled:
            """
            The pod was preempted or evicted by a controller and will be placed again \
            (PreemptionByScheduler, DeletionByTaintManager, Evicted from the eviction API).
            """
        case .jobFailed:
            """
            The Job reached its Failed condition (BackoffLimitExceeded, DeadlineExceeded).
            """
        case .other:
            """
            A failure idios records but does not classify; the reason is shown as stored.
            """
        }
    }
}

/// restartsTooltip explains the Restarts column.
public let restartsTooltip = """
    Container restarts attached to the incident (occurrences). "-" for an incident whose \
    subject is a Job.
    """

/// jobContainerTooltip explains the container column reading job.
public let jobContainerTooltip = "The incident's subject is the Job itself, not a container."

/// KindWord is one word of the kind vocabulary: what a person reads and
/// the sentence every mention of it carries.
public struct KindWord: Identifiable, Hashable, Sendable {
    /// id is the word itself, which is unique across the vocabulary.
    public let id: String
    /// word is what a person reads.
    public let word: String
    /// sentence is the one sentence every mention of the word carries.
    public let sentence: String

    /// init builds a word whose id is the word.
    public init(word: String, sentence: String) {
        self.id = word
        self.word = word
        self.sentence = sentence
    }
}

/// kindWords is the kind vocabulary in the order it teaches: the thing
/// that runs, then what is inside it, then what owns it.
public let kindWords: [KindWord] = [
    KindWord(
        word: "Pod",
        sentence: """
            One running copy of a program. The only thing that actually runs, and the thing \
            that gets replaced.
            """),
    KindWord(word: "Container", sentence: "One process inside a pod."),
    KindWord(
        word: "Workload",
        sentence: """
            What owns pods and decides how many run: a Deployment, a CronJob, a Job, or a \
            bare pod that nothing owns.
            """),
    KindWord(
        word: "Deployment", sentence: "Keeps N copies running and replaces one that dies."),
    KindWord(
        word: "Job",
        sentence: """
            Runs a task until it succeeds, retrying up to the backoff limit, each retry a new \
            pod. Failed when the retries run out.
            """),
    KindWord(word: "CronJob", sentence: "Creates a new Job on every tick of its schedule."),
    KindWord(word: "Run", sentence: "One Job created by a CronJob."),
    KindWord(
        word: "Incident",
        sentence: """
            One failure idios recorded: one per container failure, and one more on the Job \
            when it gives up.
            """),
]

/// ownerChains is how the kinds fit together, one chain per way a pod
/// comes to exist.
public let ownerChains: [String] = [
    "CronJob -> Job (a run) -> Pod (an attempt) -> Container",
    "Deployment -> ReplicaSet -> Pod -> Container",
    "bare Pod -> Container",
]
