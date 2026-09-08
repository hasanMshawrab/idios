import IdiosModel
import SwiftUI

/// Badge draws one stored value in its vocabulary colour; the text is the value
/// as the daemon stores it.
struct Badge: View {
    let text: String
    let style: BadgeStyle
    var width: CGFloat?

    var body: some View {
        Text(text)
            .font(.system(size: 10.5, weight: .semibold))
            .lineLimit(1)
            .chip(style: style, width: width)
    }
}

extension View {
    /// chip draws the badge shape: the fill, the border a style asks for, and
    /// the text colour everything inside inherits.
    fileprivate func chip(style: BadgeStyle, width: CGFloat?) -> some View {
        padding(.horizontal, 7)
            .padding(.vertical, 2)
            .frame(width: width)
            .background(RoundedRectangle(cornerRadius: 5).fill(style.fill))
            .overlay {
                if let border = style.border {
                    RoundedRectangle(cornerRadius: 5)
                        .strokeBorder(
                            border, style: StrokeStyle(lineWidth: 1, dash: style.borderDash ?? []))
                }
            }
            .foregroundStyle(style.text)
    }
}

/// CategoryBadge names the category the daemon derived from the reason.
struct CategoryBadge: View {
    let category: IdiosModel.Category
    var width: CGFloat?

    var body: some View {
        HStack(spacing: 4) {
            RoundedRectangle(cornerRadius: 1.5)
                .strokeBorder(category.badge.text, lineWidth: 1)
                .frame(width: 6, height: 6)
            Text(category.label)
                .font(.system(size: 10.5, weight: .semibold))
                .lineLimit(1)
        }
        .chip(style: category.badge, width: width)
        .help(category.tooltip)
    }
}

/// StateBadge names the lifecycle state of an incident.
struct StateBadge: View {
    let state: IncidentState
    var width: CGFloat?

    var body: some View {
        Badge(text: state.label, style: state.badge, width: width)
            .help(state.tooltip)
    }
}

/// ContainerStateBadge names a container state with the kubelet's reason when
/// there is one.
struct ContainerStateBadge: View {
    let state: ContainerState
    var reason: String?
    var exitCode: Int32?

    var body: some View {
        Badge(
            text: reason.map { "\(state.rawValue) - \($0)" } ?? state.rawValue,
            style: state.badge(exitCode: exitCode))
    }
}

/// GapBadge names why a capture produced no file; it is drawn dashed because
/// the file is absent, not empty.
struct GapBadge: View {
    let gap: CaptureGap

    var body: some View {
        Badge(text: gap.label, style: .gap)
            .help(gap.rawValue)
    }
}
