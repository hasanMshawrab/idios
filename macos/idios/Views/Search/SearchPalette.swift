import IdiosModel
import SwiftUI

/// SearchPalette is what Cmd-K opens: one field, the sections that match it,
/// and the three ways out of a hit.
struct SearchPalette: View {
    let sections: [SearchSectionHits]
    let now: Date
    @Binding var text: String
    let perform: (SearchAction) -> Void
    let copy: (String) -> Void
    let dismiss: () -> Void

    @Environment(ExplainState.self) private var explain

    @State private var selectedID: String?
    @FocusState private var focused: Bool

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .top) {
                // The dim is the palette's own shade, not a value of the
                // badge vocabulary: black with an alpha is what a macOS sheet
                // backdrop is, in either appearance.
                Color.black.opacity(0.25)
                    .onTapGesture { dismiss() }
                panel(height: geometry.size.height)
                    .frame(width: 640)
                    .padding(.top, geometry.size.height * 0.12)
            }
            .frame(width: geometry.size.width, height: geometry.size.height)
        }
        .ignoresSafeArea()
        .onAppear {
            selectedID = hits.first?.id
            focused = true
        }
        .onChange(of: sections) { selectedID = hits.first?.id }
    }

    private var hits: [SearchHit] { sections.flatMap(\.hits) }

    private var selectedHit: SearchHit? { hits.first { $0.id == selectedID } }

    private func panel(height: CGFloat) -> some View {
        VStack(spacing: 0) {
            field.explained("queryField")
            Divider()
            // A ScrollView takes every point it is offered, so the panel
            // would stand its full height over one hit; its ideal size is the
            // hits it holds, capped at the window's.
            results
                .frame(maxHeight: max(height * 0.55, 160))
                .fixedSize(horizontal: false, vertical: true)
            Divider()
            footer
        }
        .background(RoundedRectangle(cornerRadius: 12).fill(.regularMaterial))
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .shadow(radius: 24, y: 10)
        // The palette is itself an overlay, so it owns its own help mode
        // rather than the screen behind it, and its [?] sits in the footer
        // because the panel's bottom right is the keys.
        .explainable({ .palette }, placement: .inline)
    }

    private var field: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField("Search", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 15))
                .focused($focused)
                .onKeyPress(.downArrow) { move(by: 1) }
                .onKeyPress(.upArrow) { move(by: -1) }
                .onKeyPress(.escape) {
                    dismiss()
                    return .handled
                }
                .onKeyPress(keys: [.return]) { press in open(modifiers: press.modifiers) }
            Text("esc")
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 5)
                .padding(.vertical, 1)
                .background(RoundedRectangle(cornerRadius: 4).fill(.quaternary.opacity(0.5)))
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
    }

    private var results: some View {
        ScrollViewReader { proxy in
            ScrollView {
                resultRows
            }
            // The keys move the selection past the visible rows; the list
            // follows it the way a menu follows its highlight.
            .onChange(of: selectedID) { _, id in
                if let id { proxy.scrollTo(id) }
            }
        }
    }

    private var resultRows: some View {
        LazyVStack(alignment: .leading, spacing: 1) {
            ForEach(sections, id: \.section) { section in
                header(section)
                ForEach(section.hits) { hit in
                    SearchHitRow(hit: hit, now: now, selected: hit.id == selectedID)
                        .id(hit.id)
                        .onTapGesture {
                            // A first click chooses the row a second one
                            // opens, as a hit reached by the keys is.
                            if selectedID == hit.id {
                                activate(hit, modifiers: [])
                            } else {
                                selectedID = hit.id
                                focused = true
                            }
                        }
                }
            }
        }
        .padding(8)
    }

    private func header(_ section: SearchSectionHits) -> some View {
        HStack {
            Text(section.section.title.uppercased())
                .font(.system(size: 10, weight: .semibold))
                .kerning(0.5)
            Spacer()
            if section.more > 0 {
                Text("+\(section.more) more").font(.system(size: 10.5))
            }
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 9)
        .padding(.top, 8)
        .padding(.bottom, 3)
    }

    private var footer: some View {
        HStack(spacing: 14) {
            HStack(spacing: 14) {
                hint(key: "return", text: "open")
                hint(key: "cmd-return", text: "open in Workloads")
                hint(key: "shift-return", text: "copy name")
                Text("type # for an incident id")
            }
            .explained("footerKeys")
            Spacer()
            HelpButton(isOn: explaining) { explain.toggle(.palette) }
        }
        .font(.system(size: 11))
        .foregroundStyle(.secondary)
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
    }

    private var explaining: Bool { explain.screen == .palette }

    private func hint(key: String, text: String) -> some View {
        HStack(spacing: 4) {
            KeySymbols(key: key)
            Text(text)
        }
    }

    private func move(by step: Int) -> KeyPress.Result {
        let rows = hits
        guard !rows.isEmpty else { return .handled }
        let current = rows.firstIndex { $0.id == selectedID } ?? 0
        let next = min(max(current + step, 0), rows.count - 1)
        selectedID = rows[next].id
        return .handled
    }

    private func open(modifiers: EventModifiers) -> KeyPress.Result {
        guard let hit = selectedHit else { return .handled }
        activate(hit, modifiers: modifiers)
        return .handled
    }

    private func activate(_ hit: SearchHit, modifiers: EventModifiers) {
        if modifiers.contains(.command) {
            guard let action = hit.workloadAction else { return }
            perform(action)
        } else if modifiers.contains(.shift) {
            guard let text = hit.copyText else { return }
            copy(text)
        } else {
            perform(hit.action)
        }
        dismiss()
    }
}
