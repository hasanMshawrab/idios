import AppKit
import SwiftUI

/// LogPane is the fixed-width treatment: whole lines, horizontal scrolling until
/// a person asks for wrapping, the system find bar, and selection and copy that
/// behave like every other text view on the system.
struct LogPane: NSViewRepresentable {
    let text: String
    @Binding var wrap: Bool

    func makeNSView(context: Context) -> NSScrollView {
        let scrollView = NSTextView.scrollableTextView()
        scrollView.hasVerticalScroller = true
        scrollView.hasHorizontalScroller = true
        scrollView.autohidesScrollers = false
        scrollView.borderType = .noBorder
        scrollView.drawsBackground = false
        guard let textView = scrollView.documentView as? NSTextView else { return scrollView }
        textView.isEditable = false
        textView.isSelectable = true
        textView.drawsBackground = false
        textView.usesFindBar = true
        textView.isIncrementalSearchingEnabled = true
        textView.font = NSFont.monospacedSystemFont(ofSize: 11.5, weight: .regular)
        textView.textContainerInset = NSSize(width: 10, height: 8)
        apply(textView: textView, scrollView: scrollView)
        return scrollView
    }

    func updateNSView(_ scrollView: NSScrollView, context: Context) {
        guard let textView = scrollView.documentView as? NSTextView else { return }
        if textView.string != text { textView.string = text }
        apply(textView: textView, scrollView: scrollView)
    }

    // A text container that tracks the view's width is what wraps a line; one
    // that does not is what makes the view wide enough to scroll sideways.
    private func apply(textView: NSTextView, scrollView: NSScrollView) {
        let unbounded = CGFloat.greatestFiniteMagnitude
        textView.maxSize = NSSize(width: unbounded, height: unbounded)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = !wrap
        textView.textContainer?.widthTracksTextView = wrap
        textView.textContainer?.containerSize = NSSize(
            width: wrap ? scrollView.contentSize.width : unbounded, height: unbounded)
        textView.autoresizingMask = wrap ? [.width] : []
        scrollView.hasHorizontalScroller = !wrap
        if wrap { textView.frame.size.width = scrollView.contentSize.width }
    }
}

/// logText renders captured bytes for the pane; pod.json is pretty-printed when
/// it parses, and served as stored when it does not.
func logText(_ data: Data, prettyJSON: Bool) -> String {
    let raw = String(decoding: data, as: UTF8.self)
    guard prettyJSON else { return raw }
    guard let value = try? JSONSerialization.jsonObject(with: data),
        let pretty = try? JSONSerialization.data(
            withJSONObject: value, options: [.prettyPrinted, .sortedKeys])
    else { return raw }
    return String(decoding: pretty, as: UTF8.self)
}
