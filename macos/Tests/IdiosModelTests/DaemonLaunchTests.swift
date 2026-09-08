import Foundation
import Testing

@testable import IdiosModel

// presentation.md 14: the launch decision, in order: connect when something
// answers or spawning is off the table, setup when the kubeconfig is not
// configured, spawn otherwise.

private func env(
    daemonAnswered: Bool = false,
    addressOverridden: Bool = false,
    addressIsDefault: Bool = true,
    screenshot: Bool = false,
    bundledBinaryPresent: Bool = true,
    kubeconfigConfigured: Bool = true
) -> LaunchEnvironment {
    LaunchEnvironment(
        daemonAnswered: daemonAnswered,
        addressOverridden: addressOverridden,
        addressIsDefault: addressIsDefault,
        screenshot: screenshot,
        bundledBinaryPresent: bundledBinaryPresent,
        kubeconfigConfigured: kubeconfigConfigured)
}

@Test func aRunningDaemonIsUsedAsFound() {
    #expect(launchAction(env(daemonAnswered: true)) == .connect)
    #expect(launchAction(env(daemonAnswered: true, kubeconfigConfigured: false)) == .connect)
}

// Each blocker alone keeps the application a plain client, even with no
// kubeconfig configured: setup exists to precede a spawn, never to precede
// a connection to a daemon someone else configured.
@Test(
    arguments: [
        env(addressOverridden: true, kubeconfigConfigured: false),
        env(addressIsDefault: false, kubeconfigConfigured: false),
        env(screenshot: true, kubeconfigConfigured: false),
        env(bundledBinaryPresent: false, kubeconfigConfigured: false),
    ])
func aSpawnBlockerMeansConnectAndShowNotConnected(environment: LaunchEnvironment) {
    #expect(launchAction(environment) == .connect)
}

@Test func aMissingKubeconfigMeansSetupOnlyWhenSpawningIsAllowed() {
    #expect(launchAction(env(kubeconfigConfigured: false)) == .setup)
}

@Test func aConfiguredKubeconfigAndNoAnswerMeansSpawn() {
    #expect(launchAction(env()) == .spawn)
}

// A Finder launch carries only the system directories, and the kubeconfig's
// credential plugin (aws, gke-gcloud-auth-plugin) resolves through PATH; the
// daemon exec'ing it must see the package-manager directories too.
@Test(
    arguments: [
        (
            "/usr/bin:/bin:/usr/sbin:/sbin",
            "/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin:/opt/homebrew/sbin"
        ),
        (
            "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/opt/homebrew/sbin",
            "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/opt/homebrew/sbin"
        ),
    ])
func aSpawnedDaemonSeesThePackageManagerDirectories(inherited: String, want: String) {
    #expect(spawnPath(inheriting: inherited) == want)
}
