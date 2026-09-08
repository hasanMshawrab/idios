import Foundation
import IdiosAPI

/// Pod is the pod an incident is about.
public struct Pod: Identifiable, Hashable, Sendable {
    public let uid: String
    public let clusterID: String
    public let namespace: String
    public let name: String
    public let nodeName: String?
    public let phase: String
    public let statusReason: String?
    public let statusMessage: String?
    public let qosClass: String?
    public let controllerKind: String?
    public let controllerName: String?
    public let controllerUID: String?
    public let workloadKind: String
    public let workloadName: String
    public let createdAt: Timestamp
    public let startedAt: Timestamp?
    public let firstSeenAt: Timestamp
    public let lastSeenAt: Timestamp
    public let deletionRequestedAt: Timestamp?
    public let deletedAt: Timestamp?
    public let deletionSource: DeletionSource?
    public let deletionReason: DeletionReason?

    public var id: String { uid }
}

extension Pod {
    /// init builds a pod from its wire message.
    public init(wire: Components.Schemas.Pod) throws {
        self.uid = try require(wire.uid, "uid")
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.namespace = try require(wire.namespace, "namespace")
        self.name = try require(wire.name, "name")
        self.nodeName = wire.nodeName
        self.phase = try require(wire.phase, "phase")
        self.statusReason = wire.statusReason
        self.statusMessage = wire.statusMessage
        self.qosClass = wire.qosClass
        self.controllerKind = wire.controllerKind
        self.controllerName = wire.controllerName
        self.controllerUID = wire.controllerUid
        self.workloadKind = wire.workloadKind ?? ""
        self.workloadName = wire.workloadName ?? ""
        self.createdAt = Timestamp(try require(wire.createdAt, "createdAt"))
        self.startedAt = wire.startedAt.map(Timestamp.init)
        self.firstSeenAt = Timestamp(try require(wire.firstSeenAt, "firstSeenAt"))
        self.lastSeenAt = Timestamp(try require(wire.lastSeenAt, "lastSeenAt"))
        self.deletionRequestedAt = wire.deletionRequestedAt.map(Timestamp.init)
        self.deletedAt = wire.deletedAt.map(Timestamp.init)
        self.deletionSource = modelEnum(wire.deletionSource)
        self.deletionReason = modelEnum(wire.deletionReason)
    }
}

/// PodRow is one pod as a list shows it.
public struct PodRow: Identifiable, Hashable, Sendable {
    public let uid: String
    public let clusterID: String
    public let namespace: String
    public let name: String
    public let nodeName: String?
    public let phase: String
    public let statusReason: String?
    public let statusMessage: String?
    public let qosClass: String?
    public let controllerKind: String?
    public let controllerName: String?
    public let controllerUID: String?
    public let workloadKind: String
    public let workloadName: String
    public let createdAt: Timestamp
    public let startedAt: Timestamp?
    public let firstSeenAt: Timestamp
    public let lastSeenAt: Timestamp
    public let deletionRequestedAt: Timestamp?
    public let deletedAt: Timestamp?
    public let deletionSource: DeletionSource?
    public let deletionReason: DeletionReason?
    public let openIncidents: Int32
    public let containerCount: Int32
    public let worstState: ContainerState?

    public var id: String { uid }
}

extension PodRow {
    /// init builds a pod row from its wire row.
    public init(wire: Components.Schemas.PodRow) throws {
        self.uid = try require(wire.uid, "uid")
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.namespace = try require(wire.namespace, "namespace")
        self.name = try require(wire.name, "name")
        self.nodeName = wire.nodeName
        self.phase = try require(wire.phase, "phase")
        self.statusReason = wire.statusReason
        self.statusMessage = wire.statusMessage
        self.qosClass = wire.qosClass
        self.controllerKind = wire.controllerKind
        self.controllerName = wire.controllerName
        self.controllerUID = wire.controllerUid
        self.workloadKind = wire.workloadKind ?? ""
        self.workloadName = wire.workloadName ?? ""
        self.createdAt = Timestamp(try require(wire.createdAt, "createdAt"))
        self.startedAt = wire.startedAt.map(Timestamp.init)
        self.firstSeenAt = Timestamp(try require(wire.firstSeenAt, "firstSeenAt"))
        self.lastSeenAt = Timestamp(try require(wire.lastSeenAt, "lastSeenAt"))
        self.deletionRequestedAt = wire.deletionRequestedAt.map(Timestamp.init)
        self.deletedAt = wire.deletedAt.map(Timestamp.init)
        self.deletionSource = modelEnum(wire.deletionSource)
        self.deletionReason = modelEnum(wire.deletionReason)
        self.openIncidents = wire.openIncidents ?? 0
        self.containerCount = wire.containerCount ?? 0
        self.worstState = modelEnum(wire.worstState)
    }
}

extension Page where Row == PodRow {
    /// init builds a page of pods from its wire response.
    public init(wire: Components.Schemas.PodsResponse) throws {
        self.init(
            rows: try (wire.pods ?? []).map(PodRow.init(wire:)), truncated: wire.truncated ?? false)
    }
}

/// Container is one container of a pod as the kubelet last reported it.
public struct Container: Identifiable, Hashable, Sendable {
    public let id: String
    public let podUID: String
    public let name: String
    public let kind: ContainerKind
    public let image: String?
    public let imageTag: String?
    public let imageID: String?
    public let containerID: String?
    public let cpuRequest: String?
    public let cpuLimit: String?
    public let memRequest: String?
    public let memLimit: String?
    public let cpuRequestMillis: Int64?
    public let cpuLimitMillis: Int64?
    public let memRequestBytes: Int64?
    public let memLimitBytes: Int64?
    public let state: ContainerState
    public let reason: String?
    public let exitCode: Int32?
    public let signal: Int32?
    public let ready: Bool
    public let restartCount: Int32
    public let runningSince: Timestamp?
    public let lastTerminatedReason: String?
    public let lastTerminatedExitCode: Int32?
    public let lastTerminatedSignal: Int32?
    public let lastTerminatedAt: Timestamp?
    public let updatedAt: Timestamp
    public let grafanaURL: String?
}

extension Container {
    /// init builds a container from its wire row.
    public init(wire: Components.Schemas.Container) throws {
        self.id = try require(wire.id, "id")
        self.podUID = try require(wire.podUid, "podUid")
        self.name = try require(wire.name, "name")
        self.kind = try require(modelEnum(wire.kind), "kind")
        self.image = wire.image
        self.imageTag = wire.imageTag
        self.imageID = wire.imageId
        self.containerID = wire.containerId
        self.cpuRequest = wire.cpuRequest
        self.cpuLimit = wire.cpuLimit
        self.memRequest = wire.memRequest
        self.memLimit = wire.memLimit
        self.cpuRequestMillis = parseInt64(wire.cpuRequestMillis)
        self.cpuLimitMillis = parseInt64(wire.cpuLimitMillis)
        self.memRequestBytes = parseInt64(wire.memRequestBytes)
        self.memLimitBytes = parseInt64(wire.memLimitBytes)
        self.state = try require(modelEnum(wire.state), "state")
        self.reason = wire.reason
        self.exitCode = wire.exitCode
        self.signal = wire.signal
        self.ready = wire.ready ?? false
        self.restartCount = wire.restartCount ?? 0
        self.runningSince = wire.runningSince.map(Timestamp.init)
        self.lastTerminatedReason = wire.lastTerminatedReason
        self.lastTerminatedExitCode = wire.lastTerminatedExitCode
        self.lastTerminatedSignal = wire.lastTerminatedSignal
        self.lastTerminatedAt = wire.lastTerminatedAt.map(Timestamp.init)
        self.updatedAt = Timestamp(try require(wire.updatedAt, "updatedAt"))
        self.grafanaURL = wire.grafanaUrl
    }
}

/// PodCondition is the latest reading of one pod condition type.
public struct PodCondition: Hashable, Sendable {
    public let id: String
    public let podUID: String
    public let type: String
    public let status: String
    public let reason: String?
    public let message: String?
    public let k8sTransitionAt: Timestamp?
    public let observedAt: Timestamp
}

extension PodCondition {
    /// init builds a condition from its wire row.
    public init(wire: Components.Schemas.PodCondition) throws {
        self.id = try require(wire.id, "id")
        self.podUID = try require(wire.podUid, "podUid")
        self.type = try require(wire._type, "type")
        self.status = try require(wire.status, "status")
        self.reason = wire.reason
        self.message = wire.message
        self.k8sTransitionAt = wire.k8sTransitionAt.map(Timestamp.init)
        self.observedAt = Timestamp(try require(wire.observedAt, "observedAt"))
    }
}

/// ContainerTransition is one container state change idios recorded.
public struct ContainerTransition: Hashable, Sendable {
    public let id: String
    public let podUID: String
    public let containerName: String
    public let incidentID: String?
    public let image: String?
    public let imageID: String?
    public let containerID: String?
    public let state: ContainerState
    public let reason: String?
    public let exitCode: Int32?
    public let signal: Int32?
    public let restartCount: Int32
    public let category: Category?
    public let k8sStartedAt: Timestamp?
    public let k8sFinishedAt: Timestamp?
    public let observedAt: Timestamp
    public let gapReconstructed: Bool
}

extension ContainerTransition {
    /// init builds a transition from its wire row.
    public init(wire: Components.Schemas.ContainerStateHistory) throws {
        self.id = try require(wire.id, "id")
        self.podUID = try require(wire.podUid, "podUid")
        self.containerName = try require(wire.containerName, "containerName")
        self.incidentID = wire.incidentId
        self.image = wire.image
        self.imageID = wire.imageId
        self.containerID = wire.containerId
        self.state = try require(modelEnum(wire.state), "state")
        self.reason = wire.reason
        self.exitCode = wire.exitCode
        self.signal = wire.signal
        self.restartCount = wire.restartCount ?? 0
        self.category = modelEnum(wire.category)
        self.k8sStartedAt = wire.k8sStartedAt.map(Timestamp.init)
        self.k8sFinishedAt = wire.k8sFinishedAt.map(Timestamp.init)
        self.observedAt = Timestamp(try require(wire.observedAt, "observedAt"))
        self.gapReconstructed = wire.gapReconstructed ?? false
    }
}

/// SiblingPod is another pod of the same controller that idios has seen,
/// with the worst category among its open incidents when one is open.
public struct SiblingPod: Hashable, Sendable {
    public let uid: String
    public let name: String
    public let phase: String?
    public let deletedAt: Timestamp?
    public let restartCount: Int32
    public let ready: Bool
    public let worstOpenCategory: Category?
}

extension SiblingPod {
    /// init builds a sibling from its wire row.
    public init(wire: Components.Schemas.SiblingPod) throws {
        self.uid = try require(wire.uid, "uid")
        self.name = try require(wire.name, "name")
        self.phase = wire.phase
        self.deletedAt = wire.deletedAt.map(Timestamp.init)
        self.restartCount = wire.restartCount ?? 0
        self.ready = wire.ready ?? false
        self.worstOpenCategory = modelEnum(wire.worstOpenCategory)
    }
}

/// PodDetail is everything the pod screen shows about one pod.
public struct PodDetail: Hashable, Sendable {
    public let pod: PodRow
    public let containers: [Container]
    public let conditions: [PodCondition]
    public let incidents: [Incident]
    public let artifacts: [Artifact]
    public let siblings: [SiblingPod]
    public let siblingTotal: Int32
}

extension PodDetail {
    /// init builds the detail from its wire message.
    public init(wire: Components.Schemas.PodDetail) throws {
        self.pod = try PodRow(wire: try require(wire.pod, "pod"))
        self.containers = try (wire.containers ?? []).map(Container.init(wire:))
        self.conditions = try (wire.conditions ?? []).map(PodCondition.init(wire:))
        self.incidents = try (wire.incidents ?? []).map(Incident.init(wire:))
        self.artifacts = try (wire.artifacts ?? []).map(Artifact.init(wire:))
        self.siblings = try (wire.siblings ?? []).map(SiblingPod.init(wire:))
        self.siblingTotal = wire.siblingTotal ?? 0
    }
}

/// PodHistory is the recorded state changes and conditions of one pod.
public struct PodHistory: Hashable, Sendable {
    public let transitions: [ContainerTransition]
    public let conditions: [PodCondition]
}

extension PodHistory {
    /// init builds the history from its wire response.
    public init(wire: Components.Schemas.HistoryResponse) throws {
        self.transitions = try (wire.transitions ?? []).map(ContainerTransition.init(wire:))
        self.conditions = try (wire.conditions ?? []).map(PodCondition.init(wire:))
    }
}
