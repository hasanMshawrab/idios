import AppKit
import IdiosModel
import SwiftUI

/// BadgeStyle is one entry of the badge vocabulary: the only colours the
/// application hardcodes, given for both appearances.
struct BadgeStyle: Sendable {
    let fill: Color
    let text: Color
    let border: Color?
    let borderDash: [CGFloat]?
}

extension Color {
    /// init picks between two fixed colours by the appearance it draws in, so a
    /// badge stays legible in light and dark without an asset catalogue.
    init(light: NSColor, dark: NSColor) {
        self.init(
            nsColor: NSColor(name: nil) { appearance in
                appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua ? dark : light
            })
    }
}

extension BadgeStyle {
    static let red = BadgeStyle(
        fill: fill(light: rgb(0xFF453A, 0.12), dark: rgb(0xFF453A, 0.16)),
        text: pair(light: 0xC0392B, dark: 0xFF453A))
    static let orange = BadgeStyle(
        fill: fill(light: rgb(0xFF9F0A, 0.14), dark: rgb(0xFF9F0A, 0.16)),
        text: pair(light: 0xA35A00, dark: 0xFF9F0A))
    static let blue = BadgeStyle(
        fill: fill(light: rgb(0x0A84FF, 0.12), dark: rgb(0x0A84FF, 0.16)),
        text: pair(light: 0x0A5ECF, dark: 0x0A84FF))
    static let green = BadgeStyle(
        fill: fill(light: rgb(0x30D158, 0.14), dark: rgb(0x30D158, 0.16)),
        text: pair(light: 0x1A7F37, dark: 0x30D158))
    static let grey = BadgeStyle(
        fill: fill(light: black(0.06), dark: rgb(0x8E8E93, 0.20)),
        text: Color(light: black(0.55), dark: rgb(0x8E8E93)))
    static let neutral = BadgeStyle(
        fill: fill(light: black(0.06), dark: rgb(0x8E8E93, 0.20)),
        text: Color(light: black(0.55), dark: rgb(0xA1A1A6)),
        border: Color(light: black(0.22), dark: rgb(0xA1A1A6, 0.35)),
        borderDash: nil)
    static let gap = BadgeStyle(
        fill: fill(light: black(0.05), dark: rgb(0x8E8E93, 0.18)),
        text: Color(light: black(0.60), dark: rgb(0xC7C7CC)),
        border: Color(light: black(0.25), dark: white(0.30)),
        borderDash: [2, 2])

    /// askAI is the Ask AI button's fill, the one gradient in the application:
    /// a request for help is not a state, so it takes no state hue and its two
    /// blues sit outside the vocabulary above.
    static var askAI: LinearGradient {
        LinearGradient(
            colors: [pair(light: 0x1B6FE8, dark: 0x3D8CFF), pair(light: 0x6E4BFF, dark: 0x8F73FF)],
            startPoint: .topLeading, endPoint: .bottomTrailing)
    }

    private init(fill: Color, text: Color) {
        self.init(fill: fill, text: text, border: nil, borderDash: nil)
    }

    private static func fill(light: NSColor, dark: NSColor) -> Color {
        Color(light: light, dark: dark)
    }

    private static func pair(light: UInt32, dark: UInt32) -> Color {
        Color(light: rgb(light), dark: rgb(dark))
    }

    private static func rgb(_ hex: UInt32, _ alpha: Double = 1) -> NSColor {
        NSColor(
            srgbRed: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255,
            alpha: alpha)
    }

    private static func black(_ alpha: Double) -> NSColor {
        NSColor(srgbRed: 0, green: 0, blue: 0, alpha: alpha)
    }

    private static func white(_ alpha: Double) -> NSColor {
        NSColor(srgbRed: 1, green: 1, blue: 1, alpha: alpha)
    }
}

extension IdiosModel.Category {
    /// badge is the chip every category shares: a category is a noun, and the
    /// state beside it carries the colour.
    var badge: BadgeStyle { .neutral }
}

extension IncidentState {
    /// badge is the colour this lifecycle state carries everywhere it is named.
    var badge: BadgeStyle {
        switch self {
        case .open, .attention: .red
        case .acknowledged: .orange
        case .recovered, .podDeleted, .jobFinished, .manual, .dismissed: .grey
        }
    }
}

extension StateTone {
    /// badge is the hue a header's badge takes for its tone.
    var badge: BadgeStyle {
        switch self {
        case .open: .red
        case .acknowledged: .orange
        case .closed: .grey
        }
    }
}

extension ContainerState {
    /// badge is the colour this kubelet state carries where no exit code is in
    /// hand.
    var badge: BadgeStyle { badge(exitCode: nil) }

    /// badge is the colour this kubelet state carries everywhere it is named.
    func badge(exitCode: Int32?) -> BadgeStyle {
        switch self {
        case .running: .green
        case .waiting: .orange
        // A terminated container with no exit code kept is not evidence of
        // success, so only a known zero leaves the degraded colour.
        case .terminated: exitCode == 0 ? .neutral : .orange
        }
    }
}

/// podBadge is the colour a pod row carries when no container is in hand: the
/// phase is the only success the row knows.
func podBadge(worstState: ContainerState, phase: String) -> BadgeStyle {
    phase == "Succeeded" ? .neutral : worstState.badge(exitCode: nil)
}

/// clusterDot is the colour a cluster's dot carries everywhere one is drawn:
/// an error outlives the connection that produced it, so it wins over ready.
func clusterDot(ready: Bool, hasError: Bool) -> Color {
    if hasError { return BadgeStyle.red.text }
    return ready ? BadgeStyle.green.text : BadgeStyle.grey.text
}

extension TimelineEntry {
    /// badge is the colour this entry's dot carries: the kind decides, and
    /// within a kind what the entry says decides.
    var badge: BadgeStyle {
        switch kind {
        case .containerTransition: state?.badge(exitCode: exitCode) ?? .neutral
        case .condition: conditionStatus == "True" ? .green : .orange
        // Warning and Normal are not a stored vocabulary of their own; they
        // borrow the container-state colours the badge vocabulary already fixes.
        case .event: eventType == "Warning" ? ContainerState.waiting.badge : .neutral
        case .capture: .neutral
        case .rollout: .neutral
        case .lifecycle: lifecycle == .closed ? .grey : .red
        case .cut: .neutral
        }
    }
}

/// runCellStyle is the colour one run of the strip carries: the state tones the
/// vocabulary already fixes, with the dashed style for the run whose record the
/// sweeper removed.
func runCellStyle(_ outcome: RunOutcome) -> BadgeStyle {
    switch outcome {
    case .complete: .green
    case .failed(let tone): tone.badge
    case .running: .neutral
    case .swept: .gap
    }
}
