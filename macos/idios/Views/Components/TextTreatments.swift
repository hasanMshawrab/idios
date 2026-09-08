import AppKit
import IdiosModel
import SwiftUI

/// WrapText is the treatment for prose a person reads: the row grows and
/// nothing is hidden.
struct WrapText: View {
    let value: String

    var body: some View {
        Text(value)
            .fixedSize(horizontal: false, vertical: true)
            .textSelection(.enabled)
            .help(value)
    }
}

/// MiddleElidedText is the treatment for identifiers whose ends carry the
/// information: the middle goes, hover brings the whole value back and a copy
/// control puts the stored value on the clipboard.
struct MiddleElidedText: View {
    let value: String
    /// full is the stored value when what is shown is only a part of it, as a
    /// pod name suffix is part of a pod name.
    var full: String?
    var keeping = 24

    @State private var hovering = false

    private var stored: String { full ?? value }

    var body: some View {
        HStack(spacing: 3) {
            Text(hovering ? stored : middleElided(value, keeping: keeping))
                .underline(true, pattern: .dot)
                .textSelection(.enabled)
            if hovering {
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(stored, forType: .string)
                } label: {
                    Image(systemName: "doc.on.doc")
                        .font(.system(size: 9))
                }
                .buttonStyle(.borderless)
                .help("Copy the full value")
            }
        }
        .help(stored)
        .onHover { hovering = $0 }
    }
}

/// ExpandableText is the treatment for values usually short and sometimes long:
/// one line until a click opens it in place.
struct ExpandableText: View {
    let value: String

    @State private var expanded = false

    var body: some View {
        Text(value)
            .lineLimit(expanded ? nil : 1)
            .truncationMode(.tail)
            .fixedSize(horizontal: false, vertical: expanded)
            .textSelection(.enabled)
            .help(value)
            .onTapGesture { expanded.toggle() }
    }
}
