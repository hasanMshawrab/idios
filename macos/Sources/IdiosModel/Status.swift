import Foundation
import IdiosAPI

/// StatusCluster is the runtime state of one cluster watch.
public struct StatusCluster: Hashable, Sendable {
    public let id: String
    public let ready: Bool
    public let lastEventAt: Timestamp?
    public let skewSeconds: Double?
}

extension StatusCluster {
    /// init builds a cluster status from its wire row.
    public init(wire: Components.Schemas.StatusCluster) throws {
        self.id = try require(wire.id, "id")
        self.ready = wire.ready ?? false
        self.lastEventAt = wire.lastEventAt.map(Timestamp.init)
        self.skewSeconds = wire.skewSeconds
    }
}

/// WriterStats counts what the single writer did.
public struct WriterStats: Hashable, Sendable {
    public let transactions: Int32
    public let errors: Int32
    public let p99Ms: Double
}

extension WriterStats {
    /// init builds the counters from their wire message, absent being zero.
    public init(wire: Components.Schemas.WriterStats?) {
        self.transactions = wire?.transactions ?? 0
        self.errors = wire?.errors ?? 0
        self.p99Ms = wire?.p99Ms ?? 0
    }
}

/// HandlerStats counts what the API handlers hit.
public struct HandlerStats: Hashable, Sendable {
    public let errors: Int32
    public let panics: Int32
}

extension HandlerStats {
    /// init builds the counters from their wire message, absent being zero.
    public init(wire: Components.Schemas.HandlerStats?) {
        self.errors = wire?.errors ?? 0
        self.panics = wire?.panics ?? 0
    }
}

/// CaptureStats counts what the capture pool did and why it came back empty.
public struct CaptureStats: Hashable, Sendable {
    public let queued: Int32
    public let completed: Int32
    public let dropped: Int32
    public let gaps: [CaptureGap: Int32]
}

extension CaptureStats {
    /// init builds the counters from their wire message, absent being zero.
    public init(wire: Components.Schemas.CaptureStats?) {
        self.queued = wire?.queued ?? 0
        self.completed = wire?.completed ?? 0
        self.dropped = wire?.dropped ?? 0
        var gaps: [CaptureGap: Int32] = [:]
        for row in wire?.gaps ?? [] {
            // A capture that produced a file carries no gap; it is one bucket
            // of the same GROUP BY and is not a gap to name.
            guard let gap: CaptureGap = modelEnum(row.gap) else { continue }
            gaps[gap] = row.count ?? 0
        }
        self.gaps = gaps
    }
}

/// CloserStats counts what the closer did on its last ticks.
public struct CloserStats: Hashable, Sendable {
    public let lastTickAt: Timestamp?
    public let closed: Int32
    public let attached: Int32
    public let closedTotal: Int32
    public let attachedTotal: Int32
    public let opened: Int32
    public let openedTotal: Int32
}

extension CloserStats {
    /// init builds the counters from their wire message, absent being zero.
    public init(wire: Components.Schemas.CloserStats?) {
        self.lastTickAt = wire?.lastTickAt.map(Timestamp.init)
        self.closed = wire?.closed ?? 0
        self.attached = wire?.attached ?? 0
        self.closedTotal = wire?.closedTotal ?? 0
        self.attachedTotal = wire?.attachedTotal ?? 0
        self.opened = wire?.opened ?? 0
        self.openedTotal = wire?.openedTotal ?? 0
    }
}

/// RowCounts is how much the database holds.
public struct RowCounts: Hashable, Sendable {
    public let pods: Int32
    public let livePods: Int32
    public let transitions: Int32
    public let events: Int32
}

extension RowCounts {
    /// init builds the counters from their wire message, absent being zero.
    public init(wire: Components.Schemas.RowCounts?) {
        self.pods = wire?.pods ?? 0
        self.livePods = wire?.livePods ?? 0
        self.transitions = wire?.transitions ?? 0
        self.events = wire?.events ?? 0
    }
}

/// SweepRun is one retention sweep of one table.
public struct SweepRun: Hashable, Sendable {
    public let id: String
    public let ranAt: Timestamp
    public let cutoff: Timestamp?
    public let tableName: String
    public let rowsRemoved: Int32
    public let filesRemoved: Int32
    public let bytesRemoved: Int64?
    public let durationMs: Int32
    public let error: String?
}

extension SweepRun {
    /// init builds a sweep run from its wire row.
    public init(wire: Components.Schemas.SweepRun) throws {
        self.id = try require(wire.id, "id")
        self.ranAt = Timestamp(try require(wire.ranAt, "ranAt"))
        self.cutoff = wire.cutoff.map(Timestamp.init)
        self.tableName = try require(wire.tableName, "tableName")
        self.rowsRemoved = wire.rowsRemoved ?? 0
        self.filesRemoved = wire.filesRemoved ?? 0
        self.bytesRemoved = parseInt64(wire.bytesRemoved)
        self.durationMs = wire.durationMs ?? 0
        self.error = wire.error
    }
}

/// DaemonStatus is what the status screen shows about the running daemon.
public struct DaemonStatus: Hashable, Sendable {
    public let daemonRunning: Bool
    public let writtenAt: Timestamp
    public let pid: Int32
    public let version: String
    public let clusters: [StatusCluster]
    public let writer: WriterStats
    public let handlers: HandlerStats
    public let capture: CaptureStats
    public let closer: CloserStats
    public let openByCategory: [Category: Int32]
    public let closedByReason: [CloseReason: Int32]
    public let artifactsByOutcome: [String: Int32]
    public let rowCounts: RowCounts
    public let latestSweepRuns: [SweepRun]
    public let dbBytes: Int64?
    public let walBytes: Int64?
    public let artifactFiles: Int32
    public let artifactBytes: Int64?
    public let retentionDays: Int32
    public let sweepIntervalSeconds: Int32
    public let stabilizationWindowSeconds: Int32
    public let stabilizationCheckIntervalSeconds: Int32
    public let earlyCaptureDebounceSeconds: Int32
    public let apiStreamThrottleSeconds: Int32
    public let schedulingGraceSeconds: Int32
    public let probeGraceSeconds: Int32
    public let stuckAfterSeconds: Int32
    public let attentionWindowSeconds: Int32
}

extension DaemonStatus {
    /// init builds the status from its wire message.
    public init(wire: Components.Schemas.Status) throws {
        self.daemonRunning = wire.daemonRunning ?? false
        self.writtenAt = Timestamp(try require(wire.writtenAt, "writtenAt"))
        self.pid = wire.pid ?? 0
        self.version = wire.version ?? ""
        self.clusters = try (wire.clusters ?? []).map(StatusCluster.init(wire:))
        self.writer = WriterStats(wire: wire.writer)
        self.handlers = HandlerStats(wire: wire.handlers)
        self.capture = CaptureStats(wire: wire.capture)
        self.closer = CloserStats(wire: wire.closer)
        var openByCategory: [Category: Int32] = [:]
        for row in wire.openByCategory ?? [] {
            let category: Category = try require(modelEnum(row.category), "category")
            openByCategory[category] = row.count ?? 0
        }
        self.openByCategory = openByCategory
        var closedByReason: [CloseReason: Int32] = [:]
        for row in wire.closedByReason ?? [] {
            let reason: CloseReason = try require(modelEnum(row.closeReason), "closeReason")
            closedByReason[reason] = row.count ?? 0
        }
        self.closedByReason = closedByReason
        var artifactsByOutcome: [String: Int32] = [:]
        for row in wire.artifactsByOutcome ?? [] {
            artifactsByOutcome[try require(row.outcome, "outcome")] = row.count ?? 0
        }
        self.artifactsByOutcome = artifactsByOutcome
        self.rowCounts = RowCounts(wire: wire.rowCounts)
        self.latestSweepRuns = try (wire.latestSweepRuns ?? []).map(SweepRun.init(wire:))
        self.dbBytes = parseInt64(wire.dbBytes)
        self.walBytes = parseInt64(wire.walBytes)
        self.artifactFiles = wire.artifactFiles ?? 0
        self.artifactBytes = parseInt64(wire.artifactBytes)
        self.retentionDays = wire.retentionDays ?? 0
        self.sweepIntervalSeconds = wire.sweepIntervalSeconds ?? 0
        self.stabilizationWindowSeconds = wire.stabilizationWindowSeconds ?? 0
        self.stabilizationCheckIntervalSeconds = wire.stabilizationCheckIntervalSeconds ?? 0
        self.earlyCaptureDebounceSeconds = wire.earlyCaptureDebounceSeconds ?? 0
        self.apiStreamThrottleSeconds = wire.apiStreamThrottleSeconds ?? 0
        self.schedulingGraceSeconds = wire.schedulingGraceSeconds ?? 0
        self.probeGraceSeconds = wire.probeGraceSeconds ?? 0
        self.stuckAfterSeconds = wire.stuckAfterSeconds ?? 0
        self.attentionWindowSeconds = wire.attentionWindowSeconds ?? 0
    }
}

/// KubeContext is one context of the kubeconfig the daemon reads.
public struct KubeContext: Hashable, Sendable {
    public let name: String
    public let cluster: String?
    public let server: String?
}

extension KubeContext {
    /// init builds a context from its wire row.
    public init(wire: Components.Schemas.KubeContext) throws {
        self.name = try require(wire.name, "name")
        self.cluster = wire.cluster
        self.server = wire.server
    }
}

extension Page where Row == KubeContext {
    /// init builds a page of contexts from its wire response.
    public init(wire: Components.Schemas.KubeContextsResponse) throws {
        self.init(
            rows: try (wire.contexts ?? []).map(KubeContext.init(wire:)), truncated: wire.truncated ?? false)
    }
}

/// KubeNamespaces is the namespaces of one context, or the refusal to list
/// them.
public struct KubeNamespaces: Hashable, Sendable {
    public let names: [String]
    public let forbidden: Bool
}

extension KubeNamespaces {
    /// init builds the namespaces from their wire response.
    public init(wire: Components.Schemas.KubeNamespacesResponse) {
        self.names = wire.names ?? []
        self.forbidden = wire.forbidden ?? false
    }
}
