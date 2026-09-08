import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// ClustersStore holds the cluster list every screen is scoped by and keeps it
/// current from the cluster stream.
@Observable @MainActor
final class ClustersStore {
    private(set) var clusters: [Cluster] = []
    private(set) var isLoading = false
    private(set) var error: APIError?
    /// actionError is the last write's failure, separate from a load failure
    /// so the add-cluster sheet can show it without hiding the loaded list.
    private(set) var actionError: APIError?

    /// clearActionError drops the last write's failure, so a sheet reopening
    /// does not greet a person with the previous attempt's error.
    func clearActionError() {
        actionError = nil
    }

    /// cluster is the row a cluster id names, absent while the list is not in.
    func cluster(id: String) -> Cluster? {
        clusters.first { $0.id == id }
    }

    /// contexts lists the kubeconfig contexts the daemon can add a cluster
    /// from; a daemon built without kube discovery answers 501.
    func contexts(connection: DaemonConnection) async -> [KubeContext] {
        do {
            let output = try await connection.client.ListKubeContexts()
            switch output {
            case .ok(let ok):
                let page = try Page<KubeContext>(wire: try ok.body.json)
                reportAction(nil, connection: connection)
                return page.rows
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return []
    }

    /// namespaces lists one context's namespaces, or nil on failure; the
    /// result itself says whether the Role could list them.
    func namespaces(of context: String, connection: DaemonConnection) async -> KubeNamespaces? {
        do {
            let output = try await connection.client.ListKubeNamespaces(path: .init(context: context))
            switch output {
            case .ok(let ok):
                reportAction(nil, connection: connection)
                return KubeNamespaces(wire: try ok.body.json)
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return nil
    }

    /// add inserts a cluster from a kubeconfig context and applies the row
    /// locally; the cluster stream will also carry it.
    func add(context: String, name: String, connection: DaemonConnection) async -> Cluster? {
        do {
            let output = try await connection.client.AddCluster(
                body: .json(.init(contextName: context, name: name)))
            switch output {
            case .ok(let ok):
                let cluster = try Cluster(wire: try ok.body.json)
                apply(cluster)
                reportAction(nil, connection: connection)
                return cluster
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return nil
    }

    /// addNamespace starts watching name in the cluster and applies the
    /// returned row locally.
    func addNamespace(_ name: String, to clusterID: String, connection: DaemonConnection) async -> Bool
    {
        do {
            let output = try await connection.client.AddWatchedNamespace(
                path: .init(id: clusterID), body: .json(.init(id: clusterID, name: name)))
            switch output {
            case .ok(let ok):
                apply(try Cluster(wire: try ok.body.json))
                reportAction(nil, connection: connection)
                return true
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return false
    }

    /// rename changes a cluster's display name and applies the returned row
    /// locally.
    func rename(_ clusterID: String, to name: String, connection: DaemonConnection) async -> Bool {
        do {
            let output = try await connection.client.RenameCluster(
                path: .init(id: clusterID), body: .json(.init(id: clusterID, name: name)))
            switch output {
            case .ok(let ok):
                apply(try Cluster(wire: try ok.body.json))
                reportAction(nil, connection: connection)
                return true
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return false
    }

    /// setGrafana writes a cluster's Grafana log-link configuration and
    /// applies the returned row locally; an empty url clears all three
    /// fields, which the daemon treats as the same request.
    func setGrafana(
        _ clusterID: String, url: String, datasourceUID: String, selector: String,
        connection: DaemonConnection
    ) async -> Bool {
        do {
            let output = try await connection.client.SetClusterGrafana(
                path: .init(id: clusterID),
                body: .json(
                    .init(
                        id: clusterID, grafanaUrl: url, lokiDatasourceUid: datasourceUID,
                        logSelector: selector)))
            switch output {
            case .ok(let ok):
                apply(try Cluster(wire: try ok.body.json))
                reportAction(nil, connection: connection)
                return true
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return false
    }

    /// previewPod is one of the cluster's own pods, for the label builder's
    /// live preview; nil on no pods or any error, since the preview falls
    /// back to sample values rather than reporting a failure.
    func previewPod(of clusterID: String, connection: DaemonConnection) async
        -> GrafanaPreviewValues?
    {
        do {
            let output = try await connection.client.ListPods(
                query: .init(cluster_ids: [clusterID], limit: 1))
            guard case .ok(let ok) = output else { return nil }
            guard let row = try Page<PodRow>(wire: try ok.body.json).rows.first else { return nil }
            let containerName = await firstContainerName(podUID: row.uid, connection: connection)
            return GrafanaPreviewValues(
                namespace: row.namespace, pod: row.name, container: containerName ?? "",
                workload: row.workloadName, node: row.nodeName ?? "",
                cluster: cluster(id: clusterID)?.name ?? "")
        } catch {
            return nil
        }
    }

    // firstContainerName fetches the pod's detail for its first container's
    // name; the preview shows no container rather than a stale one on any
    // failure here, so it is not reported.
    private func firstContainerName(podUID: String, connection: DaemonConnection) async -> String? {
        do {
            let output = try await connection.client.GetPod(path: .init(uid: podUID))
            guard case .ok(let ok) = output else { return nil }
            return try PodDetail(wire: try ok.body.json).containers.first?.name
        } catch {
            return nil
        }
    }

    /// remove deletes a cluster and drops it locally on success, reconciling
    /// the scope so a selection naming it does not go stale.
    func remove(_ clusterID: String, connection: DaemonConnection, preferences: Preferences) async
        -> Bool
    {
        do {
            let output = try await connection.client.DeleteCluster(path: .init(id: clusterID))
            switch output {
            case .ok:
                clusters.removeAll { $0.id == clusterID }
                preferences.scope = preferences.scope.reconciled(with: clusters)
                reportAction(nil, connection: connection)
                return true
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return false
    }

    /// removeNamespace stops watching name in the cluster and applies the
    /// returned row locally.
    func removeNamespace(_ name: String, from clusterID: String, connection: DaemonConnection) async
        -> Bool
    {
        do {
            let output = try await connection.client.RemoveWatchedNamespace(
                path: .init(id: clusterID, name: name))
            switch output {
            case .ok(let ok):
                apply(try Cluster(wire: try ok.body.json))
                reportAction(nil, connection: connection)
                return true
            case .badRequest(let bad):
                reportAction(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportAction(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportAction(apiError(error), connection: connection)
        }
        return false
    }

    /// watch loads the list, then applies stream rows until it is cancelled,
    /// reloading before every resubscribe because the stream has no replay.
    func watch(connection: DaemonConnection, preferences: Preferences) async {
        while !Task.isCancelled {
            await load(connection: connection, preferences: preferences)
            do {
                let output = try await connection.streamClient.StreamClusters(query: .init())
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.Cluster.self)
                {
                    guard let row = event.data else { continue }
                    apply(try Cluster(wire: row))
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    private func load(connection: DaemonConnection, preferences: Preferences) async {
        isLoading = true
        defer { isLoading = false }
        do {
            let output = try await connection.client.ListClusters(query: .init())
            switch output {
            case .ok(let ok):
                clusters = try Page<Cluster>(wire: try ok.body.json).rows
                preferences.scope = preferences.scope.reconciled(with: clusters)
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
        Screenshot.noteFirstLoad()
    }

    private func apply(_ cluster: Cluster) {
        if let index = clusters.firstIndex(where: { $0.id == cluster.id }) {
            clusters[index] = cluster
        } else {
            clusters.append(cluster)
        }
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a folder change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }

    // Mirrors report, but into actionError: a write's failure and a load's
    // failure are shown in different places and must not overwrite each other.
    private func reportAction(_ error: APIError?, connection: DaemonConnection) {
        guard error != .cancelled else { return }
        actionError = error
        connection.note(error)
    }
}
