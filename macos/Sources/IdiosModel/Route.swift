/// Route is one addressable screen, as the -route launch argument and the
/// menu bar spell it.
public enum Route: Hashable, Sendable {
    case incidents(IncidentState?)
    case incident(String)
    case timeline(String)
    case pod(String, PodTab)
    /// run is one Job as a page of its own, keyed by the Job's uid; a
    /// scheduled Job is a run and a standalone one is a Job, and both are
    /// this route.
    case run(String)
    case workloads
    case workload(cluster: String, namespace: String, kind: String, name: String)
    case status
    /// addCluster opens the add-cluster sheet over whatever screen was
    /// showing, for the screenshot script.
    case addCluster
    /// clusters is the clusters-and-namespaces sheet, opened from the menu
    /// bar and from a sidebar cluster row's context menu.
    case clusters
    /// menubar draws the status item popover in a plain window; the window
    /// server cannot photograph the popover itself.
    case menubar
}

/// PodTab is a pane name a pod route carries; its raw value is what the
/// route spells.
public enum PodTab: String, CaseIterable, Hashable, Sendable {
    case containers
    case incidents
    case events
    case conditions
    case logs
    case podJSON = "podjson"
}

extension Route {
    /// init reads a slash-separated route as the -route argument spells it.
    public init?(path: String) {
        let parts = path.split(separator: "/").map(String.init)
        switch parts.first {
        case "incidents":
            guard parts.count <= 2 else { return nil }
            guard parts.count == 2 else {
                self = .incidents(nil)
                return
            }
            guard let state = IncidentState(rawValue: parts[1]) else { return nil }
            self = .incidents(state)
        case "incident" where parts.count == 2:
            self = .incident(parts[1])
        case "timeline" where parts.count == 2:
            self = .timeline(parts[1])
        case "pod" where parts.count == 2:
            self = .pod(parts[1], .containers)
        case "pod" where parts.count == 3:
            guard let tab = PodTab(rawValue: parts[2]) else { return nil }
            self = .pod(parts[1], tab)
        case "run" where parts.count == 2:
            self = .run(parts[1])
        case "workloads" where parts.count == 1:
            self = .workloads
        case "workload" where parts.count == 5:
            self = .workload(
                cluster: parts[1], namespace: parts[2], kind: parts[3], name: parts[4])
        case "status" where parts.count == 1:
            self = .status
        case "addcluster" where parts.count == 1:
            self = .addCluster
        case "clusters" where parts.count == 1:
            self = .clusters
        case "menubar" where parts.count == 1:
            self = .menubar
        default:
            return nil
        }
    }
}

/// incidentRoute is the page an incident opens: the Job's run page when the
/// incident's subject is the Job, the pod page otherwise. A job-subject row
/// with no job uid has no run to open and falls back to its own incident
/// route, which resolves the pod the daemon borrowed for it.
public func incidentRoute(_ incident: Incident) -> Route {
    guard incident.subjectKind == .job, let uid = incident.jobUID, !uid.isEmpty else {
        return .incident(incident.id)
    }
    return .run(uid)
}
