import AppKit
import Foundation
import IdiosModel
import Observation

/// DaemonManager owns the bundled daemon: it decides at launch whether to
/// connect, run first-time setup or spawn, and it is the only thing that
/// starts, restarts or stops the daemon process.
@Observable @MainActor
final class DaemonManager {
    /// State says where the daemon the application talks to came from.
    enum State {
        case idle
        case external
        case spawned
    }

    private(set) var state: State = .idle

    /// needsSetup presents the first-run sheet; it turns false when the sheet
    /// is dismissed after setup.
    var needsSetup = false

    private var process: Process?

    /// init arranges for a spawned daemon to be terminated with the app.
    init() {
        NotificationCenter.default.addObserver(
            forName: NSApplication.willTerminateNotification, object: nil, queue: .main
        ) { [weak self] _ in
            // willTerminate arrives on the main thread; the observer closure
            // is just not typed that way.
            MainActor.assumeIsolated { self?.stop() }
        }
    }

    /// start runs the launch decision against the current address. It is keyed
    /// on the connection generation, so a Retry after a spawned daemon died
    /// runs the decision, and the spawn, again.
    func start(connection: DaemonConnection) async {
        if process?.isRunning == true { return }
        let environment = LaunchEnvironment(
            daemonAnswered: await probe(connection: connection),
            addressOverridden: CommandLine.arguments.contains("-daemon"),
            addressIsDefault: connection.address == Preferences.defaultAddress,
            screenshot: Screenshot.requested(),
            bundledBinaryPresent: DaemonManager.bundledBinary != nil,
            kubeconfigConfigured: kubeconfigPath() != nil)
        switch launchAction(environment) {
        case .connect:
            state = environment.daemonAnswered ? .external : .idle
        case .setup:
            needsSetup = true
        case .spawn:
            _ = await spawnAndWait(connection: connection)
        }
    }

    /// kubeconfigPath is the daemon's configured kubeconfig, or nil before
    /// setup has run.
    func kubeconfigPath() -> String? {
        guard let text = try? String(contentsOf: DaemonManager.configFile, encoding: .utf8)
        else { return nil }
        return DaemonConfig.kubeconfig(in: text)
    }

    /// setKubeconfig writes the path into the daemon's idios.toml, keeping
    /// every other line.
    func setKubeconfig(_ path: String) throws {
        let text = (try? String(contentsOf: DaemonManager.configFile, encoding: .utf8)) ?? ""
        try FileManager.default.createDirectory(
            at: DaemonManager.dataDir, withIntermediateDirectories: true,
            attributes: [.posixPermissions: 0o700])
        try DaemonConfig.settingKubeconfig(to: path, in: text)
            .write(to: DaemonManager.configFile, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes(
            [.posixPermissions: 0o600], ofItemAtPath: DaemonManager.configFile.path)
    }

    /// spawnAndWait starts the bundled daemon and waits until it answers,
    /// returning the failure to show or nil on success.
    func spawnAndWait(connection: DaemonConnection) async -> String? {
        guard let binary = DaemonManager.bundledBinary else {
            return "this build does not carry the daemon; start one with idios run"
        }
        let process = Process()
        process.executableURL = binary
        process.arguments = ["run"]
        // A Finder launch carries only the system PATH, and the kubeconfig's
        // credential plugin resolves through PATH inside the daemon.
        var environment = ProcessInfo.processInfo.environment
        environment["PATH"] = spawnPath(inheriting: environment["PATH"] ?? "")
        process.environment = environment
        // The daemon writes its log file under the data directory; the pipes
        // would otherwise fill and stall it once their buffers do.
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            return "starting the daemon failed: \(error.localizedDescription)"
        }
        self.process = process
        state = .spawned
        for _ in 0..<50 {
            if await probe(connection: connection) {
                // The stores gave up while nothing was listening; a new
                // generation restarts them against the daemon that now is.
                connection.retry()
                return nil
            }
            if !process.isRunning {
                self.process = nil
                state = .idle
                return "the daemon exited during startup; its log is in "
                    + DaemonManager.dataDir.path
            }
            try? await Task.sleep(for: .milliseconds(300))
        }
        return "the daemon did not answer on \(connection.address)"
    }

    /// restart stops a spawned daemon and starts it again, for a kubeconfig
    /// change; a daemon this application did not spawn is left alone.
    func restart(connection: DaemonConnection) async -> String? {
        guard state == .spawned, let process else { return nil }
        process.terminate()
        // waitUntilExit off the main actor: SIGTERM shutdown closes watchers
        // and the store, which takes the daemon a moment.
        await withCheckedContinuation { continuation in
            process.terminationHandler = { _ in continuation.resume() }
            if !process.isRunning { process.terminationHandler = nil; continuation.resume() }
        }
        self.process = nil
        state = .idle
        return await spawnAndWait(connection: connection)
    }

    /// stop terminates a spawned daemon; the daemon shuts down cleanly on
    /// SIGTERM.
    func stop() {
        process?.terminate()
        process = nil
    }

    private func probe(connection: DaemonConnection) async -> Bool {
        let url = connection.baseURL.appending(path: "v1/status")
        var request = URLRequest(url: url)
        request.timeoutInterval = 2
        guard let (_, response) = try? await URLSession.shared.data(for: request)
        else { return false }
        return (response as? HTTPURLResponse)?.statusCode == 200
    }

    /// dataDir mirrors the daemon's own default on macOS; the two must name
    /// the same directory or setup writes a file the daemon never reads.
    static let dataDir = FileManager.default.homeDirectoryForCurrentUser
        .appending(path: "Library/Application Support/idios")

    private static let configFile = dataDir.appending(path: "idios.toml")

    /// mcpCommand is how the MCP server is invoked on this machine: the
    /// bundled binary's full path, or plain idios for a build without one,
    /// where idios is expected on PATH.
    static var mcpCommand: String { bundledBinary?.path ?? "idios" }

    // The daemon is a resource, not the app executable; a make app build has
    // no daemon and stays a plain client.
    private static let bundledBinary = Bundle.main.url(forResource: "idios", withExtension: nil)
}
