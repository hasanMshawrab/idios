import Foundation

/// GrafanaLabelValue is what a label builder row substitutes: one of the
/// identity fields a link is built for, or a fixed string.
public enum GrafanaLabelValue: Hashable, Sendable {
    case namespace, pod, container, workload, node, cluster
    case text(String)
}

extension GrafanaLabelValue {
    /// placeholder is the LogQL template token this value writes, nil for a
    /// static Text value which writes its own string instead.
    var placeholder: String? {
        switch self {
        case .namespace: "$namespace"
        case .pod: "$pod"
        case .container: "$container"
        case .workload: "$workload"
        case .node: "$node"
        case .cluster: "$cluster"
        case .text: nil
        }
    }

    /// forPlaceholder maps a template token back to its value, nil for
    /// anything else (a static value, or a token this app does not know).
    static func forPlaceholder(_ token: String) -> GrafanaLabelValue? {
        switch token {
        case "$namespace": .namespace
        case "$pod": .pod
        case "$container": .container
        case "$workload": .workload
        case "$node": .node
        case "$cluster": .cluster
        default: nil
        }
    }
}

/// GrafanaPreviewValues is one pod's identity, used to fill the label
/// builder's live preview the same way the daemon fills a served link.
public struct GrafanaPreviewValues: Hashable, Sendable {
    public var namespace: String
    public var pod: String
    public var container: String
    public var workload: String
    public var node: String
    public var cluster: String

    public init(
        namespace: String, pod: String, container: String, workload: String, node: String,
        cluster: String
    ) {
        self.namespace = namespace
        self.pod = pod
        self.container = container
        self.workload = workload
        self.node = node
        self.cluster = cluster
    }

    /// values is this pod mapped onto the placeholder vocabulary `preview`
    /// substitutes.
    public var values: [GrafanaLabelValue: String] {
        [
            .namespace: namespace, .pod: pod, .container: container, .workload: workload,
            .node: node, .cluster: cluster,
        ]
    }
}

/// GrafanaLabelRow is one LogQL matcher of the label builder: a label name
/// and the value source that fills it.
public struct GrafanaLabelRow: Hashable, Sendable {
    public var name: String
    public var value: GrafanaLabelValue

    public init(name: String, value: GrafanaLabelValue) {
        self.name = name
        self.value = value
    }
}

/// GrafanaLabels is the label builder: the LogQL selector template as rows a
/// person edits, and the only writer of the stored string.
public struct GrafanaLabels: Hashable, Sendable {
    public var rows: [GrafanaLabelRow]

    public init(rows: [GrafanaLabelRow]) {
        self.rows = rows
    }

    /// standard is the prefill: the three Kubernetes labels every cluster
    /// starts from, matching the daemon's own default byte for byte.
    public static let standard = GrafanaLabels(rows: [
        GrafanaLabelRow(name: "namespace", value: .namespace),
        GrafanaLabelRow(name: "pod", value: .pod),
        GrafanaLabelRow(name: "container", value: .container),
    ])

    /// init parses a stored selector template into rows; a matcher it
    /// cannot read becomes a Text row carrying the raw value, so nothing
    /// stored is ever lost.
    public init(selector: String) {
        rows = GrafanaLabels.splitMatchers(GrafanaLabels.body(of: selector)).map(GrafanaLabels.parse)
    }

    /// selector renders the rows back into the template the daemon
    /// substitutes; it is the inverse of `init(selector:)`.
    public var selector: String {
        let matchers = rows.map { row in
            "\(row.name)=\"\(GrafanaLabels.escape(GrafanaLabels.rawValue(row.value)))\""
        }
        return "{" + matchers.joined(separator: ", ") + "}"
    }

    /// preview substitutes values into the rows the way the daemon fills a
    /// served link: a row whose value resolves empty is dropped whole.
    public func preview(values: [GrafanaLabelValue: String]) -> String {
        let matchers = rows.compactMap { row -> String? in
            let resolved: String
            if case .text(let text) = row.value {
                resolved = text
            } else {
                resolved = values[row.value] ?? ""
            }
            guard !resolved.isEmpty else { return nil }
            return "\(row.name)=\"\(GrafanaLabels.escape(resolved))\""
        }
        return "{" + matchers.joined(separator: ", ") + "}"
    }

    // rawValue is the unescaped string a row's value writes into the
    // template: the placeholder token, or the Text value itself.
    private static func rawValue(_ value: GrafanaLabelValue) -> String {
        if case .text(let text) = value { return text }
        return value.placeholder ?? ""
    }

    // body strips the outer braces the template always carries; a selector
    // with no braces is treated as already being the body, so a hand-edited
    // string with the braces trimmed still parses.
    private static func body(of selector: String) -> String {
        var trimmed = selector.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.hasPrefix("{") { trimmed.removeFirst() }
        if trimmed.hasSuffix("}") { trimmed.removeLast() }
        return trimmed
    }

    // splitMatchers splits the body on commas outside quotes, so a Text
    // value carrying a comma stays inside its own matcher.
    private static func splitMatchers(_ body: String) -> [String] {
        guard !body.trimmingCharacters(in: .whitespaces).isEmpty else { return [] }
        var matchers: [String] = []
        var current = ""
        var inQuotes = false
        var chars = Array(body)
        var i = 0
        while i < chars.count {
            let c = chars[i]
            if c == "\\", inQuotes, i + 1 < chars.count {
                current.append(c)
                current.append(chars[i + 1])
                i += 2
                continue
            }
            if c == "\"" { inQuotes.toggle() }
            if c == "," && !inQuotes {
                matchers.append(current)
                current = ""
            } else {
                current.append(c)
            }
            i += 1
        }
        matchers.append(current)
        return matchers
    }

    // parse reads one "name=\"value\"" matcher; a matcher with no `=`
    // cannot be split into a name and a value, so the whole raw text
    // becomes a Text row rather than being dropped.
    private static func parse(_ raw: String) -> GrafanaLabelRow {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let eq = trimmed.firstIndex(of: "=") else {
            return GrafanaLabelRow(name: "", value: .text(trimmed))
        }
        let name = String(trimmed[trimmed.startIndex..<eq]).trimmingCharacters(in: .whitespaces)
        var value = String(trimmed[trimmed.index(after: eq)...]).trimmingCharacters(in: .whitespaces)
        if value.hasPrefix("\""), value.hasSuffix("\""), value.count >= 2 {
            value.removeFirst()
            value.removeLast()
        }
        let unescaped = unescape(value)
        if let mapped = GrafanaLabelValue.forPlaceholder(unescaped) {
            return GrafanaLabelRow(name: name, value: mapped)
        }
        return GrafanaLabelRow(name: name, value: .text(unescaped))
    }

    private static func escape(_ value: String) -> String {
        value.replacingOccurrences(of: "\"", with: "\\\"")
    }

    private static func unescape(_ value: String) -> String {
        value.replacingOccurrences(of: "\\\"", with: "\"")
    }
}
