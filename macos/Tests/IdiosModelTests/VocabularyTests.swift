import Testing

@testable import IdiosModel

// The kind vocabulary is text a reader is taught the product by, so it is
// written down once and asserted whole: a word added without its sentence,
// or a chain that stops naming what owns what, fails here and nowhere else.
@Test func theKindVocabularySaysWhatOwnsWhat() {
    #expect(
        kindWords == [
            KindWord(
                word: "Pod",
                sentence: """
                    One running copy of a program. The only thing that actually runs, and the \
                    thing that gets replaced.
                    """),
            KindWord(word: "Container", sentence: "One process inside a pod."),
            KindWord(
                word: "Workload",
                sentence: """
                    What owns pods and decides how many run: a Deployment, a CronJob, a Job, \
                    or a bare pod that nothing owns.
                    """),
            KindWord(
                word: "Deployment",
                sentence: "Keeps N copies running and replaces one that dies."),
            KindWord(
                word: "Job",
                sentence: """
                    Runs a task until it succeeds, retrying up to the backoff limit, each \
                    retry a new pod. Failed when the retries run out.
                    """),
            KindWord(
                word: "CronJob", sentence: "Creates a new Job on every tick of its schedule."),
            KindWord(word: "Run", sentence: "One Job created by a CronJob."),
            KindWord(
                word: "Incident",
                sentence: """
                    One failure idios recorded: one per container failure, and one more on \
                    the Job when it gives up.
                    """),
        ])
    #expect(
        ownerChains == [
            "CronJob -> Job (a run) -> Pod (an attempt) -> Container",
            "Deployment -> ReplicaSet -> Pod -> Container",
            "bare Pod -> Container",
        ])
}
