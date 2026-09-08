import IdiosModel
import SwiftUI

/// RootScreen is one of the screens the sidebar switches the window between.
enum RootScreen: CaseIterable, Hashable, Sendable {
    case incidents
    case workloads
    case status

    /// title names the screen in the sidebar.
    var title: String {
        switch self {
        case .incidents: "Incidents"
        case .workloads: "Workloads"
        case .status: "Status"
        }
    }

    /// symbol is the sidebar glyph of the screen.
    var symbol: String {
        switch self {
        case .incidents: "exclamationmark.triangle"
        case .workloads: "square.stack.3d.up"
        case .status: "waveform.path.ecg"
        }
    }
}

/// SidebarItem is one selectable row of the source list.
enum SidebarItem: Hashable {
    case screen(RootScreen)
    case view(IncidentState)
    case category(IdiosModel.Category)
}

/// IncidentsSidebar is the source list: the screens on every screen, and the
/// incident views and categories while the list is what the window shows.
struct IncidentsSidebar: View {
    let counts: IncidentCounts?
    let attentionWindowSeconds: Int32?
    let showFilters: Bool
    @Binding var filter: IncidentFilter
    @Binding var screen: RootScreen
    let openStatus: () -> Void

    /// views is the open half of the lifecycle in triage order; Attention
    /// leads because it is the default and it contains Open.
    static let views: [IncidentState] = [.attention, .open, .acknowledged]

    @State private var closedExpanded = false
    @State private var showLegend = false

    var body: some View {
        // No List selection: three rows are lit at once -- a screen, a view
        // and a category -- and a Set binding the List disagrees with is not
        // redrawn from, so each row draws its own accent from the state.
        List {
            Section {
                ForEach(RootScreen.allCases, id: \.self) { item in
                    Label(item.title, systemImage: item.symbol)
                        .modifier(SidebarRow(active: selected.contains(.screen(item))) {
                            apply(.screen(item))
                        })
                }
            } header: {
                Text("Screens")
            }
            if showFilters {
                viewSection
                categorySection
            }
        }
        .listStyle(.sidebar)
        .onAppear { openClosedForFilter() }
        .onChange(of: filter.state) { openClosedForFilter() }
    }

    private var selected: Set<SidebarItem> {
        var items: Set<SidebarItem> = [.screen(screen)]
        guard showFilters else { return items }
        if let state = filter.state { items.insert(.view(state)) }
        if let category = filter.category { items.insert(.category(category)) }
        return items
    }

    private func apply(_ item: SidebarItem) {
        switch item {
        case .screen(let value):
            screen = value
        case .view(let state):
            filter.state = state
            screen = .incidents
        case .category(let category):
            filter.category = category
            screen = .incidents
        }
    }

    private func openClosedForFilter() {
        guard let state = filter.state, IncidentState.closedStates.contains(state) else { return }
        closedExpanded = true
    }

    @ViewBuilder private var viewSection: some View {
        Section {
            ForEach(IncidentsSidebar.views, id: \.self) { state in
                viewRow(state)
            }
            DisclosureGroup(isExpanded: $closedExpanded) {
                ForEach(IncidentState.closedStates, id: \.self) { state in
                    viewRow(state)
                }
            } label: {
                HStack(spacing: 8) {
                    Text("Closed")
                    Spacer(minLength: 4)
                    count(counts.map(closedCount))
                }
            }
        } header: {
            HStack(spacing: 4) {
                Text("View")
                Button {
                    showLegend.toggle()
                } label: {
                    Image(systemName: "questionmark.circle")
                }
                .buttonStyle(.borderless)
                .help("What the states mean")
                .popover(isPresented: $showLegend) {
                    LegendPopover(
                        attentionWindowSeconds: attentionWindowSeconds, openStatus: openStatus)
                }
                Spacer(minLength: 0)
            }
        }
    }

    private func viewRow(_ state: IncidentState) -> some View {
        HStack(spacing: 8) {
            Circle().fill(state.badge.text).frame(width: 7, height: 7)
            Text(state.title)
            Spacer(minLength: 4)
            count(counts.map { $0.byState[state] ?? 0 })
        }
        .modifier(SidebarRow(active: selected.contains(.view(state))) { apply(.view(state)) })
        .help(state.tooltip)
    }

    @ViewBuilder private var categorySection: some View {
        let rows = categoryRows(counts: counts, active: filter.category)
        Section {
            ForEach(rows.shown, id: \.self) { category in
                categoryRow(category)
            }
            if rows.hidden > 0 {
                Text("\(rows.hidden) more with none")
                    .foregroundStyle(.tertiary)
            }
        } header: {
            Text("Category")
        }
    }

    private func categoryRow(_ category: IdiosModel.Category) -> some View {
        HStack(spacing: 8) {
            RoundedRectangle(cornerRadius: 2)
                .fill(BadgeStyle.neutral.text)
                .frame(width: 7, height: 7)
            Text(category.label)
            Spacer(minLength: 4)
            if filter.category == category {
                // A List row cannot deselect itself, so the one filter a
                // person can be inside carries its own way out.
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .bold))
                    .onTapGesture { filter.category = nil }
            } else {
                count(counts.map { $0.byCategory[category] ?? 0 })
            }
        }
        .modifier(
            SidebarRow(active: selected.contains(.category(category))) {
                apply(.category(category))
            })
        .help(category.tooltip)
    }

    // A nil count is the first load still in flight, which draws nothing
    // rather than a zero it does not know.
    private func count(_ value: Int32?) -> some View {
        Text(value.map(String.init) ?? "")
            .font(.system(size: 11, design: .monospaced))
            .monospacedDigit()
            .foregroundStyle(.secondary)
    }
}

/// SidebarRow is the accent an active source-list row carries and the tap
/// that makes a row active.
private struct SidebarRow: ViewModifier {
    let active: Bool
    let select: () -> Void

    func body(content: Content) -> some View {
        content
            .foregroundStyle(
                active
                    ? AnyShapeStyle(Color(nsColor: .alternateSelectedControlTextColor))
                    : AnyShapeStyle(.primary))
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .onTapGesture(perform: select)
            .listRowBackground(
                RoundedRectangle(cornerRadius: 6)
                    .fill(active ? Color.accentColor : Color.clear)
                    .padding(.horizontal, 10))
    }
}
