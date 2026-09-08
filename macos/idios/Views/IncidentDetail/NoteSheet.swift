import SwiftUI

/// NoteSheet edits an incident's free-text note; an empty text clears it.
struct NoteSheet: View {
    let save: (String) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var text: String
    private let original: String

    /// init prefills the editor with the incident's current note.
    init(note: String?, save: @escaping (String) -> Void) {
        self.save = save
        self.original = note ?? ""
        _text = State(initialValue: original)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Note").font(.system(size: 13, weight: .semibold))
            TextEditor(text: $text)
                .font(.system(size: 12.5))
                .scrollContentBackground(.hidden)
                .padding(6)
                .frame(minWidth: 420, minHeight: 180)
                .background(RoundedRectangle(cornerRadius: 6).fill(.quaternary.opacity(0.15)))
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(.quaternary))
            HStack {
                Spacer()
                Button("Cancel") { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Save") {
                    save(text)
                    dismiss()
                }
                .keyboardShortcut(.defaultAction)
                .disabled(text == original)
            }
        }
        .padding(20)
    }
}
