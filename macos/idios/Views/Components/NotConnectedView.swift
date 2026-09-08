import SwiftUI

/// NotConnectedView is the one state every screen shows while the daemon does
/// not answer: the address it tried, the last error, and a way to try again.
struct NotConnectedView: View {
    let address: String
    let error: String
    let retry: () -> Void

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "bolt.horizontal.circle")
                .font(.system(size: 34, weight: .light))
                .foregroundStyle(.secondary)
            Text("Not connected")
                .font(.title3.weight(.semibold))
            Text(address)
                .font(.system(size: 12, design: .monospaced))
                .textSelection(.enabled)
            WrapText(value: error)
                .font(.system(size: 11.5))
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 420)
            Button("Retry", action: retry)
                .keyboardShortcut("r")
        }
        .padding(40)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
