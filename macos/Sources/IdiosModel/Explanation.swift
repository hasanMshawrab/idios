import Foundation

/// Explanation is the sentence under the header title and the column it came
/// from.
public struct Explanation: Hashable, Sendable {
    public let message: String
    public let provenance: String
}

/// explanation picks the sentence that says why the incident exists: the
/// recorder's own last_message, else the newest Warning that attached to
/// this incident, else, for unclean_exit, the exit code and signal of the
/// container's last termination.
public func explanation(for incident: Incident, events: [Event], pod: Pod?) -> Explanation? {
    if let message = incident.lastMessage {
        return Explanation(message: message, provenance: "incidents.last_message")
    }
    if let event = newestWarning(events, incidentID: incident.id), let message = event.message {
        return Explanation(
            message: message, provenance: "k8s_events.message WHERE incident_id - \(event.reason)")
    }
    // The kubelet writes no message for a grace-period kill and Killing is a
    // Normal event, so an unclean_exit has nothing to quote and the row
    // itself is the source; containerd reports signal 0 for the kill, so
    // "signal 0" would read as a signal that was sent.
    guard incident.category == .uncleanExit else { return nil }
    var parts: [String] = []
    if let exit = incident.exitCode {
        parts.append("exit \(exit)")
        if let signal = incident.signal, signal != 0 { parts.append("signal \(signal)") }
    }
    let message =
        parts.isEmpty
        ? "the container exited while the pod was terminating"
        : parts.joined(separator: ", ") + ", while the pod was terminating"
    var provenance = "exit_code and signal of the container's last termination"
    if let at = pod?.deletionRequestedAt { provenance += " - pods.deletion_requested_at \(at.raw)" }
    return Explanation(message: message, provenance: provenance)
}

private func newestWarning(_ events: [Event], incidentID: String) -> Event? {
    events
        .filter { $0.type == "Warning" && $0.incidentID == incidentID }
        .max { (at($0) ?? .distantPast) < (at($1) ?? .distantPast) }
}

private func at(_ event: Event) -> Date? {
    event.lastTS?.date ?? event.firstTS?.date
}
