import SwiftUI

/// DetailCard is the boxed group every card of the pod page sits in.
/// meta is the card's provenance (a table and a predicate, or a config key);
/// it sits on the title's hover so a value can be traced without SQL on the
/// screen.
struct DetailCard<Content: View>: View {
    var title: String?
    var meta: String?
    var tint: Color?
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            if let title {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    if let meta {
                        Text(title).font(.system(size: 12, weight: .semibold)).help(meta)
                    } else {
                        Text(title).font(.system(size: 12, weight: .semibold))
                    }
                    Spacer(minLength: 0)
                }
            }
            content
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 9).fill(tint ?? Color(nsColor: .textBackgroundColor)))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(.quaternary, lineWidth: 0.5))
    }
}

/// FactRow is one label and one value of a detail grid.
struct FactRow<Value: View>: View {
    let label: String
    var labelWidth: CGFloat = 132
    @ViewBuilder let value: Value

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .frame(width: labelWidth, alignment: .leading)
            value
                .font(.system(size: 11.5))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
