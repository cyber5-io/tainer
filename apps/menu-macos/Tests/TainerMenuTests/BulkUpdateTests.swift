import Foundation
import Testing
@testable import TainerMenu

/// tain-126: "Update all pods" shows the run, the pod being updated, and how it ended.

/// Each update waits until the test lets it finish, with the result the test gives.
@MainActor
final class SteppedUpdates {
    private(set) var started: [String] = []
    private var pending: [CheckedContinuation<Bool, Never>] = []
    var waiting: Int { pending.count }

    func update(_ name: String) async -> Bool {
        started.append(name)
        return await withCheckedContinuation { pending.append($0) }
    }

    func finish(_ ok: Bool) {
        pending.removeFirst().resume(returning: ok)
    }
}

private func pod(_ name: String, _ state: PodState = .running) -> Pod {
    Pod(name: name, domain: "\(name).tainer.me", state: state, running: 1, total: 1)
}

@MainActor
@Test func bulkRunWalksThePodsOneAtATime() async {
    let bulk = BulkUpdate(outcomeHold: .seconds(60))
    let steps = SteppedUpdates()
    let run = Task { await bulk.run(["a", "b", "c"], update: steps.update) }
    await settle { steps.waiting == 1 }
    #expect(bulk.isRunning)
    #expect(bulk.current == "a")
    #expect(steps.started == ["a"])
    steps.finish(true)
    await settle { steps.waiting == 1 && bulk.current == "b" }
    #expect(steps.started == ["a", "b"])
    steps.finish(false)
    await settle { steps.waiting == 1 && bulk.current == "c" }
    steps.finish(true)
    #expect(await run.value)
    #expect(!bulk.isRunning)
    #expect(bulk.current == nil)
    #expect(bulk.outcome == BulkOutcome(updated: 2, failed: 1))
}

@MainActor
@Test func reClickDuringARunIsANoOp() async {
    let bulk = BulkUpdate(outcomeHold: .seconds(60))
    let steps = SteppedUpdates()
    let run = Task { await bulk.run(["a", "b"], update: steps.update) }
    await settle { steps.waiting == 1 }
    #expect(await bulk.run(["a", "b"], update: steps.update) == false)
    #expect(steps.started == ["a"])
    steps.finish(true)
    await settle { steps.waiting == 1 }
    steps.finish(true)
    #expect(await run.value)
    #expect(steps.started == ["a", "b"])
}

@MainActor
@Test func outcomeShowsBrieflyThenGoesIdle() async {
    let bulk = BulkUpdate(outcomeHold: .milliseconds(30))
    await bulk.run(["a", "b"]) { _ in true }
    #expect(bulk.outcome == BulkOutcome(updated: 2, failed: 0))
    await settle { bulk.outcome == nil }
    // A new run clears an outcome that is still showing.
    let slow = BulkUpdate(outcomeHold: .seconds(60))
    await slow.run(["a"]) { _ in false }
    #expect(slow.outcome == BulkOutcome(updated: 0, failed: 1))
    let steps = SteppedUpdates()
    let run = Task { await slow.run(["b"], update: steps.update) }
    await settle { steps.waiting == 1 }
    #expect(slow.outcome == nil)
    steps.finish(true)
    _ = await run.value
    #expect(slow.outcome == BulkOutcome(updated: 1, failed: 0))
}

@Test func bulkRowSaysWhatIsHappening() {
    let idle = BulkRowModel(isRunning: false, outcome: nil)
    #expect(idle.title == "Update all pods" && idle.symbol == "arrow.down.circle" && idle.enabled && !idle.busy && !idle.muted)
    let running = BulkRowModel(isRunning: true, outcome: nil)
    #expect(running.title == "Updating all pods…" && running.busy && !running.enabled && running.muted)
    let allOK = BulkRowModel(isRunning: false, outcome: BulkOutcome(updated: 3, failed: 0))
    #expect(allOK.title == "All pods updated" && allOK.symbol == "checkmark.circle" && !allOK.enabled)
    let mixed = BulkRowModel(isRunning: false, outcome: BulkOutcome(updated: 2, failed: 1))
    #expect(mixed.title == "2 pods updated, 1 failed" && mixed.symbol == "exclamationmark.triangle")
    #expect(BulkRowModel(isRunning: false, outcome: BulkOutcome(updated: 1, failed: 2)).title == "1 pod updated, 2 failed")
    #expect(BulkRowModel(isRunning: false, outcome: BulkOutcome(updated: 0, failed: 1)).title == "0 pods updated, 1 failed")
}

@Test func bulkRowStaysWhileRunningOrShowingAnOutcome() {
    #expect(BulkRowModel.visible(podsWithUpdates: 2, isRunning: false, outcome: nil))
    #expect(!BulkRowModel.visible(podsWithUpdates: 1, isRunning: false, outcome: nil))
    // The run empties the update list as it goes; the row must not vanish under the user.
    #expect(BulkRowModel.visible(podsWithUpdates: 0, isRunning: true, outcome: nil))
    #expect(BulkRowModel.visible(podsWithUpdates: 0, isRunning: false, outcome: BulkOutcome(updated: 2, failed: 0)))
}

@Test func podRowsDuringABulkRun() {
    let update = PodUpdate(name: "a", parts: "WordPress")
    // The pod being updated: spinner, no buttons, an updating note.
    let current = PodRowModel(pod: pod("a"), busy: false, update: update, updatesAllowed: false, updating: true)
    #expect(current.showsSpinner)
    #expect(current.actions.isEmpty)
    #expect(current.statusNote == "updating…")
    // Every other pod keeps its buttons but loses the update arrow.
    let waiting = PodRowModel(pod: pod("b"), busy: false, update: update, updatesAllowed: false)
    #expect(!waiting.showsSpinner)
    #expect(!waiting.actions.contains(.update))
    #expect(waiting.statusNote == nil)
    // Back to normal once the run moves on.
    let done = PodRowModel(pod: pod("a"), busy: false, update: nil, updatesAllowed: true)
    #expect(!done.showsSpinner && done.statusNote == nil && !done.actions.isEmpty)
}

@MainActor
@Test func bulkOrderFollowsTheList() {
    let state = StackState()
    state.applyList(["zed", "alpha", "mid"].map { ListedProject(name: $0, domain: "\($0).tainer.me") })
    state.apply(.event(.pod(pod("zed"))))
    // zed is running so it is listed first; "ghost" isn't listed and goes last.
    #expect(BulkUpdate.order(["alpha", "ghost", "zed"], listed: state.allPods.map(\.name)) == ["zed", "alpha", "ghost"])
}

@MainActor
@Test func actionRunReportsWhetherTheUpdateSucceeded() async {
    let ok = ActionRunner(state: StackState(), runner: FakeRunner([.stream(lines: [#"{"v":1,"name":"a","state":"running"}"#], status: 0)]),
                          sink: FakeSink(), locate: { "/bin/tainer" })
    #expect(await ok.perform(.update, pod: "a"))
    let failing = ActionRunner(state: StackState(), runner: FakeRunner([.stream(lines: [#"{"v":1,"error":{"code":"update-failed","message":"boom"}}"#], status: 1)]),
                               sink: FakeSink(), locate: { "/bin/tainer" })
    #expect(await failing.perform(.update, pod: "a") == false)
    // A reserved name without a folder can't be updated at all.
    #expect(await ok.perform(.update, pod: "core") == false)
}
