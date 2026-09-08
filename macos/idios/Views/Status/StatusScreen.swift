import IdiosModel
import SwiftUI

/// StatusScreen is the one screen about idios rather than about a cluster:
/// what the daemon is, what its writer, handlers, capture pool and closer did,
/// what it holds on disk and what the last sweep removed.
struct StatusScreen: View {
    let clusters: [Cluster]

    @Environment(DaemonConnection.self) private var connection
    @Environment(StatusStore.self) private var store
    @Environment(ExplainState.self) private var explain

    var body: some View {
        content
            // The key that explains a screen has to reach it wherever the
            // pointer is.
            .focusable()
            .focusEffectDisabled()
            .onKeyPress(keys: ["?"]) { _ in
                explain.toggle(.status)
                return .handled
            }
            // The overlay is installed on the window's split view, so the
            // sidebar is under its scrim too; this screen publishes which of
            // its tables that overlay draws.
            // The window holds two copies of this screen while it settles, so
            // a copy going away must not clear the surviving copy's screen;
            // whatever replaces it publishes its own, and the incidents list
            // reads its table from what it draws rather than from here.
            .onAppear { explain.current = .status }
            .task(id: connection.generation) { await store.poll(connection: connection) }
    }

    @ViewBuilder private var content: some View {
        if let status = store.status {
            VStack(spacing: 0) {
                header(status)
                Divider()
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        ClustersHealthCard(status: status, clusters: clusters)
                            .explained("clustersHealth")
                        ProcessCards(status: status).explained("processCards")
                        HStack(alignment: .top, spacing: 12) {
                            ArtifactOutcomesCard(status: status)
                                .explained("artifactOutcomes")
                            StorageCard(status: status)
                        }
                        SweepCard(runs: status.latestSweepRuns).explained("sweep")
                        IncidentTotalsCard(status: status)
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        } else if case .unreachable(let description) = connection.state {
            NotConnectedView(
                address: connection.address, error: description, retry: connection.retry)
        } else if let error = store.error {
            WrapText(value: error.message)
                .font(.system(size: 11.5))
                .foregroundStyle(BadgeStyle.red.text)
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // An unreachable daemon, a cluster that stopped syncing and a stale
    // snapshot each change what every number below means, so the reader is
    // told about them before any table.
    private func header(_ status: DaemonStatus) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            connectionLine(status)
            healthLine
            ageLine(status)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder private func connectionLine(_ status: DaemonStatus) -> some View {
        if status.daemonRunning {
            (Text("connected to ")
                + Text(connection.address).font(.system(size: 12, design: .monospaced))
                + Text(" - pid \(status.pid) - version " + status.version))
                .font(.system(size: 12))
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        } else {
            Text("no recorder behind this API (mock or status-only)")
                .font(.system(size: 12))
                .foregroundStyle(BadgeStyle.orange.text)
                .textSelection(.enabled)
        }
    }

    private var healthLine: some View {
        let total = clusters.count
        let notReady = clusters.filter { !$0.ready }.count
        let hasError = clusters.contains { $0.lastError != nil }
        let text: String
        if total == 0 {
            text = "no cluster watched"
        } else if notReady == 0 {
            text = "\(total) cluster\(total == 1 ? "" : "s") watched, all ready"
        } else {
            text = "\(notReady) of \(total) cluster\(total == 1 ? "" : "s") not ready"
        }
        return Text(text)
            .font(.system(size: 12))
            .foregroundStyle(hasError ? BadgeStyle.red.text : .secondary)
    }

    private func ageLine(_ status: DaemonStatus) -> some View {
        Text("snapshot \(snapshotAge(status)), written every 10 s")
            .font(.system(size: 11))
            .monospacedDigit()
            .foregroundStyle(.tertiary)
            .help("written_at \(status.writtenAt.raw)")
    }

    private func snapshotAge(_ status: DaemonStatus) -> String {
        guard let date = status.writtenAt.date else {
            return "\(clockTime(status.writtenAt, seconds: true)) UTC"
        }
        let age = Int(Date().timeIntervalSince(date).rounded())
        return "\(max(age, 0)) s ago"
    }
}

/// ClustersHealthCard is what each watched cluster is doing right now: synced,
/// how long ago an object last arrived, the clock difference and the last error.
struct ClustersHealthCard: View {
    let status: DaemonStatus
    let clusters: [Cluster]

    private static let nameWidth: CGFloat = 170
    private static let syncedWidth: CGFloat = 70
    private static let lastObjectWidth: CGFloat = 170
    private static let skewWidth: CGFloat = 100

    var body: some View {
        DetailCard(title: "Clusters", meta: "status.json, per watcher") {
            if status.clusters.isEmpty {
                Text("No cluster is watched.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(status.clusters, id: \.id) { cluster in
                        Divider()
                        row(cluster)
                    }
                }
            }
        }
    }

    private var heading: some View {
        HStack(spacing: 7) {
            Text("NAME").frame(width: Self.nameWidth, alignment: .leading)
            Text("SYNCED").frame(width: Self.syncedWidth, alignment: .leading)
            Text("LAST OBJECT").frame(width: Self.lastObjectWidth, alignment: .leading)
            Text("CLOCK SKEW").frame(width: Self.skewWidth, alignment: .leading)
            Text("LAST_ERROR").frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ cluster: StatusCluster) -> some View {
        HStack(alignment: .top, spacing: 7) {
            HStack(spacing: 8) {
                Circle()
                    .fill(dotColor(cluster))
                    .frame(width: 7, height: 7)
                Text(name(cluster.id))
                    .font(.system(size: 11.5))
                    .lineLimit(1)
            }
            .frame(width: Self.nameWidth, alignment: .leading)
            Badge(
                text: cluster.ready ? "yes" : "no",
                style: cluster.ready ? BadgeStyle.green : BadgeStyle.orange)
                .frame(width: Self.syncedWidth, alignment: .leading)
            Text(lastObject(cluster))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: Self.lastObjectWidth, alignment: .leading)
            Text(skew(cluster))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: Self.skewWidth, alignment: .leading)
            lastError(cluster)
                .font(.system(size: 11))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 6)
    }

    @ViewBuilder private func lastError(_ cluster: StatusCluster) -> some View {
        if let error = self.error(cluster.id) {
            WrapText(value: error).foregroundStyle(BadgeStyle.red.text)
        } else {
            Text("-").foregroundStyle(.secondary)
        }
    }

    private func name(_ clusterID: String) -> String {
        clusters.first { $0.id == clusterID }?.name ?? clusterID
    }

    private func error(_ clusterID: String) -> String? {
        clusters.first { $0.id == clusterID }?.lastError
    }

    private func dotColor(_ cluster: StatusCluster) -> Color {
        clusterDot(ready: cluster.ready, hasError: error(cluster.id) != nil)
    }

    private func lastObject(_ cluster: StatusCluster) -> String {
        guard let last = cluster.lastEventAt else { return "-" }
        let clock = clockTime(last, seconds: true)
        guard let date = last.date else { return clock }
        let age = Int(Date().timeIntervalSince(date).rounded())
        return "\(clock) (\(max(age, 0)) s ago)"
    }

    private func skew(_ cluster: StatusCluster) -> String {
        guard let seconds = cluster.skewSeconds else { return "-" }
        return String(format: "%+.1f s", seconds)
    }
}

/// ProcessCards is the four counters of the running process: the writer, the
/// API handlers, the capture pool and the closer.
struct ProcessCards: View {
    let status: DaemonStatus

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            SummaryCard(label: "Writer") {
                big(String(status.writer.transactions))
                note(
                    "transactions - \(status.writer.errors) errors - p99 "
                        + String(format: "%.1f", status.writer.p99Ms) + " ms")
            }
            SummaryCard(label: "Handlers") {
                big(String(status.handlers.errors), suffix: "errors")
                note("\(status.handlers.panics) recovered panics")
            }
            SummaryCard(label: "Capture queue") {
                big(String(status.capture.dropped), suffix: "dropped")
                note(
                    "queued \(status.capture.queued) - completed \(status.capture.completed)")
            }
            SummaryCard(label: "Closer") {
                big(status.closer.lastTickAt.map { clockTime($0, seconds: true) } ?? "-")
                note(
                    "closed \(status.closer.closed) this tick, "
                        + "\(status.closer.closedTotal) since start; late-attached "
                        + "\(status.closer.attached), \(status.closer.attachedTotal); "
                        + "opened \(status.closer.opened), \(status.closer.openedTotal)")
            }
        }
    }

    private func big(_ value: String, suffix: String? = nil) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 5) {
            Text(value)
                .font(.system(size: 22, weight: .semibold))
                .monospacedDigit()
            if let suffix {
                Text(suffix)
                    .font(.system(size: 12))
                    .foregroundStyle(.tertiary)
            }
        }
    }

    private func note(_ value: String) -> some View {
        Text(value)
            .font(.system(size: 10.5))
            .monospacedDigit()
            .foregroundStyle(.tertiary)
            .lineLimit(3)
    }
}

/// ArtifactOutcomesCard is what the captures produced: the files that exist by
/// outcome, and under them the attempts that came back with a gap instead.
struct ArtifactOutcomesCard: View {
    let status: DaemonStatus

    var body: some View {
        DetailCard(title: "Artifacts by outcome", meta: "artifacts GROUP BY capture_gap") {
            VStack(alignment: .leading, spacing: 7) {
                ForEach(outcomes, id: \.0) { outcome, count in
                    bar(label: outcome, count: count, peak: outcomePeak, style: style(outcome))
                }
            }
            Divider()
            Text("capture attempts by gap")
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
            if gaps.isEmpty {
                Text("Every capture produced a file.")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 7) {
                    ForEach(gaps, id: \.0) { gap, count in
                        bar(label: gap.rawValue, count: count, peak: gapPeak, style: .gap)
                    }
                }
            }
        }
    }

    private func bar(label: String, count: Int32, peak: Int32, style: BadgeStyle) -> some View {
        HStack(spacing: 10) {
            Text(label)
                .font(.system(size: 11, design: .monospaced))
                .lineLimit(1)
                .frame(width: 130, alignment: .leading)
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    RoundedRectangle(cornerRadius: 2).fill(.quaternary)
                    RoundedRectangle(cornerRadius: 2)
                        .fill(style.text)
                        .frame(width: geometry.size.width * Double(count) / Double(peak))
                }
            }
            .frame(height: 8)
            Text(String(count))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 44, alignment: .trailing)
        }
    }

    private var outcomes: [(String, Int32)] {
        status.artifactsByOutcome
            .map { ($0.key, $0.value) }
            .sorted { ($1.1, $0.0) < ($0.1, $1.0) }
    }

    private var gaps: [(CaptureGap, Int32)] {
        status.capture.gaps
            .map { ($0.key, $0.value) }
            .sorted { ($1.1, $0.0.rawValue) < ($0.1, $1.0.rawValue) }
    }

    private var outcomePeak: Int32 {
        max(outcomes.map(\.1).max() ?? 0, 1)
    }

    private var gapPeak: Int32 {
        max(gaps.map(\.1).max() ?? 0, 1)
    }

    // A file is the outcome that is not a gap; the rest of the vocabulary is a
    // capture_gap value and is coloured by how bad it is.
    private func style(_ outcome: String) -> BadgeStyle {
        switch outcome {
        case "file": .green
        case "forbidden", "kubelet_error": .red
        case "unknown": .orange
        default: .grey
        }
    }
}

/// StorageCard is what the data directory holds and the intervals the daemon
/// was configured with.
struct StorageCard: View {
    let status: DaemonStatus

    var body: some View {
        DetailCard(title: "Storage", meta: "data_dir") {
            VStack(alignment: .leading, spacing: 5) {
                fact("idios.db", bytes(status.dbBytes))
                fact("idios.db-wal", bytes(status.walBytes))
                fact(
                    "artifacts/",
                    "\(bytes(status.artifactBytes)) - \(status.artifactFiles) files")
                fact("retention", "\(status.retentionDays) d", key: "retention_days")
                fact("rows", rows)
            }
            Divider()
            VStack(alignment: .leading, spacing: 5) {
                interval("sweep interval", status.sweepIntervalSeconds, key: "sweep_interval")
                interval(
                    "stabilization window", status.stabilizationWindowSeconds,
                    key: "stabilization_window")
                interval(
                    "stabilization check interval",
                    status.stabilizationCheckIntervalSeconds,
                    key: "stabilization_check_interval")
                interval(
                    "early capture debounce", status.earlyCaptureDebounceSeconds,
                    key: "early_capture_debounce")
                interval(
                    "api stream throttle", status.apiStreamThrottleSeconds,
                    key: "api_stream_throttle")
                interval(
                    "scheduling grace", status.schedulingGraceSeconds,
                    key: "scheduling_grace")
                interval("probe grace", status.probeGraceSeconds, key: "probe_grace")
                interval("stuck after", status.stuckAfterSeconds, key: "stuck_after")
                interval(
                    "attention window", status.attentionWindowSeconds,
                    key: "attention_window")
            }
        }
    }

    @ViewBuilder private func fact(_ label: String, _ value: String, key: String? = nil)
        -> some View
    {
        let row = FactRow(label: label, labelWidth: 150) {
            Text(value)
                .font(.system(size: 11.5))
                .monospacedDigit()
                .textSelection(.enabled)
        }
        if let key {
            row.help(key)
        } else {
            row
        }
    }

    // The config file holds these in seconds, so the hover keeps the raw
    // value even after the label switches to human units.
    private func interval(_ label: String, _ value: Int32, key: String) -> some View {
        FactRow(label: label, labelWidth: 150) {
            Text(humanDuration(seconds: value))
                .font(.system(size: 11.5))
                .monospacedDigit()
                .textSelection(.enabled)
        }
        .help("\(key) = \(value) s")
    }

    private var rows: String {
        let counts = status.rowCounts
        return "pods \(counts.pods) - live \(counts.livePods) - transitions "
            + "\(counts.transitions) - events \(counts.events)"
    }

    private func bytes(_ value: Int64?) -> String {
        value.map(byteCount) ?? "-"
    }
}

/// SweepCard is the newest retention sweep of every table it touches.
struct SweepCard: View {
    let runs: [SweepRun]

    var body: some View {
        DetailCard(title: "Latest sweep", meta: "sweep_runs") {
            if runs.isEmpty {
                Text("The sweep has not run yet.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                Text(ranAtLine)
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                    .help(ranAtHelp)
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(runs, id: \.id) { run in
                        Divider()
                        row(run)
                    }
                }
            }
        }
    }

    // The run's own timestamps, not the card's provenance: they carry when it
    // ran and how far back it cut, so they read as clock words in the body
    // rather than joining the hover the card's table-and-predicate meta owns.
    private var ranAtLine: String {
        guard let first = runs.first else { return "" }
        let cutoff = first.cutoff.map { "cutoff \(clockTime($0, seconds: true))" } ?? "no cutoff"
        return "ran at \(clockTime(first.ranAt, seconds: true)) UTC, \(cutoff)"
    }

    private var ranAtHelp: String {
        guard let first = runs.first else { return "" }
        return "ran_at \(first.ranAt.raw), cutoff \(first.cutoff?.raw ?? "none")"
    }

    private var heading: some View {
        HStack(spacing: 7) {
            Text("TABLE").frame(width: 200, alignment: .leading)
            Text("ROWS").frame(width: 80, alignment: .trailing)
            Text("FILES").frame(width: 80, alignment: .trailing)
            Text("BYTES").frame(width: 90, alignment: .trailing)
            Text("DURATION").frame(width: 110, alignment: .trailing)
            Text("ERROR").frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ run: SweepRun) -> some View {
        HStack(alignment: .top, spacing: 7) {
            Text(run.tableName)
                .font(.system(size: 11.5, design: .monospaced))
                .frame(width: 200, alignment: .leading)
            number(String(run.rowsRemoved)).frame(width: 80, alignment: .trailing)
            number(String(run.filesRemoved)).frame(width: 80, alignment: .trailing)
            number(run.bytesRemoved.map(byteCount) ?? "-")
                .frame(width: 90, alignment: .trailing)
            number("\(run.durationMs) ms").frame(width: 110, alignment: .trailing)
            error(run).font(.system(size: 11)).frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 5)
    }

    @ViewBuilder private func error(_ run: SweepRun) -> some View {
        if let message = run.error {
            WrapText(value: message).foregroundStyle(BadgeStyle.red.text)
        } else {
            Text("-").foregroundStyle(.secondary)
        }
    }

    private func number(_ value: String) -> some View {
        Text(value)
            .font(.system(size: 11))
            .monospacedDigit()
            .foregroundStyle(.secondary)
    }
}

/// IncidentTotalsCard is the whole database in two lines: what is open, by
/// category, and what was closed, by the reason that closed it.
struct IncidentTotalsCard: View {
    let status: DaemonStatus

    var body: some View {
        DetailCard(title: "Incidents", meta: "incidents GROUP BY category, close_reason") {
            FactRow(label: "open by category", labelWidth: 150) {
                Flow(spacing: 5, lineSpacing: 4) {
                    ForEach(openCategories, id: \.0) { category, count in
                        Badge(text: "\(category.label) \(count)", style: category.badge)
                            .help(category.rawValue)
                    }
                }
            }
            FactRow(label: "closed by reason", labelWidth: 150) {
                Flow(spacing: 5, lineSpacing: 4) {
                    ForEach(closedReasons, id: \.0) { reason, count in
                        Badge(text: "\(reason.label) \(count)", style: badge(reason))
                            .help(reason.rawValue)
                    }
                }
            }
        }
    }

    private var openCategories: [(IdiosModel.Category, Int32)] {
        status.openByCategory
            .map { ($0.key, $0.value) }
            .sorted { ($1.1, $0.0.rawValue) < ($0.1, $1.0.rawValue) }
    }

    private var closedReasons: [(CloseReason, Int32)] {
        status.closedByReason
            .map { ($0.key, $0.value) }
            .sorted { ($1.1, $0.0.rawValue) < ($0.1, $1.0.rawValue) }
    }

    // A close reason and the lifecycle state it puts an incident in share a
    // name, so they share a colour.
    private func badge(_ reason: CloseReason) -> BadgeStyle {
        IncidentState(rawValue: reason.rawValue)?.badge ?? .neutral
    }
}
