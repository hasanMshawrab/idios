import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// PromptStore holds the prompt the daemon renders for one incident. The prompt
/// endpoint is outside the OpenAPI document, so it is fetched with URLSession
/// against the same base URL.
@Observable @MainActor
final class PromptStore {
    /// text is the prompt as the daemon rendered it, absent until it arrives.
    private(set) var text: String?
    private(set) var error: APIError?
    private(set) var isLoading = false

    /// size is what the person is told before copying or saving. The daemon
    /// sends the length in the header and URLSession has already counted it, so
    /// the bytes on hand are the size.
    var size: String? {
        text.map { byteCount(Int64($0.utf8.count)) }
    }

    /// load fetches one mode's prompt for one incident.
    func load(id: String, mode: PromptMode, connection: DaemonConnection) async {
        text = nil
        error = nil
        isLoading = true
        defer { isLoading = false }
        var url = connection.baseURL.appending(path: "v1/incidents/\(id)/prompt")
        url.append(queryItems: [URLQueryItem(name: "mode", value: mode.rawValue)])
        do {
            let (data, response) = try await URLSession.shared.data(from: url)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 200
            guard status != 200 else {
                text = String(decoding: data, as: UTF8.self)
                return
            }
            report(promptError(status: status, body: data), connection: connection)
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    // The daemon answers a rejected mode with its violations and an unknown
    // incident with a message, both as JSON, exactly as the endpoints inside
    // the document do.
    private func promptError(status: Int, body: Data) -> APIError {
        let decoder = JSONDecoder()
        if status == 400,
            let wire = try? decoder.decode(Components.Schemas.ValidationError.self, from: body)
        {
            return APIError(wire: wire)
        }
        guard let wire = try? decoder.decode(Components.Schemas._Error.self, from: body) else {
            return .unreachable("HTTP \(status)")
        }
        return status == 404 ? APIError(wire: wire) : .unreachable(wire.message ?? "HTTP \(status)")
    }

    private func report(_ error: APIError, connection: DaemonConnection) {
        // Closing the sheet cancels the fetch; that is the app itself, not the
        // daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
