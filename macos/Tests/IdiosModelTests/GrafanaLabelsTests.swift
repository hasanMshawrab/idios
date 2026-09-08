import Testing

@testable import IdiosModel

// grafana.DefaultSelector, internal/grafana/grafana.go: the app mirrors it
// as its own constant and the two must render byte for byte.
private let defaultSelector = "{namespace=\"$namespace\", pod=\"$pod\", container=\"$container\"}"

@Test func standardMatchesTheDaemonDefaultSelectorByteForByte() {
    #expect(GrafanaLabels.standard.selector == defaultSelector)
}

@Test(
    arguments: [
        // The standard prefill.
        defaultSelector,
        // A renamed label keeps its placeholder mapping.
        "{ns=\"$namespace\", pod=\"$pod\", container=\"$container\"}",
        // A static Text row.
        "{namespace=\"$namespace\", pod=\"$pod\", cluster=\"eu-west-1\"}",
        // A Text value carrying a comma and an escaped quote.
        "{namespace=\"$namespace\", cluster=\"eu, west \\\"1\\\"\"}",
    ])
func selectorRoundTripsThroughRows(selector: String) {
    #expect(GrafanaLabels(selector: selector).selector == selector)
}

@Test func unreadableMatcherIsPreservedAsARawTextRow() {
    let labels = GrafanaLabels(selector: "{namespace=\"$namespace\", garbage}")
    #expect(labels.rows == [
        GrafanaLabelRow(name: "namespace", value: .namespace),
        GrafanaLabelRow(name: "", value: .text("garbage")),
    ])
}

@Test func previewSubstitutesAFullValueSet() {
    let values: [GrafanaLabelValue: String] = [
        .namespace: "payments", .pod: "ledger-rollup-29142600-p4x9c", .container: "rollup",
        .workload: "ledger-rollup", .node: "node-a", .cluster: "eu-west-1",
    ]
    let labels = GrafanaLabels(rows: [
        GrafanaLabelRow(name: "namespace", value: .namespace),
        GrafanaLabelRow(name: "pod", value: .pod),
        GrafanaLabelRow(name: "cluster", value: .text("eu-west-1")),
    ])
    #expect(
        labels.preview(values: values)
            == "{namespace=\"payments\", pod=\"ledger-rollup-29142600-p4x9c\", cluster=\"eu-west-1\"}")
}

@Test func previewDropsARowWhoseValueResolvesEmpty() {
    let values: [GrafanaLabelValue: String] = [.namespace: "payments", .pod: "ledger-rollup-p4x9c"]
    #expect(
        GrafanaLabels.standard.preview(values: values)
            == "{namespace=\"payments\", pod=\"ledger-rollup-p4x9c\"}")
}
