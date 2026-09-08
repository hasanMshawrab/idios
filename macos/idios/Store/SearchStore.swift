import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// SearchStore holds what the palette matches beyond the rows the other
/// stores have: the live pods of the scope, the rows a daemon lookup
/// found for an id or an exact pod name, and the pages a person visited.
@Observable @MainActor
final class SearchStore {
    private(set) var pods: [PodRow] = []
    private(set) var lookupIncidents: [Incident] = []
    private(set) var lookupPods: [PodRow] = []
    private(set) var recent: [SearchCommand] = []
    private(set) var error: APIError?

    /// recentLimit is how many visited pages the empty query lists.
    static let recentLimit = 8

    /// loadPods reads the live pods of the scope once per palette opening.
    func loadPods(connection: DaemonConnection, scope: ClusterScope) async {
        // No cluster in scope is a real answer, and the wire has no way to ask
        // for it: an absent cluster_ids means every cluster.
        guard !scope.isEmpty else {
            pods = []
            return
        }
        do {
            let output = try await connection.client.ListPods(
                query: .init(
                    cluster_ids: scope.isAll ? nil : scope.selected.sorted(), live: "true"))
            switch output {
            case .ok(let ok):
                pods = try Page<PodRow>(wire: try ok.body.json).rows
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    /// lookup asks the daemon for what the held rows may have lost to the
    /// list limit: the incident of an id, and the incidents and pods of an
    /// exact pod name.
    func lookup(query: SearchQuery, connection: DaemonConnection, scope: ClusterScope) async {
        if let id = query.incidentID {
            await fetchIncident(id: id, connection: connection)
            return
        }
        // The daemon matches a pod name whole, so only a name pasted entire is
        // worth a call; a bare word or a suffix is already a client-side match.
        guard query.text.count >= 3, query.text.contains("-") else {
            clearLookup()
            return
        }
        let clusterIDs = scope.isAll || scope.isEmpty ? nil : scope.selected.sorted()
        await fetchIncidents(name: query.text, clusterIDs: clusterIDs, connection: connection)
        await fetchPods(name: query.text, clusterIDs: clusterIDs, connection: connection)
    }

    /// visited records a page the person opened, most recent first, once.
    func visited(_ command: SearchCommand) {
        recent.removeAll { $0.id == command.id }
        recent.insert(command, at: 0)
        if recent.count > SearchStore.recentLimit {
            recent = Array(recent.prefix(SearchStore.recentLimit))
        }
    }

    /// clearLookup drops the last lookup when the query changes.
    func clearLookup() {
        lookupIncidents = []
        lookupPods = []
    }

    private func fetchIncident(id: String, connection: DaemonConnection) async {
        lookupPods = []
        do {
            let output = try await connection.client.GetIncident(path: .init(id: id))
            switch output {
            case .ok(let ok):
                lookupIncidents = [try IncidentDetail(wire: try ok.body.json).incident]
                report(nil, connection: connection)
            case .badRequest(let bad):
                lookupIncidents = []
                report(APIError(wire: try bad.body.json), connection: connection)
            // A typed id that names nothing is a query with no hits, not a
            // failure to tell anyone about.
            case .default(404, _):
                lookupIncidents = []
            case .default(let statusCode, let payload):
                lookupIncidents = []
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            lookupIncidents = []
            report(apiError(error), connection: connection)
        }
    }

    private func fetchIncidents(
        name: String, clusterIDs: [String]?, connection: DaemonConnection
    ) async {
        do {
            let output = try await connection.client.ListIncidents(
                query: .init(cluster_ids: clusterIDs, pod_name: name))
            switch output {
            case .ok(let ok):
                lookupIncidents = try Page<Incident>(wire: try ok.body.json).rows
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    private func fetchPods(
        name: String, clusterIDs: [String]?, connection: DaemonConnection
    ) async {
        do {
            let output = try await connection.client.ListPods(
                query: .init(cluster_ids: clusterIDs, pod_name: name))
            switch output {
            case .ok(let ok):
                lookupPods = try Page<PodRow>(wire: try ok.body.json).rows
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A keystroke, a scope change or the palette closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
