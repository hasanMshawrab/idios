import AppKit
import Foundation
import IdiosAPI
import IdiosModel
import Observation
import OpenAPIRuntime

/// MenuBarStore holds the always-visible summary: how many incidents are
/// open, how many need attention, the attention rows and the state of every
/// cluster. It ignores the cluster scope the screens are filtered by, so the
/// count is the whole truth.
@Observable @MainActor
final class MenuBarStore {
    /// openCount is how many incidents are open and unacknowledged: what the
    /// headline says and what the Dock badge carries.
    private(set) var openCount: Int32 = 0

    /// attentionCount is the subline: everything open plus what closed inside
    /// the daemon's window without anyone looking at it.
    private(set) var attentionCount: Int32 = 0

    /// rows are the attention rows the popover draws, up to the daemon's list
    /// limit.
    private(set) var rows: [Incident] = []

    private(set) var clusters: [Cluster] = []
    private(set) var error: APIError?

    private let writes = IncidentWriteStore()

    /// clustersWithError is how many clusters carry a last error.
    var clustersWithError: Int { clusters.filter { $0.lastError != nil }.count }

    /// watch loads the summary and keeps it current from both streams and from
    /// a slow refresh, until it is cancelled.
    func watch(connection: DaemonConnection) async {
        async let refreshed: Void = refresh(connection: connection)
        async let incidents: Void = watchIncidents(connection: connection)
        async let clusters: Void = watchClusters(connection: connection)
        _ = await (refreshed, incidents, clusters)
    }

    // A quiet stream is a quiet cluster, not a dead connection; the refresh is
    // what turns the ages in the cluster lines over on its own.
    private func refresh(connection: DaemonConnection) async {
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(30))
            guard !Task.isCancelled else { return }
            await load(connection: connection)
        }
    }

    private func watchIncidents(connection: DaemonConnection) async {
        while !Task.isCancelled {
            await load(connection: connection)
            do {
                let output = try await connection.streamClient.StreamIncidents(query: .init())
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.IncidentRow.self)
                {
                    guard event.data != nil else { continue }
                    // The rows are ordered by last_seen_at: refetching is
                    // cheaper to be right about than merging one row into the
                    // order.
                    await load(connection: connection)
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    private func watchClusters(connection: DaemonConnection) async {
        while !Task.isCancelled {
            // The stream has no replay, so every subscribe starts from a load.
            await loadClusters(connection: connection)
            do {
                let output = try await connection.streamClient.StreamClusters(query: .init())
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.Cluster.self)
                {
                    guard event.data != nil else { continue }
                    await load(connection: connection)
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    private func load(connection: DaemonConnection) async {
        await loadIncidents(connection: connection)
        await loadCounts(connection: connection)
        await loadClusters(connection: connection)
        Screenshot.noteFirstLoad()
    }

    private func loadIncidents(connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListIncidents(
                query: .init(state: IncidentState.attention.rawValue))
            switch output {
            case .ok(let ok):
                rows = try Page<Incident>(wire: try ok.body.json).rows
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

    private func loadCounts(connection: DaemonConnection) async {
        do {
            let output = try await connection.client.GetIncidentCounts(query: .init())
            switch output {
            case .ok(let ok):
                let counts = try IncidentCounts(wire: try ok.body.json)
                openCount = counts.byState[.open] ?? 0
                attentionCount = counts.byState[.attention] ?? 0
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
        showBadge()
    }

    /// acknowledgeShown acknowledges every open, unacknowledged row behind the
    /// popover's rows and answers how many it changed.
    func acknowledgeShown(connection: DaemonConnection) async -> Int {
        var done = 0
        for row in rows where row.closedAt == nil && row.acknowledgedAt == nil {
            if await writes.acknowledge(id: row.id, connection: connection) != nil { done += 1 }
        }
        // The counts the popover draws are the daemon's, not a local guess.
        await load(connection: connection)
        return done
    }

    // The Dock tile is a process-wide object with no owner of its own; the
    // store that holds the count is the one place that can keep it true, and
    // an empty label is how AppKit spells no badge.
    private func showBadge() {
        NSApp.dockTile.badgeLabel = openCount > 0 ? "\(openCount)" : nil
    }

    private func loadClusters(connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListClusters(query: .init())
            switch output {
            case .ok(let ok):
                clusters = try Page<Cluster>(wire: try ok.body.json).rows
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
        // A route change, a folder change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        // A daemon that goes away must not leave its last number on the Dock.
        if case .unreachable = error {
            openCount = 0
            showBadge()
        }
        connection.note(error)
    }
}
