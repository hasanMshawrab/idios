/// RegionID names one explained region of one screen; a literal is enough
/// because a screen's regions and its explanations are compared at run time,
/// which catches a typo the compiler cannot.
public struct RegionID: Hashable, Sendable, ExpressibleByStringLiteral,
    CustomStringConvertible
{
    /// raw is the id as the screen hangs it on a view.
    public let raw: String

    /// init takes the id.
    public init(_ raw: String) { self.raw = raw }

    /// init(stringLiteral:) lets a table read as a table.
    public init(stringLiteral value: String) { self.raw = value }

    /// description is the id itself, which is what an assertion prints.
    public var description: String { raw }
}

/// ExplainedRegion is one part of a screen and what its help mark says about
/// it: the name a person reads, the stored names it draws, and why to look
/// there.
public struct ExplainedRegion: Identifiable, Hashable, Sendable {
    /// id is the region the screen registers under this explanation.
    public let id: RegionID
    /// title is the name a person reads on the note.
    public let title: String
    /// tags are the stored fields, tables and endpoints behind the region,
    /// drawn as monospace chips, because the name of the thing is what a
    /// person greps for afterwards.
    public let tags: [String]
    /// sentence is what the region means and why to look there.
    public let sentence: String

    /// init builds one region's explanation.
    public init(id: RegionID, title: String, tags: [String], sentence: String) {
        self.id = id
        self.title = title
        self.tags = tags
        self.sentence = sentence
    }
}

/// ExplainedScreen is a screen as help mode knows it: a screen whose regions
/// depend on what is selected is one value per selection, so the set drawn
/// and the set declared can be compared.
public enum ExplainedScreen: Hashable, Sendable {
    case incidents(ListState)
    case palette
    case workloads(WorkloadsPart)
    case podCard(PodPane)
    case container(ContainerTab)
    case run(RunPart)
    case status
}

/// ListState is the incidents list with rows and the incidents list with
/// none; an empty view explains itself and nothing else.
public enum ListState: String, CaseIterable, Hashable, Sendable {
    case rows, empty
}

/// WorkloadsPart is which half of the Workloads screen is explained, and
/// which tab of the detail, because the tabs share no region.
public enum WorkloadsPart: String, CaseIterable, Hashable, Sendable {
    case tree, overview, pods, runs, rollouts, incidents
}

/// RunPart is the Run page's tab, which decides the middle pane's regions
/// and nothing else.
public enum RunPart: String, CaseIterable, Hashable, Sendable {
    case overview, timeline, logs, events
}

// The order leads with the screens that carry the least, which is the order
// the audit walks. The menu bar popover and the sheets are not screens here:
// a 360-point popover and a form have no room for marks, and their controls
// carry their own tooltips.
/// explainedScreens is every screen help mode can be turned on over, which
/// is what the audit walks.
public let explainedScreens: [ExplainedScreen] =
    ListState.allCases.map(ExplainedScreen.incidents)
    + [.palette]
    + WorkloadsPart.allCases.map(ExplainedScreen.workloads)
    + PodPane.allCases.map(ExplainedScreen.podCard)
    + ContainerTab.allCases.map(ExplainedScreen.container)
    + RunPart.allCases.map(ExplainedScreen.run)
    + [.status]

// The tag and the container column are on screen whichever card is selected,
// so every table of the pod page draws these two values rather than a copy of
// the same words each.
private let podPageTopRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "podStateTag", title: "Pod state tag",
        tags: ["pods.phase", "conditions[Ready]", "deleted_at"],
        sentence: """
            Look here first. The kubelet keeps reporting phase Running while every container \
            fails, so the Ready condition is what says the pod is not serving. It reads \
            LOOPING when readiness flipped more than three times in the window, and DELETED \
            once the pod is gone.
            """),
    ExplainedRegion(
        id: "containerCards", title: "Container cards",
        tags: ["containers.kind", "category", "occurrences"],
        sentence: """
            One card per container, init then app then sidecar then ephemeral, the selected \
            one filled with the accent. Each badge is one incident with its category and how \
            many times it has happened, so this column says whether one container is failing \
            or the whole pod is.
            """),
]

private let podVerdictRegion = ExplainedRegion(
    id: "verdict", title: "Verdict block",
    tags: ["occurrences", "mem_limit_bytes", "capture_gap", "image_id"],
    sentence: """
        Read this second, and often stop here: sentences built from fields the page \
        already holds. What happened and how often since when, the memory limit when the \
        kill was an OOM, what was and was not captured, and whether the image digest \
        changed since the incident opened.
        """)

private let kubeletFactsRegion = ExplainedRegion(
    id: "kubeletFacts", title: "What the kubelet reports",
    tags: ["restart_count", "last_terminated_reason", "image_id", "mem_limit_bytes"],
    sentence: """
        Read this third: the evidence the verdict was built from, in the kubelet's own \
        words. Restart count is the container's whole life while occurrences above is \
        this incident's share of it, and the image and digest are the ones recorded when \
        the incident opened, not the current ones.
        """)

private let capturedLogsRegion = ExplainedRegion(
    id: "capturedLogs", title: "Captured logs",
    tags: ["artifacts", "capture_gap", "capture_note"],
    sentence: """
        Read this fourth. The scrubber walks one file per dead instance, oldest to \
        newest, so you can see whether every attempt failed the same way. A missing file \
        is never an empty pane: the gap is named and the API's own words are quoted, \
        which separates a container that printed nothing from a log that could never \
        have been fetched.
        """)

private let podPageRailRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "ownerChain", title: "Owner chain",
        tags: ["workload_kind", "controller_kind", "pod_uid", "container_name"],
        sentence: """
            Read this last, when the verdict is not enough: four links from what decides how \
            many copies run down to the process that failed, each with the uid that \
            identifies it. It answers who will replace this pod, and whether the thing to \
            change is this container or the workload above it.
            """),
    ExplainedRegion(
        id: "siblings", title: "Siblings",
        tags: ["controller_uid", "category"],
        sentence: """
            This pod first, then the others of the same controller, with a category badge \
            where one is failing and a readiness word where none is. One red dot among green \
            is this pod's problem; all red is the image, the config or the cluster.
            """),
]

private let podEventsRegion = ExplainedRegion(
    id: "podEvents", title: "Events",
    tags: ["k8s_events", "involved_uid"],
    sentence: """
        Every container's events together in one served-order table with a container \
        column, plus the pod-level events no container claims. It is the pod's whole \
        event stream, not one container's slice of it.
        """)

private let podConditionsRegion = ExplainedRegion(
    id: "podConditions", title: "Conditions",
    tags: ["pod_condition_history"],
    sentence: """
        The latest reading of every condition type the pod carries, PodScheduled through \
        Ready, with the history beneath it. A condition is a fact about the pod as a \
        whole; no container has one.
        """)

private let podFilesRegion = ExplainedRegion(
    id: "podFiles", title: "Files",
    tags: ["artifacts"],
    sentence: """
        Every container's captured files in one list together, container named on each \
        row. Grouped here because a person comparing two containers' logs for the same \
        failure should not have to leave the pod.
        """)

private let podJSONRegion = ExplainedRegion(
    id: "podJSON", title: "pod.json",
    tags: ["artifacts", "pod_json"],
    sentence: """
        The captured pod object as idios stored it, sanitized before an agent ever sees \
        it. One object describes the pod's whole spec and status; no container has its \
        own copy.
        """)

private let relatedIncidentsRegion = ExplainedRegion(
    id: "relatedIncidents", title: "Related incidents",
    tags: ["job_uid"],
    sentence: """
        The job_failed row and the retries in the Job's other pods, joined by job_uid. \
        Shown only when the pod's controller is a Job, because only a Job has other pods \
        to compare this one against.
        """)

private let listRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "summaryLine", title: "Summary line",
        tags: ["incidents.id", "pods.uid", "last_seen_at"],
        sentence: """
            What the whole view amounts to before a single group is opened: how many \
            problems, meaning groups, how many incidents inside them are still open, how \
            many workloads and bare pods they touch, and the newest time anything was seen. \
            Collapse all folds every group; Acknowledge all marks every open incident of the \
            view seen and asks first, because the rows it writes are not all on screen.
            """),
    ExplainedRegion(
        id: "groupHeader", title: "Group header",
        tags: ["workload_kind", "workload_name", "controller_uid"],
        sentence: """
            One problem, not one incident: every incident of the same workload on one line, \
            with the worst category among them, how many are still open and how many pods \
            they touch. Clicking it folds the group and opens it again, and a key pressed on \
            it acts on every incident it stands for at once.
            """),
    ExplainedRegion(
        id: "runRow", title: "Run row",
        tags: ["job_uid", "GET /jobs", "occurrences"],
        sentence: """
            One Job and the attempts it made, which is what a CronJob produces on every tick \
            of its schedule. Each retry is a new pod, so this line is what says whether the \
            whole task failed or only one attempt did; opening it lists the attempts, and \
            clicking the run opens the page that puts them side by side.
            """),
    ExplainedRegion(
        id: "rollupRow", title: "Rollup row",
        tags: ["workload_kind", "category", "pods.uid"],
        sentence: """
            One line standing for several pods of one workload that are failing the same way, \
            drawn instead of repeating the same reason down the screen. The number is how \
            many pods, which is the fact that says the workload is broken rather than one \
            pod; opening it lists them.
            """),
    ExplainedRegion(
        id: "podRow", title: "Pod row",
        tags: ["incidents.id", "pods.uid", "incidents.state"],
        sentence: """
            One incident: the pod, the container, the reason and how long it has been going \
            on. The badge at the end of the line is its state, which says whether anyone has \
            dealt with it and how it ended if it is over. Return opens the pod page on that \
            container, a, d and r act on the row without leaving the list, and a pod that \
            failed more than once folds its other incidents under this line.
            """),
]

// A view with nothing in it has one thing to say, which is what the state
// that empties it means.
private let listEmptyScreenRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "emptySentence", title: "What the state means",
        tags: ["incidents.state", "incidents.category"],
        sentence: """
            The definition of the state whose view this is, word for word the sentence its \
            badge carries on hover, with the chosen category's definition under it. The last \
            line is the key that puts a row here: a for acknowledged, d for dismissed, r for \
            resolved. An empty view is where those words are worth reading.
            """),
]

private let paletteRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "queryField", title: "Query field",
        tags: ["ns:", "node:", "tag:", "reason:", "#"],
        sentence: """
            One field over whatever screen is up: type part of a pod, a workload or a reason \
            and the sections below fill in as you go. A prefix narrows what is searched, ns: \
            to a namespace, node: to a node, tag: to an image tag and reason: to the \
            kubelet's word, and a leading # goes straight to an incident id. Nothing typed \
            here changes the list behind it.
            """),
    ExplainedRegion(
        id: "footerKeys", title: "Footer keys",
        tags: ["Return", "Cmd-Return", "Shift-Return"],
        sentence: """
            The three ways out of the chosen hit: Return opens it, Cmd-Return opens the \
            workload it belongs to in Workloads, and Shift-Return copies its name and leaves \
            the palette open. Esc closes the palette without opening anything.
            """),
]

// The tree keeps its column beside a selected workload, so its regions are on
// screen on every tab of the detail and all six tables draw these values
// rather than six copies of the same words.
private let workloadsTreeRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "paneHeader", title: "Pane header",
        tags: ["open_incidents"],
        sentence: """
            It names what every trailing number down the tree counts: incidents still open, \
            not pods and not restarts, summed up the tree so a cluster's number is its \
            namespaces' and a namespace's is its workloads'. Collapse all folds every cluster \
            and namespace at once and Expand all opens them again, which is how a tree of \
            many namespaces is read one at a time.
            """),
    ExplainedRegion(
        id: "kindCaption", title: "Kind caption",
        tags: ["workload_kind"],
        sentence: """
            A heading and not a level: it names the kind of the rows under it and counts them. \
            The kinds are in a fixed order, the long-lived ones first and the pods nothing \
            owns last, so the same namespace reads the same way every time rather than \
            reordering itself as failures move around.
            """),
    ExplainedRegion(
        id: "barePods", title: "Pods nothing owns",
        tags: ["workload_kind = none", "pods.name"],
        sentence: """
            A pod with no controller has no workload to be grouped under, so it is listed \
            under its own caption and opens a pod page rather than a pane. There is one row \
            per name, not per pod: a name that has been used by a pod that was deleted and one \
            that is live reads "2 pods" and asks which of them to open, because the name is \
            what a person knows and the uid is what identifies one of them.
            """),
]

private let workloadOverviewRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "statCards", title: "Window cards",
        tags: ["incidents", "occurrences", "category", "image tag"],
        sentence: """
            What the window amounts to in four numbers: how many incidents and how many of \
            them are still open, which kinds of failure they were, how many restarts are \
            attached to them and across how many pods, and which image tags were running when \
            they opened. A tag that appears once beside a count of many is the fact that says \
            a single build is behind them.
            """),
    ExplainedRegion(
        id: "restartsChart", title: "Restarts per hour",
        tags: ["container_state_history", "gap_reconstructed"],
        sentence: """
            One bar per hour of the window, including the quiet hours, because a chart that \
            skips them draws a busy stretch where there was none. The axis is labelled with \
            the peak hour, half of it and zero. A hollow dashed bar is an hour idios pieced \
            together after missing part of it, so a bar you can see through is a count that \
            may be short.
            """),
]

private let workloadPodsRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "podChips", title: "Pod chips",
        tags: ["pods_live", "worst container state", "open_incidents"],
        sentence: """
            Live is the only one the daemon filters by, and it is where the tab opens: pods it \
            has not seen deleted. The other two are read off the page that came back, so they \
            narrow what is already here rather than asking for more: whose worst container is \
            waiting or terminated, and who carries an open incident.
            """),
    ExplainedRegion(
        id: "podsScope", title: "What the page stands for",
        tags: ["pods_truncated", "live_pods", "deleted_pods"],
        sentence: """
            How many pods this is out of how many exist, and it says so when the page is the \
            newest few of many. A bounded page is not a lie about the total: the number after \
            "of" is the stored count, so a workload that has churned through hundreds of pods \
            still says so.
            """),
]

private let workloadRunsRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "runStrip", title: "Run strip",
        tags: ["GET /jobs", "condition_type", "condition_reason"],
        sentence: """
            One cell per run, oldest at the left, coloured by how the run ended. A hollow \
            dashed cell is a run whose record the sweeper removed: the schedule ticked and idios no \
            longer holds what happened, which is not the same as a run that succeeded. The \
            strip stands for the whole window whatever the chips say, because a picture that \
            changes with a filter is no longer a picture. Hovering a cell gives the condition, \
            the duration and the exit code, clicking one opens that run, and dragging across \
            several acknowledges their open incidents. Past two hundred runs the line folds \
            into one row per hour and one cell per minute, so a schedule that ticks every \
            minute still fits the pane.
            """),
    ExplainedRegion(
        id: "runCondition", title: "Condition column",
        tags: ["condition_type", "condition_reason", "failed counter"],
        sentence: """
            Whether the run succeeded comes from the condition the cluster wrote on it, never \
            from the counters: the failed counter counts pods, and a run that retried twice \
            and then succeeded would read as a failure. A run with nothing on it yet is still \
            going. Missed schedules are not detected, on purpose.
            """),
    ExplainedRegion(
        id: "runFold", title: "Folded runs",
        tags: ["condition_reason"],
        sentence: """
            Runs that follow one another and failed for the same reason are one line with the \
            rest counted behind it, so a schedule that has failed every tick for a day does \
            not fill the pane with one repeated sentence. Show opens them, and the times are \
            what say whether it is still happening.
            """),
]

private let workloadRolloutsRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "rolloutColumns", title: "Rollouts table",
        tags: ["rollout_history", "revision", "ready_replicas"],
        sentence: """
            One line per revision a Deployment has had, newest first: the revision number, the \
            ReplicaSet that carried it, its images, when it shipped, how many of its copies \
            came up ready, and how many incidents opened on it. This is the table that answers \
            whether the failures arrived with a deploy.
            """),
    ExplainedRegion(
        id: "rolloutCaveat", title: "What SHIPPED means",
        tags: ["rollout_history.created_at"],
        sentence: """
            The time is the ReplicaSet's own creation time, so it says when the revision \
            shipped and not when idios met it, which is what makes reading it against the \
            incident times worth anything. Reading them together is a correlation; idios does \
            not assert that the deploy caused the failure.
            """),
]

private let workloadIncidentsRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "workloadIncidents", title: "Incidents of this workload",
        tags: ["GET /incidents", "workload_kind", "workload_name", "pod_uid"],
        sentence: """
            Every incident idios opened on this workload, open and closed, in the same row the \
            triage list uses, so a badge means here what it means there and clicking a row \
            opens the pod page on the container that failed. Closed ones are kept: a workload \
            that failed and recovered twice this week is a different workload from one that \
            has failed once, and the tab's own count says how many of these are still open.
            """),
]

// The tag, the counts and the verdict are on screen on every tab of the Run
// page, so all four tables draw these values rather than four copies of the
// same words.
private let runTopRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "runStateTag", title: "Run outcome tag",
        tags: ["GET /jobs", "condition_type", "condition_reason", "jobs.deleted_at"],
        sentence: """
            Look here first. How a run ended is the condition the cluster wrote on the Job \
            and never its counters: a run that retried twice and then succeeded has a failed \
            counter above zero and still completed. A Job with no condition on it yet reads \
            RUNNING, a Job that is gone from the cluster reads DELETED, and NOT RECORDED means \
            the sweep removed the Job's own row and what is left here are the incidents.
            """),
    ExplainedRegion(
        id: "runCounts", title: "Counts line",
        tags: ["incidents.job_uid", "jobs.started_at", "jobs.finished_at"],
        sentence: """
            Attempts, then incidents and how many are still open, then how long the run \
            took. They are separate words because they count different things: an attempt is \
            one pod the Job started, and the Job giving up is an incident of its own, so two \
            attempts can carry three incidents.
            """),
    ExplainedRegion(
        id: "verdict", title: "Verdict block",
        tags: ["backoff_limit", "failed", "succeeded", "exit_code", "image_tag"],
        sentence: """
            Read this second, and often stop here: sentences built from fields the page \
            already holds. How the run ended and after how many attempts, the retry budget it \
            was given and what was counted against it, how the attempts exited, what they \
            captured, the image tag they ran, and whether earlier runs of the same schedule \
            failed the same way.
            """),
]

// The Attempts card and the Job's card are the Overview tab's own pane.
private let runOverviewRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "attemptsCard", title: "Attempts card",
        tags: ["pods.controller_uid", "incidents.job_uid", "exit_code", "failed"],
        sentence: """
            Read this third: one line per pod the Job started, oldest first, with the reason, \
            the exit code and the incident it opened; an attempt that opened none says how its \
            pod ended instead, and a last line closes the card where the Job gave up. The pod \
            cell opens that attempt's captured logs. The footer is the one thing the page \
            cannot draw: a pod pruned before idios saw it is counted by the Job and has no \
            line here.
            """),
    ExplainedRegion(
        id: "jobCard", title: "What the Job reports",
        tags: ["condition_message", "completions", "parallelism", "active_deadline_seconds"],
        sentence: """
            Read this fourth: the Job's own row as stored, in its own words. The condition \
            message often names what the controller objected to, the counters say what the run \
            was told to do and how far it got, and the deadline is the one a DeadlineExceeded \
            run exceeded. A run whose row the sweep removed says so here rather than drawing \
            zeroes.
            """),
]

private let runTimelineRegion = ExplainedRegion(
    id: "runTimeline", title: "Timeline of the lead incident",
    tags: ["GET /incidents/{id}/timeline", "container_state_history", "k8s_events"],
    sentence: """
        The transitions and events of the incident the header's actions act on, oldest \
        first, with repeated crash cycles folded into one entry that counts them. It is one \
        attempt's window and not the whole run's: the way to another attempt's is its pod, \
        from the Attempts card on Overview.
        """)

private let runLogsRegion = ExplainedRegion(
    id: "runLogs", title: "Captured logs of this run",
    tags: ["artifacts", "capture_gap", "capture_note"],
    sentence: """
        Every log idios captured for this run in one scrubber, so the attempts can be read \
        one after another and compared. A missing file is never an empty pane: the gap is \
        named and the API's own words are quoted, which separates an attempt that printed \
        nothing from a log that could never have been fetched.
        """)

private let runEventsRegion = ExplainedRegion(
    id: "runEvents", title: "Events of this run",
    tags: ["k8s_events", "involved_uid"],
    sentence: """
        The Job's own events and those of its attempts' pods together in one served-order \
        table with a container column. The Job's events are where the controller says why \
        it created another pod or stopped creating them, which no pod's own stream carries.
        """)

// Other runs is drawn only beside a run a CronJob created, because a
// standalone Job has no neighbours; it is declared on every tab whatever the
// run is, so the note exists for the run that has them.
private let runRailRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "otherRuns", title: "Other runs",
        tags: ["GET /jobs", "cronjob_name", "condition_reason"],
        sentence: """
            The five newest other runs of the same schedule, newest first, each with the \
            condition it ended on, and a link to the schedule's whole history when there are \
            more. It answers whether this run is the exception or the rule without leaving the \
            page.
            """),
]

// The Status screen is the one screen about idios rather than about a
// cluster, so the cluster scope above it narrows nothing here: every card
// counts what is stored.
private let statusRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "clustersHealth", title: "What each watcher is doing",
        tags: ["status.clusters", "ready", "last_event_at", "skew_seconds", "last_error"],
        sentence: """
            Read this second: one line per cluster idios watches, whether its watch is synced, \
            when an object last arrived, how far its clock is from this machine's, and the last \
            error it stored. The dot is coloured by one rule and one only: green while the \
            cluster is ready and carries no stored error, and otherwise not green, because a \
            list that has stopped filling is the one failure that makes every other number on \
            this screen a lie.
            """),
    ExplainedRegion(
        id: "processCards", title: "Process counters",
        tags: ["writer", "handlers", "capture", "closer"],
        sentence: """
            Read this third: what the four moving parts have done since the daemon started. \
            The writer's p99 is how long a transaction takes. The capture queue is bounded on \
            purpose, so queued is how much work is waiting and dropped is captures it refused \
            rather than fall behind: a dropped count above zero is why a log is missing. The \
            closer is the pass that ends incidents nothing is reporting any more.
            """),
    ExplainedRegion(
        id: "artifactOutcomes", title: "Artifacts by outcome",
        tags: ["artifacts", "capture_gap"],
        sentence: """
            What the captures produced: a bar per outcome, file being the ones that exist and \
            every other word a reason a log could not be read. Under them the attempts that \
            came back with a gap instead of a file, counted by the same word, which is what \
            says whether the misses are one cluster's permissions or the kubelet's.
            """),
    ExplainedRegion(
        id: "sweep", title: "Latest sweep",
        tags: ["sweep_runs", "cutoff", "rows_removed", "retention_days"],
        sentence: """
            Read this fourth: the newest retention sweep of every table it touches, with the \
            cutoff it cut at and what it removed. This is why a row goes away: idios keeps \
            what is newer than the retention and deletes the rest, so a run or a pod that is \
            no longer on a page was not lost, it aged out.
            """),
]

// The three container tabs differ in what their pane draws and in nothing
// else, so a tab's table is the shared regions plus what its own pane holds.
private let containerOverviewRegions: [ExplainedRegion] =
    podPageTopRegions + [podVerdictRegion, kubeletFactsRegion, capturedLogsRegion]
    + podPageRailRegions

private let containerLogsRegions: [ExplainedRegion] =
    podPageTopRegions + [podVerdictRegion, capturedLogsRegion] + podPageRailRegions

private let containerTimelineRegions: [ExplainedRegion] =
    podPageTopRegions + [podVerdictRegion] + podPageRailRegions

private func podCardRegions(_ pane: PodPane) -> [ExplainedRegion] {
    let tab =
        switch pane {
        case .events: podEventsRegion
        case .conditions: podConditionsRegion
        case .files: podFilesRegion
        case .podJSON: podJSONRegion
        case .related: relatedIncidentsRegion
        }
    return podPageTopRegions + [tab] + podPageRailRegions
}

private func runTabRegions(_ content: ExplainedRegion) -> [ExplainedRegion] {
    runTopRegions + [content] + runRailRegions
}

/// explainedRegions is the parts of a screen that carry a help mark, in the
/// order they are read: the state of the thing, then the pane, then the rail.
public func explainedRegions(for screen: ExplainedScreen) -> [ExplainedRegion] {
    switch screen {
    case .container(.overview): containerOverviewRegions
    case .container(.logs): containerLogsRegions
    case .container(.timeline): containerTimelineRegions
    case .podCard(let pane): podCardRegions(pane)
    case .incidents(.rows): listRegions
    case .incidents(.empty): listEmptyScreenRegions
    case .palette: paletteRegions
    case .workloads(.tree): workloadsTreeRegions
    case .workloads(.overview): workloadsTreeRegions + workloadOverviewRegions
    case .workloads(.pods): workloadsTreeRegions + workloadPodsRegions
    case .workloads(.runs): workloadsTreeRegions + workloadRunsRegions
    case .workloads(.rollouts): workloadsTreeRegions + workloadRolloutsRegions
    case .workloads(.incidents): workloadsTreeRegions + workloadIncidentsRegions
    case .run(.overview): runTopRegions + runOverviewRegions + runRailRegions
    case .run(.timeline): runTabRegions(runTimelineRegion)
    case .run(.logs): runTabRegions(runLogsRegion)
    case .run(.events): runTabRegions(runEventsRegion)
    case .status: statusRegions
    }
}

/// ScreenAudit is what a screen's table must satisfy: it says how many parts
/// carry a mark, that no id repeats, and that no sentence or title is
/// missing.
public struct ScreenAudit: Hashable, Sendable {
    /// regions is how many parts of the screen have something to say.
    public let regions: Int
    /// uniqueIDs is false where two regions claim one id, which would leave
    /// one of them undrawn.
    public let uniqueIDs: Bool
    /// everyRegionSpeaks is false where a region carries no name or no
    /// sentence, which is a mark that opens on nothing rather than a short
    /// note.
    public let everyRegionSpeaks: Bool
}

/// audit reads a screen's table into the values a test compares.
public func audit(_ screen: ExplainedScreen) -> ScreenAudit {
    let regions = explainedRegions(for: screen)
    let ids = Set(regions.map(\.id))
    return ScreenAudit(
        regions: regions.count,
        uniqueIDs: ids.count == regions.count,
        everyRegionSpeaks: regions.allSatisfy { !$0.title.isEmpty && !$0.sentence.isEmpty })
}
