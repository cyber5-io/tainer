import Foundation
import Testing
@testable import TainerMenu

private let t0 = Date(timeIntervalSince1970: 1_793_448_000)
private func pod(_ name: String, _ state: PodState, running: Int = 0, total: Int = 1) -> Pod {
    Pod(name: name, domain: "\(name).tainer.me", state: state, running: running, total: total)
}
private let healthy = EngineState(state: .healthy, since: t0, detail: "")

@MainActor @Test func helloWithRunningPodsIsHealthy() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("b", .running, running: 1), pod("a", .stopped)])))
    #expect(s.engine == .healthy(runningPods: 1))
    #expect(s.pods.map(\.name) == ["b", "a"])
    #expect(s.statusLine.pill == "HEALTHY")
    #expect(s.statusLine.counts == "1 of 2 pods running")
}

@MainActor @Test func helloWithNoRunningPodsIsIdle() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("a", .stopped)])))
    #expect(s.engine == .idle)
    #expect(s.statusLine.pill == "HEALTHY")
    #expect(s.statusLine.counts == "0 of 1 pods running")
}

@MainActor @Test func podOrderIsRunningFirstThenName() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("z", .running, running: 1), pod("m", .stopped), pod("c", .running, running: 1)])))
    #expect(s.pods.map(\.name) == ["c", "z", "m"])
}

@MainActor @Test func podEventRederivesBarHealth() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("a", .stopped)])))
    s.apply(.event(.pod(pod("a", .running, running: 1))))
    #expect(s.engine == .healthy(runningPods: 1))
    s.apply(.event(.pod(pod("a", .stopped))))
    #expect(s.engine == .idle)
}

@MainActor @Test func engineRecoveringEventMapsToRecovering() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [])))
    s.apply(.event(.engine(EngineState(state: .recovering, since: t0, detail: ""))))
    #expect(s.engine == .recovering)
    s.apply(.event(.engine(healthy)))
    #expect(s.engine == .idle)
}

@MainActor @Test func recoveringDoesNotDowngradeUnreachable() {
    let s = StackState()
    s.apply(.recovering(hint: nil))
    s.apply(.unreachable)
    s.apply(.recovering(hint: nil))
    #expect(s.engine == .unreachable)
}

@MainActor @Test func progressEventDoesNotChangePods() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("a", .stopped)])))
    s.apply(.event(.progress(Progress(op: "start", pod: "a", step: "pull", message: "", percent: -1, done: false))))
    #expect(s.pods.count == 1)
}

@MainActor @Test func noPodsKeepsHealthyPillWithFriendlyCounts() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [])))
    #expect(s.engine == .idle)
    #expect(s.statusLine.pill == "HEALTHY")
    #expect(s.statusLine.counts == "No pods yet")
}

@MainActor @Test func partialPodCountsAsRunning() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("a", .stopped), pod("b", .partial, running: 1, total: 2)])))
    #expect(s.engine == .healthy(runningPods: 1))
    #expect(s.pods.map(\.name) == ["b", "a"])
    #expect(s.statusLine.counts == "1 of 2 pods running")
    s.apply(.event(.hello(engine: healthy, pods: [pod("b", .partial, running: 1, total: 2)])))
    #expect(s.statusLine.counts == "1 of 1 pods running")
}

@MainActor @Test func recoveringAndUnreachablePills() {
    let s = StackState()
    s.apply(.recovering(hint: nil))
    #expect(s.statusLine.pill == "RECOVERING")
    s.apply(.unreachable)
    #expect(s.statusLine.pill == "UNREACHABLE")
}
