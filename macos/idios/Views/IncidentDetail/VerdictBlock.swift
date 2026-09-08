import IdiosModel
import SwiftUI

/// VerdictBlock is what the middle pane leads with: the answer, in
/// sentences, before the tags and the fields that support it.
struct VerdictBlock: View {
    let incident: Incident
    let container: Container?
    let artifacts: [Artifact]

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            ForEach(Array(sentences.enumerated()), id: \.offset) { index, sentence in
                Text(sentence)
                    .font(.system(size: 12, weight: index == 0 ? .semibold : .regular))
                    .foregroundStyle(
                        index == 0 ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                    // fixedSize would report an ideal width that overflows the
                    // window; lineLimit alone wraps.
                    .lineLimit(nil)
                    .textSelection(.enabled)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var sentences: [String] {
        verdictSentences(incident: incident, container: container, artifacts: artifacts)
    }
}
