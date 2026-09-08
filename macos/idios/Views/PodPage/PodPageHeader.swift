import IdiosModel
import SwiftUI

/// PodPageHeader is the pod page's own header: it never moves, whichever
/// card or container is selected in the columns beside it.
struct PodPageHeader: View {
    let detail: PodDetail
    let clusterName: String
    let flips: Int
    let openWorkload: (Route, WorkloadTab) -> Void
    let openRun: (String) -> Void
    let revealNamespace: (String, String) -> Void

    /// shownTag is the tag on screen; empty until the first one is adopted.
    @State private var shownTag = ""

    private var pod: PodRow { detail.pod }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 9) {
                Badge(text: shownTag.isEmpty ? tag : shownTag, style: tagStyle)
                    .explained("podStateTag")
                Text(countsLine)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }
            (Text("Pod ") + mono(pod.name))
                .font(.system(size: 20, weight: .semibold))
                .textSelection(.enabled)
            identityLine
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 20)
        .padding(.top, 16)
        .padding(.bottom, 14)
        // A readiness that flips every few seconds would otherwise make the
        // header blink; a new tag has to hold before it is shown.
        .task(id: tag) {
            guard !shownTag.isEmpty else {
                shownTag = tag
                return
            }
            try? await Task.sleep(for: .seconds(5))
            guard !Task.isCancelled else { return }
            shownTag = tag
        }
    }

    private var tag: String {
        podStateTag(
            phase: pod.phase, deleted: pod.deletedAt != nil, ready: ready, flips: flips)
    }

    // The kubelet keeps reporting phase Running while every container
    // fails; Ready is what says the pod is not serving.
    private var ready: Bool? {
        guard let condition = detail.conditions.first(where: { $0.type == "Ready" }) else {
            return nil
        }
        return condition.status == "True"
    }

    private var tagStyle: BadgeStyle {
        if pod.deletedAt != nil { return IncidentState.podDeleted.badge }
        switch ready {
        case .some(true): return ContainerState.running.badge
        case .some(false): return ContainerState.waiting.badge
        case .none: return .neutral
        }
    }

    private var countsLine: String {
        let open = detail.incidents.filter { $0.closedAt == nil }.count
        let node =
            pod.nodeName.map { name in
                pod.deletedAt != nil ? "was on node \(name)" : "node \(name)"
            } ?? "not scheduled"
        return plural(detail.containers.count, "container") + " - "
            + plural(detail.incidents.count, "incident") + ", \(open) open - \(node)"
    }

    // The line is where a person leaves the pod: each segment opens what it
    // names, and a segment with nothing behind it stays plain text.
    private var identityLine: some View {
        HStack(spacing: 0) {
            segment(clusterName) { revealNamespace(pod.clusterID, pod.namespace) }
            separator
            segment(pod.namespace) { revealNamespace(pod.clusterID, pod.namespace) }
            if pod.workloadKind != "none" && !pod.workloadName.isEmpty {
                separator
                segment("\(pod.workloadKind) \(pod.workloadName)") {
                    openWorkload(workloadRoute(pod.workloadKind, pod.workloadName), .pods)
                }
            }
            if let kind = pod.controllerKind, let name = pod.controllerName {
                separator
                // A Job in Workloads is a page about one run, and the run page
                // is that page.
                segment("\(kind) \(name)") {
                    guard kind == "Job", let uid = pod.controllerUID else {
                        openWorkload(workloadRoute(kind, name), .pods)
                        return
                    }
                    openRun(uid)
                }
            }
            Spacer(minLength: 0)
        }
        .font(.system(size: 12, design: .monospaced))
    }

    private var separator: some View {
        Text(" / ").font(.system(size: 12, design: .monospaced)).foregroundStyle(.secondary)
    }

    @ViewBuilder private func segment(_ text: String, open: @escaping () -> Void) -> some View {
        if text.isEmpty {
            Text(text).foregroundStyle(.secondary)
        } else {
            Button(text, action: open)
                .buttonStyle(.link)
                .font(.system(size: 12, design: .monospaced))
        }
    }

    private func workloadRoute(_ kind: String, _ name: String) -> Route {
        .workload(cluster: pod.clusterID, namespace: pod.namespace, kind: kind, name: name)
    }
}
