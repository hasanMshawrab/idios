import Testing

@testable import IdiosModel

private let t1 = "2026-08-27T14:39:00.000000Z"
private let t2 = "2026-08-27T14:37:00.000000Z"
private let t3 = "2026-08-27T14:35:00.000000Z"

// Every rolled-up case below shares this key: crashIncident's cluster,
// namespace, workload kind and name, container "api", category .crash.
private let crashRollupID = "g/rollup/1/idios-smoke/Deployment/checkout-api/api/crash"

// Each case is its own typed constant, not an array literal entry: the type
// checker times out inferring ten inline tuples of nested Incident and
// GroupEntry literals at once.

private let noRows: ([Incident], [GroupEntry]) = ([], [])

// Three pods on the same key, all open: one rollup of the three single-row
// folds, in served order.
private let threePodsAllOpen: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .crash, lastSeen: t2),
        row("3", pod: "p3", category: .crash, lastSeen: t3),
    ],
    [
        .rollup(
            Rollup(
                id: crashRollupID, containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1), siblings: []),
                    PodFold(
                        id: "2", podUID: "p2",
                        lead: row("2", pod: "p2", category: .crash, lastSeen: t2), siblings: []),
                    PodFold(
                        id: "3", podUID: "p3",
                        lead: row("3", pod: "p3", category: .crash, lastSeen: t3), siblings: []),
                ]))
    ]
)

// A third pod's oom row served between two crash rows does not join the
// rollup; the rollup takes the place of its first served row.
private let thirdPodOtherCategoryBetween: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .oom, lastSeen: t2),
        row("3", pod: "p3", category: .crash, lastSeen: t3),
    ],
    [
        .rollup(
            Rollup(
                id: crashRollupID, containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1), siblings: []),
                    PodFold(
                        id: "3", podUID: "p3",
                        lead: row("3", pod: "p3", category: .crash, lastSeen: t3), siblings: []),
                ])),
        .fold(
            PodFold(
                id: "2", podUID: "p2",
                lead: row("2", pod: "p2", category: .oom, lastSeen: t2), siblings: [])),
    ]
)

// One pod with two rows on the key: a key held by one pod never rolls up,
// so this is a plain fold.
private let oneKeyOnePod: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, closed: true, lastSeen: t1),
        row("2", pod: "p1", category: .crash, lastSeen: t2),
    ],
    [
        .fold(
            PodFold(
                id: "2", podUID: "p1",
                lead: row("2", pod: "p1", category: .crash, lastSeen: t2),
                siblings: [
                    row("1", pod: "p1", category: .crash, closed: true, lastSeen: t1)
                ]))
    ]
)

// Same category, different containers: the key differs per row, so both
// are plain folds.
private let sameCategoryDifferentContainers: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1, container: "api"),
        row("2", pod: "p2", category: .crash, lastSeen: t2, container: "sidecar"),
    ],
    [
        .fold(
            PodFold(
                id: "1", podUID: "p1",
                lead: row("1", pod: "p1", category: .crash, lastSeen: t1, container: "api"),
                siblings: [])),
        .fold(
            PodFold(
                id: "2", podUID: "p2",
                lead: row("2", pod: "p2", category: .crash, lastSeen: t2, container: "sidecar"),
                siblings: [])),
    ]
)

// Same container, different categories: the key differs per row, so both
// are plain folds.
private let sameContainerDifferentCategories: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .oom, lastSeen: t2),
    ],
    [
        .fold(
            PodFold(
                id: "1", podUID: "p1",
                lead: row("1", pod: "p1", category: .crash, lastSeen: t1), siblings: [])),
        .fold(
            PodFold(
                id: "2", podUID: "p2",
                lead: row("2", pod: "p2", category: .oom, lastSeen: t2), siblings: [])),
    ]
)

// A job-subject row served between two crash rows never joins the rollup;
// it keeps its own fold.
private let jobRowBetween: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: nil, category: .jobFailed, lastSeen: t2, container: nil),
        row("3", pod: "p2", category: .crash, lastSeen: t3),
    ],
    [
        .rollup(
            Rollup(
                id: crashRollupID, containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1), siblings: []),
                    PodFold(
                        id: "3", podUID: "p2",
                        lead: row("3", pod: "p2", category: .crash, lastSeen: t3), siblings: []),
                ])),
        .fold(
            PodFold(
                id: "2", podUID: nil,
                lead: row("2", pod: nil, category: .jobFailed, lastSeen: t2, container: nil),
                siblings: [])),
    ]
)

// Two bare pods sharing a container and a category: a bare pod is not
// replica fan-out, so both are plain folds.
private let twoBarePods: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .oom, lastSeen: t1, container: "app", workload: ""),
        row("2", pod: "p2", category: .oom, lastSeen: t2, container: "app", workload: ""),
    ],
    [
        .fold(
            PodFold(
                id: "1", podUID: "p1",
                lead: row(
                    "1", pod: "p1", category: .oom, lastSeen: t1, container: "app", workload: ""),
                siblings: [])),
        .fold(
            PodFold(
                id: "2", podUID: "p2",
                lead: row(
                    "2", pod: "p2", category: .oom, lastSeen: t2, container: "app", workload: ""),
                siblings: [])),
    ]
)

// Two pods, same container and category, different workload: the key
// carries the workload, so two workloads' pods never roll up together
// under a namespace, category, cluster or time group.
private let sameContainerCategoryDifferentWorkloads: ([Incident], [GroupEntry]) = (
    [
        row(
            "1", pod: "p1", category: .crash, lastSeen: t1, container: "api",
            workload: "checkout-api"),
        row(
            "2", pod: "p2", category: .crash, lastSeen: t2, container: "api",
            workload: "ledger-worker"),
    ],
    [
        .fold(
            PodFold(
                id: "1", podUID: "p1",
                lead: row(
                    "1", pod: "p1", category: .crash, lastSeen: t1, container: "api",
                    workload: "checkout-api"),
                siblings: [])),
        .fold(
            PodFold(
                id: "2", podUID: "p2",
                lead: row(
                    "2", pod: "p2", category: .crash, lastSeen: t2, container: "api",
                    workload: "ledger-worker"),
                siblings: [])),
    ]
)

// A pod contributing two rows to the key: its fold under the rollup is led
// by the open row with the closed one its sibling.
private let onePodTwoRowsInKey: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .crash, lastSeen: t2),
        row("3", pod: "p1", category: .crash, closed: true, lastSeen: t3),
    ],
    [
        .rollup(
            Rollup(
                id: crashRollupID, containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1),
                        siblings: [
                            row("3", pod: "p1", category: .crash, closed: true, lastSeen: t3)
                        ]),
                    PodFold(
                        id: "2", podUID: "p2",
                        lead: row("2", pod: "p2", category: .crash, lastSeen: t2), siblings: []),
                ]))
    ]
)

// A rolled-up pod's row outside the key falls to its own plain fold.
private let rolledPodRowOutsideKey: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1, container: "api"),
        row("2", pod: "p1", category: .probe, lastSeen: t2, container: "sidecar"),
        row("3", pod: "p2", category: .crash, lastSeen: t3, container: "api"),
    ],
    [
        .rollup(
            Rollup(
                id: crashRollupID, containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1, container: "api"),
                        siblings: []),
                    PodFold(
                        id: "3", podUID: "p2",
                        lead: row("3", pod: "p2", category: .crash, lastSeen: t3, container: "api"),
                        siblings: []),
                ])),
        .fold(
            PodFold(
                id: "2", podUID: "p1",
                lead: row("2", pod: "p1", category: .probe, lastSeen: t2, container: "sidecar"),
                siblings: [])),
    ]
)

// A pod-level row (no container) on two pods rolls up with a nil
// containerName.
private let podLevelRows: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .scheduling, lastSeen: t1, container: nil),
        row("2", pod: "p2", category: .scheduling, lastSeen: t2, container: nil),
    ],
    [
        .rollup(
            Rollup(
                id: "g/rollup/1/idios-smoke/Deployment/checkout-api//scheduling",
                containerName: nil, category: .scheduling,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .scheduling, lastSeen: t1, container: nil),
                        siblings: []),
                    PodFold(
                        id: "2", podUID: "p2",
                        lead: row("2", pod: "p2", category: .scheduling, lastSeen: t2, container: nil),
                        siblings: []),
                ]))
    ]
)

@Test(
    arguments: [
        noRows,
        threePodsAllOpen,
        thirdPodOtherCategoryBetween,
        oneKeyOnePod,
        sameCategoryDifferentContainers,
        sameContainerDifferentCategories,
        jobRowBetween,
        twoBarePods,
        sameContainerCategoryDifferentWorkloads,
        onePodTwoRowsInKey,
        rolledPodRowOutsideKey,
        podLevelRows,
    ])
func rowsOfTwoOrMorePodsSharingAContainerAndACategoryRollUp(rows: [Incident], want: [GroupEntry]) {
    #expect(groupEntries(rows, groupID: "g") == want)
}

// Rollup A: every row shares the same reason and exit code.
private let sameReasonSameExitAllOpen = Rollup(
    id: "g", containerName: "api", category: .crash,
    folds: [
        PodFold(
            id: "1", podUID: "p1",
            lead: row("1", pod: "p1", category: .crash, lastSeen: t1, lastReason: "Error", exitCode: 1),
            siblings: [
                row("3", pod: "p1", category: .crash, lastSeen: t3, lastReason: "Error", exitCode: 1)
            ]),
        PodFold(
            id: "2", podUID: "p2",
            lead: row("2", pod: "p2", category: .crash, lastSeen: t2, lastReason: "Error", exitCode: 1),
            siblings: []),
    ])

// Rollup B: a differing reason and exit code, and a closed row.
private let differingReasonExitOneClosed = Rollup(
    id: "g", containerName: "api", category: .crash,
    folds: [
        PodFold(
            id: "1", podUID: "p1",
            lead: row("1", pod: "p1", category: .crash, lastSeen: t1, lastReason: "Error", exitCode: 1),
            siblings: [
                row(
                    "3", pod: "p1", category: .crash, closed: true, lastSeen: t3,
                    lastReason: "Error", exitCode: 137)
            ]),
        PodFold(
            id: "2", podUID: "p2",
            lead: row(
                "2", pod: "p2", category: .crash, lastSeen: t2, lastReason: "CrashLoopBackOff",
                exitCode: nil),
            siblings: []),
    ])

@Test(
    arguments: [
        (sameReasonSameExitAllOpen, (["Error"], [1] as [Int32], 3)),
        (
            differingReasonExitOneClosed,
            (["Error", "CrashLoopBackOff"], [1, 137] as [Int32], 2)
        ),
    ] as [(Rollup, ([String], [Int32], Int))])
func theRollupLineNamesEveryReasonAndExitCodeOnce(rollup: Rollup, want: ([String], [Int32], Int)) {
    #expect((rollup.reasons, rollup.exitCodes, rollup.openCount) == want)
}
