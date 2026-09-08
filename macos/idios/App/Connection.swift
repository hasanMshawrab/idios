import Foundation
import IdiosAPI
import IdiosModel
import Observation
import OpenAPIURLSession

/// DaemonConnection owns the client every store calls and the reachability the
/// screens show when the daemon does not answer.
@Observable @MainActor
final class DaemonConnection {
    /// State is what the screens say about the daemon right now.
    enum State: Hashable, Sendable {
        case connected
        case unreachable(String)
    }

    private(set) var address: String
    private(set) var state: State = .connected

    /// generation changes whenever the calls in flight are worthless: a new
    /// address, or a person pressing Retry. Stores key their task on it.
    private(set) var generation = 0

    private(set) var client: Client

    /// streamClient is the client every server-sent-event subscription calls.
    private(set) var streamClient: Client

    /// init points the clients at the given host and port.
    init(address: String) {
        self.address = address
        self.client = DaemonConnection.makeClient(address: address)
        self.streamClient = DaemonConnection.makeStreamClient(address: address)
    }

    /// baseURL is the daemon root, also used for the reads outside the OpenAPI
    /// document.
    var baseURL: URL { DaemonConnection.url(address: address) }

    /// setAddress repoints the clients and restarts every store.
    func setAddress(_ address: String) {
        guard address != self.address else { return }
        self.address = address
        self.client = DaemonConnection.makeClient(address: address)
        self.streamClient = DaemonConnection.makeStreamClient(address: address)
        self.state = .connected
        self.generation += 1
    }

    /// retry restarts every store against the same address.
    func retry() {
        state = .connected
        generation += 1
    }

    /// note records what a store's call returned, so one screen's failure is
    /// every screen's not-connected state.
    func note(_ error: APIError?) {
        switch error {
        case .unreachable(let description): state = .unreachable(description)
        default: state = .connected
        }
    }

    // The streams and the request/response calls must not share a session: the
    // app holds four to five open streams, and a shared connection pool makes
    // every click wait behind them.
    private static func makeClient(address: String) -> Client {
        let configuration = URLSessionConfiguration.default
        configuration.httpMaximumConnectionsPerHost = 8
        configuration.waitsForConnectivity = false
        return client(address: address, configuration: configuration)
    }

    private static func makeStreamClient(address: String) -> Client {
        let configuration = URLSessionConfiguration.default
        // A quiet cluster sends nothing for as long as it stays quiet, so the
        // request timeout must never be reached by silence alone.
        configuration.timeoutIntervalForRequest = 24 * 60 * 60
        configuration.httpMaximumConnectionsPerHost = 16
        return client(address: address, configuration: configuration)
    }

    private static func client(address: String, configuration: URLSessionConfiguration) -> Client {
        let session = URLSession(configuration: configuration)
        return Client(
            serverURL: url(address: address),
            transport: URLSessionTransport(configuration: .init(session: session)))
    }

    // A bare host:port is what a person types; URL needs the scheme.
    private static func url(address: String) -> URL {
        let text = address.contains("://") ? address : "http://\(address)"
        return URL(string: text) ?? URL(string: "http://\(Preferences.defaultAddress)")!
    }
}
