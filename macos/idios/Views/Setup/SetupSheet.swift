import AppKit
import SwiftUI

/// SetupSheet is the first run: the kubeconfig the daemon will read, which
/// cannot be skipped, then the first cluster, which can.
struct SetupSheet: View {
    let manager: DaemonManager
    let clusters: ClustersStore

    @Environment(DaemonConnection.self) private var connection

    private enum Step {
        case kubeconfig
        case addCluster
    }

    @State private var step: Step = .kubeconfig
    @State private var path = SetupSheet.suggestedKubeconfig()
    @State private var error: String?
    @State private var isStarting = false

    var body: some View {
        switch step {
        case .kubeconfig: kubeconfigStep
        case .addCluster: AddClusterSheet(store: clusters, cancelLabel: "Skip")
        }
    }

    private var kubeconfigStep: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Welcome to idios").font(.system(size: 13, weight: .semibold))
            Text(
                "idios watches your clusters and records pod and job failures"
                    + " with their evidence. It needs a kubeconfig to reach them;"
                    + " only the path is kept, credentials stay in the file."
            )
            .font(.system(size: 11.5))
            .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 4) {
                Text("Kubeconfig")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(.secondary)
                HStack {
                    TextField("/path/to/kubeconfig", text: $path)
                    Button("Browse...") { browse() }
                }
            }
            if let error {
                Text(error)
                    .font(.system(size: 11.5))
                    .foregroundStyle(BadgeStyle.red.text)
            }
            HStack {
                Spacer()
                if isStarting { ProgressView().controlSize(.small) }
                Button("Continue") { Task { await start() } }
                    .keyboardShortcut(.defaultAction)
                    .disabled(path.isEmpty || isStarting)
            }
        }
        .padding(20)
        .frame(minWidth: 440)
    }

    private func browse() {
        if let chosen = SetupSheet.chooseKubeconfig() {
            path = chosen
        }
    }

    /// chooseKubeconfig runs the open panel every kubeconfig choice uses,
    /// here and in Settings.
    static func chooseKubeconfig() -> String? {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = false
        // ~/.kube is a dotfile directory; a picker that hides it would hide
        // the answer.
        panel.showsHiddenFiles = true
        panel.directoryURL = FileManager.default.homeDirectoryForCurrentUser
            .appending(path: ".kube")
        guard panel.runModal() == .OK, let url = panel.url else { return nil }
        return url.path
    }

    private func start() async {
        let expanded = NSString(string: path).expandingTildeInPath
        guard FileManager.default.isReadableFile(atPath: expanded) else {
            error = "no readable file at \(expanded)"
            return
        }
        isStarting = true
        defer { isStarting = false }
        do {
            try manager.setKubeconfig(expanded)
        } catch {
            self.error = "writing the setting failed: \(error.localizedDescription)"
            return
        }
        if let failure = await manager.spawnAndWait(connection: connection) {
            error = failure
            return
        }
        step = .addCluster
    }

    private static func suggestedKubeconfig() -> String {
        let usual = NSString(string: "~/.kube/config").expandingTildeInPath
        return FileManager.default.isReadableFile(atPath: usual) ? usual : ""
    }
}
