import Foundation
import IdiosAPI

/// TagCount is one image tag seen on a workload; a tag-less entry is an image
/// pinned by digest.
public struct TagCount: Hashable, Sendable {
    public let tag: String?
    public let count: Int32
}

extension TagCount {
    /// init builds a tag count from its wire row.
    public init(wire: Components.Schemas.TagCount) {
        self.tag = wire.tag
        self.count = wire.count ?? 0
    }
}

/// Workload is one workload with its incident aggregates over the window.
public struct Workload: Identifiable, Hashable, Sendable {
    public let clusterID: String
    public let namespace: String
    public let workloadKind: String
    public let workloadName: String
    public let incidentsByCategory: [Category: Int32]
    public let openIncidents: Int32
    public let occurrences: Int32
    public let imageTags: [TagCount]
    public let livePods: Int32
    public let deletedPods: Int32
    public let podUID: String?
    public let podName: String?

    // Every pod no controller owns shares kind "none" and an empty name, so
    // the pod uid is what keeps their rows apart.
    public var id: String {
        "\(clusterID)/\(namespace)/\(workloadKind)/"
            + (workloadName.isEmpty ? (podUID ?? "") : workloadName)
    }
}

extension Workload {
    /// init builds a workload from its wire row.
    public init(wire: Components.Schemas.WorkloadRow) throws {
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.namespace = try require(wire.namespace, "namespace")
        self.workloadKind = try require(wire.workloadKind, "workloadKind")
        // A pod with no controller has kind "none" and an empty name, which
        // proto3 drops from the JSON; an absent name is that pod, not a fault.
        self.workloadName = wire.workloadName ?? ""
        var byCategory: [Category: Int32] = [:]
        for row in wire.incidentsByCategory ?? [] {
            let category: Category = try require(modelEnum(row.category), "category")
            byCategory[category] = row.count ?? 0
        }
        self.incidentsByCategory = byCategory
        self.openIncidents = wire.openIncidents ?? 0
        self.occurrences = wire.occurrences ?? 0
        self.imageTags = (wire.imageTags ?? []).map(TagCount.init(wire:))
        self.livePods = wire.livePods ?? 0
        self.deletedPods = wire.deletedPods ?? 0
        self.podUID = wire.podUid
        self.podName = wire.podName
    }
}

extension Page where Row == Workload {
    /// init builds a page of workloads from its wire response.
    public init(wire: Components.Schemas.WorkloadsResponse) throws {
        self.init(
            rows: try (wire.workloads ?? []).map(Workload.init(wire:)),
            truncated: wire.truncated ?? false)
    }
}

/// Rollout is one ReplicaSet of a workload.
public struct Rollout: Hashable, Sendable {
    public let replicasetUID: String
    public let replicasetName: String
    public let revision: String?
    public let images: [String]
    public let firstSeenAt: Timestamp?
    public let lastSeenAt: Timestamp?
    public let deletedAt: Timestamp?
    public let incidents: Int32
    public let createdAt: Timestamp
    public let replicas: Int32?
    public let readyReplicas: Int32?
    public let availableReplicas: Int32?
}

extension Rollout {
    /// init builds a rollout from its wire row.
    public init(wire: Components.Schemas.Rollout) throws {
        self.replicasetUID = try require(wire.replicasetUid, "replicasetUid")
        self.replicasetName = try require(wire.replicasetName, "replicasetName")
        self.revision = wire.revision
        self.images = wire.images ?? []
        self.firstSeenAt = wire.firstSeenAt.map(Timestamp.init)
        self.lastSeenAt = wire.lastSeenAt.map(Timestamp.init)
        self.deletedAt = wire.deletedAt.map(Timestamp.init)
        self.incidents = wire.incidents ?? 0
        self.createdAt = Timestamp(try require(wire.createdAt, "createdAt"))
        self.replicas = wire.replicas
        self.readyReplicas = wire.readyReplicas
        self.availableReplicas = wire.availableReplicas
    }
}

/// HourBucket is the restarts of a workload in one hour of the window.
public struct HourBucket: Hashable, Sendable {
    public let hour: Timestamp
    public let restarts: Int32
    public let reconstructed: Bool
}

extension HourBucket {
    /// init builds an hour bucket from its wire row.
    public init(wire: Components.Schemas.HourBucket) throws {
        self.hour = Timestamp(try require(wire.hour, "hour"))
        self.restarts = wire.restarts ?? 0
        self.reconstructed = wire.reconstructed ?? false
    }
}

/// WorkloadDetail is everything the workload screen shows about one workload.
public struct WorkloadDetail: Hashable, Sendable {
    public let workload: Workload
    public let rollouts: [Rollout]
    public let restartsByHour: [HourBucket]
    public let pods: [PodRow]
    public let podsTruncated: Bool
}

extension WorkloadDetail {
    /// init builds the detail from its wire message.
    public init(wire: Components.Schemas.WorkloadDetail) throws {
        self.workload = try Workload(wire: try require(wire.workload, "workload"))
        self.rollouts = try (wire.rollouts ?? []).map(Rollout.init(wire:))
        self.restartsByHour = try (wire.restartsByHour ?? []).map(HourBucket.init(wire:))
        self.pods = try (wire.pods ?? []).map(PodRow.init(wire:))
        self.podsTruncated = wire.podsTruncated ?? false
    }
}

/// Job is one Job of a CronJob workload.
public struct Job: Identifiable, Hashable, Sendable {
    public let uid: String
    public let clusterID: String
    public let namespace: String
    public let name: String
    public let cronjobUID: String?
    public let cronjobName: String?
    public let active: Int32
    public let succeeded: Int32
    public let failed: Int32
    public let backoffLimit: Int32
    public let completions: Int32
    public let parallelism: Int32
    public let activeDeadlineSeconds: Int64?
    public let restartPolicy: String?
    public let conditionType: String?
    public let conditionReason: String?
    public let conditionMessage: String?
    public let createdAt: Timestamp
    public let startedAt: Timestamp?
    public let finishedAt: Timestamp?
    public let firstSeenAt: Timestamp
    public let lastSeenAt: Timestamp
    public let deletedAt: Timestamp?
    public let complete: Bool

    public var id: String { uid }
}

extension Job {
    /// init builds a job from its wire row.
    public init(wire: Components.Schemas.JobRow) throws {
        self.uid = try require(wire.uid, "uid")
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.namespace = try require(wire.namespace, "namespace")
        self.name = try require(wire.name, "name")
        self.cronjobUID = wire.cronjobUid
        self.cronjobName = wire.cronjobName
        self.active = wire.active ?? 0
        self.succeeded = wire.succeeded ?? 0
        self.failed = wire.failed ?? 0
        self.backoffLimit = wire.backoffLimit ?? 0
        self.completions = wire.completions ?? 0
        self.parallelism = wire.parallelism ?? 0
        self.activeDeadlineSeconds = parseInt64(wire.activeDeadlineSeconds)
        self.restartPolicy = wire.restartPolicy
        self.conditionType = wire.conditionType
        self.conditionReason = wire.conditionReason
        self.conditionMessage = wire.conditionMessage
        self.createdAt = Timestamp(try require(wire.createdAt, "createdAt"))
        self.startedAt = wire.startedAt.map(Timestamp.init)
        self.finishedAt = wire.finishedAt.map(Timestamp.init)
        self.firstSeenAt = Timestamp(try require(wire.firstSeenAt, "firstSeenAt"))
        self.lastSeenAt = Timestamp(try require(wire.lastSeenAt, "lastSeenAt"))
        self.deletedAt = wire.deletedAt.map(Timestamp.init)
        self.complete = wire.complete ?? false
    }
}

extension Job {
    /// runOutcomeLabel reads the run's condition, the one fact that tells a
    /// retry that went on to succeed from a run that failed outright: a
    /// crashed subject pod under a Job that later completed is a retry.
    public var runOutcomeLabel: String {
        if complete { return "run succeeded after retry" }
        if conditionType == "Failed" { return "run failed" }
        return "run still going"
    }
}

extension Page where Row == Job {
    /// init builds a page of jobs from its wire response.
    public init(wire: Components.Schemas.JobsResponse) throws {
        self.init(
            rows: try (wire.jobs ?? []).map(Job.init(wire:)),
            truncated: wire.truncated ?? false,
            total: wire.total ?? 0,
            failedTotal: wire.failedTotal ?? 0)
    }
}

/// jobCounters is what the Job's spec asked for and what its status counted:
/// the completions wanted, how many pods reached them, how many ran at once and
/// how many failed against the backoff limit.
public func jobCounters(_ job: Job) -> String {
    // The counters are context; the run's outcome is read from its condition,
    // never from succeeded.
    var parts = [
        job.completions > 0
            ? "succeeded \(job.succeeded) of \(job.completions)"
            : "succeeded \(job.succeeded)"
    ]
    if job.parallelism > 0 { parts.append("parallelism \(job.parallelism)") }
    parts.append("failed \(job.failed) of backoff limit \(job.backoffLimit)")
    return parts.joined(separator: " - ")
}
