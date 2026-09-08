import Testing

@testable import IdiosModel

// The Ask AI menu offers one prompt for a local agent and one snapshot, and
// the snapshot can be saved, so each mode names itself and the file it is
// saved as.
@Test(
    arguments: [
        (PromptMode.mcp, "mcp", "Prompt for a local agent (MCP)", "incident-42-prompt.txt"),
        (PromptMode.snapshot, "snapshot", "Full snapshot", "incident-42-snapshot.txt"),
    ])
func eachPromptModeNamesItselfAndTheFileItIsSavedAs(
    mode: PromptMode, query: String, title: String, fileName: String
) {
    #expect(mode.rawValue == query)
    #expect(mode.title == title)
    #expect(mode.fileName(incident: "42") == fileName)
}

// The explainer beside the MCP prompt shows the entry a person adds to their
// agent's configuration; an installed app is not on PATH, so the entry names
// the command exactly as it resolves on this machine.
@Test(
    arguments: [
        ("idios", "{\"command\": \"idios\", \"args\": [\"mcp\"]}"),
        (
            "/Applications/idios.app/Contents/Resources/idios",
            "{\"command\": \"/Applications/idios.app/Contents/Resources/idios\","
                + " \"args\": [\"mcp\"]}"
        ),
    ])
func theMCPExplainerCarriesTheServerEntry(command: String, want: String) {
    #expect(mcpServerConfig(command: command) == want)
}
