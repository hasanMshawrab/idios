import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// GroupFactsStore holds what a group header cannot read off its rows: the
/// live pods of every workload in scope, and the run totals of every CronJob
/// or Job that has a group on screen.
@Observable @MainActor
final class GroupFactsStore {
    private(set) var livePods: [WorkloadKey: Int32] = [:]
    private(set) var runs: [WorkloadKey: (total: Int32, failed: Int32)] = [:]
    private(set) var error: APIError?

    /// facts is the header's view of one workload, nil when nothing is known yet.
    func facts(for key: WorkloadKey) -> GroupFacts? {
        let pods = livePods[key]
        let run = runs[key]
        guard pods != nil || run != nil else { return nil }
        return GroupFacts(livePods: pods, runsTotal: run?.total, runsFailed: run?.failed)
    }

    /// loadWorkloads reads the workload rows of the scope once per watch.
    func loadWorkloads(connection: DaemonConnection, scope: ClusterScope) async {
        // No cluster in scope is a real answer, and the wire has no way to ask
        // for it: an absent cluster_ids means every cluster.
        guard !scope.isEmpty else {
            livePods = [:]
            return
        }
        do {
            let output = try await connection.client.ListWorkloads(
                query: .init(cluster_ids: scope.isAll ? nil : scope.selected.sorted()))
            switch output {
            case .ok(let ok):
                let rows = try Page<Workload>(wire: try ok.body.json).rows
                livePods = Dictionary(
                    rows.map { ($0.key, $0.livePods) }, uniquingKeysWith: { first, _ in first })
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

    /// loadRuns reads one jobs page per key, limit 1, for the totals it carries.
    func loadRuns(keys: Set<WorkloadKey>, connection: DaemonConnection) async {
        // The job list filters runs by their CronJob's name and has no filter
        // for a Job's own name, so a Job key has no page to ask for; its group
        // is the one run its rows already are.
        for key in keys.filter({ $0.kind == "CronJob" }).sorted(by: { $0.name < $1.name }) {
            await loadRun(key: key, connection: connection)
        }
    }

    private func loadRun(key: WorkloadKey, connection: DaemonConnection) async {
        do {
            // The page stands for every run of the CronJob, so one row is
            // enough to read the totals off.
            let output = try await connection.client.ListJobs(
                query: .init(
                    cluster_ids: [key.cluster], namespace: key.namespace, limit: 1,
                    cronjob_name: key.name))
            switch output {
            case .ok(let ok):
                let page = try Page<Job>(wire: try ok.body.json)
                runs[key] = (total: page.total, failed: page.failedTotal)
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
        // A route change, a scope change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
