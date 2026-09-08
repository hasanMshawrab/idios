import Foundation
import Testing

@testable import IdiosModel

@Test(
    arguments: [
        ("checkout-api-7d9f8b6c4-x2kqp", "checkout-api", "7d9f8b6c4-x2kqp"),
        ("standalone-pod", "checkout-api", "standalone-pod"),
        ("checkout-api", "", "checkout-api"),
        ("checkout-api", "checkout-api", "checkout-api"),
        ("checkout-api-", "checkout-api", "checkout-api-"),
    ])
func aPodNameKeepsOnlyWhatItsWorkloadNameDoesNotSay(
    name: String, workloadName: String, want: String
) {
    #expect(podNameSuffix(name: name, workloadName: workloadName) == want)
}

@Test(
    arguments: [
        ("abcdef", 6, "abcdef"),
        ("abcdefg", 6, "abc...efg"),
        ("registry.example.com/web@sha256:1111", 12, "regist...6:1111"),
    ])
func aValueTooLongToShowLosesItsMiddleAndNotItsTail(
    value: String, keeping: Int, want: String
) {
    #expect(middleElided(value, keeping: keeping) == want)
}

@Test(
    arguments: [
        (1, "idios-smoke"),
        (2, "prod / idios-smoke"),
    ])
func theClusterNamesTheNamespaceOnlyWhileMoreThanOneIsSelected(count: Int, want: String) {
    #expect(
        scopePrefix(clusterName: "prod", namespace: "idios-smoke", selectedClusterCount: count)
            == want)
}

@Test(
    arguments: [
        (0.0, "<1m"),
        (59.0, "<1m"),
        (60.0, "1m"),
        (38.0 * 60, "38m"),
        (89.0 * 60, "1h 29m"),
        (74.0 * 3600, "3d 2h"),
    ])
func anElapsedTimeIsReadInItsTwoCoarsestUnits(seconds: Double, want: String) {
    let from = Date(timeIntervalSince1970: 0)
    #expect(durationText(from: from, to: from.addingTimeInterval(seconds)) == want)
}

@Test(
    arguments: [
        (
            ["Job", "none", "Cluster", "DaemonSet", "CronJob", "StatefulSet", "Deployment"],
            ["Deployment", "StatefulSet", "DaemonSet", "Cluster", "CronJob", "Job", "none"]
        ),
        (
            ["Zookeeper", "CronJob", "Alertmanager", "Deployment"],
            ["Deployment", "Alertmanager", "Zookeeper", "CronJob"]
        ),
    ])
func kindsSortLongLivedFirstTransientLastAndAnUnknownKindInBetween(
    kinds: [String], want: [String]
) {
    let sorted = kinds.sorted { left, right in
        (kindRank(left), left) < (kindRank(right), right)
    }
    #expect(sorted == want)
}

// A row with no container is about the pod, unless its subject is a Job:
// a Job's incident has no pod to be about, so it must not read pod.
@Test(
    arguments: [
        (Optional("app"), SubjectKind.pod, "app"),
        (Optional("app"), SubjectKind.job, "app"),
        (nil, SubjectKind.pod, "pod"),
        (nil, SubjectKind.job, "job"),
    ])
func aRowWithNoContainerNamesTheSubjectItIsAbout(
    containerName: String?, subjectKind: SubjectKind, want: String
) {
    #expect(scopeLabel(containerName: containerName, subjectKind: subjectKind) == want)
}

// Plurals agree with their counts: one is singular and every other number,
// zero included, is not.
@Test(
    arguments: [
        (0, "restart", "0 restarts"),
        (1, "restart", "1 restart"),
        (2, "pod", "2 pods"),
    ])
func pluralAgreesWithItsCount(count: Int, noun: String, want: String) {
    #expect(plural(count, noun) == want)
}

private let resourceCases: [(String, String?, String?, Int64?, Int64?, ResourceUnit, String?)] = [
    ("cpu", nil, nil, nil, nil, .millicores, nil),
    (
        "memory", "256Mi", "512Mi", 268_435_456, 536_870_912, .bytes,
        "memory request 256Mi (268.4 MB), limit 512Mi (536.9 MB)"
    ),
    ("cpu", "500m", "1", 500, 1000, .millicores, "cpu request 500m, limit 1 (1000m)"),
    ("cpu", "250m", "500m", 250, 500, .millicores, "cpu request 250m, limit 500m"),
    ("memory", "256Mi", nil, nil, nil, .bytes, "memory request 256Mi"),
    ("memory", nil, nil, nil, 536_870_912, .bytes, "memory limit 536.9 MB"),
]

// The kubelet writes the quantity the spec held; the daemon parses it so two
// containers can be compared, and only a quantity that does not already read
// as the parsed value needs both.
@Test(arguments: resourceCases)
func resourceLinePutsTheParsedValueBesideTheWrittenOne(
    name: String, request: String?, limit: String?, requestValue: Int64?, limitValue: Int64?,
    unit: ResourceUnit, want: String?
) {
    #expect(
        resourceLine(
            name: name, request: request, limit: limit, requestValue: requestValue,
            limitValue: limitValue, unit: unit) == want)
}

@Test(
    arguments: [
        (Int64(0), "0m"),
        (Int64(500), "500m"),
        (Int64(1000), "1000m"),
        (Int64(2500), "2500m"),
    ])
func millicoresSpellTheSmallEndOfTheScale(millis: Int64, want: String) {
    #expect(millicores(millis) == want)
}
