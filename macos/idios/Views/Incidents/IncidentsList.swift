import IdiosModel
import SwiftUI

/// ListExpansion is what a person opened or folded, by id, for as long as the
/// window lives and across a grouping change.
struct ListExpansion: Hashable {
    var collapsedGroups: Set<String> = []
    var expandedPods: Set<String> = []
    var expandedRollups: Set<String> = []
    var expandedRuns: Set<String> = []
}

/// BulkAction is one of the two closing actions a line that stands for several
/// rows asks about before it is carried out over every one of them.
private enum BulkAction {
    case dismiss, resolve
}

/// PendingBulkAction is the confirmation a closing action over several rows is
/// waiting on.
private struct PendingBulkAction {
    let rows: [Incident]
    let action: BulkAction

    var title: String {
        let count = incidentCount(rows.count)
        let workload = workloadTitle(rows[0])
        switch action {
        case .dismiss: return "Dismiss \(count) of \(workload)?"
        case .resolve: return "Mark \(count) of \(workload) resolved?"
        }
    }

    var verb: String {
        switch action {
        case .dismiss: "Dismiss"
        case .resolve: "Mark resolved"
        }
    }
}

/// PendingNote is the rows the note editor is open over; a set of rows shares
/// the one text a person writes.
private struct PendingNote: Identifiable {
    let id: String
    let rows: [Incident]

    init(rows: [Incident]) {
        self.id = rows.map(\.id).joined(separator: ",")
        self.rows = rows
    }
}

// Every count in the list is a count of incidents, and the noun agrees.
private func incidentCount(_ n: Int) -> String {
    "\(n) incident\(n == 1 ? "" : "s")"
}

/// FirstRows is the first line of each kind the list draws, which are the
/// lines its explanation marks: a List builds and recycles rows, so an
/// explanation on every row would follow the scroll position, and the note is
/// about the shape of a line rather than about one incident.
private struct FirstRows {
    var header: String?
    var run: String?
    var rollup: String?
    var pod: String?
}

/// IncidentsList draws the grouped rows the filter keeps.
struct IncidentsList: View {
    @Environment(ExplainState.self) private var explain

    let clusterGroups: [IncidentClusterGroup]
    let summary: ListSummary
    let cluster: (String) -> Cluster?
    let selectedClusters: Int
    let isLoading: Bool
    let hasRows: Bool
    let scopeIsEmpty: Bool
    let filterText: String
    let filter: IncidentFilter
    let density: Density
    @Binding var expansion: ListExpansion
    @Binding var selection: String?

    /// reveal is a group a menu bar row asked for; only a reveal scrolls the
    /// list, because a selection a person moved with the arrow keys is already
    /// where they are looking.
    let reveal: String?

    let open: (String) -> Void
    let openRun: (String) -> Void
    let acknowledge: (String) -> Void
    let dismiss: (String) -> Void
    let resolve: (String) -> Void
    let acknowledgeAll: () -> Void
    let note: ([String], String) -> Void
    let clearFilter: () -> Void
    let focusFilter: () -> Void
    let showAttention: () -> Void
    let openRuns: (Incident) -> Void
    let delete: (String) -> Void

    /// runLimit is how many runs of one group read as its recent history; the
    /// rest are the run history in Workloads.
    private static let runLimit = 5

    // Which row the pending confirmation is about; a delete cannot be undone,
    // so the key press only asks.
    @State private var pendingDelete: String?

    // Which set of rows has a confirmation pending. `a` is reversible and acts
    // at once; a dismiss or a manual close of twenty incidents in one
    // keystroke deserves the confirmation Command-Delete already has.
    @State private var pendingBulk: PendingBulkAction?

    // Acknowledging every row of the view is one keystroke over rows that are
    // not all on screen, so it asks first.
    @State private var confirmingAcknowledgeAll = false

    @State private var pendingNote: PendingNote?

    var body: some View {
        if clusterGroups.allSatisfy({ bucket in bucket.groups.allSatisfy { $0.rows.isEmpty } }) {
            empty
        } else {
            VStack(spacing: 0) {
                summaryLine.explained("summaryLine")
                ColumnHeaderRow()
                list
            }
        }
    }

    // What the whole view amounts to, above the columns: one problem is one
    // group, and every other number counts incidents.
    private var summaryLine: some View {
        HStack(spacing: 12) {
            Text("\(summary.problems) problem\(summary.problems == 1 ? "" : "s")")
                .font(.system(size: 12, weight: .semibold))
            Group {
                Text(
                    "\(summary.openIncidents) open "
                        + "incident\(summary.openIncidents == 1 ? "" : "s")")
                Text(
                    "\(summary.workloads) workload\(summary.workloads == 1 ? "" : "s"), "
                        + "\(summary.barePods) bare pod\(summary.barePods == 1 ? "" : "s")")
                if let newest = summary.newest {
                    Text("newest \(clockTime(newest))")
                        .help("newest last_seen_at \(clockTime(newest, seconds: true)) UTC")
                }
            }
            .font(.system(size: 11.5))
            .foregroundStyle(.secondary)
            Spacer(minLength: 12)
            Button(allCollapsed ? "Expand all" : "Collapse all") { toggleAll() }
                .help(allCollapsed ? "open every group" : "fold every group")
            Button("Acknowledge all") { confirmingAcknowledgeAll = true }
                .disabled(openUnacknowledged.isEmpty)
                .help("mark every open incident of this view seen")
        }
        .lineLimit(1)
        .buttonStyle(.link)
        .padding(.horizontal, 20)
        .padding(.top, 8)
        .padding(.bottom, 6)
        .alert(
            "Acknowledge \(incidentCount(openUnacknowledged.count))?",
            isPresented: $confirmingAcknowledgeAll)
        {
            Button("Acknowledge") { acknowledgeAll() }
            Button("Cancel", role: .cancel) {}
                .keyboardShortcut(.cancelAction)
        } message: {
            Text(
                "Every open incident of this view is marked seen. Each one is written on "
                    + "its own, so one that fails leaves the others done.")
        }
    }

    private var list: some View {
        ScrollViewReader { proxy in
            rows
                .onChange(of: reveal) { _, id in
                    guard let id else { return }
                    withAnimation { proxy.scrollTo(id, anchor: .center) }
                }
        }
    }

    // The lines the explanation marks, walked in the order they are drawn so
    // one pass over the model decides them for every builder below.
    private var firstRows: FirstRows {
        var first = FirstRows()
        for bucket in clusterGroups {
            if selectedClusters > 1, !bucket.title.isEmpty, first.header == nil {
                first.header = clusterHeaderGroup(bucket).id
            }
            for group in bucket.groups {
                if hasHeader(group), first.header == nil { first.header = group.id }
                let cut = shownRuns(group.entries, limit: Self.runLimit)
                for entry in cut.shown {
                    switch entry {
                    case .run(let run):
                        if first.run == nil { first.run = run.id }
                        if expansion.expandedRuns.contains(run.id) {
                            markPod(&first, folds: orderedFolds(run))
                        }
                    case .rollup(let rollup):
                        if first.rollup == nil { first.rollup = rollup.id }
                        if expansion.expandedRollups.contains(rollup.id) {
                            markPod(&first, folds: rollup.folds)
                        }
                    case .fold(let fold):
                        markPod(&first, folds: [fold])
                    }
                }
            }
        }
        return first
    }

    private func markPod(_ first: inout FirstRows, folds: [PodFold]) {
        guard first.pod == nil, let fold = folds.first else { return }
        first.pod = fold.lead.id
    }

    private var rows: some View {
        // The row height varies with the second line, so the list is a List of
        // custom rows rather than a Table. Every header is a row of the list
        // rather than a Section header, so scrolling never pins one group's
        // header over another group's rows.
        List(selection: $selection) {
            let marks = firstRows
            ForEach(clusterGroups) { bucket in
                if selectedClusters > 1, !bucket.title.isEmpty {
                    let synthetic = clusterHeaderGroup(bucket)
                    header(synthetic, first: true, marks: marks)
                    if !expansion.collapsedGroups.contains(synthetic.id) {
                        groups(bucket, marks: marks)
                    }
                } else {
                    groups(bucket, marks: marks)
                }
            }
        }
        .listStyle(.inset)
        .alternatingRowBackgrounds(.disabled)
        // Single click selects and double click opens, which is what a
        // primaryAction on the selection is; Return reaches the same opener
        // through onKeyPress, because a focused List does not route it here.
        .contextMenu(forSelectionType: String.self) { _ in
        } primaryAction: { ids in
            if let id = ids.first { openOrToggle(id) }
        }
        .onKeyPress(.return) { act { openOrToggle($0) } }
        // Space opens what a chevron would open; a plain row has nothing to
        // open, so the press falls through to the list's own scrolling.
        .onKeyPress(keys: [.space]) { press in
            guard press.modifiers.isEmpty, let selection else { return .ignored }
            if rollups[selection] != nil {
                toggleRollup(selection)
            } else if runs[selection] != nil {
                toggleRun(selection)
            } else if let fold = leadFolds[selection] {
                toggleFold(fold)
            } else if incidentsByID[selection] != nil {
                return .ignored
            } else {
                toggleGroup(selection)
            }
            return .handled
        }
        // A keyboardShortcut with no modifiers would fire while a person types
        // in the filter field; a key press reaches the list only while the list
        // has the focus.
        .onKeyPress(keys: ["/"]) { press in
            guard press.modifiers.isEmpty else { return .ignored }
            focusFilter()
            return .handled
        }
        .onKeyPress(keys: ["a", "d", "r", "n", "?"]) { press in
            // "?" arrives with shift held, so it is answered before the
            // no-modifier guard the four verbs need.
            if press.key == "?" {
                explain.toggle(.incidents(.rows))
                return .handled
            }
            guard press.modifiers.isEmpty, let selection, let rows = selectedRows(selection)
            else { return .ignored }
            // A header, a run and a rollup are not one incident: acknowledge
            // acts on every row at once because it is reversible, but dismiss
            // and resolve ask first. Each row is written independently, so a
            // failure on one never stops the others.
            let single = incidentsByID[selection] != nil
            switch press.key {
            case "a": rows.forEach { acknowledge($0.id) }
            case "n": pendingNote = PendingNote(rows: rows)
            case "d":
                if single {
                    dismiss(selection)
                } else {
                    pendingBulk = PendingBulkAction(rows: rows, action: .dismiss)
                }
            default:
                if single {
                    resolve(selection)
                } else {
                    pendingBulk = PendingBulkAction(rows: rows, action: .resolve)
                }
            }
            return .handled
        }
        // Command-Delete is the destructive list action everywhere else on the
        // Mac; a bare Delete would be one keystroke away from an unrecoverable
        // row. A command-modified press is consumed as a key equivalent before
        // keyDown, so it never reaches onKeyPress; the shortcut must be a key
        // equivalent itself. The confirmation keeps a press that lands while
        // the filter field is being edited from destroying anything.
        .background(
            // A delete is per incident, and a header, a run and a rollup are
            // not one, so the chord does nothing on them.
            Button("") {
                if let selection, incidentsByID[selection] != nil { pendingDelete = selection }
            }
            .keyboardShortcut(.delete, modifiers: .command)
            .opacity(0)
            .accessibilityHidden(true)
        )
        // Return deliberately cancels: a destructive default would let the
        // Return reflex destroy a row. Repeating the chord that opened the
        // dialog is the deliberate second keystroke that confirms it.
        .alert("Delete incident \(pendingDelete ?? "")?", isPresented: confirmingDelete) {
            Button("Delete", role: .destructive) {
                if let id = pendingDelete { delete(id) }
                pendingDelete = nil
            }
            .keyboardShortcut(.delete, modifiers: .command)
            Button("Cancel", role: .cancel) { pendingDelete = nil }
                .keyboardShortcut(.defaultAction)
        } message: {
            Text(
                "The row and its captured files are removed now. History and events stay "
                    + "until their own sweep. This cannot be undone.")
        }
        // Return cancels here as it does for a delete: a manual close is final,
        // and twenty of them should not ride on the Return reflex.
        .alert(pendingBulk?.title ?? "", isPresented: confirmingBulk) {
            Button(pendingBulk?.verb ?? "") {
                if let pending = pendingBulk {
                    let write = pending.action == .dismiss ? dismiss : resolve
                    pending.rows.forEach { write($0.id) }
                }
                pendingBulk = nil
            }
            Button("Cancel", role: .cancel) { pendingBulk = nil }
                .keyboardShortcut(.defaultAction)
        } message: {
            Text(
                "Each incident is written on its own, so one that fails leaves the others "
                    + "done.")
        }
        .sheet(item: $pendingNote) { pending in
            // A set of rows has no one note to start from, so the editor opens
            // empty and what is written lands on every row.
            NoteSheet(note: pending.rows.count == 1 ? pending.rows[0].note : nil) { text in
                note(pending.rows.map(\.id), text)
            }
        }
    }

    private var confirmingDelete: Binding<Bool> {
        Binding(get: { pendingDelete != nil }, set: { if !$0 { pendingDelete = nil } })
    }

    private var confirmingBulk: Binding<Bool> {
        Binding(get: { pendingBulk != nil }, set: { if !$0 { pendingBulk = nil } })
    }

    private func act(_ body: (String) -> Void) -> KeyPress.Result {
        guard let selection else { return .ignored }
        body(selection)
        return .handled
    }

    @ViewBuilder private func groups(_ bucket: IncidentClusterGroup, marks: FirstRows)
        -> some View
    {
        ForEach(Array(bucket.groups.enumerated()), id: \.element.id) { index, group in
            if hasHeader(group) {
                header(group, first: index == 0, marks: marks)
            }
            // The collapsed state outlives the view that set it, and the same
            // group can lose its header in another view; a group with no
            // chevron to reopen it is never drawn collapsed.
            if !hasHeader(group) || !expansion.collapsedGroups.contains(group.id) {
                entries(group, marks: marks)
            }
        }
    }

    // A group with neither a kind nor a name has nothing to say that the
    // cluster header above it does not.
    private func hasHeader(_ group: IncidentGroup) -> Bool {
        !group.headerless && !(group.kind.isEmpty && group.title.isEmpty)
    }

    private func header(_ group: IncidentGroup, first: Bool, marks: FirstRows) -> some View {
        GroupHeaderView(
            group: group, collapsed: expansion.collapsedGroups.contains(group.id), now: Date(),
            toggle: { toggleGroup(group.id) })
            .contentShape(Rectangle())
            // A group header starts a new problem, and the eye needs a
            // stronger line than the one between two children of the same
            // problem.
            .overlay(alignment: .top) {
                if !first { Divider().overlay(.tertiary) }
            }
            .tag(group.id)
            .explained("groupHeader", when: marks.header == group.id)
    }

    // A headerless group's row has no parent to be a child of, so it takes
    // neither the indent nor the guide.
    @ViewBuilder private func groupChild(_ view: some View, child: Bool, isLast: Bool)
        -> some View
    {
        if child {
            view.modifier(GroupChild(isLast: isLast))
        } else {
            view
        }
    }

    @ViewBuilder private func entries(_ group: IncidentGroup, marks: FirstRows) -> some View {
        let cut = shownRuns(group.entries, limit: Self.runLimit)
        let child = hasHeader(group)
        let lead = cut.more > 0 ? firstRun(group)?.lead : nil
        ForEach(Array(cut.shown.enumerated()), id: \.element.id) { index, entry in
            // The more runs line closes the group when it is there.
            let isLast = lead == nil && index == cut.shown.count - 1
            switch entry {
            case .run(let run):
                runRow(run, child: child, isLast: isLast, marks: marks)
            case .fold(let fold):
                foldRows(fold, indented: false, child: child, isLast: isLast, marks: marks)
            case .rollup(let rollup):
                rollupRow(rollup, child: child, isLast: isLast, marks: marks)
            }
        }
        if let lead {
            moreRuns(cut.more, lead: lead, child: child)
        }
    }

    @ViewBuilder private func runRow(
        _ run: RunFold, child: Bool, isLast: Bool, marks: FirstRows
    ) -> some View {
        let expanded = expansion.expandedRuns.contains(run.id)
        let ordered = orderedFolds(run)
        groupChild(
            RunRowView(
                run: run, kind: run.lead.workloadKind, workloadName: run.lead.workloadName,
                now: Date(), expanded: expanded, density: density,
                toggle: { toggleRun(run.id) }, openRun: openRun)
                .contentShape(Rectangle()),
            child: child, isLast: isLast && !(expanded && !ordered.isEmpty))
            .tag(run.id)
            .explained("runRow", when: marks.run == run.id)
        // The run's rows are out of the list while it is folded, so the arrow
        // keys and the actions reach them only once it is open.
        if expanded {
            ForEach(Array(ordered.enumerated()), id: \.element.id) { index, fold in
                foldRows(
                    fold, indented: true, child: child,
                    isLast: isLast && index == ordered.count - 1, marks: marks)
            }
        }
    }

    @ViewBuilder private func rollupRow(
        _ rollup: Rollup, child: Bool, isLast: Bool, marks: FirstRows
    ) -> some View {
        let expanded = expansion.expandedRollups.contains(rollup.id)
        groupChild(
            RollupRowView(
                rollup: rollup,
                clusterName: cluster(rollup.rows[0].clusterID)?.name ?? rollup.rows[0].clusterID,
                selectedClusters: selectedClusters, now: Date(), expanded: expanded,
                density: density, toggle: { toggleRollup(rollup.id) })
                .contentShape(Rectangle()),
            child: child, isLast: isLast && !(expanded && !rollup.folds.isEmpty))
            .tag(rollup.id)
            .explained("rollupRow", when: marks.rollup == rollup.id)
        // The pods are out of the list while the rollup is folded, so the
        // arrow keys and the actions reach them only once it is open.
        if expanded {
            ForEach(Array(rollup.folds.enumerated()), id: \.element.id) { index, fold in
                foldRows(
                    fold, indented: true, child: child,
                    isLast: isLast && index == rollup.folds.count - 1, marks: marks)
            }
        }
    }

    // The cut runs are not hidden rows: the whole history of the workload's
    // runs is a screen of its own, and this line is the way to it.
    private func moreRuns(_ more: Int, lead: Incident, child: Bool) -> some View {
        groupChild(
            Button { openRuns(lead) } label: {
                Text("+ \(more) more runs - open the run history in Workloads")
                    .font(.system(size: 11))
                    .foregroundStyle(Color.accentColor)
                    .padding(.leading, ListColumns.disclosure + ListColumns.gap)
            }
            .buttonStyle(.plain),
            child: child, isLast: true)
            .selectionDisabled()
    }

    // The job row says least about what failed, so it reads last.
    private func orderedFolds(_ run: RunFold) -> [PodFold] {
        let folds = podFolds(run.rows)
        return folds.filter { $0.podUID != nil } + folds.filter { $0.podUID == nil }
    }

    private func firstRun(_ group: IncidentGroup) -> RunFold? {
        for entry in group.entries {
            if case .run(let run) = entry { return run }
        }
        return nil
    }

    @ViewBuilder private func foldRows(
        _ fold: PodFold, indented: Bool, child: Bool, isLast: Bool, marks: FirstRows
    ) -> some View {
        let expanded = isExpanded(fold)
        let siblings = expanded ? fold.siblings : []
        groupChild(
            IncidentRowView(
                incident: fold.lead,
                clusterName: cluster(fold.lead.clusterID)?.name ?? fold.lead.clusterID,
                selectedClusters: selectedClusters, now: Date(),
                siblings: fold.siblings.count,
                expanded: expanded, toggleFold: { toggleFold(fold) }, indented: indented,
                density: density)
                .contentShape(Rectangle()),
            child: child, isLast: isLast && siblings.isEmpty)
            .tag(fold.lead.id)
            .explained("podRow", when: marks.pod == fold.lead.id)
        // A folded sibling is out of the list, so the arrow keys and
        // every action reach it only while its pod is expanded.
        ForEach(Array(siblings.enumerated()), id: \.element.id) { index, sibling in
            groupChild(
                IncidentRowView(
                    incident: sibling,
                    clusterName: cluster(sibling.clusterID)?.name ?? sibling.clusterID,
                    selectedClusters: selectedClusters, now: Date(), isSibling: true,
                    indented: indented, density: density)
                    .contentShape(Rectangle()),
                child: child, isLast: isLast && index == siblings.count - 1)
                .tag(sibling.id)
        }
    }

    private func isExpanded(_ fold: PodFold) -> Bool {
        guard let uid = fold.podUID else { return false }
        return expansion.expandedPods.contains(uid)
    }

    private func toggleFold(_ fold: PodFold) {
        guard let uid = fold.podUID else { return }
        toggle(uid, in: \.expandedPods)
    }

    private func toggleRollup(_ id: String) { toggle(id, in: \.expandedRollups) }

    private func toggleRun(_ id: String) { toggle(id, in: \.expandedRuns) }

    // A group is stored the other way round: a group nobody has touched is
    // open, so what is remembered is the fold.
    private func toggleGroup(_ id: String) { toggle(id, in: \.collapsedGroups) }

    private func toggle(_ id: String, in field: WritableKeyPath<ListExpansion, Set<String>>) {
        if expansion[keyPath: field].contains(id) {
            expansion[keyPath: field].remove(id)
        } else {
            expansion[keyPath: field].insert(id)
        }
    }

    // The cluster bucket's own header is the group shape over every row the
    // bucket holds.
    private func clusterHeaderGroup(_ bucket: IncidentClusterGroup) -> IncidentGroup {
        let rows = bucket.groups.flatMap(\.rows)
        let open = rows.filter { $0.closedAt == nil }
        let incidents = rows.count == 1 ? "1 incident" : "\(rows.count) incidents"
        return IncidentGroup(
            id: "cluster/\(bucket.id)", kind: "", title: bucket.title, meta: "", help: "",
            openCount: open.count,
            worstCategory: open.map(\.category).min { $0.rank < $1.rank },
            newestSeen: rows.map(\.lastSeenAt).max { $0.raw < $1.raw }, rows: rows, entries: [],
            factsKey: nil, summary: "\(incidents), \(open.count) open",
            badge: groupBadge(rows), scope: true)
    }

    // Every rollup entry across every group, keyed by its id: the key
    // handlers ask this whether a selection is a rollup rather than an
    // incident.
    private var rollups: [String: Rollup] {
        var result: [String: Rollup] = [:]
        for bucket in clusterGroups {
            for group in bucket.groups {
                for entry in group.entries {
                    if case .rollup(let rollup) = entry {
                        result[rollup.id] = rollup
                    }
                }
            }
        }
        return result
    }

    // Every run entry across every group, keyed by its id.
    private var runs: [String: RunFold] {
        var result: [String: RunFold] = [:]
        for bucket in clusterGroups {
            for group in bucket.groups {
                for entry in group.entries {
                    if case .run(let run) = entry {
                        result[run.id] = run
                    }
                }
            }
        }
        return result
    }

    private var incidentsByID: [String: Incident] {
        var result: [String: Incident] = [:]
        for bucket in clusterGroups {
            for group in bucket.groups {
                for row in group.rows { result[row.id] = row }
            }
        }
        return result
    }

    // The rows every group header stands for, the cluster buckets' synthetic
    // headers included: what the keys act on when a header is selected.
    private var groupRows: [String: [Incident]] {
        var result: [String: [Incident]] = [:]
        for bucket in clusterGroups {
            if selectedClusters > 1, !bucket.title.isEmpty {
                result[clusterHeaderGroup(bucket).id] = bucket.groups.flatMap(\.rows)
            }
            for group in bucket.groups { result[group.id] = group.rows }
        }
        return result
    }

    // Every pod fold that has siblings to show, by its lead row's id: a lead
    // with nothing folded under it opens nothing.
    private var leadFolds: [String: PodFold] {
        var result: [String: PodFold] = [:]
        for bucket in clusterGroups {
            for group in bucket.groups {
                for entry in group.entries {
                    let folds: [PodFold]
                    switch entry {
                    case .fold(let fold): folds = [fold]
                    case .rollup(let rollup): folds = rollup.folds
                    case .run(let run): folds = podFolds(run.rows)
                    }
                    for fold in folds where fold.podUID != nil && !fold.siblings.isEmpty {
                        result[fold.lead.id] = fold
                    }
                }
            }
        }
        return result
    }

    // One incident, or every incident the selected header, run or rollup
    // stands for.
    private func selectedRows(_ id: String) -> [Incident]? {
        if let rollup = rollups[id] { return rollup.rows }
        if let run = runs[id] { return run.rows }
        if let incident = incidentsByID[id] { return [incident] }
        return groupRows[id]
    }

    private var openUnacknowledged: [Incident] {
        clusterGroups.flatMap { $0.groups.flatMap(\.rows) }
            .filter { $0.closedAt == nil && $0.acknowledgedAt == nil }
    }

    // Only a group that draws a header can be folded, so those are the ones
    // the two words of the one button are about.
    private var headerIDs: [String] {
        var ids: [String] = []
        for bucket in clusterGroups {
            if selectedClusters > 1, !bucket.title.isEmpty {
                ids.append(clusterHeaderGroup(bucket).id)
            }
            for group in bucket.groups where hasHeader(group) { ids.append(group.id) }
        }
        return ids
    }

    // A rollup or a run is folded by its own id being absent from its own set,
    // not by its ancestor header folding: "Collapse all" only reads as
    // collapsing everything if it folds those too, and not just the headers
    // that happen to hide them.
    private var allCollapsed: Bool {
        !headerIDs.isEmpty && headerIDs.allSatisfy { expansion.collapsedGroups.contains($0) }
            && rollups.keys.allSatisfy { !expansion.expandedRollups.contains($0) }
            && runs.keys.allSatisfy { !expansion.expandedRuns.contains($0) }
    }

    private func toggleAll() {
        if allCollapsed {
            headerIDs.forEach { expansion.collapsedGroups.remove($0) }
        } else {
            expansion.collapsedGroups.formUnion(headerIDs)
            expansion.expandedRollups.subtract(rollups.keys)
            expansion.expandedRuns.subtract(runs.keys)
        }
    }

    // A header, a run and a rollup are not incidents and have no detail to
    // push, so the gesture that opens a row opens them up instead.
    private func openOrToggle(_ id: String) {
        if rollups[id] != nil {
            toggleRollup(id)
        } else if runs[id] != nil {
            toggleRun(id)
        } else if incidentsByID[id] != nil {
            open(id)
        } else {
            toggleGroup(id)
        }
    }

    @ViewBuilder private var empty: some View {
        VStack(spacing: 6) {
            if isLoading {
                Text("Loading...")
                    .font(.title3)
                    .foregroundStyle(.secondary)
            } else if scopeIsEmpty {
                Text("Nothing here")
                    .font(.title3)
                    .foregroundStyle(.secondary)
                Text("No cluster is in scope. Check a cluster in the sidebar.")
                    .foregroundStyle(.tertiary)
            } else if hasRows {
                Text("Nothing here")
                    .font(.title3)
                    .foregroundStyle(.secondary)
                Text(noMatch).foregroundStyle(.tertiary)
                if !filterText.isEmpty {
                    Button("Clear filter", action: clearFilter)
                }
            } else {
                // A view with nothing in it is where the vocabulary of the
                // state that empties it is worth reading.
                Text(emptyTitle)
                    .font(.title3)
                    .foregroundStyle(.secondary)
                Text(emptyBody)
                    .foregroundStyle(.tertiary)
                    .multilineTextAlignment(.center)
                    .lineLimit(nil)
                    .frame(maxWidth: 460)
                    .explained("emptySentence")
                if filter.state != .attention {
                    Button("Show Attention", action: showAttention)
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        // A view with nothing in it has no list to focus, and the key that
        // explains a screen has to work on the screen that needs it most.
        .focusable()
        .focusEffectDisabled()
        .onKeyPress(keys: ["?"]) { _ in
            explain.toggle(.incidents(.empty))
            return .handled
        }
    }

    private var emptyTitle: String {
        if let category = filter.category {
            guard let state = filter.state else { return "No \(category.label) incidents." }
            return "No \(category.label) incidents in \(state.title)."
        }
        switch filter.state {
        case .attention: return "Nothing needs attention."
        case .some(let state): return "No \(state.title.lowercased()) incidents."
        case nil: return "No incidents."
        }
    }

    private var emptyBody: String {
        var lines: [String] = []
        if let state = filter.state { lines.append(state.tooltip) }
        if let category = filter.category { lines.append(category.tooltip) }
        if let key = filter.state?.key {
            lines.append("Press \(key) on a row to put it here.")
        }
        return lines.joined(separator: "\n")
    }

    // The field that hid every row sits in a corner of the toolbar, so the
    // empty state repeats the text a person would otherwise go looking for.
    private var noMatch: String {
        filterText.isEmpty
            ? "No incident matches the filter."
            : "No incident matches \"\(filterText)\"."
    }
}
