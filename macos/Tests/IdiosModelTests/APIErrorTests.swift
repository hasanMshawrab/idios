import IdiosAPI
import Testing

@testable import IdiosModel

@Test func theErrorBodyBecomesTheNotFoundMessage() throws {
    let error = try APIError(wire: fixture(Components.Schemas._Error.self, "error.json"))
    #expect(error == .notFound(message: "incident 999999 not found"))
}

@Test func theValidationBodyBecomesEveryFieldViolation() throws {
    let error = try APIError(
        wire: fixture(Components.Schemas.ValidationError.self, "validation_error.json"))
    #expect(
        error
            == .badRequest(violations: [
                FieldViolation(field: "state", description: "unknown incident state bogus"),
                FieldViolation(field: "limit", description: "limit must not be negative"),
            ]))
}
