import Foundation
import IdiosAPI
import IdiosModel
import OpenAPIRuntime

/// apiError turns anything thrown by a client call into the error a screen can
/// explain: a transport failure reads as not connected, a wire message the
/// model layer rejects reads as a decoding failure naming the field, and a
/// task the app itself stopped reads as cancelled, which no screen shows.
func apiError(_ error: any Error) -> APIError {
    switch error {
    case let error as APIError:
        return error
    case is CancellationError:
        return .cancelled
    case let error as URLError where error.code == .cancelled:
        return .cancelled
    case ModelError.missing(let field):
        return .decoding("missing field \(field)")
    case let error as ClientError:
        // The client wraps the transport failure in a paragraph naming the
        // operation and the request; a person needs the sentence underneath.
        return apiError(error.underlyingError)
    default:
        return .unreachable((error as NSError).localizedDescription)
    }
}

/// apiError reads a daemon error body that arrived with a status code the
/// document does not name.
func apiError(statusCode: Int, body: Components.Schemas._Error) -> APIError {
    statusCode == 404 ? APIError(wire: body) : .unreachable(body.message ?? "HTTP \(statusCode)")
}

extension APIError {
    /// message is the one line a screen puts in front of a person.
    var message: String {
        switch self {
        case .badRequest(let violations):
            return violations.map { "\($0.field): \($0.description)" }.joined(separator: "\n")
        case .notFound(let message):
            return message
        case .unreachable(let description):
            return description
        case .decoding(let description):
            return description
        case .cancelled:
            return ""
        }
    }
}
