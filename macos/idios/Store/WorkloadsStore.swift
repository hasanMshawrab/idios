import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// WorkloadKey addresses one workload the way the endpoint path spells it.
struct WorkloadKey: Hashable, Sendable {
    let cluster: String
    let namespace: String
    let kind: String
    let name: String
}

extension Workload {
    /// key is this row's address on the workload endpoint.
    var key: WorkloadKey {
        WorkloadKey(
            cluster: clusterID, namespace: namespace, kind: workloadKind, name: workloadName)
    }
}

/// WorkloadsStore holds the workload tree of the current scope, the detail of
/// the selected workload, and the runs and incidents its tabs ask for.
@Observable @MainActor
final class WorkloadsStore {
    private(set) var workloads: [Workload] = []
    private(set) var detail: WorkloadDetail?
    private(set) var isLoading = false
    private(set) var error: APIError?

    /// writes is the one write path the tree has: acknowledging every open
    /// incident of a row.
    let writes = IncidentWriteStore()

    /// podsLimit is what the Pods tab stands for: enough rows to read, and a
    /// truncation the screen says out loud rather than a page that grows with
    /// every pod the workload has ever had.
    static let podsLimit: Int32 = 50
    /// runsLimit bounds the Runs tab, which grows one row per schedule tick.
    static let runsLimit: Int32 = 50
    /// stripLimit bounds the run strip's page: a schedule every two minutes
    /// makes 720 runs a day, and past 200 the strip is an hour-by-minute
    /// matrix that holds a day and more without growing.
    static let stripLimit: Int32 = 2000

    private var loadedScope = ClusterScope.all
    private var runsPage: Page<Job>?
    private var runsKey: WorkloadKey?
    private var stripPage: Page<Job>?
    private var stripKey: WorkloadKey?
    private var incidentRows: [Incident]?
    private var incidentsKey: WorkloadKey?

    /// runs is the page of Job rows loaded for key, nil while the answer for
    /// another workload is all the store holds.
    func runs(of key: WorkloadKey) -> Page<Job>? { runsKey == key ? runsPage : nil }

    /// strip is the unfiltered page of Job rows loaded for key: the run strip
    /// stands for the window and not for the chips the table is set to.
    func strip(of key: WorkloadKey) -> Page<Job>? { stripKey == key ? stripPage : nil }

    /// incidents are the incidents loaded for key, nil until its call answered.
    func incidents(of key: WorkloadKey) -> [Incident]? { incidentsKey == key ? incidentRows : nil }

    /// load fetches the workloads of the scope, as screen entry, a scope change
    /// and the window becoming key ask.
    func load(connection: DaemonConnection, scope: ClusterScope) async {
        loadedScope = scope
        // No cluster in scope is a real answer, and the wire has no way to ask
        // for it: an absent cluster_ids means every cluster.
        guard !scope.isEmpty else {
            workloads = []
            Screenshot.noteFirstLoad()
            return
        }
        isLoading = true
        defer { isLoading = false }
        do {
            let output = try await connection.client.ListWorkloads(
                query: .init(cluster_ids: scope.isAll ? nil : scope.selected.sorted()))
            switch output {
            case .ok(let ok):
                workloads = try Page<Workload>(wire: try ok.body.json).rows
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

    /// loadDetail fetches one workload, with the pods bounded by podsLimit and
    /// live-only when the Pods tab asks for it.
    func loadDetail(_ key: WorkloadKey, podsLive: Bool, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.GetWorkload(
                path: .init(
                    cluster_id: key.cluster, namespace: key.namespace, kind: key.kind,
                    name: key.name),
                query: .init(pods_live: podsLive ? "true" : nil, pods_limit: Self.podsLimit))
            switch output {
            case .ok(let ok):
                detail = try WorkloadDetail(wire: try ok.body.json)
                report(nil, connection: connection)
            case .badRequest(let bad):
                detail = nil
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                detail = nil
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            detail = nil
            report(apiError(error), connection: connection)
        }
        Screenshot.noteFirstLoad()
    }

    /// loadRuns fetches a bounded page of the CronJob's runs under the flags the
    /// Runs tab has engaged, and the totals the page stands for.
    func loadRuns(
        _ key: WorkloadKey, live: Bool, failed: Bool, connection: DaemonConnection
    ) async {
        guard
            let page = await jobsPage(
                key, live: live, failed: failed, limit: Self.runsLimit, connection: connection)
        else { return }
        runsPage = page
        runsKey = key
    }

    /// loadStrip fetches the same runs under no filter at all, which is what the
    /// run strip draws whatever the table's chips are set to.
    func loadStrip(_ key: WorkloadKey, connection: DaemonConnection) async {
        guard
            let page = await jobsPage(
                key, live: false, failed: false, limit: Self.stripLimit, connection: connection)
        else { return }
        stripPage = page
        stripKey = key
    }

    private func jobsPage(
        _ key: WorkloadKey, live: Bool, failed: Bool, limit: Int32, connection: DaemonConnection
    ) async -> Page<Job>? {
        do {
            // The workload row carries no cronjob_uid, so cronjob_name is what
            // ties a run to its workload, a name chain like every other key.
            let output = try await connection.client.ListJobs(
                query: .init(
                    cluster_ids: [key.cluster], namespace: key.namespace,
                    limit: limit, cronjob_name: key.name,
                    live: live ? "true" : nil, failed: failed ? "true" : nil))
            switch output {
            case .ok(let ok):
                let page = try Page<Job>(wire: try ok.body.json)
                report(nil, connection: connection)
                return page
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
        return nil
    }

    /// loadIncidents fetches the incidents of one workload, open and closed. The
    /// screen owns the call rather than the incidents store, which holds the
    /// triage list and its stream.
    func loadIncidents(key: WorkloadKey, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListIncidents(
                query: .init(
                    cluster_ids: [key.cluster], namespace: key.namespace,
                    workload_kind: key.kind, workload_name: key.name))
            switch output {
            case .ok(let ok):
                incidentRows = try Page<Incident>(wire: try ok.body.json).rows
                incidentsKey = key
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

    /// acknowledgeOpen acknowledges every open incident of one workload or one
    /// pod and answers how many it changed, so the tree can say so.
    func acknowledgeOpen(key: WorkloadKey?, podUID: String?, connection: DaemonConnection)
        async -> Int
    {
        // The tree holds counts and not rows, so the ids have to be asked for
        // before any of them can be written.
        let query: Operations.ListIncidents.Input.Query
        if let key {
            query = .init(
                cluster_ids: [key.cluster], state: "open", namespace: key.namespace,
                workload_kind: key.kind, workload_name: key.name)
        } else if let podUID {
            query = .init(state: "open", pod_uid: podUID)
        } else {
            return 0
        }
        var ids: [String] = []
        do {
            let output = try await connection.client.ListIncidents(query: query)
            switch output {
            case .ok(let ok):
                ids = try Page<Incident>(wire: try ok.body.json).rows.map(\.id)
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
                return 0
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
                return 0
            }
        } catch {
            report(apiError(error), connection: connection)
            return 0
        }
        return await acknowledge(ids: ids, connection: connection)
    }

    /// acknowledge acknowledges the incidents a caller already holds the ids of,
    /// which is what a drag over the run strip has.
    func acknowledge(ids: [String], connection: DaemonConnection) async -> Int {
        var done = 0
        for id in ids {
            if await writes.acknowledge(id: id, connection: connection) != nil { done += 1 }
        }
        // The counts the tree draws are the daemon's, not a local guess.
        await load(connection: connection, scope: loadedScope)
        return done
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a folder change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
