import Foundation
import Testing
@testable import TainerMenu

private let helloLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"hello","data":{"engine":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"},"pods":[{"name":"blog","domain":"blog.tainer.me","state":"running","running":2,"total":2},{"name":"api","domain":"api.tainer.me","state":"stopped","running":0,"total":1}]}}"#
private let podLine = #"{"v":1,"ts":"2026-10-31T12:00:01.000Z","type":"pod","data":{"name":"api","domain":"api.tainer.me","state":"running","running":1,"total":1}}"#
private let endedLine = #"{"v":1,"error":{"code":"watch-ended","message":"the event stream ended"}}"#
private let unreachableLine = #"{"v":1,"error":{"code":"stack-unreachable","message":"cannot reach the stack"}}"#

@MainActor
private struct Harness {
    let state = StackState()
    let clock = FakeClock()
    let runner: FakeRunner
    let client: WatchClient

    init(_ script: [FakeRunner.Step]) {
        runner = FakeRunner(script)
        let state = state
        client = WatchClient(executable: "/fake/tainer", runner: runner, clock: clock) { update in
            state.apply(update)
        }
    }

    /// Wait for the loop to park in a backoff sleep, then let that time pass.
    func elapseBackoff() async {
        await settle { clock.pendingBackoff != nil }
        clock.advance(by: clock.pendingBackoff ?? 0)
    }
}

@MainActor @Test func helloPopulatesState() async {
    let h = Harness([.stream(lines: [helloLine], status: 1), .blockUntilCancelled])
    h.client.start()
    await settle { h.state.pods.count == 2 }
    #expect(h.state.pods.map(\.name) == ["blog", "api"])
    #expect(h.runner.arguments.first == ["watch", "--json"])
    h.client.stop()
}

@MainActor @Test func podEventUpserts() async {
    let h = Harness([.stream(lines: [helloLine, podLine], status: 1), .blockUntilCancelled])
    h.client.start()
    await settle { h.state.pods.first(where: { $0.name == "api" })?.state == .running }
    #expect(h.state.pods.count == 2)
    #expect(h.state.pods.map(\.name) == ["api", "blog"])
    h.client.stop()
}

@MainActor @Test func backoffCapsAtThirtySeconds() async {
    let h = Harness(Array(repeating: .stream(lines: [unreachableLine]), count: 8))
    h.client.start()
    for _ in 0..<7 { await h.elapseBackoff() }
    await settle { h.clock.backoffs.count == 7 }
    #expect(h.clock.backoffs == [1, 2, 4, 8, 16, 30, 30])
    h.client.stop()
}

@MainActor @Test func backoffResetsAfterHello() async {
    let h = Harness([
        .stream(lines: [unreachableLine]),
        .stream(lines: [unreachableLine]),
        .stream(lines: [unreachableLine]),
        .stream(lines: [helloLine, endedLine]),
        .stream(lines: [unreachableLine]),
    ])
    h.client.start()
    for _ in 0..<5 { await h.elapseBackoff() }
    await settle { h.clock.backoffs.count == 5 }
    #expect(h.clock.backoffs == [1, 2, 4, 1, 2])
    h.client.stop()
}

@MainActor @Test func oldCLIUsageTextLandsUnreachableWithHint() async {
    let h = Harness(Array(repeating: .stream(lines: [], stderr: "usage: tainer watch --json", status: 1), count: 20))
    h.client.start()
    await settle { h.clock.pendingBackoff != nil }
    #expect(h.state.engine == .recovering)
    #expect(h.state.hint == "update tainer")
    #expect(h.state.statusLine.counts == "update tainer")

    // Keep failing: must never leave recovering early, never tight-spin.
    h.clock.advance(by: 59)
    await settle { h.clock.pendingBackoff != nil }
    #expect(h.state.engine == .recovering)
    #expect(h.runner.spawnCount <= 3)

    h.clock.advance(by: 1)
    await settle { h.state.engine == .unreachable }
    #expect(h.state.hint == "update tainer")
    h.client.stop()
}

@MainActor @Test func helloClearsEscalation() async {
    let h = Harness([.stream(lines: [unreachableLine]), .streamThenBlock(lines: [helloLine])])
    h.client.start()
    await settle { h.clock.pendingBackoff != nil }
    await h.elapseBackoff()
    await settle { h.state.pods.count == 2 }
    h.clock.advance(by: 120)
    for _ in 0..<200 { await Task.yield() }
    #expect(h.state.engine == .healthy(runningPods: 1))
    h.client.stop()
}

@MainActor @Test func invalidFinalLineSkippedThenEndedRespawns() async {
    let h = Harness([
        .stream(lines: [helloLine, #"{"v":1,"ts":"2026-10-31T12:00:"#, endedLine]),
        .blockUntilCancelled,
    ])
    h.client.start()
    await settle { h.clock.pendingBackoff != nil }
    #expect(h.state.engine == .recovering)
    #expect(h.state.pods.count == 2)
    await h.elapseBackoff()
    await settle { h.runner.spawnCount == 2 }
    #expect(h.clock.backoffs == [1])
    h.client.stop()
}

@MainActor @Test func sigintDuringQuitDoesNotRespawn() async {
    let h = Harness([.blockUntilCancelled])
    h.client.start()
    await settle { h.runner.spawnCount == 1 }
    h.client.stop()
    await settle { h.runner.terminations == 1 }
    h.clock.advance(by: 300)
    for _ in 0..<200 { await Task.yield() }
    #expect(h.runner.spawnCount == 1)
    #expect(h.clock.backoffs.isEmpty)
    #expect(h.state.engine == .recovering || h.state.engine == .idle)
}
