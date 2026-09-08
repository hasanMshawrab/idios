import IdiosAPI

/// ModelError is a wire message the model layer cannot turn into a model.
public enum ModelError: Error, Hashable, Sendable {
    /// missing names the field the message had to carry and did not.
    case missing(field: String)
}

/// FieldViolation is one rejected request field.
public struct FieldViolation: Hashable, Sendable {
    public let field: String
    public let description: String
}

extension FieldViolation {
    /// init builds a violation from its wire message.
    public init(wire: Components.Schemas.FieldViolation) {
        self.field = wire.field
        self.description = wire.description
    }
}

/// APIError is a call to the daemon that a screen must explain to a person.
public enum APIError: Error, Hashable, Sendable {
    case badRequest(violations: [FieldViolation])
    case notFound(message: String)
    case unreachable(String)
    case decoding(String)
    /// cancelled is the caller having stopped waiting; there is nothing to show.
    case cancelled
}

extension APIError {
    /// init reads the daemon's error body for a row the daemon does not have.
    public init(wire: Components.Schemas._Error) {
        self = .notFound(message: wire.message ?? "")
    }

    /// init reads the daemon's validation body for a rejected request.
    public init(wire: Components.Schemas.ValidationError) {
        self = .badRequest(violations: wire.violations.map(FieldViolation.init(wire:)))
    }
}

func require<T>(_ value: T?, _ field: String) throws -> T {
    guard let value else { throw ModelError.missing(field: field) }
    return value
}
