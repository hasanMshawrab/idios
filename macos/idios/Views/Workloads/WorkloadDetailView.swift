import IdiosModel
import SwiftUI

/// WorkloadTab is one pane of the workload detail. Every list that grows with
/// the cluster lives behind one, so the bound the tab states belongs to
/// something a person chose to open.
enum WorkloadTab: Hashable, Sendable {
    case overview
    case pods
    case rollouts
    case runs
    case incidents

    /// part is the tab as the screen's explanation names it, because every tab
    /// draws its own regions and shares none of them.
    var part: WorkloadsPart {
        switch self {
        case .overview: .overview
        case .pods: .pods
        case .rollouts: .rollouts
        case .runs: .runs
        case .incidents: .incidents
        }
    }
}

/// workloadTabs is which tabs one workload offers: the kind decides them.
func workloadTabs(kind: String, hasRollouts: Bool) -> [WorkloadTab] {
    if kind == "CronJob" { return [.overview, .runs, .pods, .incidents] }
    // Only a Deployment's ReplicaSets fill rollout_history, so another kind
    // with no row has no rollout to wait for.
    let rollouts: [WorkloadTab] = !hasRollouts && kind != "Deployment" ? [] : [.rollouts]
    return [.overview, .pods] + rollouts + [.incidents]
}

/// selectedWorkloadTab is the tab in view: the one a person chose while this
/// workload offers it, and otherwise the one its kind opens on. A CronJob's
/// runs are its signal and its pods are transient, so it opens on Runs; every
/// other kind opens on the summary.
func selectedWorkloadTab(_ chosen: WorkloadTab?, kind: String, hasRollouts: Bool)
    -> WorkloadTab
{
    let tabs = workloadTabs(kind: kind, hasRollouts: hasRollouts)
    if let chosen, tabs.contains(chosen) { return chosen }
    let opening: WorkloadTab = kind == "CronJob" ? .runs : .overview
    return tabs.contains(opening) ? opening : tabs[0]
}

/// WorkloadPodFilter is one chip over the Pods tab. Live is the request's own
/// pods_live; the other two are read off the page the daemon returned, which
/// has no filter for either.
enum WorkloadPodFilter: CaseIterable, Hashable, Sendable {
    case all
    case live
    case notRunning
    case openIncident

    /// title is the chip's word.
    var title: String {
        switch self {
        case .all: "All"
        case .live: "Live"
        case .notRunning: "Not running"
        case .openIncident: "Open incident"
        }
    }

    /// help says which stored fact the chip stands for.
    var help: String {
        switch self {
        case .all: "every pod on the page, live and deleted"
        case .live: "pods_live=true: pods idios has not seen deleted"
        case .notRunning: "worst container state is waiting or terminated"
        case .openIncident: "pods carrying at least one open incident"
        }
    }

    /// keeps decides one row of the returned page; the live chip is served by
    /// the daemon and keeps everything it sent back.
    func keeps(_ pod: PodRow) -> Bool {
        switch self {
        case .all, .live: true
        case .notRunning: pod.worstState != .running
        case .openIncident: pod.openIncidents > 0
        }
    }
}

/// WorkloadRunFilter is one chip over the Runs tab; both map to a flag on the
/// request, so the page and its counts come back already filtered.
enum WorkloadRunFilter: CaseIterable, Hashable, Sendable {
    case all
    case live
    case failed

    /// title is the chip's word.
    var title: String {
        switch self {
        case .all: "All"
        case .live: "Live"
        case .failed: "Failed"
        }
    }

    /// help says which stored fact the chip stands for.
    var help: String {
        switch self {
        case .all: "every run idios has kept"
        case .live: "live=true: runs still in the cluster"
        case .failed: "failed=true: runs whose condition_type is Failed"
        }
    }
}

/// WorkloadWindow is how far back the Overview's cards and chart look.
enum WorkloadWindow: String, CaseIterable, Hashable, Sendable {
    case all, day, sixHours

    /// title is the control's label.
    var title: String {
        switch self {
        case .all: "3d"
        case .day: "24h"
        case .sixHours: "6h"
        }
    }

    /// hours is the cut, nil for the daemon's whole window.
    var hours: Int? {
        switch self {
        case .all: nil
        case .day: 24
        case .sixHours: 6
        }
    }
}

/// defaultRunFilter opens a CronJob on the runs that failed, which is what a
/// person came to read, and on everything when none of them did.
func defaultRunFilter(_ page: Page<Job>?) -> WorkloadRunFilter {
    (page?.failedTotal ?? 0) > 0 ? .failed : .all
}

/// WorkloadDetailView is one workload's window: what failed, on which revision
/// or run, when the restarts happened and which pods carried them.
struct WorkloadDetailView: View {
    let detail: WorkloadDetail
    let runs: Page<Job>?
    let strip: Page<Job>?
    let incidents: [Incident]?
    let clusterName: String
    @Binding var tab: WorkloadTab?
    @Binding var podFilter: WorkloadPodFilter?
    @Binding var runFilter: WorkloadRunFilter?
    let openPod: (String) -> Void
    let openIncident: (String) -> Void
    let openRun: (String) -> Void
    let acknowledge: ([String]) -> Void

    @State private var window: WorkloadWindow = .all

    private var workload: Workload { detail.workload }

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            tabStrip
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    tabContent
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }

    // The pane carries no scope pill of its own, so the cluster is named here.
    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(workload.workloadName)
                .font(.system(size: 15, weight: .semibold))
                .textSelection(.enabled)
            Text("\(workload.workloadKind) - \(clusterName) / " + workload.namespace)
                .font(.system(size: 11.5, design: .monospaced))
                .foregroundStyle(.secondary)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 16)
        .frame(height: 44)
    }

    private var tabs: [WorkloadTab] {
        workloadTabs(kind: workload.workloadKind, hasRollouts: !detail.rollouts.isEmpty)
    }

    private var selectedTab: WorkloadTab {
        selectedWorkloadTab(
            tab, kind: workload.workloadKind, hasRollouts: !detail.rollouts.isEmpty)
    }

    private var tabStrip: some View {
        HStack(spacing: 2) {
            ForEach(tabs, id: \.self) { item in
                tabButton(title: title(of: item), selected: item == selectedTab) { tab = item }
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 14)
        .frame(height: 38)
    }

    private func tabButton(title: String, selected: Bool, action: @escaping () -> Void)
        -> some View
    {
        Button(action: action) {
            Text(title)
                .font(.system(size: 12.5, weight: selected ? .semibold : .regular))
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(selected ? AnyShapeStyle(.quaternary) : AnyShapeStyle(.clear)))
        }
        .buttonStyle(.plain)
    }

    private func title(of tab: WorkloadTab) -> String {
        switch tab {
        case .overview: "Overview"
        case .pods: "Pods \(detail.pods.count)"
        case .rollouts: "Rollouts \(detail.rollouts.count)"
        case .runs: runs.map { "Runs \($0.total), \($0.failedTotal) failed" } ?? "Runs"
        case .incidents: incidents.map { "Incidents \($0.count), \(openCount($0)) open" }
            ?? "Incidents"
        }
    }

    @ViewBuilder private var tabContent: some View {
        switch selectedTab {
        case .overview:
            summary
            RestartsCard(buckets: windowBuckets).explained("restartsChart")
        case .pods:
            PodsTab(
                detail: detail, kind: workload.workloadKind, name: workload.workloadName,
                filter: $podFilter, openPod: openPod)
        case .rollouts:
            RolloutsCard(
                rollouts: detail.rollouts, uncontrolled: workload.workloadName.isEmpty)
                // The fold is a reading position on one workload's history, not
                // a preference that should follow the selection to the next.
                .id(workload.id)
        case .runs:
            RunsTab(
                page: runs, strip: strip, incidents: incidents, name: workload.workloadName,
                filter: $runFilter, openPod: openPod, openIncident: openIncident,
                openRun: openRun, acknowledge: acknowledge)
        case .incidents:
            IncidentsTab(
                incidents: incidents, clusterName: clusterName, open: openIncident)
        }
    }

    private var summary: some View {
        HStack(alignment: .top, spacing: 12) {
            // The four cards are one region, so they are a row of their
            // own: the window control beside them is not part of what the
            // card's note explains.
            HStack(alignment: .top, spacing: 12) {
                SummaryCard(label: "Incidents in window") {
                    Text(String(counts.total))
                        .font(.system(size: 24, weight: .semibold))
                        .monospacedDigit()
                    Text("\(counts.open) open - \(counts.closed) closed")
                        .font(.system(size: 10.5))
                        .foregroundStyle(.tertiary)
                    windowLine
                }
                SummaryCard(label: "By category") {
                    Flow(spacing: 5, lineSpacing: 4) {
                        ForEach(counts.categories, id: \.0) { category, count in
                            Badge(text: "\(category.label) \(count)", style: category.badge)
                                .help(category.rawValue)
                        }
                    }
                    .padding(.top, 4)
                    windowLine
                }
                SummaryCard(label: "Occurrences (restarts attached)") {
                    Text(String(counts.occurrences))
                        .font(.system(size: 24, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(BadgeStyle.red.text)
                    Text(counts.podLine)
                        .font(.system(size: 10.5))
                        .foregroundStyle(.tertiary)
                    windowLine
                }
                SummaryCard(label: "Image tags at open") {
                    Flow(spacing: 5, lineSpacing: 4) {
                        ForEach(counts.tags, id: \.0) { tag, count in
                            Badge(text: "\(tag ?? "digest") x\(count)", style: .neutral)
                        }
                    }
                    .padding(.top, 4)
                    windowLine
                }
            }
            .explained("statCards")
            Picker("", selection: $window) {
                ForEach(WorkloadWindow.allCases, id: \.self) { choice in
                    Text(choice.title).tag(choice)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(width: 132)
            .help("how far back the cards and the chart look")
        }
    }

    @ViewBuilder private var windowLine: some View {
        if let hours = window.hours {
            Text("last \(hours)h")
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
        }
    }

    // The daemon's aggregates stand for its own window; a narrower one is
    // counted from the incident rows the store already holds for this workload.
    private var counts: WindowCounts {
        guard let hours = window.hours, let rows = incidents else {
            return WindowCounts(workload: workload)
        }
        // An incident belongs to the narrower window when it was still being
        // seen inside it: opened_at alone drops the failure that is happening
        // now and started before the cut.
        let cut = Date().addingTimeInterval(-Double(hours) * 3600)
        return WindowCounts(incidents: rows.filter { ($0.lastSeenAt.date ?? .distantPast) >= cut })
    }

    private var windowBuckets: [HourBucket] {
        guard let hours = window.hours else { return detail.restartsByHour }
        let cut = Date().addingTimeInterval(-Double(hours) * 3600)
        return detail.restartsByHour.filter { ($0.hour.date ?? .distantPast) >= cut }
    }
}

/// WindowCounts is what the Overview's four cards stand for, whether they are
/// the daemon's aggregates or the incident rows counted under a shorter window.
private struct WindowCounts {
    let total: Int32
    let open: Int32
    let categories: [(IdiosModel.Category, Int32)]
    let occurrences: Int32
    let pods: Int
    let tags: [(String?, Int32)]

    var closed: Int32 { max(total - open, 0) }
    var podLine: String { "across \(plural(pods, "pod"))" }

    init(workload: Workload) {
        self.total = workload.incidentsByCategory.values.reduce(0, +)
        self.open = workload.openIncidents
        self.categories = sortedCounts(workload.incidentsByCategory)
        self.occurrences = workload.occurrences
        self.pods = Int(workload.livePods + workload.deletedPods)
        self.tags = workload.imageTags.map { ($0.tag, $0.count) }
    }

    init(incidents: [Incident]) {
        var byCategory: [IdiosModel.Category: Int32] = [:]
        var byTag: [String?: Int32] = [:]
        var pods: Set<String> = []
        for row in incidents {
            byCategory[row.category, default: 0] += 1
            byTag[row.imageTag, default: 0] += 1
            if let uid = row.podUID { pods.insert(uid) }
        }
        self.total = Int32(incidents.count)
        self.open = Int32(incidents.filter { $0.closedAt == nil }.count)
        self.categories = sortedCounts(byCategory)
        // An incident's occurrences are its whole life, so a window says which
        // incidents it holds rather than trimming what each one counted.
        self.occurrences = incidents.reduce(0) { $0 + $1.occurrences }
        self.pods = pods.count
        self.tags = byTag.map { ($0.key, $0.value) }.sorted { ($1.1, $0.0 ?? "") < ($0.1, $1.0 ?? "") }
    }
}

private func sortedCounts(_ byCategory: [IdiosModel.Category: Int32])
    -> [(IdiosModel.Category, Int32)]
{
    byCategory.map { ($0.key, $0.value) }
        .sorted { ($1.1, $0.0.rawValue) < ($0.1, $1.0.rawValue) }
}

// An incident that has not closed is open, the same fact the daemon's open list
// keys on.
private func openCount(_ incidents: [Incident]) -> Int {
    incidents.filter { $0.closedAt == nil }.count
}

/// FilterChip is one word over a bounded list, engaged or not.
struct FilterChip: View {
    let title: String
    let selected: Bool
    let help: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: 10.5, weight: .semibold))
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .foregroundStyle(selected ? Color.white : .secondary)
                .background(
                    Capsule().fill(
                        selected
                            ? AnyShapeStyle(Color.accentColor)
                            : AnyShapeStyle(.quaternary.opacity(0.6))))
        }
        .buttonStyle(.plain)
        .help(help)
    }
}

/// SummaryCard is one of the four boxes of the Overview tab: a label and
/// whatever counts under it.
struct SummaryCard<Content: View>: View {
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.system(size: 10.5))
                .foregroundStyle(.tertiary)
                .lineLimit(1)
            content
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, minHeight: 84, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 9).fill(Color(nsColor: .textBackgroundColor)))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(.quaternary, lineWidth: 0.5))
    }
}

/// RolloutsCard is the ReplicaSets of a Deployment with the incidents each one
/// carried, the newest five open and the rest folded behind their count.
struct RolloutsCard: View {
    let rollouts: [Rollout]
    let uncontrolled: Bool

    /// shown is how many revisions stand open: a Deployment keeps ten plus the
    /// live one, and the newest are the ones a person is reading about.
    static let shown = 5

    @State private var older = false

    var body: some View {
        DetailCard(title: "Rollouts", meta: "rollout_history WHERE deployment_uid = ?") {
            if uncontrolled {
                Text(
                    "Pods without a controller share nothing but the namespace; idios keeps no "
                        + "rollouts or restart history for them.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            } else if rollouts.isEmpty {
                Text("none recorded")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading.explained("rolloutColumns")
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(newest, id: \.replicasetUID) { rollout in
                        Divider()
                        row(rollout)
                    }
                }
                folded
                Text(
                    "SHIPPED is the ReplicaSet's own creation time, so it says when a revision "
                        + "shipped rather than when idios met it. Reading it against when the "
                        + "incidents opened is a correlation; idios does not assert a cause.")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.tertiary)
                    .fixedSize(horizontal: false, vertical: true)
                    .explained("rolloutCaveat")
            }
        }
    }

    // rollout_history is served revision-descending, so the newest revisions are
    // already the head of the list.
    private var newest: [Rollout] { Array(rollouts.prefix(Self.shown)) }
    private var rest: [Rollout] { Array(rollouts.dropFirst(Self.shown)) }

    @ViewBuilder private var folded: some View {
        if !rest.isEmpty {
            DisclosureGroup(isExpanded: $older) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rest, id: \.replicasetUID) { rollout in
                        Divider()
                        row(rollout)
                    }
                }
            } label: {
                Text("\(rest.count) older revision\(rest.count == 1 ? "" : "s")")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var heading: some View {
        HStack(spacing: 7) {
            Text("REV").frame(width: 40, alignment: .trailing)
            Text("REPLICASET").frame(width: 200, alignment: .leading)
            Text("IMAGES").frame(maxWidth: .infinity, alignment: .leading)
            Text("SHIPPED").frame(width: 74, alignment: .leading)
            Text("READY").frame(width: 52, alignment: .leading)
            Text("INCIDENTS").frame(width: 62, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ rollout: Rollout) -> some View {
        HStack(alignment: .top, spacing: 7) {
            Text(rollout.revision ?? "-")
                .font(.system(size: 11.5))
                .monospacedDigit()
                .frame(width: 40, alignment: .trailing)
            ExpandableText(value: rollout.replicasetName)
                .font(.system(size: 11, design: .monospaced))
                .frame(width: 200, alignment: .leading)
            ExpandableText(value: rollout.images.joined(separator: ", "))
                .font(.system(size: 11, design: .monospaced))
                .frame(maxWidth: .infinity, alignment: .leading)
            Text(clockTime(rollout.createdAt, seconds: true))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .help("rollout_history.created_at \(rollout.createdAt.raw)")
                .frame(width: 74, alignment: .leading)
            Text(ready(rollout))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 52, alignment: .leading)
            Badge(
                text: String(rollout.incidents),
                style: rollout.incidents > 0 ? IdiosModel.Category.crash.badge : .neutral)
                .frame(width: 62, alignment: .leading)
        }
        .padding(.vertical, 5)
        .opacity(rollout.deletedAt == nil ? 1 : 0.65)
    }

    // A revision recorded before the counts were on the wire has none to show,
    // and a made-up zero would read as a revision that never came up.
    private func ready(_ rollout: Rollout) -> String {
        guard let replicas = rollout.replicas, let ready = rollout.readyReplicas else { return "" }
        return "\(ready)/\(replicas)"
    }
}

/// RestartsCard is the restarts of the workload hour by hour over the window.
struct RestartsCard: View {
    let buckets: [HourBucket]

    var body: some View {
        DetailCard(title: "Restarts per hour", meta: "container_state_history") {
            if buckets.isEmpty {
                Text("none recorded")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                chart
                HStack {
                    Text(clockTime(hours[0].hour))
                    Spacer(minLength: 0)
                    Text(clockTime(hours[hours.count - 1].hour))
                }
                .padding(.leading, Self.axisWidth)
                .font(.system(size: 10))
                .monospacedDigit()
                .foregroundStyle(.tertiary)
                Text(
                    "Hollow bars mark hours containing gap_reconstructed rows. Nothing before "
                        + "the window exists to show.")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.tertiary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    /// axisWidth is the leading column the three y labels sit in.
    static let axisWidth: CGFloat = 28

    private var chart: some View {
        HStack(alignment: .bottom, spacing: 0) {
            axis
            Rectangle()
                .fill(.quaternary)
                .frame(width: 0.5, height: 90)
            HStack(alignment: .bottom, spacing: 2) {
                ForEach(hours) { hour in
                    bar(hour)
                }
            }
            .padding(.leading, 3)
        }
        .frame(height: 90, alignment: .bottom)
    }

    private var axis: some View {
        VStack(alignment: .trailing, spacing: 0) {
            Text(String(peak))
            Spacer(minLength: 0)
            Text(String(peak / 2))
            Spacer(minLength: 0)
            Text("0")
        }
        .font(.system(size: 9))
        .monospacedDigit()
        .foregroundStyle(.tertiary)
        .frame(width: Self.axisWidth - 4, height: 90, alignment: .trailing)
        .padding(.trailing, 4)
    }

    @ViewBuilder private func bar(_ hour: ChartHour) -> some View {
        let height = max(90 * Double(hour.restarts) / Double(peak), hour.restarts > 0 ? 2 : 1)
        let help = "\(clockTime(hour.hour)) - \(plural(Int(hour.restarts), "restart"))"
        if hour.reconstructed {
            RoundedRectangle(cornerRadius: 1)
                .strokeBorder(
                    BadgeStyle.orange.text, style: StrokeStyle(lineWidth: 1, dash: [2, 2]))
                .frame(maxWidth: .infinity)
                .frame(height: max(height, 6))
                .help(help)
        } else {
            RoundedRectangle(cornerRadius: 1)
                .fill(
                    hour.restarts > 0
                        ? AnyShapeStyle(BadgeStyle.red.text) : AnyShapeStyle(.quaternary))
                .frame(maxWidth: .infinity)
                .frame(height: height)
                .help(help)
        }
    }

    // Every hour between the first and the last is a bar, because a chart that
    // skips the quiet hours draws a busy stretch where there was none.
    private var hours: [ChartHour] {
        let sorted = buckets.sorted { $0.hour.raw < $1.hour.raw }
        guard let start = sorted.first?.hour.date, let end = sorted.last?.hour.date else {
            return []
        }
        var recorded: [String: HourBucket] = [:]
        for bucket in sorted { recorded[String(bucket.hour.raw.prefix(13))] = bucket }
        var rows: [ChartHour] = []
        var at = start
        while at <= end {
            let stamp = Timestamp(hourLabel(at))
            let bucket = recorded[String(stamp.raw.prefix(13))]
            rows.append(
                ChartHour(
                    hour: stamp, restarts: bucket?.restarts ?? 0,
                    reconstructed: bucket?.reconstructed ?? false))
            at = at.addingTimeInterval(3600)
        }
        return rows
    }

    private var peak: Int32 {
        max(buckets.map(\.restarts).max() ?? 0, 1)
    }
}

/// ChartHour is one bar of the restarts chart: an hour of the window and what
/// was recorded in it, zero for an hour the daemon sent no row for.
private struct ChartHour: Identifiable {
    let hour: Timestamp
    let restarts: Int32
    let reconstructed: Bool

    var id: String { hour.raw }
}

private let utcCalendar: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
    return calendar
}()

private func hourLabel(_ date: Date) -> String {
    let parts = utcCalendar.dateComponents([.year, .month, .day, .hour], from: date)
    return String(
        format: "%04d-%02d-%02dT%02d:00:00Z", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0,
        parts.hour ?? 0)
}

/// PodsTab is the bounded page of the workload's pods, with the chips that
/// narrow it and the line that says what the page stands for.
struct PodsTab: View {
    let detail: WorkloadDetail
    let kind: String
    let name: String
    @Binding var filter: WorkloadPodFilter?
    let openPod: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            chips
            PodsCard(pods: rows, kind: kind, name: name, openPod: openPod)
        }
    }

    // A workload's pods are read to see what is running now, so the tab opens
    // on the live ones and the daemon serves that page.
    private var defaultFilter: WorkloadPodFilter { .live }

    private var selectedFilter: WorkloadPodFilter { filter ?? defaultFilter }

    // The chips and the line are two regions of the screen's explanation, so
    // the chips are a row of their own: one mark each says what each of them
    // narrows.
    private var chips: some View {
        HStack(spacing: 6) {
            HStack(spacing: 6) {
                ForEach(WorkloadPodFilter.allCases, id: \.self) { chip in
                    FilterChip(
                        title: chip.title, selected: selectedFilter == chip, help: chip.help
                    ) {
                        filter = chip
                    }
                }
            }
            .explained("podChips")
            Spacer(minLength: 12)
            Text(line)
                .font(.system(size: 10.5))
                .foregroundStyle(.tertiary)
                .explained("podsScope")
        }
    }

    private var rows: [PodRow] {
        detail.pods.filter(selectedFilter.keeps)
    }

    // The live chip is served by the daemon, so the page it returned is already
    // the answer and only the other chips count against it.
    private var line: String {
        let shown = detail.pods.count
        let total = Int(
            selectedFilter == .live
                ? detail.workload.livePods
                : detail.workload.livePods + detail.workload.deletedPods)
        let scope = detail.podsTruncated ? "the newest \(shown)" : "\(total)"
        if selectedFilter != .live, selectedFilter != .all {
            return "\(selectedFilter.title.lowercased()) - \(rows.count) of \(scope)"
        }
        if detail.podsTruncated { return "showing the newest \(shown) of \(total)" }
        return "\(total) pod\(total == 1 ? "" : "s")"
    }
}

/// PodsCard is the page of pods the daemon returned for this workload.
struct PodsCard: View {
    let pods: [PodRow]
    let kind: String
    let name: String
    let openPod: (String) -> Void

    var body: some View {
        DetailCard(
            title: "Pods of this workload",
            meta: "pods WHERE workload_kind = '\(kind)' AND workload_name = '\(name)'")
        {
            if pods.isEmpty {
                Text("No pod of this workload matches.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(pods) { pod in
                        Divider()
                        Button { openPod(pod.uid) } label: { row(pod) }
                            .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private var heading: some View {
        HStack(spacing: 7) {
            Text("POD").frame(maxWidth: .infinity, alignment: .leading)
            Text("STATE").frame(width: 150, alignment: .leading)
            Text("OPEN INC.").frame(width: 60, alignment: .trailing)
            Text("NODE").frame(width: 110, alignment: .leading)
            Text("CREATED").frame(width: 90, alignment: .leading)
            Text("DELETED").frame(width: 170, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ pod: PodRow) -> some View {
        HStack(alignment: .top, spacing: 7) {
            ExpandableText(value: pod.name)
                .font(.system(size: 11.5, design: .monospaced))
                .frame(maxWidth: .infinity, alignment: .leading)
            state(pod).frame(width: 150, alignment: .leading)
            Text(String(pod.openIncidents))
                .font(.system(size: 11.5))
                .monospacedDigit()
                .foregroundStyle(
                    pod.openIncidents > 0
                        ? AnyShapeStyle(BadgeStyle.red.text) : AnyShapeStyle(.secondary))
                .frame(width: 60, alignment: .trailing)
            Text(pod.nodeName ?? "-")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .frame(width: 110, alignment: .leading)
            Text(clockTime(pod.createdAt, seconds: true))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 90, alignment: .leading)
            deletion(pod).frame(width: 170, alignment: .leading)
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
        .opacity(pod.deletedAt == nil ? 1 : 0.65)
    }

    @ViewBuilder private func state(_ pod: PodRow) -> some View {
        if pod.deletedAt != nil {
            Badge(text: "deleted", style: IncidentState.podDeleted.badge)
        } else if let worst = pod.worstState {
            Badge(text: worst.rawValue, style: podBadge(worstState: worst, phase: pod.phase))
        } else {
            Text("-").font(.system(size: 11.5)).foregroundStyle(.secondary)
        }
    }

    // deletion_reason is idios's reading of why the pod went, never the API
    // server's word, so it is labelled every time it is shown.
    @ViewBuilder private func deletion(_ pod: PodRow) -> some View {
        if let deletedAt = pod.deletedAt {
            HStack(spacing: 4) {
                Text(
                    "\(clockTime(deletedAt, seconds: true)) - "
                        + (pod.deletionReason?.label ?? "unknown"))
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .help(
                        "deleted_at \(deletedAt.raw), deletion_reason "
                            + (pod.deletionReason?.rawValue ?? "unknown"))
                Text("(inferred)").font(.system(size: 10)).foregroundStyle(.tertiary)
            }
        } else {
            Text("-").font(.system(size: 11)).foregroundStyle(.secondary)
        }
    }
}

/// RunsTab is a CronJob's runs: what the page holds, what it stands for, and
/// the chips the daemon filters it by.
struct RunsTab: View {
    let page: Page<Job>?
    let strip: Page<Job>?
    let incidents: [Incident]?
    let name: String
    @Binding var filter: WorkloadRunFilter?
    let openPod: (String) -> Void
    let openIncident: (String) -> Void
    let openRun: (String) -> Void
    let acknowledge: ([String]) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // The strip and the matrix that stands in for it past its limit
            // are one region: a line of identical cells says nothing cell by
            // cell, and the note says which picture is drawn.
            runStrip.explained("runStrip")
            chips
            JobsCard(
                rows: runTableRows(tableCells), name: name, openPod: openPod,
                openIncident: openIncident, openRun: openRun)
        }
    }

    // The strip stands for the whole window the store holds, never for the
    // chips: a picture that changes with a filter is no longer a picture.
    @ViewBuilder private var runStrip: some View {
        let cells = stripCells
        if cells.count <= runStripLimit {
            RunStripView(
                cells: cells, now: Date(), openRun: openRun, acknowledge: acknowledge)
        } else {
            RunMatrixView(matrix: runMatrix(cells), now: Date(), openRun: openRun)
        }
    }

    private var stripCells: [RunCell] {
        runCells(jobs: strip?.rows ?? [], incidents: incidents ?? [], cronjobName: name)
    }

    private var tableCells: [RunCell] {
        let rows = page?.rows ?? []
        return runCells(jobs: rows, incidents: tableIncidents(rows), cronjobName: name)
    }

    // A run the sweeper cut is still a run under All, but a chip is a predicate
    // the daemon applied to the jobs, and a run it did not return has not been
    // shown to match it.
    private func tableIncidents(_ rows: [Job]) -> [Incident] {
        let all = incidents ?? []
        guard selectedFilter != .all else { return all }
        let kept = Set(rows.map(\.uid))
        return all.filter { $0.jobUID.map(kept.contains) ?? false }
    }

    private var selectedFilter: WorkloadRunFilter { filter ?? defaultRunFilter(strip ?? page) }

    private var chips: some View {
        HStack(spacing: 6) {
            HStack(spacing: 6) {
                ForEach(WorkloadRunFilter.allCases, id: \.self) { chip in
                    FilterChip(
                        title: chip.title, selected: selectedFilter == chip, help: chip.help
                    ) {
                        filter = chip
                    }
                }
            }
            Spacer(minLength: 12)
            Text(line)
                .font(.system(size: 10.5))
                .foregroundStyle(.tertiary)
        }
    }

    // total and failed_total are counted over the filter with live and failed
    // ignored, so they say how many runs the page stands for whatever is chosen.
    private var line: String {
        guard let page else { return "" }
        let runs = "\(page.total) run\(page.total == 1 ? "" : "s"), \(page.failedTotal) failed"
        return page.truncated ? runs + " - showing the newest \(page.rows.count)" : runs
    }
}

/// JobsCard is the runs a CronJob started, with success read from the condition
/// rather than from the counters.
struct JobsCard: View {
    let rows: [RunTableRow]
    let name: String
    let openPod: (String) -> Void
    let openIncident: (String) -> Void
    let openRun: (String) -> Void

    @State private var expandedFolds: Set<String> = []

    var body: some View {
        DetailCard(title: "Runs", meta: "jobs WHERE cronjob_name = '\(name)'") {
            if rows.isEmpty {
                Text("idios has kept no run matching this.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rows) { row in
                        Divider()
                        runRow(row.run)
                        fold(row)
                            .explained("runFold", when: row.id == firstFold)
                    }
                }
            }
            Text(
                "Success and failure come from condition_type and condition_reason; the failed "
                    + "counter is context only, because it counts pods, not attempts. Missed "
                    + "schedules are not detected, on purpose.")
                .font(.system(size: 10.5))
                .foregroundStyle(.tertiary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // The first row that folds anything, which is the one the explanation
    // outlines: a table whose runs all failed differently folds nothing.
    private var firstFold: String? {
        rows.first { !$0.folded.isEmpty }?.id
    }

    private var heading: some View {
        HStack(spacing: 7) {
            Text("JOB").frame(width: 100, alignment: .leading)
            Text("CONDITION").frame(width: 78, alignment: .leading)
                .explained("runCondition")
            Text("STARTED").frame(width: 52, alignment: .leading)
            Text("DURATION").frame(width: 56, alignment: .leading)
            Text("POD").frame(width: 120, alignment: .leading)
            Text("REASON").frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    // Every row of the table is a run, kept pod or not, and the run's page is
    // what it opens. The row is a tap gesture rather than a button because the
    // POD cell and the incident link inside it are clicks of their own, and a
    // control inside a button's label never receives one.
    private func runRow(_ run: RunCell) -> some View {
        cells(run)
            .contentShape(Rectangle())
            .onTapGesture { openRun(run.jobUID) }
    }

    private func cells(_ run: RunCell) -> some View {
        HStack(alignment: .top, spacing: 7) {
            Text(run.name)
                .font(.system(size: 11, design: .monospaced))
                .lineLimit(1)
                .truncationMode(.middle)
                .help(fullName(run))
                .frame(width: 100, alignment: .leading)
            Badge(text: run.condition, style: runCellStyle(run.outcome))
                .frame(width: 78, alignment: .leading)
            Text(run.startedAt.map { clockTime($0) } ?? "-")
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 52, alignment: .leading)
            Text(duration(run))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 56, alignment: .leading)
            pod(run).frame(width: 120, alignment: .leading)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                ExpandableText(value: run.reason ?? "-")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                if let id = run.incidentIDs.first {
                    Button("#\(id)") { openIncident(id) }
                        .buttonStyle(.link)
                        .font(.system(size: 11))
                        .help("the incident this run opened")
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 5)
    }

    @ViewBuilder private func pod(_ run: RunCell) -> some View {
        if let uid = run.podUID {
            Button { openPod(uid) } label: {
                Text(run.podName ?? "pod")
                    .font(.system(size: 11, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .foregroundStyle(Color.accentColor)
            }
            .buttonStyle(.plain)
            .help(run.podName ?? "")
        } else {
            Text("pods pruned")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .help("idios has no kept pod for this run")
        }
    }

    @ViewBuilder private func fold(_ row: RunTableRow) -> some View {
        if !row.folded.isEmpty {
            if expandedFolds.contains(row.id) {
                ForEach(row.folded) { run in
                    Divider()
                    runRow(run)
                }
            } else {
                HStack(spacing: 6) {
                    Text("+ \(plural(row.folded.count, "older run")), same reason")
                        .font(.system(size: 10.5))
                        .foregroundStyle(.tertiary)
                    Button("show") { expandedFolds.insert(row.id) }
                        .buttonStyle(.link)
                        .font(.system(size: 10.5))
                }
                .padding(.vertical, 3)
            }
        }
    }

    // The cell carries the suffix, which is all a person reads a column of
    // runs by; the whole name is what they would grep the cluster for.
    private func fullName(_ run: RunCell) -> String {
        name.isEmpty || run.name.hasPrefix(name) ? run.name : "\(name)-\(run.name)"
    }

    private func duration(_ run: RunCell) -> String {
        guard let from = run.startedAt?.date, let to = run.finishedAt?.date else { return "-" }
        return durationText(from: from, to: to)
    }
}

/// IncidentsTab is the workload's incidents, open and closed, in the row the
/// triage list uses.
struct IncidentsTab: View {
    let incidents: [Incident]?
    let clusterName: String
    let open: (String) -> Void

    var body: some View {
        // The tab is one card, so the card is the region: one mark on it says
        // what every row under it is.
        DetailCard(
            title: heading,
            meta:
                "incidents WHERE workload_kind = ? AND workload_name = ?, "
                + "AND pod_uid = ? for the no-controller row")
        {
            if let incidents, !incidents.isEmpty {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(incidents) { incident in
                        Divider()
                        Button { open(incident.id) } label: {
                            IncidentRowView(
                                incident: incident, clusterName: clusterName,
                                selectedClusters: 1, now: Date())
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            } else if incidents != nil {
                Text("idios opened no incident on this workload.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                ProgressView().controlSize(.small)
            }
        }
        .explained("workloadIncidents")
    }

    private var heading: String {
        guard let incidents else { return "Incidents" }
        let noun = incidents.count == 1 ? "incident" : "incidents"
        return "\(incidents.count) \(noun), \(openCount(incidents)) open"
    }
}
