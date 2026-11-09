import Foundation
import Testing
@testable import TainerMenu

private let newCheckDoc = #"""
{"v":1,"checkedAt":"2026-11-02T12:00:00.000Z","pods":[
 {"name":"blog","type":"wordpress","domain":"blog.tainer.me","state":"running","updateAvailable":true,"parts":"WordPress · PHP 8.3","roles":[]},
 {"name":"shop","type":"react","domain":"shop.tainer.me","state":"stopped","updateAvailable":false,"parts":"React · Node 22","roles":[],"error":"registry unreachable"}],
 "upToDate":0,"available":1,"unknown":1,
 "tainer":{"installed":"1.4.0","available":"1.5.0","updateAvailable":true,"notes":"Faster starts"},
 "menu":{"installed":"1.4.0","available":"1.5.0","updateAvailable":true,"notes":"New badge","minTainer":"1.5.0","needsTainerFirst":true},
 "releaseCheck":"ok","podsCheck":"ok","control":{"future":"key"}}
"""#

private let olderCheckDoc = #"{"v":1,"checkedAt":"2026-11-02T12:00:00.000Z","pods":[{"name":"blog","type":"wordpress","domain":"blog.tainer.me","state":"running","updateAvailable":true,"parts":"WordPress","roles":[]}],"upToDate":0,"available":1,"unknown":0}"#

private let errDoc = #"{"v":1,"error":{"code":"internal","message":"boom"}}"#

@Test func testDecodesNewUpdateKeys() throws {
    let doc = try #require(UpdateCheckDocument.parse(newCheckDoc.components(separatedBy: "\n")))
    #expect(doc.tainer == ReleaseInfo(installed: "1.4.0", available: "1.5.0", updateAvailable: true, notes: "Faster starts"))
    #expect(doc.menu == ReleaseInfo(installed: "1.4.0", available: "1.5.0", updateAvailable: true, notes: "New badge",
                                    minTainer: "1.5.0", needsTainerFirst: true))
    #expect(doc.pods == ["blog": PodUpdate(name: "blog", parts: "WordPress · PHP 8.3")])
}

@Test func testOlderCLIHasNoUpdateRows() throws {
    let doc = try #require(UpdateCheckDocument.parse([olderCheckDoc]))
    #expect(doc.tainer == nil)
    #expect(doc.menu == nil)
    #expect(doc.pods.keys.sorted() == ["blog"])
    #expect(UpdateCheckDocument.parse([errDoc]) == nil)
    #expect(UpdateCheckDocument.parse([]) == nil)
    #expect(UpdateCheckDocument.parse(["not json"]) == nil)
}

@MainActor
@Test func testOlderCLIShowsNoReleaseRowsInChecker() async {
    let fake = FakeRunner([.stream(lines: [olderCheckDoc], status: 0)])
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    await checker.check()
    #expect(checker.tainerRow == nil)
    #expect(checker.menuRow == nil)
    #expect(checker.pods["blog"]?.parts == "WordPress")
    #expect(checker.hasUpdates)
}

@Test func testBadgeOnlyWhenHealthyOrIdle() {
    #expect(BarIconStyle.full.showsUpdateBadge(hasUpdates: true))
    #expect(BarIconStyle.dimmed.showsUpdateBadge(hasUpdates: true))
    #expect(!BarIconStyle.pulsing.showsUpdateBadge(hasUpdates: true))
    #expect(!BarIconStyle.error.showsUpdateBadge(hasUpdates: true))
    for style in [BarIconStyle.full, .dimmed, .pulsing, .error] {
        #expect(!style.showsUpdateBadge(hasUpdates: false))
    }
}

@MainActor
@Test func testPollerNeverOverlaps() async {
    let fake = FakeRunner([.blockUntilCancelled])
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    let first = Task { await checker.check() }
    await settle { fake.spawnCount == 1 }
    await checker.check()
    #expect(fake.spawnCount == 1)
    first.cancel()
    await first.value
    #expect(fake.arguments == [["update", "--check", "--json"]])
}

@MainActor
@Test func pollerChecksAtLaunchDelayThenEveryFourHours() async {
    let clock = FakeClock()
    let fake = FakeRunner([.stream(lines: [newCheckDoc], status: 0), .stream(lines: [newCheckDoc], status: 0)])
    let checker = UpdateChecker(runner: fake, clock: clock, locate: { "/bin/tainer" })
    checker.start()
    await settle { clock.requested == [10] }
    #expect(fake.spawnCount == 0)
    clock.advance(by: 10)
    await settle { clock.requested == [10, 4 * 3600] }
    #expect(fake.spawnCount == 1)
    #expect(checker.tainerRow == "Tainer 1.5.0 is available — Update…")
    clock.advance(by: 4 * 3600)
    await settle { fake.spawnCount == 2 }
    checker.stop()
}

@MainActor
@Test func failedCheckKeepsPreviousResult() async {
    let fake = FakeRunner([.stream(lines: [newCheckDoc], status: 0), .stream(lines: [errDoc], status: 1), .spawnFailure])
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    await checker.check()
    await checker.check()
    await checker.check()
    #expect(checker.tainerRow == "Tainer 1.5.0 is available — Update…")
    #expect(checker.menuRow == "Menu 1.5.0 is available — Update…")
    #expect(checker.pods.count == 1)
}

@Test func updateCopyIsExact() {
    #expect(UpdateCopy.restartWarning(["shop", "blog", "c5test"]) == "Updating tainer restarts your running pods (shop, blog, c5test).")
    #expect(UpdateCopy.podWarning("blog") == "blog restarts while it updates.")
    #expect(UpdateCopy.podHelp(parts: "WordPress · PHP 8.3") == "Update available: WordPress · PHP 8.3")
    #expect(UpdateActivity.updatingTainer.text == "Updating tainer…")
    #expect(UpdateActivity.restarting(3).text == "Restarting 3 pods…")
    #expect(UpdateActivity.restarting(1).text == "Restarting 1 pod…")
}

@Test func podRowGainsUpdateAction() {
    let running = Pod(name: "blog", domain: "blog.tainer.me", state: .running, running: 2, total: 2)
    let m = PodRowModel(pod: running, busy: false, update: PodUpdate(name: "blog", parts: "WordPress"))
    #expect(m.actions == [.update, .open, .restart, .stop, .manage])
    #expect(m.updateHelp == "Update available: WordPress")
    #expect(m.showsTooltip)
    let stopped = Pod(name: "blog", domain: "blog.tainer.me", state: .stopped, running: 0, total: 2)
    #expect(PodRowModel(pod: stopped, busy: false, update: PodUpdate(name: "blog", parts: "")).actions == [.update, .start, .manage])
    #expect(PodRowModel(pod: running, busy: false).updateHelp == nil)
}

@Test func resumePendingCountReadsTheResumeFile() throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: dir.appendingPathComponent("tainer"), withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    let env = ["XDG_CONFIG_HOME": dir.path]
    #expect(ResumeFile.pendingCount(env: env) == 0)
    let body = #"{"forVersion":"1.5.0","fromVersion":"1.4.0","writtenAt":"2026-11-08T12:00:00Z","pods":[{"name":"a","path":"/p/a"},{"name":"b","path":"/p/b"}]}"#
    try body.write(to: dir.appendingPathComponent("tainer/update-resume.json"), atomically: true, encoding: .utf8)
    #expect(ResumeFile.pendingCount(env: env) == 2)
}

// MARK: - Commands

private let podDoc = #"{"v":1,"mode":"project","pod":"blog","updated":true,"started":true,"elapsedMs":1}"#
private let selfDoc = #"{"v":1,"mode":"tainer","from":"1.4.0","to":"1.5.0","updated":true,"resumed":["blog"],"failed":[]}"#

@MainActor
@Test func testUpdateActionRunsTheRightCommand() async {
    let fake = FakeRunner([.stream(lines: [selfDoc], status: 0), .stream(lines: [selfDoc], status: 0), .stream(lines: [podDoc], status: 0)])
    let sink = FakeSink()
    let runner = ActionRunner(state: StackState(), runner: fake, sink: sink, locate: { "/bin/tainer" })
    await runner.runSelfUpdate(.tainer)
    await runner.runSelfUpdate(.menu)
    await runner.run(.update, pod: "blog")
    #expect(fake.arguments == [
        ["update", "tainer", "--yes", "--json"],
        ["update", "menu", "--yes", "--json"],
        ["update", "blog", "--json"],
    ])
    #expect(sink.posted.isEmpty)
}

@MainActor
@Test func reservedPodNameUpdatesByFolderPath() async {
    let state = StackState()
    state.applyList([ListedProject(name: "menu", domain: "menu.tainer.me", path: "/p/menu")])
    let fake = FakeRunner([.stream(lines: [podDoc], status: 0)])
    let runner = ActionRunner(state: state, runner: fake, sink: FakeSink(), locate: { "/bin/tainer" })
    await runner.run(.update, pod: "menu")
    #expect(fake.arguments == [["update", "/p/menu", "--json"]])
}

@MainActor
@Test func reservedPodNameWithoutPathIsRefused() async {
    let fake = FakeRunner([])
    let sink = FakeSink()
    let runner = ActionRunner(state: StackState(), runner: fake, sink: sink, locate: { "/bin/tainer" })
    await runner.run(.update, pod: "tainer")
    #expect(fake.spawnCount == 0)
    #expect(sink.posted.count == 1)
}

@MainActor
@Test func selfUpdateFailureListsPodsThatDidNotComeBack() async {
    let doc = #"{"v":1,"mode":"tainer","from":"1.4.0","to":"1.5.0","updated":true,"resumed":[],"failed":[{"name":"shop","reason":"port in use"}]}"#
    let sink = FakeSink()
    let runner = ActionRunner(state: StackState(), runner: FakeRunner([.stream(lines: [doc], status: 0)]), sink: sink, locate: { "/bin/tainer" })
    await runner.runSelfUpdate(.tainer)
    #expect(sink.posted == ["shop didn't come back: port in use"])
}

@MainActor
@Test func resumeRunsQuietly() async {
    let fake = FakeRunner([.stream(lines: ["blog is back"], stderr: "x", status: 1)])
    let sink = FakeSink()
    let runner = ActionRunner(state: StackState(), runner: fake, sink: sink, locate: { "/bin/tainer" })
    await runner.runResume()
    #expect(fake.arguments == [["update", "resume"]])
    #expect(sink.posted.isEmpty)
}

@Test func listDocumentDecodesPath() {
    let doc = #"{"v":1,"projects":[{"name":"blog","domain":"blog.tainer.me","path":"/p/blog"},{"name":"x"}]}"#
    #expect(ListDocument.parse([doc]) == [ListedProject(name: "blog", domain: "blog.tainer.me", path: "/p/blog"), ListedProject(name: "x", domain: "")])
}

// MARK: - Bar icon dot

import AppKit

@MainActor
private func pixels(_ image: NSImage) -> [UInt8] {
    let w = 90, h = 72
    guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: w, pixelsHigh: h, bitsPerSample: 8, samplesPerPixel: 4,
                                     hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: w * 4, bitsPerPixel: 32),
          let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return [] }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx
    image.draw(in: NSRect(x: 0, y: 0, width: w, height: h))
    NSGraphicsContext.restoreGraphicsState()
    return Array(UnsafeBufferPointer(start: rep.bitmapData, count: w * h * 4))
}

/// Opaque near-white pixels in the top-right quarter (bitmap rows run top-down).
private func whiteTopRight(_ px: [UInt8]) -> Int {
    let w = 90, h = 72
    var n = 0
    for y in 0..<(h / 2) { for x in (w / 2)..<w {
        let i = (y * w + x) * 4
        if px[i + 3] > 230, px[i] > 230, px[i + 1] > 230, px[i + 2] > 230 { n += 1 }
    } }
    return n
}

@Test @MainActor func updateDotDrawsOnlyOnHealthyAndIdleIcons() {
    #expect(whiteTopRight(pixels(BarIconRenderer.markImage(style: .full, pulseHigh: true))) == 0)
    #expect(whiteTopRight(pixels(BarIconRenderer.markImage(style: .full, pulseHigh: true, updateBadge: true))) > 0)
    let idle = BarIconRenderer.markImage(style: .dimmed, pulseHigh: true, updateBadge: true)
    #expect(idle.isTemplate)
    #expect(pixels(idle) != pixels(BarIconRenderer.markImage(style: .dimmed, pulseHigh: true)))
    for style in [BarIconStyle.pulsing, .error] {
        #expect(pixels(BarIconRenderer.markImage(style: style, pulseHigh: true, updateBadge: true))
                == pixels(BarIconRenderer.markImage(style: style, pulseHigh: true)))
    }
}
