import SwiftUI

/// GrafanaLinkButton opens a served Grafana Explore URL as it was served; a
/// string that is absent or does not parse as a URL renders nothing rather
/// than a dead button.
struct GrafanaLinkButton: View {
    let urlString: String?

    @Environment(\.openURL) private var openURL

    var body: some View {
        if let urlString, let url = URL(string: urlString) {
            Button {
                openURL(url)
            } label: {
                // The mark alone did not say where the button goes; the verb
                // and the outward arrow do.
                HStack(spacing: 4) {
                    GrafanaMark()
                    Text("Open Grafana")
                    Image(systemName: "arrow.up.right")
                        .font(.system(size: 9, weight: .semibold))
                }
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
            }
            .buttonStyle(.borderless)
            .help("Open logs in Grafana")
        }
    }
}
