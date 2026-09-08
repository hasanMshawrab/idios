import IdiosAPI
import Testing

@testable import IdiosModel

let unhealthyEvent = Event(
    id: "71",
    clusterID: "1",
    eventUID: "ev-unhealthy-1",
    namespace: "idios-smoke",
    type: "Warning",
    involvedKind: "Pod",
    involvedName: "checkout-api-7d9f8b6c4-x2kqp",
    involvedUID: "pod-crash",
    fieldPath: "spec.containers{api}",
    reason: "Unhealthy",
    message: "Readiness probe failed: HTTP probe failed with statuscode: 503",
    sourceComponent: "kubelet",
    count: 3,
    firstTS: Timestamp("2026-08-27T14:35:00.000000Z"),
    lastTS: Timestamp("2026-08-27T14:39:00.000000Z"),
    category: .probe,
    incidentID: "412")

/// killingEvent is an event of the same pod that attached to no incident.
let killingEvent = Event(
    id: "72",
    clusterID: "1",
    eventUID: "ev-killing-1",
    namespace: "idios-smoke",
    type: "Normal",
    involvedKind: "Pod",
    involvedName: "checkout-api-7d9f8b6c4-x2kqp",
    involvedUID: "pod-crash",
    fieldPath: "spec.containers{api}",
    reason: "Killing",
    message: "Stopping container api",
    sourceComponent: "kubelet",
    count: 1,
    firstTS: Timestamp("2026-08-27T14:39:01.000000Z"),
    lastTS: Timestamp("2026-08-27T14:39:01.000000Z"),
    category: nil,
    incidentID: nil)

@Test func eventListCarriesItsRowsAndTheTruncationFlag() throws {
    let page = try Page<Event>(
        wire: fixture(Components.Schemas.EventsResponse.self, "events.json"))
    #expect(page == Page(rows: [unhealthyEvent], truncated: true))
}

private let spanCases: [(Int32, Timestamp?, Timestamp?, String?)] = [
    (1, Timestamp("2026-08-27T14:35:00.000000Z"), Timestamp("2026-08-27T14:39:00.000000Z"), nil),
    (17, nil, Timestamp("2026-08-27T14:39:00.000000Z"), nil),
    (17, Timestamp("2026-08-27T14:39:00.000000Z"), Timestamp("2026-08-27T14:39:00.000000Z"), nil),
    (
        17, Timestamp("2026-08-27T14:35:00.000000Z"), Timestamp("2026-08-27T15:17:00.000000Z"),
        "x17 over 42m, first 14:35"
    ),
    (
        17, Timestamp("2026-08-27 14:35:00"), Timestamp("2026-08-27 15:17:00"),
        "x17, first 2026-08-27 14:35:00"
    ),
]

// Kubernetes keeps one row per reason and counts the repeats, so a pod whose
// whole story happens inside one minute needs first_ts beside the row's stamp.
@Test(arguments: spanCases)
func eventSpanSaysHowLongARepeatedEventWentOn(
    count: Int32, firstTS: Timestamp?, lastTS: Timestamp?, want: String?
) {
    #expect(eventSpan(count: count, firstTS: firstTS, lastTS: lastTS) == want)
}
