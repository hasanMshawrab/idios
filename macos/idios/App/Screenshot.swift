import AppKit
import IdiosModel

/// Screenshot serves the -screenshot launch argument: it opens the route, waits
/// for the window to settle and prints the window number for a capture tool to
/// photograph. The window server owns the pixels of the split view sidebar and
/// the toolbar, so an in-process render of the view hierarchy gets them blank.
enum Screenshot {
    /// mainWindowIdentifier names the scene whose window is the review image;
    /// the settings window and later scenes must not be captured instead.
    static let mainWindowIdentifier = "main"

    @MainActor private static var firstLoadDone = false

    @MainActor private static var helpShown = false

    /// noteFirstLoad says a store finished its first call, so the capture waits
    /// for data rather than for a fixed time.
    @MainActor
    static func noteFirstLoad() {
        firstLoadDone = true
    }

    /// noteHelpShown says a screen's help marks are up and have been checked,
    /// so a -explain capture waits for them rather than for a fixed time.
    @MainActor
    static func noteHelpShown() {
        helpShown = true
    }

    // These regions are drawn only when the data has one, so a screen without
    // them is the data's answer and not a hole in the tables: a list has no
    // group header when nothing is grouped, no run row without a Job, no
    // rollup row unless several pods fail the same way and no pod row while
    // every group is folded; a namespace of the tree has no kind caption and
    // no bare pods until something is under it; a runs table folds nothing
    // and has no condition column while it holds no run; a workload that has
    // never been deployed has no rollouts table and so no caveat under it;
    // a Job no CronJob created has no other runs; and a container nothing has
    // ever opened an incident on has no verdict to draw.
    private static let dataDependent: Set<RegionID> = [
        "groupHeader", "runRow", "rollupRow", "podRow", "kindCaption", "barePods",
        "runCondition", "runFold", "rolloutColumns", "rolloutCaveat", "otherRuns",
        "verdict",
    ]

    // A pushed page keeps the screen it covers in the view tree, so the ids
    // that climb to the root are not only the ones in view; an id no table
    // anywhere names is a hole whichever screen registered it.
    private static let everyExplainedID: Set<RegionID> = Set(
        explainedScreens.flatMap(explainedRegions(for:)).map(\.id))

    /// reportCoverage says on stderr when a screen's marks and its
    /// explanations disagree: an id a view claimed that no table names, and
    /// an explanation nothing on the screen drew.
    @MainActor
    static func reportCoverage(screen: ExplainedScreen, registered: Set<RegionID>) {
        let declared = Set(explainedRegions(for: screen).map(\.id))
        let unexplained = registered.subtracting(everyExplainedID)
        let undrawn = declared.subtracting(registered).subtracting(dataDependent)
        guard !unexplained.isEmpty || !undrawn.isEmpty else { return }
        let line = "idios-explain \(String(describing: screen)): "
            + "unexplained \(list(unexplained)); undrawn \(list(undrawn))\n"
        FileHandle.standardError.write(Data(line.utf8))
        #if DEBUG
            assertionFailure(line)
        #endif
    }

    private static func list(_ ids: Set<RegionID>) -> String {
        ids.isEmpty ? "none" : ids.map(\.raw).sorted().joined(separator: ", ")
    }

    /// explainRequested says this run was asked to open the route's help
    /// marks before the capture.
    static func explainRequested() -> Bool {
        CommandLine.arguments.contains("-explain")
    }

    @MainActor
    static func captureIfRequested() async {
        guard requested() else { return }
        // A window that was never key leaves its title bar widgets unpainted.
        NSApp.activate(ignoringOtherApps: true)
        NSApp.windows.first(where: \.isVisible)?.makeKeyAndOrderFront(nil)
        await waitForFirstLoad()
        if explainRequested() { await waitForHelp() }
        // SwiftUI reports the scene before the window has drawn its final
        // layout; capturing at once yields an empty or half-sized view.
        try? await Task.sleep(for: .seconds(1.5))
        guard let window = mainWindow() else {
            fail("no main window to capture")
        }
        FileHandle.standardOutput.write(Data("idios-window \(window.windowNumber)\n".utf8))
        try? FileHandle.standardOutput.synchronize()
        // The capture tool sends SIGTERM once it has the image; the cap keeps a
        // forgotten process from living forever.
        try? await Task.sleep(for: .seconds(60))
        NSApp.terminate(nil)
    }

    // Five seconds is longer than any local call takes; past it the screen is
    // worth capturing anyway, with whatever it managed to show.
    @MainActor
    private static func waitForFirstLoad() async {
        for _ in 0..<50 {
            if firstLoadDone { return }
            try? await Task.sleep(for: .milliseconds(100))
        }
    }

    // The same cap: past it the marks are worth photographing anyway, and the
    // coverage line, if there is one, has already been written.
    @MainActor
    private static func waitForHelp() async {
        for _ in 0..<50 {
            if helpShown { return }
            try? await Task.sleep(for: .milliseconds(100))
        }
    }

    @MainActor
    private static func mainWindow() -> NSWindow? {
        // A sheet is its own window to the window server, with no identifier
        // of its own; when one is up it is the key window and the review
        // image, not the screen it sits over.
        if let key = NSApp.keyWindow, key.sheetParent != nil { return key }
        let visible = NSApp.windows.filter(\.isVisible)
        return visible.first { $0.identifier?.rawValue.contains(mainWindowIdentifier) == true }
            ?? visible.first
    }

    /// requested says this is a -screenshot run, which must never spawn or
    /// set up a daemon.
    static func requested() -> Bool {
        let args = CommandLine.arguments
        guard let flag = args.firstIndex(of: "-screenshot") else { return false }
        return flag + 1 < args.count
    }

    private static func fail(_ message: String) -> Never {
        FileHandle.standardError.write(Data("idios: screenshot: \(message)\n".utf8))
        exit(1)
    }
}
