import Foundation
import Observation

/// PaletteState is whether the palette is up and what it opened with; the
/// Go menu and the overlay share it through the environment.
@Observable @MainActor
final class PaletteState {
    var isPresented = false
    /// launchQuery is the "-search <query>" launch argument, so a
    /// screenshot run opens the palette on a query without a keyboard.
    let launchQuery: String?

    init(arguments: [String] = CommandLine.arguments) {
        var query: String?
        if let flag = arguments.firstIndex(of: "-search"), flag + 1 < arguments.count {
            query = arguments[flag + 1]
        }
        launchQuery = query
        isPresented = query != nil
    }
}
