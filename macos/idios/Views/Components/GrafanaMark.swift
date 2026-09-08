import SwiftUI

/// GrafanaMark is the Grafana flame, drawn in the brand orange -- nominative
/// use of the trademark beside the section it names, and the third
/// documented exception to the app's system-colour rule (CLAUDE.md), beside
/// BadgeStyle.swift and hack/macos/icon.py.
struct GrafanaMark: View {
    var size: CGFloat = 12

    var body: some View {
        GrafanaFlame()
            .fill(Color(red: 0xF4 / 255, green: 0x68 / 255, blue: 0x00 / 255))
            .frame(width: size, height: size)
    }
}

// GrafanaFlame is a redrawn flame silhouette, not the brand's own path
// data, scaled off a 24x24 grid; only the colour is Grafana's. The body is
// kept heavy because the mark is drawn at 12pt, where a thin crescent reads
// as a smudge.
private struct GrafanaFlame: Shape {
    func path(in rect: CGRect) -> Path {
        let scale = rect.width / 24
        func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
            CGPoint(x: rect.minX + x * scale, y: rect.minY + y * scale)
        }
        var path = Path()
        path.move(to: point(12, 1.5))
        path.addCurve(to: point(10.2, 6.9), control1: point(12.6, 4.2), control2: point(11.2, 5.5))
        path.addCurve(to: point(8.9, 12), control1: point(9.1, 8.4), control2: point(8.7, 10.1))
        path.addCurve(to: point(13, 16.4), control1: point(9.1, 14.3), control2: point(10.8, 16.1))
        path.addCurve(
            to: point(17.8, 12.8), control1: point(15.4, 16.7), control2: point(17.5, 15.1))
        path.addCurve(
            to: point(17.6, 10.8), control1: point(17.9, 12.1), control2: point(17.8, 11.5))
        path.addCurve(
            to: point(21.3, 15.9), control1: point(19.6, 12.3), control2: point(20.9, 14.2))
        path.addCurve(
            to: point(13.2, 22.9), control1: point(21.1, 20), control2: point(17.4, 23))
        path.addCurve(
            to: point(4.7, 14.6), control1: point(8.6, 22.8), control2: point(4.9, 19.1))
        path.addCurve(
            to: point(12, 1.5), control1: point(4.5, 10.1), control2: point(8.2, 6))
        path.closeSubpath()
        return path
    }
}
