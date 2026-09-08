import IdiosAPI
import Testing

@testable import IdiosModel

let prodCluster = Cluster(
    id: "1",
    identity: "id-prod",
    name: "prod",
    contextName: "prod",
    apiServerURL: "https://127.0.0.1:26443",
    firstSeenAt: Timestamp("2026-08-27T13:00:00.000000Z"),
    lastConnectedAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    lastError: "connection refused",
    lastErrorAt: Timestamp("2026-08-27T14:38:00.000000Z"),
    namespaces: ["default", "idios-smoke"],
    ready: true,
    lastEventAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    skewSeconds: 1.5,
    grafanaURL: "https://logs.example.grafana.net",
    lokiDatasourceUID: "grafanacloud-logs",
    logSelector: "{namespace=\"$namespace\", pod=\"$pod\", container=\"$container\"}")

let stagingCluster = Cluster(
    id: "2",
    identity: nil,
    name: "staging",
    contextName: "staging",
    apiServerURL: "https://127.0.0.1:26444",
    firstSeenAt: Timestamp("2026-08-27T13:00:00.000000Z"),
    lastConnectedAt: nil,
    lastError: nil,
    lastErrorAt: nil,
    namespaces: [],
    ready: false,
    lastEventAt: nil,
    skewSeconds: nil,
    grafanaURL: "",
    lokiDatasourceUID: "",
    logSelector: "")

@Test func clustersCarryRuntimeStateOnlyWhenTheWatchHasRun() throws {
    let page = try Page<Cluster>(
        wire: fixture(Components.Schemas.ClustersResponse.self, "clusters.json"))
    #expect(page == Page(rows: [prodCluster, stagingCluster], truncated: false))
}

@Test(
    arguments: [
        (ClusterScope.all, ClusterScope.all),
        (ClusterScope(selected: []), ClusterScope(selected: [])),
        (ClusterScope(selected: ["1", "9"]), ClusterScope(selected: ["1"])),
        (ClusterScope(selected: ["9"]), ClusterScope(selected: [])),
        (ClusterScope(selected: ["2"]), ClusterScope(selected: ["2"])),
    ])
func scopeKeepsOnlyClustersTheDaemonStillReports(scope: ClusterScope, want: ClusterScope) {
    #expect(scope.reconciled(with: [prodCluster, stagingCluster]) == want)
}

@Test(
    arguments: [
        (ClusterScope(selected: []), "1", false),
        (ClusterScope.all, "9", true),
        (ClusterScope(selected: ["1"]), "1", true),
        (ClusterScope(selected: ["1"]), "2", false),
    ])
func onlyTheAllScopeOrAMemberPutsAClusterInScope(
    scope: ClusterScope, clusterID: String, want: Bool
) {
    #expect(scope.includes(clusterID) == want)
}

@Test(
    arguments: [
        (ClusterScope.all, "1", ClusterScope.all),
        (ClusterScope(selected: ["1", "2"]), "1", ClusterScope(selected: ["1", "2"])),
        (ClusterScope(selected: ["1"]), "2", ClusterScope(selected: ["1", "2"])),
        (ClusterScope(selected: []), "2", ClusterScope(selected: ["2"])),
    ])
func aWidenedScopeHoldsTheClusterItWasGivenAndKeepsAll(
    scope: ClusterScope, clusterID: String, want: ClusterScope
) {
    #expect(scope.including(clusterID) == want)
}
