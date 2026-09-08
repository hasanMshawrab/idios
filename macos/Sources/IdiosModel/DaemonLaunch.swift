/// LaunchEnvironment is everything the launch decision looks at, gathered by
/// the application before it decides what to do about the daemon.
public struct LaunchEnvironment: Sendable {
    /// daemonAnswered is whether /v1/status answered on the connection address.
    public let daemonAnswered: Bool
    /// addressOverridden is whether a -daemon launch argument was passed.
    public let addressOverridden: Bool
    /// addressIsDefault is whether the address is the daemon's own default.
    public let addressIsDefault: Bool
    /// screenshot is whether this is a -screenshot run.
    public let screenshot: Bool
    /// bundledBinaryPresent is whether the app bundle carries the daemon.
    public let bundledBinaryPresent: Bool
    /// kubeconfigConfigured is whether the daemon's idios.toml has a kubeconfig.
    public let kubeconfigConfigured: Bool

    /// init gathers the whole decision input.
    public init(
        daemonAnswered: Bool,
        addressOverridden: Bool,
        addressIsDefault: Bool,
        screenshot: Bool,
        bundledBinaryPresent: Bool,
        kubeconfigConfigured: Bool
    ) {
        self.daemonAnswered = daemonAnswered
        self.addressOverridden = addressOverridden
        self.addressIsDefault = addressIsDefault
        self.screenshot = screenshot
        self.bundledBinaryPresent = bundledBinaryPresent
        self.kubeconfigConfigured = kubeconfigConfigured
    }
}

/// LaunchAction is what the application does about the daemon at launch.
public enum LaunchAction: Equatable, Sendable {
    /// connect uses whatever answers on the address, the not-connected screen
    /// included; the application stays a plain client.
    case connect
    /// setup opens the first-run sheet before anything is spawned.
    case setup
    /// spawn starts the bundled daemon.
    case spawn
}

/// spawnPath is the PATH a spawned daemon gets: the inherited one plus the
/// package-manager directories a Finder launch does not carry. Kubeconfig
/// credential plugins (aws, gke-gcloud-auth-plugin) resolve through PATH,
/// and the GUI PATH is only the system directories.
public func spawnPath(inheriting path: String) -> String {
    let usual = ["/usr/local/bin", "/opt/homebrew/bin", "/opt/homebrew/sbin"]
    var parts = path.split(separator: ":").map(String.init)
    for directory in usual where !parts.contains(directory) {
        parts.append(directory)
    }
    return parts.joined(separator: ":")
}

/// launchAction decides connect, setup or spawn, in that order: a running
/// daemon is used as found, a spawn blocker keeps the application a plain
/// client, and setup exists only to precede a spawn.
public func launchAction(_ environment: LaunchEnvironment) -> LaunchAction {
    if environment.daemonAnswered { return .connect }
    let spawnAllowed =
        !environment.addressOverridden && environment.addressIsDefault
        && !environment.screenshot && environment.bundledBinaryPresent
    if !spawnAllowed { return .connect }
    return environment.kubeconfigConfigured ? .spawn : .setup
}
