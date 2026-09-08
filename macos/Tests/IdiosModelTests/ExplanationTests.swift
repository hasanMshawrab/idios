import Testing

@testable import IdiosModel

/// withLastMessage copies crashIncident with only last_message varying, the
/// one field explanation reads before it falls back to events.
private func withLastMessage(_ lastMessage: String?) -> Incident {
    Incident(
        id: crashIncident.id,
        clusterID: crashIncident.clusterID,
        namespace: crashIncident.namespace,
        subjectKind: crashIncident.subjectKind,
        podUID: crashIncident.podUID,
        jobUID: crashIncident.jobUID,
        containerName: crashIncident.containerName,
        workloadKind: crashIncident.workloadKind,
        workloadName: crashIncident.workloadName,
        category: crashIncident.category,
        firstReason: crashIncident.firstReason,
        lastReason: crashIncident.lastReason,
        lastMessage: lastMessage,
        image: crashIncident.image,
        imageTag: crashIncident.imageTag,
        imageID: crashIncident.imageID,
        occurrences: crashIncident.occurrences,
        openedAt: crashIncident.openedAt,
        lastSeenAt: crashIncident.lastSeenAt,
        closedAt: crashIncident.closedAt,
        closeReason: crashIncident.closeReason,
        acknowledgedAt: crashIncident.acknowledgedAt,
        dismissedAt: crashIncident.dismissedAt,
        note: crashIncident.note,
        state: crashIncident.state,
        podName: crashIncident.podName,
        podDeletedAt: crashIncident.podDeletedAt,
        podDeletionReason: crashIncident.podDeletionReason,
        containerCount: crashIncident.containerCount,
        exitCode: crashIncident.exitCode,
        signal: crashIncident.signal,
        nodeName: crashIncident.nodeName)
}

/// pod copies crashPod with only deletion_requested_at varying, the one
/// field the unclean_exit provenance reads.
private func pod(deletionRequestedAt: String?) -> Pod {
    Pod(
        uid: crashPod.uid,
        clusterID: crashPod.clusterID,
        namespace: crashPod.namespace,
        name: crashPod.name,
        nodeName: crashPod.nodeName,
        phase: crashPod.phase,
        statusReason: crashPod.statusReason,
        statusMessage: crashPod.statusMessage,
        qosClass: crashPod.qosClass,
        controllerKind: crashPod.controllerKind,
        controllerName: crashPod.controllerName,
        controllerUID: crashPod.controllerUID,
        workloadKind: crashPod.workloadKind,
        workloadName: crashPod.workloadName,
        createdAt: crashPod.createdAt,
        startedAt: crashPod.startedAt,
        firstSeenAt: crashPod.firstSeenAt,
        lastSeenAt: crashPod.lastSeenAt,
        deletionRequestedAt: deletionRequestedAt.map(Timestamp.init),
        deletedAt: crashPod.deletedAt,
        deletionSource: crashPod.deletionSource,
        deletionReason: crashPod.deletionReason)
}

/// uncleanExitIncident copies crashIncident as an unclean_exit row with only
/// exit_code, signal and last_message varying, the fields the sentence reads.
private func uncleanExitIncident(exitCode: Int32?, signal: Int32?) -> Incident {
    Incident(
        id: crashIncident.id,
        clusterID: crashIncident.clusterID,
        namespace: crashIncident.namespace,
        subjectKind: crashIncident.subjectKind,
        podUID: crashIncident.podUID,
        jobUID: crashIncident.jobUID,
        containerName: crashIncident.containerName,
        workloadKind: crashIncident.workloadKind,
        workloadName: crashIncident.workloadName,
        category: .uncleanExit,
        firstReason: crashIncident.firstReason,
        lastReason: crashIncident.lastReason,
        lastMessage: nil,
        image: crashIncident.image,
        imageTag: crashIncident.imageTag,
        imageID: crashIncident.imageID,
        occurrences: crashIncident.occurrences,
        openedAt: crashIncident.openedAt,
        lastSeenAt: crashIncident.lastSeenAt,
        closedAt: crashIncident.closedAt,
        closeReason: crashIncident.closeReason,
        acknowledgedAt: crashIncident.acknowledgedAt,
        dismissedAt: crashIncident.dismissedAt,
        note: crashIncident.note,
        state: crashIncident.state,
        podName: crashIncident.podName,
        podDeletedAt: crashIncident.podDeletedAt,
        podDeletionReason: crashIncident.podDeletionReason,
        containerCount: crashIncident.containerCount,
        exitCode: exitCode,
        signal: signal,
        nodeName: crashIncident.nodeName)
}

/// warning builds a Warning event the fixtures do not already cover, so a
/// second candidate can compete with unhealthyEvent on lastTS.
private func warning(id: String, incidentID: String?, reason: String, message: String, lastTS: String)
    -> Event
{
    Event(
        id: id,
        clusterID: unhealthyEvent.clusterID,
        eventUID: "ev-\(id)",
        namespace: unhealthyEvent.namespace,
        type: "Warning",
        involvedKind: unhealthyEvent.involvedKind,
        involvedName: unhealthyEvent.involvedName,
        involvedUID: unhealthyEvent.involvedUID,
        fieldPath: unhealthyEvent.fieldPath,
        reason: reason,
        message: message,
        sourceComponent: unhealthyEvent.sourceComponent,
        count: 1,
        firstTS: Timestamp(lastTS),
        lastTS: Timestamp(lastTS),
        category: unhealthyEvent.category,
        incidentID: incidentID)
}

private let newerWarning = warning(
    id: "73", incidentID: "412", reason: "BackOff",
    message: "Back-off restarting failed container", lastTS: "2026-08-27T14:40:00.000000Z")

// The newest Warning overall belongs to a sibling's probe incident; the
// older one is this incident's own.
private let siblingsNewerWarning = warning(
    id: "74", incidentID: "999", reason: "Unhealthy",
    message: "Readiness probe failed: HTTP probe failed with statuscode: 503",
    lastTS: "2026-08-27T14:41:00.000000Z")
private let ownOlderWarning = warning(
    id: "75", incidentID: "412", reason: "BackOff",
    message: "Back-off restarting failed container", lastTS: "2026-08-27T14:38:00.000000Z")

// Every Warning here belongs to another incident.
private let otherIncidentWarning = warning(
    id: "76", incidentID: "999", reason: "Unhealthy",
    message: "Readiness probe failed: HTTP probe failed with statuscode: 503",
    lastTS: "2026-08-27T14:39:00.000000Z")

@Test(
    arguments: [
        // The recorder's own last_message wins no matter what the events say.
        (
            crashIncident, [unhealthyEvent, killingEvent], nil,
            Explanation(
                message: crashIncident.lastMessage ?? "", provenance: "incidents.last_message")
        ),
        // With no last_message, the newest Warning by lastTS wins.
        (
            withLastMessage(nil), [unhealthyEvent, newerWarning], nil,
            Explanation(
                message: newerWarning.message ?? "",
                provenance: "k8s_events.message WHERE incident_id - \(newerWarning.reason)")
        ),
        // A Normal event is never a candidate.
        (withLastMessage(nil), [killingEvent], nil, nil),
        // No events, no last_message: nothing to show.
        (withLastMessage(nil), [], nil, nil),
        // The newest Warning overall carries a sibling's incident id; the
        // filter picks the older Warning that carries this incident's own id.
        (
            withLastMessage(nil), [siblingsNewerWarning, ownOlderWarning], nil,
            Explanation(
                message: ownOlderWarning.message ?? "",
                provenance: "k8s_events.message WHERE incident_id - \(ownOlderWarning.reason)")
        ),
        // Every Warning carries another incident's id: nothing to show for a
        // crash.
        (withLastMessage(nil), [otherIncidentWarning], nil, nil),
        // unclean_exit, no last_message, no Warning of its own: exit code and
        // signal 0 (a grace-period kill) build the sentence, signal unsaid.
        (
            uncleanExitIncident(exitCode: 137, signal: 0), [],
            pod(deletionRequestedAt: "2026-08-27T14:39:00.000000Z"),
            Explanation(
                message: "exit 137, while the pod was terminating",
                provenance:
                    "exit_code and signal of the container's last termination - pods.deletion_requested_at 2026-08-27T14:39:00.000000Z"
            )
        ),
        // The same with a real signal: it joins the sentence.
        (
            uncleanExitIncident(exitCode: 137, signal: 9), [],
            pod(deletionRequestedAt: "2026-08-27T14:39:00.000000Z"),
            Explanation(
                message: "exit 137, signal 9, while the pod was terminating",
                provenance:
                    "exit_code and signal of the container's last termination - pods.deletion_requested_at 2026-08-27T14:39:00.000000Z"
            )
        ),
        // No exit_code: the termination was not observed.
        (
            uncleanExitIncident(exitCode: nil, signal: nil), [],
            pod(deletionRequestedAt: "2026-08-27T14:39:00.000000Z"),
            Explanation(
                message: "the container exited while the pod was terminating",
                provenance:
                    "exit_code and signal of the container's last termination - pods.deletion_requested_at 2026-08-27T14:39:00.000000Z"
            )
        ),
        // No pod row (it was swept): the provenance ends with no timestamp
        // clause.
        (
            uncleanExitIncident(exitCode: 137, signal: 0), [], nil,
            Explanation(
                message: "exit 137, while the pod was terminating",
                provenance: "exit_code and signal of the container's last termination")
        ),
    ] as [(Incident, [Event], Pod?, Explanation?)])
func theHeaderExplainsWithTheRecorderThenTheEventsThenTheRow(
    incident: Incident, events: [Event], pod: Pod?, want: Explanation?
) {
    #expect(explanation(for: incident, events: events, pod: pod) == want)
}
