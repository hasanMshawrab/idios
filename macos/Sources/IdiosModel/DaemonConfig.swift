import Foundation

/// DaemonConfig reads and rewrites the one daemon setting the application
/// owns: the kubeconfig line of the daemon's idios.toml. Every other line is
/// the daemon's business and is preserved byte for byte.
public enum DaemonConfig {
    /// kubeconfig is the path of the first kubeconfig line, or nil when the
    /// text has none.
    public static func kubeconfig(in text: String) -> String? {
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if let value = value(ofLine: line) { return value }
        }
        return nil
    }

    /// settingKubeconfig is the text with its kubeconfig line replaced, or
    /// appended when there was none.
    public static func settingKubeconfig(to path: String, in text: String) -> String {
        let written = "kubeconfig = \"" + escaped(path) + "\""
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        // Splitting text that ends in a newline leaves one empty trailing
        // element; dropping it makes the join below restore the exact ending.
        if lines.last == "" { lines.removeLast() }
        if let at = lines.firstIndex(where: { value(ofLine: Substring($0)) != nil }) {
            lines[at] = written
        } else {
            lines.append(written)
        }
        return lines.joined(separator: "\n") + "\n"
    }

    // The daemon parses full TOML; the application recognizes only the exact
    // shape it writes itself, a basic string on one line, so anything fancier
    // a person wrote by hand is left alone rather than half-understood.
    private static func value(ofLine line: Substring) -> String? {
        var rest = line.drop(while: { $0 == " " || $0 == "\t" })
        guard rest.hasPrefix("kubeconfig") else { return nil }
        rest = rest.dropFirst("kubeconfig".count).drop(while: { $0 == " " || $0 == "\t" })
        guard rest.hasPrefix("=") else { return nil }
        rest = rest.dropFirst().drop(while: { $0 == " " || $0 == "\t" })
        guard rest.hasPrefix("\"") else { return nil }
        rest = rest.dropFirst()
        var value = ""
        var escapedNext = false
        for character in rest {
            if escapedNext {
                value.append(character)
                escapedNext = false
            } else if character == "\\" {
                escapedNext = true
            } else if character == "\"" {
                return value
            } else {
                value.append(character)
            }
        }
        return nil
    }

    private static func escaped(_ path: String) -> String {
        path.replacingOccurrences(of: "\\", with: "\\\\")
            .replacingOccurrences(of: "\"", with: "\\\"")
    }
}
