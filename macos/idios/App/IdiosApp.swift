import AppKit
import IdiosModel
import ServiceManagement
import SwiftUI

/// IdiosApp is the application entry point.
@main
struct IdiosApp: App {
    @State private var preferences: Preferences
    @State private var connection: DaemonConnection
    @State private var daemon = DaemonManager()
    @State private var status = StatusStore()
    @State private var menuBar = MenuBarStore()
    @State private var navigator = Navigator()
    @State private var palette = PaletteState()
    @State private var explain = ExplainState()
    @Environment(\.openWindow) private var openMainWindow
    private let route: Route

    init() {
        let preferences = Preferences()
        _preferences = State(initialValue: preferences)
        _connection = State(initialValue: DaemonConnection(address: preferences.daemonAddress))
        route = Route.launched ?? .incidents(nil)
    }

    var body: some Scene {
        WindowGroup(id: Screenshot.mainWindowIdentifier) {
            main
                .environment(preferences)
                .environment(connection)
                .environment(daemon)
                .environment(status)
                .environment(navigator)
                .environment(palette)
                .environment(explain)
                .task(id: connection.generation) { await daemon.start(connection: connection) }
                .task(id: connection.generation) { await status.load(connection: connection) }
                .task { await Screenshot.captureIfRequested() }
        }
        .defaultSize(width: 1420, height: 820)
        // Cmd-modified keys are menu items, where they are discoverable; the
        // bare letters that act on a row live on the focused list instead.
        .commands {
            CommandGroup(after: .sidebar) {
                Picker("Group by", selection: grouping) {
                    ForEach(Grouping.allCases, id: \.self) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                Picker("Density", selection: density) {
                    ForEach(Density.allCases, id: \.self) { mode in
                        Text(mode.title).tag(mode)
                    }
                }
                Divider()
            }
            CommandGroup(replacing: .help) {
                Button("Explain This Screen") { explain.requestFromMenu() }
                    .keyboardShortcut("/", modifiers: .command)
            }
            CommandMenu("Go") {
                Button("Incidents") { navigator.open(.incidents(nil)) }
                    .keyboardShortcut("1", modifiers: .command)
                Button("Workloads") { navigator.open(.workloads) }
                    .keyboardShortcut("2", modifiers: .command)
                Button("Status") { navigator.open(.status) }
                    .keyboardShortcut("3", modifiers: .command)
                Divider()
                Button("Search") { palette.isPresented = true }
                    .keyboardShortcut("k", modifiers: .command)
                Divider()
                Button("Back") { navigator.goBack() }
                    .keyboardShortcut("[", modifiers: .command)
                Button("Forward") { navigator.goForward() }
                    .keyboardShortcut("]", modifiers: .command)
            }
        }

        MenuBarExtra {
            MenuBarView(
                store: menuBar, connection: connection, open: openMain, openGroup: openMain,
                attentionWindowSeconds: status.status?.attentionWindowSeconds)
        } label: {
            MenuBarLabel(store: menuBar, connection: connection)
        }
        .menuBarExtraStyle(.window)

        Settings {
            SettingsScreen()
                .environment(preferences)
                .environment(connection)
                .environment(daemon)
        }
    }

    private var grouping: Binding<Grouping> {
        Binding(get: { preferences.grouping }, set: { preferences.grouping = $0 })
    }

    private var density: Binding<Density> {
        Binding(get: { preferences.density }, set: { preferences.density = $0 })
    }

    @ViewBuilder private var main: some View {
        if route == .menubar {
            MenuBarView(
                store: menuBar, connection: connection, open: openMain, openGroup: openMain,
                attentionWindowSeconds: status.status?.attentionWindowSeconds)
        } else {
            IncidentsScreen(route: route)
                .frame(minWidth: 900, minHeight: 560)
        }
    }

    // openWindow(id:) on a WindowGroup opens a second window rather than
    // raising the one already on screen, which is not what a menu bar item
    // asking for the application means.
    private func openMain(_ route: Route) {
        navigator.open(route)
        NSApp.activate(ignoringOtherApps: true)
        raiseMain()
    }

    private func openMain(_ group: GroupTarget) {
        navigator.open(group: group)
        NSApp.activate(ignoringOtherApps: true)
        raiseMain()
    }

    private func raiseMain() {
        let existing = NSApp.windows.first {
            $0.isVisible
                && $0.identifier?.rawValue.contains(Screenshot.mainWindowIdentifier) == true
        }
        if let existing {
            existing.makeKeyAndOrderFront(nil)
        } else {
            openMainWindow(id: Screenshot.mainWindowIdentifier)
        }
    }
}

/// SettingsScreen edits what the application needs to be told: where the
/// daemon listens, which kubeconfig it reads, and whether idios starts at
/// login.
struct SettingsScreen: View {
    @Environment(Preferences.self) private var preferences
    @Environment(DaemonConnection.self) private var connection
    @Environment(DaemonManager.self) private var daemon

    @State private var address = ""
    @State private var startAtLogin = SMAppService.mainApp.status == .enabled
    @State private var kubeconfigError: String?
    @State private var isRestarting = false

    var body: some View {
        Form {
            TextField("Daemon address:", text: $address)
                .onSubmit(apply)
            HStack {
                Spacer()
                Button("Apply", action: apply)
                    .disabled(address == connection.address)
            }
            kubeconfigRow
            if let kubeconfigError {
                Text(kubeconfigError)
                    .font(.system(size: 11.5))
                    .foregroundStyle(BadgeStyle.red.text)
            }
            Toggle("Start at login", isOn: $startAtLogin)
                .onChange(of: startAtLogin) { applyStartAtLogin() }
        }
        .formStyle(.grouped)
        .frame(width: 420)
        .onAppear { address = preferences.daemonAddress }
    }

    @ViewBuilder private var kubeconfigRow: some View {
        LabeledContent("Kubeconfig:") {
            HStack {
                Text(daemon.kubeconfigPath() ?? "not set")
                    .truncationMode(.middle)
                    .lineLimit(1)
                if isRestarting {
                    ProgressView().controlSize(.small)
                }
                Button("Change...") { Task { await changeKubeconfig() } }
                    .disabled(daemon.state == .external || isRestarting)
            }
        }
        if daemon.state == .external {
            // The running daemon read its config at its own start; the app
            // rewriting the file would change nothing until someone restarts
            // what the app did not start.
            Text("The daemon was started outside the app; change its idios.toml instead.")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
        }
    }

    private func apply() {
        preferences.daemonAddress = address
        connection.setAddress(address)
    }

    private func changeKubeconfig() async {
        guard let chosen = SetupSheet.chooseKubeconfig() else { return }
        kubeconfigError = nil
        do {
            try daemon.setKubeconfig(chosen)
        } catch {
            kubeconfigError = "writing the setting failed: \(error.localizedDescription)"
            return
        }
        isRestarting = true
        kubeconfigError = await daemon.restart(connection: connection)
        isRestarting = false
    }

    private func applyStartAtLogin() {
        do {
            if startAtLogin {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
        } catch {
            kubeconfigError = "start at login failed: \(error.localizedDescription)"
            startAtLogin = SMAppService.mainApp.status == .enabled
        }
    }
}
