import SwiftUI

/// Flow lays its subviews out left to right and starts a new line when the next
/// one does not fit, so a row of facts grows downwards instead of being cut.
struct Flow: Layout {
    var spacing: CGFloat = 5
    var lineSpacing: CGFloat = 2

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let lines = layout(width: proposal.width ?? .infinity, subviews: subviews)
        let width = lines.map { $0.width }.max() ?? 0
        let height = lines.reduce(0) { $0 + $1.height } + lineSpacing
            * CGFloat(max(lines.count - 1, 0))
        return CGSize(width: width, height: height)
    }

    func placeSubviews(
        in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()
    ) {
        var y = bounds.minY
        for line in layout(width: bounds.width, subviews: subviews) {
            var x = bounds.minX
            for item in line.items {
                subviews[item.index].place(
                    at: CGPoint(x: x, y: y + (line.height - item.size.height) / 2),
                    proposal: ProposedViewSize(item.size))
                x += item.size.width + spacing
            }
            y += line.height + lineSpacing
        }
    }

    private struct Item {
        let index: Int
        let size: CGSize
    }

    private struct Line {
        var items: [Item] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    private func layout(width: CGFloat, subviews: Subviews) -> [Line] {
        var lines = [Line()]
        for index in subviews.indices {
            let size = subviews[index].sizeThatFits(.unspecified)
            var line = lines[lines.count - 1]
            let advance = line.items.isEmpty ? size.width : line.width + spacing + size.width
            if !line.items.isEmpty && advance > width {
                lines.append(Line(items: [Item(index: index, size: size)], width: size.width,
                    height: size.height))
                continue
            }
            line.items.append(Item(index: index, size: size))
            line.width = advance
            line.height = max(line.height, size.height)
            lines[lines.count - 1] = line
        }
        return lines
    }
}
