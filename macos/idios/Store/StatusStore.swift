import Foundation
import IdiosModel
import Observation

/// StatusStore holds the daemon's own snapshot: read once when the window opens
/// so any screen can ask for the retention, and again every ten seconds while
/// the status screen is up.
@Observable @MainActor
final class StatusStore {
    private(set) var status: DaemonStatus?
    private(set) var error: APIError?

    /// retentionDays is how long the daemon keeps a deleted pod's rows and
    /// files; the deleted banners count from it.
    var retentionDays: Int32? { status?.retentionDays }

    /// load reads the snapshot once, as the window opening and a new daemon
    /// address ask for it.
    func load(connection: DaemonConnection) async {
        do {
            let output = try await connection.client.GetStatus()
            switch output {
            case .ok(let ok):
                status = try DaemonStatus(wire: try ok.body.json)
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

    /// poll reloads the snapshot for as long as the status screen is on screen.
    /// The daemon writes it every ten seconds, so asking faster shows the same
    /// numbers twice.
    func poll(connection: DaemonConnection) async {
        while !Task.isCancelled {
            await load(connection: connection)
            do {
                try await Task.sleep(for: .seconds(10))
            } catch {
                return
            }
        }
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a folder change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
