import Foundation

/// PromptMode is which prompt the daemon renders for an incident: the short one
/// for a local agent that pulls its own evidence over the idios MCP server, and
/// the self-contained snapshot for an agent that cannot reach this machine.
public enum PromptMode: String, Sendable, CaseIterable {
    case mcp
    case snapshot

    /// title names the prompt in front of a person.
    public var title: String {
        switch self {
        case .mcp: "Prompt for a local agent (MCP)"
        case .snapshot: "Full snapshot"
        }
    }

    /// fileName is what a saved prompt is called before a person renames it.
    public func fileName(incident id: String) -> String {
        switch self {
        case .mcp: "incident-\(id)-prompt.txt"
        case .snapshot: "incident-\(id)-snapshot.txt"
        }
    }
}

/// mcpServerConfig is the entry a person adds to their agent's MCP
/// configuration to give it the read-only idios server. The command is the
/// binary as it resolves on this machine: an installed app is not on PATH,
/// so the entry carries the bundled binary's full path.
public func mcpServerConfig(command: String) -> String {
    let escaped = command
        .replacingOccurrences(of: "\\", with: "\\\\")
        .replacingOccurrences(of: "\"", with: "\\\"")
    return "{\"command\": \"" + escaped + "\", \"args\": [\"mcp\"]}"
}
