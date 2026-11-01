import Foundation
import Testing
@testable import TainerMenu

private let t0 = Date(timeIntervalSince1970: 1_793_448_000)
private let healthy = EngineState(state: .healthy, since: t0, detail: "")
private func pod(_ name: String, _ state: PodState) -> Pod {
    Pod(name: name, domain: "\(name).tainer.me", state: state, running: state == .running ? 1 : 0, total: 1)
}
private func listed(_ names: [String]) -> [ListedProject] {
    names.map { ListedProject(name: $0, domain: "\($0).tainer.me") }
}
private func names(_ n: Int, prefix: String = "p") -> [String] {
    (0..<n).map { prefix + String(format: "%02d", $0) }
}

@Test func listDocumentParses() {
    let doc = #"{"v":1,"projects":[{"name":"blog","type":"wordpress","domain":"blog.tainer.me","path":"/p/blog","status":"running"},{"name":"shop"}]}"#
    #expect(ListDocument.parse([doc]) == [ListedProject(name: "blog", domain: "blog.tainer.me"), ListedProject(name: "shop", domain: "")])
    #expect(ListDocument.parse(["{\"v\":1,", "\"projects\":[]}"]) == [])
    #expect(ListDocument.parse([#"{"v":1,"error":{"code":"x","message":"m"}}"#]) == nil)
    #expect(ListDocument.parse(["garbage"]) == nil)
    #expect(ListDocument.parse([]) == nil)
}

@MainActor @Test func listedPodsAbsentFromStreamRenderStopped() {
    let s = StackState()
    s.apply(.event(.hello(engine: healthy, pods: [pod("live", .running)])))
    s.applyList(listed(["live", "cold"]))
    #expect(s.allPods.map(\.name) == ["live", "cold"])
    #expect(s.allPods.first { $0.name == "cold" }?.state == .stopped)
    #expect(s.allPods.first { $0.name == "cold" }?.domain == "cold.tainer.me")
    #expect(s.statusLine.counts == "1 of 2 pods running")
}

@MainActor @Test func streamOwnsLiveStateForKnownPods() {
    let s = StackState()
    s.applyList(listed(["a"]))
    #expect(s.allPods.map(\.state) == [.stopped])
    s.apply(.event(.hello(engine: healthy, pods: [pod("a", .running)])))
    #expect(s.allPods.count == 1)
    #expect(s.allPods[0].state == .running)
    s.applyList(listed(["a"]))
    #expect(s.allPods[0].state == .running)
}

@MainActor @Test func listRendersBeforeStreamArrives() {
    let s = StackState()
    s.applyList(listed(["a", "b"]))
    #expect(s.allPods.map(\.name) == ["a", "b"])
}

@MainActor @Test func refreshedListDropsDeletedProjects() {
    let s = StackState()
    s.applyList(listed(["a", "b"]))
    s.applyList(listed(["a"]))
    #expect(s.allPods.map(\.name) == ["a"])
}

@MainActor @Test func runningPodsAreFirstThenStoppedAlphabetical() {
    let s = StackState()
    s.applyList(listed(["m", "a", "z"]))
    s.apply(.event(.hello(engine: healthy, pods: [pod("z", .running), pod("q", .partial)])))
    #expect(s.allPods.map(\.name) == ["q", "z", "a", "m"])
}

@MainActor @Test func pagesOfTenWithBoundaries() {
    let s = StackState()
    s.applyList(listed(names(30)))
    #expect(s.pageCount == 3)
    #expect(s.visiblePods.count == 10)
    #expect(s.pageLabel == "1-10 of 30")
    #expect(s.canGoPrevious == false && s.canGoNext == true)
    s.nextPage(); s.nextPage()
    #expect(s.pageLabel == "21-30 of 30")
    #expect(s.canGoNext == false)
    s.nextPage()
    #expect(s.pageLabel == "21-30 of 30")
    s.previousPage()
    #expect(s.pageLabel == "11-20 of 30")
}

@MainActor @Test func lastPageMayBeShort() {
    let s = StackState()
    s.applyList(listed(names(23)))
    s.nextPage(); s.nextPage()
    #expect(s.visiblePods.count == 3)
    #expect(s.pageLabel == "21-23 of 23")
}

@MainActor @Test func runningPodsPinnedToPageOne() {
    let s = StackState()
    s.applyList(listed(names(30)))
    s.apply(.event(.hello(engine: healthy, pods: [pod("p29", .running), pod("p15", .partial)])))
    #expect(s.visiblePods.prefix(2).map(\.name) == ["p15", "p29"])
    #expect(s.visiblePods.dropFirst(2).first?.name == "p00")
    s.nextPage()
    #expect(s.visiblePods.allSatisfy { !$0.isActive })
}

@MainActor @Test func pageResetsAndClampsWhenListShrinks() {
    let s = StackState()
    s.applyList(listed(names(30)))
    s.nextPage(); s.nextPage()
    s.applyList(listed(names(12)))
    #expect(s.pageLabel == "11-12 of 12")
    s.resetPage()
    #expect(s.pageLabel == "1-10 of 12")
}

@MainActor @Test func pagerHiddenForOnePage() {
    let s = StackState()
    #expect(!s.showsPager)
    s.applyList(listed(names(10)))
    #expect(!s.showsPager)
    s.applyList(listed(names(11)))
    #expect(s.showsPager)
}

@MainActor @Test func refresherFetchesListViaCLIAndMerges() async {
    let doc = #"{"v":1,"projects":[{"name":"blog","domain":"blog.tainer.me","status":"stopped"}]}"#
    let runner = FakeRunner([.stream(lines: [doc], status: 0)])
    let s = StackState()
    let r = ListRefresher(state: s, runner: runner, locate: { "/bin/tainer" })
    await r.refresh()
    #expect(runner.arguments == [["list", "--json"]])
    #expect(s.allPods.map(\.name) == ["blog"])
}

@MainActor @Test func refresherKeepsPreviousListOnFailure() async {
    let good = #"{"v":1,"projects":[{"name":"blog","domain":"blog.tainer.me"}]}"#
    let runner = FakeRunner([.stream(lines: [good], status: 0), .stream(lines: [#"{"v":1,"error":{"code":"x","message":"m"}}"#], status: 1), .spawnFailure])
    let s = StackState()
    let r = ListRefresher(state: s, runner: runner, locate: { "/bin/tainer" })
    await r.refresh(); await r.refresh(); await r.refresh()
    #expect(s.allPods.map(\.name) == ["blog"])
}

@MainActor @Test func refresherWithoutCLIDoesNothing() async {
    let s = StackState()
    let r = ListRefresher(state: s, runner: FakeRunner([]), locate: { nil })
    await r.refresh()
    #expect(s.allPods.isEmpty)
}
