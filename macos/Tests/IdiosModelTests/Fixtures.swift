import Foundation

// The fixtures are the Go testdata directory; the Swift tests read the same
// bytes the daemon serves rather than hand-written JSON.
func fixture<Wire: Decodable>(_ type: Wire.Type, _ name: String) throws -> Wire {
    var dir = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
    while dir.path != "/" {
        let candidate = dir.appendingPathComponent("api/testdata/\(name)")
        if FileManager.default.fileExists(atPath: candidate.path) {
            return try JSONDecoder().decode(Wire.self, from: Data(contentsOf: candidate))
        }
        dir = dir.deletingLastPathComponent()
    }
    throw CocoaError(.fileNoSuchFile)
}
