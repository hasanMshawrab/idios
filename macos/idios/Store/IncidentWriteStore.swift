import Foundation
import IdiosAPI
import IdiosModel
import OpenAPIRuntime

/// IncidentWriteStore is the incident writes (acknowledge, resolve, dismiss,
/// note, delete and their inverses), shared by the list's delete and the pod
/// page's actions so there is one client for them. actionError is the last
/// write's failure, kept apart from any load error so a button's failure
/// shows without hiding the page.
@Observable @MainActor
final class IncidentWriteStore {
    private(set) var actionError: APIError?

    /// acknowledge stamps the incident acknowledged once; the daemon answers
    /// the current row either way.
    func acknowledge(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.AcknowledgeIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    /// unacknowledge clears the acknowledgement; a repeat call is a no-op.
    func unacknowledge(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.UnacknowledgeIncident(path: .init(id: id))
            return try Self.row(output)
        }
    }

    /// resolve closes an open incident as manual; a closed incident keeps its
    /// close.
    func resolve(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.ResolveIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    /// unresolve reopens an incident closed as manual; a row closed any other
    /// way keeps its close.
    func unresolve(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.UnresolveIncident(path: .init(id: id))
            return try Self.row(output)
        }
    }

    /// dismiss stamps the incident dismissed; a repeat call is a no-op.
    func dismiss(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.DismissIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    /// undismiss clears the dismissal; a repeat call is a no-op.
    func undismiss(id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.UndismissIncident(path: .init(id: id))
            return try Self.row(output)
        }
    }

    /// setNote replaces the note; an empty note clears it.
    func setNote(_ note: String, id: String, connection: DaemonConnection) async -> Incident? {
        await write(connection: connection) {
            let output = try await connection.client.SetIncidentNote(
                path: .init(id: id), body: .json(.init(id: id, note: note)))
            return try Self.row(output)
        }
    }

    /// delete removes the incident row and its captured files now; it answers
    /// true only on .ok, so the caller pops before the next reload sees a 404.
    func delete(id: String, connection: DaemonConnection) async -> Bool {
        do {
            let output = try await connection.client.DeleteIncident(path: .init(id: id))
            switch output {
            case .ok:
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

    // The write's response is the row as the daemon now has it; the stream
    // will carry the same row again, but a person expects the button they
    // pressed to show its effect at once.
    private func write(
        connection: DaemonConnection, call: () async throws -> Components.Schemas.IncidentRow
    ) async -> Incident? {
        do {
            let incident = try Incident(wire: try await call())
            reportAction(nil, connection: connection)
            return incident
        } catch {
            reportAction(apiError(error), connection: connection)
            return nil
        }
    }

    // Each write operation generates its own Output type with the same three
    // cases, so unwrapping it is one overload per operation rather than a
    // shared generic (the generator gives these no common protocol).
    private static func row(_ output: Operations.AcknowledgeIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.UnacknowledgeIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.ResolveIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.UnresolveIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.DismissIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.UndismissIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.SetIncidentNote.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    // A write's failure and a load's failure are shown in different places
    // and must not overwrite each other.
    private func reportAction(_ error: APIError?, connection: DaemonConnection) {
        guard error != .cancelled else { return }
        actionError = error
        connection.note(error)
    }
}
