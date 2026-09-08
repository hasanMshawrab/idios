import AppKit
import IdiosModel
import SwiftUI

/// CapturedLogsCard is the evidence: a scrubber over the capture attempts, the
/// file in the log pane, and the gap card in place of the pane when there is no
/// file.
struct CapturedLogsCard: View {
    let artifacts: [Artifact]
    let selected: Artifact?
    let contents: [String: Result<Data, APIError>]
    @Binding var wrap: Bool
    let select: (String) -> Void
    /// emptyText says what has no capture, which the pod screen and the incident
    /// screen name differently.
    var emptyText = "No capture is attached to this incident."

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            DetailCard {
                header
                if let selected {
                    // pod.json is overwritten on every capture, so there is no
                    // series of restarts for a scrubber to walk.
                    if selected.kind != .podJSON {
                        LogScrubber(artifacts: artifacts, selected: selected, select: select)
                    }
                    if selected.filePath == nil {
                        gap(selected)
                    } else {
                        pane(selected)
                        footer(selected)
                    }
                } else {
                    Text(emptyText)
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text(selected?.kind == .podJSON ? "pod.json" : "Captured logs")
                .font(.system(size: 12, weight: .semibold))
            if selected?.kind == .podJSON {
                Text("the Pod object as last captured; overwritten on each capture")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Toggle("Wrap", isOn: $wrap)
                .toggleStyle(.checkbox)
                .font(.system(size: 11))
                .help("lines scroll horizontally by default; Wrap folds long lines for reading")
        }
    }

    private func pane(_ artifact: Artifact) -> some View {
        Group {
            switch contents[artifact.id] {
            case .success(let data):
                LogPane(text: logText(data, prettyJSON: artifact.kind == .podJSON), wrap: $wrap)
            case .failure(let error):
                WrapText(value: error.message)
                    .font(.system(size: 11.5, design: .monospaced))
                    .padding(10)
            case nil:
                ProgressView().padding(10)
            }
        }
        .frame(height: 200)
        .frame(maxWidth: .infinity)
        .background(RoundedRectangle(cornerRadius: 7).fill(.quaternary.opacity(0.35)))
    }

    // A missing file is never an empty pane: the row says why, and the note is a
    // quotation of the API's own words, never log content.
    private func gap(_ artifact: Artifact) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Badge(text: "\(artifactLabel(artifact)) - no file", style: .gap)
            VStack(alignment: .leading, spacing: 4) {
                Text("capture_gap = \(artifact.captureGap?.rawValue ?? "null")")
                    .font(.system(size: 11.5, design: .monospaced))
                if let note = artifact.captureNote {
                    WrapText(value: "capture_note: \"\(note)\"")
                        .font(.system(size: 11.5, design: .monospaced))
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 7).fill(.quaternary.opacity(0.35)))
    }

    private func footer(_ artifact: Artifact) -> some View {
        HStack(spacing: 14) {
            Text(size(artifact))
            Text("truncated: \(artifact.truncated ? "yes" : "no")")
            Text("captured_early: \(artifact.capturedEarly ? "yes" : "no")")
            Text("captured \(artifact.capturedAt?.raw ?? "null")")
            Spacer(minLength: 6)
            Button("Copy", action: { copy(artifact) })
                .buttonStyle(.link)
                .disabled(data(artifact) == nil)
        }
        .font(.system(size: 10.5, design: .monospaced))
        .foregroundStyle(.secondary)
    }

    private func size(_ artifact: Artifact) -> String {
        let bytes = artifact.sizeBytes.map(byteCount) ?? "unknown size"
        guard artifact.kind != .podJSON, let data = data(artifact) else { return bytes }
        // A log's last line usually has no trailing newline; counting
        // separators alone would drop it.
        var lines = data.count(where: { $0 == UInt8(ascii: "\n") })
        if let last = data.last, last != UInt8(ascii: "\n") { lines += 1 }
        return "\(plural(lines, "line")) - \(bytes)"
    }

    private func data(_ artifact: Artifact) -> Data? {
        guard case .success(let data) = contents[artifact.id] else { return nil }
        return data
    }

    private func copy(_ artifact: Artifact) {
        guard let data = data(artifact) else { return }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(String(decoding: data, as: UTF8.self), forType: .string)
    }
}

/// LogScrubber is how a person walks the captured files: one body at a time,
/// restart N of M, with the previous and next restart a click away.
private struct LogScrubber: View {
    let artifacts: [Artifact]
    let selected: Artifact
    let select: (String) -> Void

    var body: some View {
        HStack(spacing: 8) {
            Text("Restart")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .fixedSize()
            step(symbol: "chevron.left", to: index - 1)
            Text("\(index + 1) of \(artifacts.count)")
                .font(.system(size: 11, design: .monospaced))
                .monospacedDigit()
                .fixedSize()
                .help(artifactLabel(selected))
            if let capturedAt = selected.capturedAt {
                Text(clockTime(capturedAt, seconds: true))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .fixedSize()
            }
            step(symbol: "chevron.right", to: index + 1)
            // A container that restarted two hundred times needs more cells
            // than the row is wide; the bar scrolls rather than squeezing the
            // count and the legend out of the row.
            ScrollViewReader { proxy in
                ScrollView(.horizontal) { bar }
                    .scrollIndicators(.never)
                    .frame(height: 14)
                    .onAppear { proxy.scrollTo(selected.id, anchor: .center) }
                    .onChange(of: selected.id) { proxy.scrollTo(selected.id, anchor: .center) }
            }
            Text("dashed: no output captured - last cell: current.log")
                .font(.system(size: 10.5))
                .foregroundStyle(.secondary)
                .fixedSize()
        }
    }

    private var index: Int {
        artifacts.firstIndex(of: selected) ?? 0
    }

    private func step(symbol: String, to target: Int) -> some View {
        Button {
            guard artifacts.indices.contains(target) else { return }
            select(artifacts[target].id)
        } label: {
            Image(systemName: symbol)
        }
        .buttonStyle(.borderless)
        .disabled(!artifacts.indices.contains(target))
    }

    private var bar: some View {
        HStack(spacing: 1) {
            ForEach(artifacts) { artifact in
                Button { select(artifact.id) } label: {
                    cell(artifact)
                }
                .buttonStyle(.plain)
                // A plain button takes the space its row offers it; the cell
                // is a fixed four points whatever the row is given.
                .frame(width: 4, height: 14)
                .contentShape(Rectangle())
                .id(artifact.id)
                .help(help(artifact))
            }
        }
        .fixedSize()
    }

    @ViewBuilder private func cell(_ artifact: Artifact) -> some View {
        let shape = RoundedRectangle(cornerRadius: 1)
        ZStack {
            if artifact.id == selected.id {
                shape.fill(Color.accentColor)
            } else if artifact.filePath == nil {
                shape.fill(BadgeStyle.gap.fill)
            } else {
                shape.fill(.quaternary)
            }
            if artifact.filePath == nil, let dash = BadgeStyle.gap.borderDash {
                shape.strokeBorder(
                    BadgeStyle.gap.border ?? BadgeStyle.gap.text,
                    style: StrokeStyle(lineWidth: 1, dash: dash))
            }
        }
        .frame(width: 4, height: 14)
        .fixedSize()
    }

    private func help(_ artifact: Artifact) -> String {
        guard let gap = artifact.captureGap else { return artifactLabel(artifact) }
        return "\(artifactLabel(artifact)) - capture_gap = \(gap.rawValue)"
    }
}
