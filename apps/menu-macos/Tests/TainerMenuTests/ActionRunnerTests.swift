import Foundation
import Testing
@testable import TainerMenu

final class FakeSink: NotificationSink, @unchecked Sendable {
    private let lock = NSLock()
    private var bodies: [String] = []
    var posted: [String] { lock.withLock { bodies } }
    func post(_ message: String) async { lock.withLock { bodies.append(message) } }
}

private let podDoc = #"{"v":1,"name":"ops","state":"running"}"#
private let errDoc = #"{"v":1,"error":{"code":"start-failed","message":"port 80 is in use"}}"#

@MainActor
private func makeRunner(_ runner: FakeRunner, state: StackState = StackState(), sink: FakeSink = FakeSink(), cli: String? = "/bin/tainer") -> ActionRunner {
    ActionRunner(state: state, runner: runner, sink: sink, locate: { cli })
}

@MainActor
@Test func successClearsBusyWithoutMutatingPods() async {
    let state = StackState()
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: [podDoc], status: 0)])
    let runner = makeRunner(fake, state: state, sink: sink)
    let before = state.pods
    await runner.run(.stop, pod: "ops")
    #expect(state.busy.isEmpty)
    #expect(state.pods == before)
    #expect(sink.posted.isEmpty)
    #expect(fake.arguments == [["stop", "ops", "--json"]])
}

@MainActor
@Test func busyIsSetWhileRunning() async {
    let state = StackState()
    let fake = FakeRunner([.blockUntilCancelled])
    let runner = makeRunner(fake, state: state)
    let task = Task { await runner.run(.start, pod: "ops") }
    await settle { state.busy.contains("ops") }
    task.cancel()
    await task.value
    #expect(state.busy.isEmpty)
}

@MainActor
@Test func envelopeFailurePostsNotificationAndReverts() async {
    let state = StackState()
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: [errDoc], status: 1)])
    await makeRunner(fake, state: state, sink: sink).run(.start, pod: "ops")
    #expect(state.busy.isEmpty)
    #expect(sink.posted == ["ops: port 80 is in use"])
}

@MainActor
@Test func spawnFailurePostsOneBanner() async {
    let state = StackState()
    let sink = FakeSink()
    await makeRunner(FakeRunner([.spawnFailure]), state: state, sink: sink).run(.restart, pod: "ops")
    #expect(state.busy.isEmpty)
    #expect(sink.posted.count == 1)
    #expect(sink.posted[0].hasPrefix("ops: "))
}

@MainActor
@Test func missingCLIPostsBannerWithoutSpawning() async {
    let sink = FakeSink()
    let fake = FakeRunner([])
    await makeRunner(fake, sink: sink, cli: nil).run(.stop, pod: "ops")
    #expect(fake.spawnCount == 0)
    #expect(sink.posted.count == 1)
}

@MainActor
@Test func nonZeroExitWithReportDocumentIsNotAFailure() async {
    let sink = FakeSink()
    let report = #"{"v":1,"ok":false,"checks":[{"name":"engine","status":"fail"}]}"#
    let fake = FakeRunner([.stream(lines: [report], status: 1)])
    await makeRunner(fake, sink: sink).run(.doctor, pod: nil)
    #expect(sink.posted.isEmpty)
}

@MainActor
@Test func nonZeroExitWithNoOutputIsAFailure() async {
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: [], stderr: "boom", status: 2)])
    await makeRunner(fake, sink: sink).run(.stop, pod: "ops")
    #expect(sink.posted.count == 1)
}

@MainActor
@Test func doctorFixSpawnsCorrectArgv() async {
    let state = StackState()
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: [errDoc], status: 1)])
    await makeRunner(fake, state: state, sink: sink).run(.doctor, pod: nil)
    #expect(fake.arguments == [["doctor", "--fix", "--json"]])
    #expect(state.busy.isEmpty)
    #expect(sink.posted == ["doctor: port 80 is in use"])
}

@MainActor
@Test func concurrentActionOnSamePodRefused() async {
    let state = StackState()
    let fake = FakeRunner([.blockUntilCancelled, .stream(lines: [podDoc], status: 0)])
    let runner = makeRunner(fake, state: state)
    let first = Task { await runner.run(.restart, pod: "ops") }
    await settle { state.busy.contains("ops") }
    await runner.run(.stop, pod: "ops")
    #expect(fake.spawnCount == 1)
    await runner.run(.stop, pod: "other")
    #expect(fake.spawnCount == 2)
    first.cancel()
    await first.value
    #expect(state.busy.isEmpty)
}

@MainActor
@Test func nonRunnableActionsAreIgnored() async {
    let fake = FakeRunner([])
    let runner = makeRunner(fake)
    await runner.run(.open, pod: "ops")
    await runner.run(.manage, pod: "ops")
    await runner.run(.stop, pod: nil)
    #expect(fake.spawnCount == 0)
}
