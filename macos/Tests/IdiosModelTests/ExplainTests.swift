import Testing

@testable import IdiosModel

private func screen(regions: Int) -> ScreenAudit {
    ScreenAudit(regions: regions, uniqueIDs: true, everyRegionSpeaks: true)
}

// Help mode marks the major parts of a screen and nothing else, so a table is
// a handful of regions: the tag and the column of the pod page, the pane's own
// cards, and the two links in the rail.
private let expectedAudits: [ExplainedScreen: ScreenAudit] = [
    .incidents(.rows): screen(regions: 5),
    .incidents(.empty): screen(regions: 1),
    .palette: screen(regions: 2),
    .workloads(.tree): screen(regions: 3),
    .workloads(.overview): screen(regions: 5),
    .workloads(.pods): screen(regions: 5),
    .workloads(.runs): screen(regions: 6),
    .workloads(.rollouts): screen(regions: 5),
    .workloads(.incidents): screen(regions: 4),
    .podCard(.events): screen(regions: 5),
    .podCard(.conditions): screen(regions: 5),
    .podCard(.files): screen(regions: 5),
    .podCard(.podJSON): screen(regions: 5),
    .podCard(.related): screen(regions: 5),
    .container(.overview): screen(regions: 7),
    .container(.timeline): screen(regions: 5),
    .container(.logs): screen(regions: 6),
    .run(.overview): screen(regions: 6),
    .run(.timeline): screen(regions: 5),
    .run(.logs): screen(regions: 5),
    .run(.events): screen(regions: 5),
    .status: screen(regions: 4),
]

// Every screen the [?] turns help mode on over declares a table. The audit is
// compared whole: a table that gained a duplicated id and lost a region would
// pass two of three separate assertions.
@Test(arguments: explainedScreens)
func everyScreenExplainsItself(screen: ExplainedScreen) {
    #expect(audit(screen) == expectedAudits[screen])
}

private let expectedContainerRegions: [ExplainedRegion] = [
    ExplainedRegion(
        id: "podStateTag", title: "Pod state tag",
        tags: ["pods.phase", "conditions[Ready]", "deleted_at"],
        sentence: """
            Look here first. The kubelet keeps reporting phase Running while every container \
            fails, so the Ready condition is what says the pod is not serving. It reads \
            LOOPING when readiness flipped more than three times in the window, and DELETED \
            once the pod is gone.
            """),
    ExplainedRegion(
        id: "containerCards", title: "Container cards",
        tags: ["containers.kind", "category", "occurrences"],
        sentence: """
            One card per container, init then app then sidecar then ephemeral, the selected \
            one filled with the accent. Each badge is one incident with its category and how \
            many times it has happened, so this column says whether one container is failing \
            or the whole pod is.
            """),
    ExplainedRegion(
        id: "verdict", title: "Verdict block",
        tags: ["occurrences", "mem_limit_bytes", "capture_gap", "image_id"],
        sentence: """
            Read this second, and often stop here: sentences built from fields the page \
            already holds. What happened and how often since when, the memory limit when the \
            kill was an OOM, what was and was not captured, and whether the image digest \
            changed since the incident opened.
            """),
    ExplainedRegion(
        id: "kubeletFacts", title: "What the kubelet reports",
        tags: ["restart_count", "last_terminated_reason", "image_id", "mem_limit_bytes"],
        sentence: """
            Read this third: the evidence the verdict was built from, in the kubelet's own \
            words. Restart count is the container's whole life while occurrences above is \
            this incident's share of it, and the image and digest are the ones recorded when \
            the incident opened, not the current ones.
            """),
    ExplainedRegion(
        id: "capturedLogs", title: "Captured logs",
        tags: ["artifacts", "capture_gap", "capture_note"],
        sentence: """
            Read this fourth. The scrubber walks one file per dead instance, oldest to \
            newest, so you can see whether every attempt failed the same way. A missing file \
            is never an empty pane: the gap is named and the API's own words are quoted, \
            which separates a container that printed nothing from a log that could never \
            have been fetched.
            """),
    ExplainedRegion(
        id: "ownerChain", title: "Owner chain",
        tags: ["workload_kind", "controller_kind", "pod_uid", "container_name"],
        sentence: """
            Read this last, when the verdict is not enough: four links from what decides how \
            many copies run down to the process that failed, each with the uid that \
            identifies it. It answers who will replace this pod, and whether the thing to \
            change is this container or the workload above it.
            """),
    ExplainedRegion(
        id: "siblings", title: "Siblings",
        tags: ["controller_uid", "category"],
        sentence: """
            This pod first, then the others of the same controller, with a category badge \
            where one is failing and a readiness word where none is. One red dot among green \
            is this pod's problem; all red is the image, the config or the cluster.
            """),
]

// The tag, the column and the rail stay marked with the Pod card selected,
// and the pane's one region takes the place of the container's three.
private let expectedPodCardRegions: [ExplainedRegion] = [
    expectedContainerRegions[0],
    expectedContainerRegions[1],
    ExplainedRegion(
        id: "podEvents", title: "Events",
        tags: ["k8s_events", "involved_uid"],
        sentence: """
            Every container's events together in one served-order table with a container \
            column, plus the pod-level events no container claims. It is the pod's whole \
            event stream, not one container's slice of it.
            """),
    expectedContainerRegions[5],
    expectedContainerRegions[6],
]

// The words of the pod page's notes are the approved text, and nothing else
// compares them with the code that carries them: a sentence reworded in the
// tables and nowhere else fails here.
@Test func thePodPageSaysTheSameWordsTheOverlayWasApprovedWith() {
    #expect(explainedRegions(for: .container(.overview)) == expectedContainerRegions)
    #expect(explainedRegions(for: .podCard(.events)) == expectedPodCardRegions)
}

private let states: [IncidentState] = [
    .open, .acknowledged, .recovered, .podDeleted, .jobFinished, .manual, .dismissed,
    .attention,
]

private let categories: [Category] = [
    .oom, .crash, .uncleanExit, .imagePull, .config, .probe, .scheduling, .stuck,
    .nodePressure, .rescheduled, .jobFailed, .other,
]

// A vocabulary word is matched exactly, because "pod" in a title is prose
// and "Pod" is the kind, and a category's wire value is not the word a
// person reads.
private func definedSentence(forTitle title: String) -> String? {
    if let state = states.first(where: { $0.title == title }) { return state.tooltip }
    if let category = categories.first(where: { $0.label == title }) { return category.tooltip }
    if let word = kindWords.first(where: { $0.word == title }) { return word.sentence }
    return nil
}

private func rewritesADefinedWord(_ region: ExplainedRegion) -> Bool {
    guard let defined = definedSentence(forTitle: region.title) else { return false }
    return region.sentence != defined
}

private let matchCases: [(region: ExplainedRegion, rewrites: Bool)] = [
    (ExplainedRegion(id: "a", title: "pod", tags: [], sentence: "One pod of many."), false),
    (
        ExplainedRegion(id: "b", title: "unclean_exit", tags: [], sentence: "Not the label."),
        false
    ),
    (
        ExplainedRegion(id: "c", title: "Verdict block", tags: [], sentence: "Sentences."),
        false
    ),
    (
        ExplainedRegion(id: "d", title: "Open", tags: [], sentence: "Nobody looked yet."),
        true
    ),
    (
        ExplainedRegion(id: "e", title: "Open", tags: [], sentence: IncidentState.open.tooltip),
        false
    ),
]

// Where a region's name is a state, a category or a kind, its sentence is
// that word's tooltip unchanged, so a note and the badge a person hovers
// never say two different things.
@Test func noExplanationRewritesAWordTheApplicationAlreadyDefines() {
    for row in matchCases {
        #expect(rewritesADefinedWord(row.region) == row.rewrites, "\(row.region.title)")
    }
    let offenders = Set(
        explainedScreens
            .flatMap(explainedRegions(for:))
            .filter(rewritesADefinedWord)
            .map(\.id))
    #expect(offenders == [])
}
