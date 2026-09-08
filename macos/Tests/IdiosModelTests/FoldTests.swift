import Testing

@testable import IdiosModel

/// row builds an incident row with the fields the fold and the rollup tests
/// vary; every other field copies crashIncident.
func row(
    _ id: String, pod: String?, category: Category, closed: Bool = false,
    lastSeen: String, occurrences: Int32 = 1,
    container: String? = crashIncident.containerName,
    workload: String = crashIncident.workloadName,
    lastReason: String = crashIncident.lastReason,
    exitCode: Int32? = crashIncident.exitCode,
    job: String? = crashIncident.jobUID,
    podName: String? = crashIncident.podName,
    kind: String = crashIncident.workloadKind,
    imageTag: String? = crashIncident.imageTag,
    opened: String = crashIncident.openedAt.raw,
    acknowledged: Bool = false
) -> Incident {
    Incident(
        id: id,
        clusterID: crashIncident.clusterID,
        namespace: crashIncident.namespace,
        subjectKind: pod == nil ? .job : .pod,
        podUID: pod,
        jobUID: job,
        containerName: container,
        workloadKind: kind,
        workloadName: workload,
        category: category,
        firstReason: crashIncident.firstReason,
        lastReason: lastReason,
        lastMessage: crashIncident.lastMessage,
        image: crashIncident.image,
        imageTag: imageTag,
        imageID: crashIncident.imageID,
        occurrences: occurrences,
        openedAt: Timestamp(opened),
        lastSeenAt: Timestamp(lastSeen),
        closedAt: closed ? Timestamp("2026-08-27T14:40:00.000000Z") : nil,
        closeReason: closed ? .podDeleted : nil,
        acknowledgedAt: acknowledged ? Timestamp("2026-08-27T14:20:00.000000Z") : nil,
        dismissedAt: crashIncident.dismissedAt,
        note: crashIncident.note,
        state: closed ? .podDeleted : .open,
        podName: podName,
        podDeletedAt: crashIncident.podDeletedAt,
        podDeletionReason: crashIncident.podDeletionReason,
        containerCount: crashIncident.containerCount,
        exitCode: exitCode,
        signal: crashIncident.signal,
        nodeName: crashIncident.nodeName)
}

@Test(
    arguments: [
        // Two categories on one pod: the worse category leads, oom
        // outranking probe.
        (
            [
                row("1", pod: "p1", category: .probe, lastSeen: "2026-08-27T14:39:00.000000Z"),
                row("2", pod: "p1", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "2", podUID: "p1",
                    lead: row("2", pod: "p1", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z"),
                    siblings: [
                        row("1", pod: "p1", category: .probe, lastSeen: "2026-08-27T14:39:00.000000Z")
                    ])
            ]
        ),
        // Two rows of the same category on one pod: a tie falls to the row
        // served first, which is the newest.
        (
            [
                row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                row("2", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "1", podUID: "p1",
                    lead: row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                    siblings: [
                        row("2", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:35:00.000000Z")
                    ])
            ]
        ),
        // Rows of two pods fold separately, in served order.
        (
            [
                row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                row("2", pod: "p2", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "1", podUID: "p1",
                    lead: row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                    siblings: []),
                PodFold(
                    id: "2", podUID: "p2",
                    lead: row("2", pod: "p2", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z"),
                    siblings: []),
            ]
        ),
        // A job-subject row between two rows of one pod stands alone; it
        // never joins the pod's fold.
        (
            [
                row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                row("2", pod: nil, category: .jobFailed, lastSeen: "2026-08-27T14:37:00.000000Z"),
                row("3", pod: "p1", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "1", podUID: "p1",
                    lead: row("1", pod: "p1", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z"),
                    siblings: [
                        row("3", pod: "p1", category: .oom, lastSeen: "2026-08-27T14:35:00.000000Z")
                    ]),
                PodFold(
                    id: "2", podUID: nil,
                    lead: row("2", pod: nil, category: .jobFailed, lastSeen: "2026-08-27T14:37:00.000000Z"),
                    siblings: []),
            ]
        ),
        // A closed crash (newer) and an open probe (older) on one pod: the
        // open row leads, ahead of the worst category.
        (
            [
                row(
                    "1", pod: "p1", category: .crash, closed: true,
                    lastSeen: "2026-08-27T14:39:00.000000Z"),
                row("2", pod: "p1", category: .probe, lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "2", podUID: "p1",
                    lead: row("2", pod: "p1", category: .probe, lastSeen: "2026-08-27T14:35:00.000000Z"),
                    siblings: [
                        row(
                            "1", pod: "p1", category: .crash, closed: true,
                            lastSeen: "2026-08-27T14:39:00.000000Z")
                    ])
            ]
        ),
        // Two closed rows, probe newer and oom older: among closed rows the
        // rank decides, oom outranking probe.
        (
            [
                row(
                    "1", pod: "p1", category: .probe, closed: true,
                    lastSeen: "2026-08-27T14:39:00.000000Z"),
                row(
                    "2", pod: "p1", category: .oom, closed: true,
                    lastSeen: "2026-08-27T14:35:00.000000Z"),
            ],
            [
                PodFold(
                    id: "2", podUID: "p1",
                    lead: row(
                        "2", pod: "p1", category: .oom, closed: true,
                        lastSeen: "2026-08-27T14:35:00.000000Z"),
                    siblings: [
                        row(
                            "1", pod: "p1", category: .probe, closed: true,
                            lastSeen: "2026-08-27T14:39:00.000000Z")
                    ])
            ]
        ),
    ] as [([Incident], [PodFold])])
func rowsOfOnePodFoldUnderTheOpenRowThenTheWorstCategory(rows: [Incident], want: [PodFold]) {
    #expect(podFolds(rows) == want)
}
