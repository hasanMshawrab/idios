/// relatedElsewhere keeps the related rows a pod's own page cannot already
/// show: the Job's own row and the retries that ran in other pods. This pod's
/// incidents are the cards in the left column, so a related list that repeated
/// them would say the same thing twice.
public func relatedElsewhere(_ rows: [Incident], podUID: String?) -> [Incident] {
    rows.filter { $0.podUID == nil || $0.podUID != podUID }
}
