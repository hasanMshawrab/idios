import Foundation
import Testing

@testable import IdiosModel

// presentation.md 14: "the app reads and writes only the kubeconfig line of
// <data_dir>/idios.toml ... and preserves every other line byte for byte".

@Test(
    arguments: [
        ("", nil),
        ("retention_days = 3\n", nil),
        ("kubeconfig = \"/Users/h/.kube/config\"\n", "/Users/h/.kube/config"),
        ("  kubeconfig   =  \"/tmp/c\"\n", "/tmp/c"),
        ("retention_days = 3\nkubeconfig = \"/tmp/c\"\n", "/tmp/c"),
        ("kubeconfig = \"/a dir with spaces/config\"\n", "/a dir with spaces/config"),
        ("kubeconfig = \"/q\\\"uote\\\\back\"\n", "/q\"uote\\back"),
        ("# kubeconfig = \"/commented/out\"\n", nil),
    ])
func theKubeconfigLineIsReadAsTheDaemonWouldReadIt(text: String, want: String?) {
    #expect(DaemonConfig.kubeconfig(in: text) == want)
}

@Test(
    arguments: [
        ("", "/tmp/c", "kubeconfig = \"/tmp/c\"\n"),
        (
            "retention_days = 3\n", "/tmp/c",
            "retention_days = 3\nkubeconfig = \"/tmp/c\"\n"
        ),
        (
            "kubeconfig = \"/old\"\n", "/new",
            "kubeconfig = \"/new\"\n"
        ),
        (
            "api_listen = \"127.0.0.1:7772\"\nkubeconfig = \"/old\"\nretention_days = 3\n",
            "/new",
            "api_listen = \"127.0.0.1:7772\"\nkubeconfig = \"/new\"\nretention_days = 3\n"
        ),
        (
            "retention_days = 3", "/tmp/c",
            "retention_days = 3\nkubeconfig = \"/tmp/c\"\n"
        ),
        ("", "/a dir with spaces/config", "kubeconfig = \"/a dir with spaces/config\"\n"),
        ("", "/q\"uote\\back", "kubeconfig = \"/q\\\"uote\\\\back\"\n"),
    ])
func rewritingTheKubeconfigLeavesEveryOtherLineByteForByte(
    text: String, path: String, want: String
) {
    #expect(DaemonConfig.settingKubeconfig(to: path, in: text) == want)
}

// A path the rewrite escaped must come back as itself, or the daemon reads a
// different file than the person chose.
@Test(arguments: ["/plain/config", "/a dir with spaces/config", "/q\"uote\\back"])
func aWrittenKubeconfigReadsBackAsItself(path: String) {
    let text = DaemonConfig.settingKubeconfig(to: path, in: "retention_days = 3\n")
    #expect(DaemonConfig.kubeconfig(in: text) == path)
}
