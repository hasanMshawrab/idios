import Foundation
import IdiosModel
import Observation
import SwiftUI

/// ExplainedIDs carries the ids the regions on screen registered up to the
/// screen's root, which is the only view that sees all of them; the coverage
/// check compares them with the screen's table and needs nothing else.
struct ExplainedIDs: PreferenceKey {
    static let defaultValue: Set<RegionID> = []

    static func reduce(value: inout Set<RegionID>, nextValue: () -> Set<RegionID>) {
        value.formUnion(nextValue())
    }
}

/// HelpButtonPlacement is where the [?] sits: its own corner on a screen, and
/// a screen's button row where a popover or a sheet has no free bottom right.
enum HelpButtonPlacement {
    case bottomTrailing, inline
}

/// HelpButton is the round [?] that turns help mode on, and that reads as
/// pressed while it is on, because it is also the way out of it.
struct HelpButton: View {
    let isOn: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: "questionmark")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(isOn ? Color.accentColor : .primary)
                .frame(width: 24, height: 24)
                .background(Circle().fill(.regularMaterial))
                .overlay(Circle().fill(Color.accentColor.opacity(isOn ? 0.2 : 0)))
                .overlay(
                    Circle().strokeBorder(
                        isOn ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.quaternary),
                        lineWidth: isOn ? 1.5 : 1))
        }
        .buttonStyle(.plain)
        .help("Explain this screen (? or Cmd-/)")
    }
}

/// ExplainState is whether help mode is on, over which screen, and which
/// region's note is open; one per window, because a sheet's marks and the
/// marks of the screen behind it must never be up together.
@Observable @MainActor
final class ExplainState {
    private(set) var screen: ExplainedScreen?
    var pinned: RegionID?

    /// openAtLaunch is the -explain argument, which turns help mode on over
    /// whichever screen the -route argument landed on, so a screenshot run
    /// needs no keystroke.
    let openAtLaunch: Bool

    /// menuSerial counts what the Help menu item asked for; the explainable
    /// in front reads it, so the menu opens whichever screen that is without
    /// the menu knowing which.
    private(set) var menuSerial = 0

    /// current is the screen in view: a pushed page's regions depend on a
    /// selection that lives inside it, so the page publishes its screen here
    /// and the window's one explainable reads it.
    var current: ExplainedScreen?

    /// front is the explainable the menu item means: the one that appeared
    /// last and is still on screen. A sheet and a pushed page install their
    /// marks after the screen they cover, so the newest one is the one a
    /// person is looking at.
    var front: UUID?

    /// init reads the launch arguments.
    init() {
        openAtLaunch = Screenshot.explainRequested()
    }

    /// open turns help mode on over one screen.
    func open(_ screen: ExplainedScreen) {
        self.screen = screen
        pinned = nil
    }

    /// toggle turns help mode on over a screen, or off when it is the one
    /// already up.
    func toggle(_ screen: ExplainedScreen) {
        if self.screen == screen {
            close()
        } else {
            open(screen)
        }
    }

    /// close takes help mode down.
    func close() {
        screen = nil
        pinned = nil
    }

    /// requestFromMenu asks the frontmost explainable to toggle itself.
    func requestFromMenu() {
        menuSerial += 1
    }
}

extension View {
    /// explained claims this view as a region help mode marks; the id must
    /// name one of that screen's explanations, which is checked against the
    /// table every time help mode opens.
    func explained(_ id: RegionID) -> some View {
        modifier(ExplainedRegionModifier(id: id))
    }

    /// explained claims this view only when the condition holds, which is how
    /// a list marks the first row of each kind: a List builds and recycles its
    /// rows, so a region hung on every row would put a mark on every line.
    func explained(_ id: RegionID, when condition: Bool) -> some View {
        modifier(ExplainedRegionModifier(id: condition ? id : nil))
    }

    /// explained with an optional id claims the view only when it has one,
    /// for a view whose region depends on what it happens to draw.
    func explained(_ id: RegionID?) -> some View {
        modifier(ExplainedRegionModifier(id: id))
    }

    /// explainable installs the [?] at the root of the view that holds every
    /// region; the screen is read on each pass, because which table a window
    /// draws changes with what is pushed and selected inside it.
    func explainable(
        _ screen: @escaping () -> ExplainedScreen?,
        placement: HelpButtonPlacement = .bottomTrailing
    ) -> some View {
        modifier(Explainable(screen: screen, placement: placement))
    }
}

/// ExplainedRegionModifier is one region's half of help mode: the mark is
/// part of the region's own view, so it moves and clips with the region
/// rather than floating over content that has scrolled away.
private struct ExplainedRegionModifier: ViewModifier {
    let id: RegionID?

    @Environment(ExplainState.self) private var state
    @State private var height: CGFloat = 0

    // A row, a tag or a column header has no corner to spare, so its mark
    // stands beside it; anything taller has room inside its own top right.
    private static let shortest: CGFloat = 30
    private static let mark: CGFloat = 16

    private var region: ExplainedRegion? {
        guard let id, let screen = state.screen else { return nil }
        return explainedRegions(for: screen).first { $0.id == id }
    }

    private var isPinned: Bool { id != nil && state.pinned == id }

    func body(content: Content) -> some View {
        content
            // A transform rather than a value: a region drawn inside another
            // one would otherwise replace the ids its subtree published.
            .transformPreference(ExplainedIDs.self) { ids in
                if let id { ids.insert(id) }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
            .overlay {
                if isPinned {
                    RoundedRectangle(cornerRadius: 5)
                        .strokeBorder(Color.accentColor, lineWidth: 1.5)
                        .padding(-3)
                        .allowsHitTesting(false)
                }
            }
            .overlay(alignment: height < Self.shortest ? .trailing : .topTrailing) {
                if let region {
                    mark(region)
                        .offset(
                            x: height < Self.shortest ? Self.mark + 3 : -3,
                            y: height < Self.shortest ? 0 : 3)
                }
            }
    }

    private func mark(_ region: ExplainedRegion) -> some View {
        Button {
            state.pinned = isPinned ? nil : region.id
        } label: {
            Text("?")
                .font(.system(size: 10, weight: .semibold))
                .foregroundStyle(Color.accentColor)
                .frame(width: Self.mark, height: Self.mark)
                .background(Circle().fill(.regularMaterial))
                .overlay(Circle().strokeBorder(Color.accentColor, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .help(region.title)
        .popover(isPresented: pinnedBinding, arrowEdge: .trailing) {
            ExplainNote(region: region)
        }
    }

    private var pinnedBinding: Binding<Bool> {
        Binding(get: { isPinned }, set: { open in state.pinned = open ? id : nil })
    }
}

/// ExplainNote is what one region says: its name, the stored names behind it
/// and why to look there.
private struct ExplainNote: View {
    let region: ExplainedRegion

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(region.title)
                .font(.system(size: 13, weight: .semibold))
                .foregroundStyle(.primary)
            if !region.tags.isEmpty {
                Flow(spacing: 4, lineSpacing: 4) {
                    ForEach(region.tags, id: \.self) { tag in
                        Text(tag)
                            .font(.system(size: 11, design: .monospaced))
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(RoundedRectangle(cornerRadius: 4).fill(.quaternary))
                    }
                }
            }
            Text(region.sentence)
                .font(.system(size: 12.5))
                .foregroundStyle(.secondary)
                .lineLimit(nil)
        }
        .padding(11)
        .frame(width: 270, alignment: .leading)
    }
}

/// Explainable is the screen root's half of help mode: the [?] that turns it
/// on for this screen, the Esc that turns it off, and the check that the
/// marks on screen are the screen's table.
private struct Explainable: ViewModifier {
    let screen: () -> ExplainedScreen?
    let placement: HelpButtonPlacement

    @Environment(ExplainState.self) private var state
    @State private var token = UUID()
    @State private var launched = false
    @State private var registered: Set<RegionID> = []

    private var target: ExplainedScreen? { screen() }

    private var isUp: Bool { target != nil && state.screen == target }

    func body(content: Content) -> some View {
        content
            .onPreferenceChange(ExplainedIDs.self) { ids in registered = ids }
            // Esc reaches an ancestor of whatever holds the focus, so help
            // mode closes without taking the focus away from the screen.
            .onKeyPress(.escape) {
                guard isUp else { return .ignored }
                state.close()
                return .handled
            }
            .overlay(alignment: .bottomTrailing) {
                // The button stays while help mode is on, because it is the
                // way out of it; a screen with no table has nothing to open.
                if let target, placement == .bottomTrailing {
                    HelpButton(isOn: isUp) { state.toggle(target) }
                        .padding(12)
                }
            }
            .onAppear {
                state.front = token
                openAtLaunch()
            }
            // The window is on screen before the page that names the screen
            // is, so a launch that asks for help mode waits for the table. A
            // screen whose table changes while help mode is up -- the last row
            // of a list closing -- carries it to the new table; nothing is
            // raised when help mode was off, which nil == nil would say it was.
            .onChange(of: target) { old, new in
                openAtLaunch()
                if let new, state.screen != nil, state.screen == old { state.open(new) }
            }
            // A task keyed on the screen is cancelled when the screen
            // changes, so a table help mode passed through on the way to the
            // one the data settled on never reports.
            .task(id: state.screen) {
                guard let screen = state.screen, screen == target else { return }
                // The ids climb on the passes that follow the one help mode
                // opened on, and a conditional row settles with the data.
                try? await Task.sleep(for: .milliseconds(600))
                guard !Task.isCancelled else { return }
                Screenshot.reportCoverage(screen: screen, registered: registered)
                Screenshot.noteHelpShown()
            }
            .onDisappear {
                if state.front == token { state.front = nil }
            }
            .onChange(of: state.menuSerial) {
                guard state.front == token, let target else { return }
                state.toggle(target)
            }
    }

    private func openAtLaunch() {
        guard state.openAtLaunch, !launched, state.screen == nil, let target else { return }
        launched = true
        state.open(target)
    }
}
