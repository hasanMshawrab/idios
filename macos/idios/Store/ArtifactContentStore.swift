import Foundation
import IdiosAPI
import IdiosModel
import Observation

/// ArtifactContentStore holds the bytes of the artifacts a person opened. The
/// content endpoint is outside the OpenAPI document, so it is fetched with
/// URLSession against the same base URL.
@Observable @MainActor
final class ArtifactContentStore {
    /// contents is what the artifact content endpoint answered, by artifact id.
    /// A failure is kept so a gap is not asked for again on every redraw.
    private(set) var contents: [String: Result<Data, APIError>] = [:]

    /// content fetches an artifact's bytes once and keeps what came back.
    func content(of artifact: Artifact, connection: DaemonConnection) async {
        guard contents[artifact.id] == nil else { return }
        let url = connection.baseURL.appending(path: "v1/artifacts/\(artifact.id)/content")
        do {
            let (data, response) = try await URLSession.shared.data(from: url)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 200
            guard status != 200 else {
                contents[artifact.id] = .success(data)
                return
            }
            contents[artifact.id] = .failure(contentError(status: status, body: data))
        } catch {
            contents[artifact.id] = .failure(.unreachable((error as NSError).localizedDescription))
        }
    }

    // The daemon answers a capture that produced no file with its own error
    // message; the typed gap is on the artifact row the screen already holds.
    private func contentError(status: Int, body: Data) -> APIError {
        guard let wire = try? JSONDecoder().decode(Components.Schemas._Error.self, from: body)
        else { return .unreachable("HTTP \(status)") }
        return status == 404 ? APIError(wire: wire) : .unreachable(wire.message ?? "HTTP \(status)")
    }
}
