import IdiosAPI
import Testing

@testable import IdiosModel

@Test func timelineEntriesKeepEveryKindsFieldsAndAlwaysTheObservedTime() throws {
    let page = try Page<TimelineEntry>(
        wire: fixture(Components.Schemas.TimelineResponse.self, "timeline.json"))
    #expect(
        page
            == Page(
                rows: [
                    TimelineEntry(
                        kind: .containerTransition,
                        k8sAt: Timestamp("2026-08-27T14:38:00.000000Z"),
                        observedAt: Timestamp("2026-08-27T14:38:02.000000Z"),
                        containerName: "api",
                        state: .terminated,
                        reason: "Error",
                        exitCode: 137,
                        signal: 9,
                        restartCount: 7,
                        gapReconstructed: true,
                        conditionType: "Ready",
                        conditionStatus: "False",
                        message: "back-off 5m0s restarting failed container",
                        eventType: "Warning",
                        eventReason: "Unhealthy",
                        count: 3,
                        artifactID: "81",
                        artifactKind: .logPrevious,
                        captureGap: .kubeletError,
                        replicasetName: "checkout-api-7d9f8b6c4",
                        imageTag: "1.4.2",
                        revision: "8",
                        lifecycle: .closed,
                        closeReason: .podDeleted),
                    TimelineEntry(
                        kind: .lifecycle,
                        k8sAt: nil,
                        observedAt: Timestamp("2026-08-27T14:03:11.482913Z"),
                        containerName: nil,
                        state: nil,
                        reason: nil,
                        exitCode: nil,
                        signal: nil,
                        restartCount: 0,
                        gapReconstructed: false,
                        conditionType: nil,
                        conditionStatus: nil,
                        message: nil,
                        eventType: nil,
                        eventReason: nil,
                        count: 0,
                        artifactID: nil,
                        artifactKind: nil,
                        captureGap: nil,
                        replicasetName: nil,
                        imageTag: nil,
                        revision: nil,
                        lifecycle: .opened,
                        closeReason: nil),
                ],
                truncated: false))
}

/// foldEntry builds a timeline entry with the kind, container, state and
/// observed time the fold table varies; every other field is empty, because
/// the fold key does not read them.
private func foldEntry(
    _ kind: TimelineKind, at: String, container: String? = "api", state: ContainerState? = nil
) -> TimelineEntry {
    TimelineEntry(
        kind: kind, k8sAt: nil, observedAt: Timestamp("2026-08-27T\(at):00.000000Z"),
        containerName: container, state: state, reason: nil, exitCode: nil, signal: nil,
        restartCount: 0, gapReconstructed: false, conditionType: nil, conditionStatus: nil,
        message: nil, eventType: nil, eventReason: nil, count: 0, artifactID: nil,
        artifactKind: nil, captureGap: nil, replicasetName: nil, imageTag: nil, revision: nil,
        lifecycle: nil, closeReason: nil)
}

private let stepTimes = ["07:32", "07:36", "07:40", "07:44", "07:48", "07:52", "07:56", "08:00"]

private let alternating: [TimelineEntry] = stepTimes.enumerated().map {
    foldEntry(.containerTransition, at: $0.element, state: $0.offset % 2 == 0 ? .waiting : .terminated)
}

private func waitingSteps(_ n: Int) -> [TimelineEntry] {
    stepTimes.prefix(n).map { foldEntry(.containerTransition, at: $0, state: .waiting) }
}

private let cut = foldEntry(.cut, at: "07:32", container: nil)
private let lifecycle = foldEntry(.lifecycle, at: "07:46", container: nil)

private func onlyFold(_ entry: TimelineEntry) -> TimelineFold {
    TimelineFold(
        id: "\(entry.observedAt.raw)/\(entry.kind.rawValue)", lead: entry, repeats: [],
        summary: nil)
}

// A repeating cycle folds into one entry with a summary; lifecycle, rollout
// and cut entries are the spine of the story and never fold.
@Test(
    arguments: [
        ([cut], 3, [onlyFold(cut)]),
        (
            waitingSteps(2), 3,
            [onlyFold(waitingSteps(2)[0]), onlyFold(waitingSteps(2)[1])]
        ),
        (
            waitingSteps(4), 3,
            [
                TimelineFold(
                    id: "2026-08-27T07:32:00.000000Z/container_transition",
                    lead: waitingSteps(4)[0],
                    repeats: Array(waitingSteps(4).dropFirst()),
                    summary: "x4, every 4m, first 07:32, last 07:44"),
            ]
        ),
        (
            Array(alternating.prefix(4)) + [lifecycle] + Array(alternating.suffix(4)), 2,
            [
                TimelineFold(
                    id: "2026-08-27T07:32:00.000000Z/container_transition",
                    lead: alternating[0], repeats: Array(alternating[1..<4]),
                    summary: "x2, first 07:32, last 07:44"),
                onlyFold(lifecycle),
                TimelineFold(
                    id: "2026-08-27T07:48:00.000000Z/container_transition",
                    lead: alternating[4], repeats: Array(alternating[5..<8]),
                    summary: "x2, first 07:48, last 08:00"),
            ]
        ),
        (
            alternating, 3,
            [
                TimelineFold(
                    id: "2026-08-27T07:32:00.000000Z/container_transition",
                    lead: alternating[0], repeats: Array(alternating.dropFirst()),
                    summary: "x4, every 8m, first 07:32, last 08:00"),
            ]
        ),
    ] as [([TimelineEntry], Int, [TimelineFold])])
func timelineFoldsCollapseARepeatingCycleAndNeverTheSpine(
    entries: [TimelineEntry], minimumCycles: Int, want: [TimelineFold]
) {
    #expect(timelineFolds(entries, minimumCycles: minimumCycles) == want)
}
