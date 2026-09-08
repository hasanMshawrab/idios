import Testing

@testable import IdiosModel

/// verdict builds an incident of crashIncident's fields but the occurrences,
/// exit code, image id and subject the verdict table varies.
private func verdict(
    occurrences: Int32 = 8, exitCode: Int32? = 137, imageID: String? = crashIncident.imageID,
    subjectKind: SubjectKind = .pod
) -> Incident {
    Incident(
        id: crashIncident.id,
        clusterID: crashIncident.clusterID,
        namespace: crashIncident.namespace,
        subjectKind: subjectKind,
        podUID: subjectKind == .job ? nil : crashIncident.podUID,
        jobUID: crashIncident.jobUID,
        containerName: subjectKind == .job ? nil : crashIncident.containerName,
        workloadKind: crashIncident.workloadKind,
        workloadName: crashIncident.workloadName,
        category: crashIncident.category,
        firstReason: crashIncident.firstReason,
        lastReason: crashIncident.lastReason,
        lastMessage: crashIncident.lastMessage,
        image: crashIncident.image,
        imageTag: crashIncident.imageTag,
        imageID: imageID,
        occurrences: occurrences,
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
        signal: crashIncident.signal,
        nodeName: crashIncident.nodeName)
}

/// unlimited is apiContainer with neither memory field set, which is what a
/// container that was never given a limit reports.
private let unlimited = Container(
    id: apiContainer.id, podUID: apiContainer.podUID, name: apiContainer.name,
    kind: apiContainer.kind, image: apiContainer.image, imageTag: apiContainer.imageTag,
    imageID: apiContainer.imageID, containerID: apiContainer.containerID,
    cpuRequest: apiContainer.cpuRequest, cpuLimit: apiContainer.cpuLimit,
    memRequest: nil, memLimit: nil, cpuRequestMillis: apiContainer.cpuRequestMillis,
    cpuLimitMillis: apiContainer.cpuLimitMillis, memRequestBytes: nil, memLimitBytes: nil,
    state: apiContainer.state, reason: apiContainer.reason, exitCode: apiContainer.exitCode,
    signal: apiContainer.signal, ready: apiContainer.ready,
    restartCount: apiContainer.restartCount, runningSince: apiContainer.runningSince,
    lastTerminatedReason: apiContainer.lastTerminatedReason,
    lastTerminatedExitCode: apiContainer.lastTerminatedExitCode,
    lastTerminatedSignal: apiContainer.lastTerminatedSignal,
    lastTerminatedAt: apiContainer.lastTerminatedAt, updatedAt: apiContainer.updatedAt,
    grafanaURL: apiContainer.grafanaURL)

/// log builds a capture attempt of crashArtifact's fields but the restart it
/// belongs to, the file it wrote and the gap that explains a missing one.
private func log(_ restartCount: Int32, filePath: String?, gap: CaptureGap? = nil) -> Artifact {
    Artifact(
        id: "log-\(restartCount)", podUID: crashArtifact.podUID,
        incidentID: crashArtifact.incidentID, containerName: crashArtifact.containerName,
        kind: .logPrevious, restartCount: restartCount, filePath: filePath,
        sizeBytes: filePath == nil ? nil : crashArtifact.sizeBytes,
        truncated: crashArtifact.truncated, capturedEarly: crashArtifact.capturedEarly,
        captureGap: gap, captureNote: crashArtifact.captureNote,
        capturedAt: crashArtifact.capturedAt)
}

// Eight restarts, of which the fourth wrote nothing.
private let eightRestarts: [Artifact] =
    (0..<8).map { restart in
        restart == 3
            ? log(3, filePath: nil, gap: .kubeletError)
            : log(Int32(restart), filePath: "prod/pod-crash/api-\(restart).log")
    }

private let crashSentence = "Exit 137 (Error) 8 times since 14:03, last 14:39."
private let memorySentence = "Memory limit 536.9 MB, request 268.4 MB."
private let captureSentence =
    "7 of 8 restarts captured a log; restart 3 wrote nothing (capture gap: kubelet error)."
private let sameImageSentence = "Same image digest since open."

// The verdict block is built from the fields the page already receives:
// what happened, how many times since when and last when, the memory limit
// and request, what was and was not captured, and whether the image digest
// moved since the incident opened.
@Test(
    arguments: [
        // A Job's incident has no container, so neither the memory nor the
        // image sentence has anything to read.
        (
            verdict(subjectKind: .job), nil, [],
            [crashSentence, "Nothing was captured."]
        ),
        (
            verdict(), apiContainer, [],
            [crashSentence, memorySentence, "Nothing was captured.", sameImageSentence]
        ),
        (
            verdict(), apiContainer, eightRestarts.map { log($0.restartCount, filePath: "a.log") },
            [
                crashSentence, memorySentence, "Every restart captured a log.", sameImageSentence,
            ]
        ),
        (
            verdict(exitCode: nil), apiContainer, eightRestarts,
            [
                "Error 8 times since 14:03, last 14:39.", memorySentence, captureSentence,
                sameImageSentence,
            ]
        ),
        (
            verdict(), unlimited, eightRestarts,
            [crashSentence, captureSentence, sameImageSentence]
        ),
        (
            verdict(occurrences: 1), apiContainer, eightRestarts,
            [
                "Exit 137 (Error) once since 14:03, last 14:39.", memorySentence, captureSentence,
                sameImageSentence,
            ]
        ),
        (
            verdict(imageID: "registry.example.com/web@sha256:2222"), apiContainer, eightRestarts,
            [
                crashSentence, memorySentence, captureSentence,
                "The image digest changed since this opened.",
            ]
        ),
        (
            verdict(), apiContainer, eightRestarts,
            [crashSentence, memorySentence, captureSentence, sameImageSentence]
        ),
    ] as [(Incident, Container?, [Artifact], [String])])
func verdictSentencesSayWhatHappenedAndWhatWasCaptured(
    incident: Incident, container: Container?, artifacts: [Artifact], want: [String]
) {
    #expect(verdictSentences(incident: incident, container: container, artifacts: artifacts) == want)
}
