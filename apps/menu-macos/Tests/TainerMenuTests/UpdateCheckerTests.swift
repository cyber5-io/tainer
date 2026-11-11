import AppKit
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
@Test func pollerChecksAtLaunchDelayThenDaily() async {
    let clock = FakeClock()
    let fake = FakeRunner([.stream(lines: [newCheckDoc], status: 0), .stream(lines: [newCheckDoc], status: 0)])
    let checker = UpdateChecker(runner: fake, clock: clock, locate: { "/bin/tainer" })
    checker.start()
    await settle { clock.requested == [10] }
    #expect(fake.spawnCount == 0)
    clock.advance(by: 10)
    await settle { clock.requested == [10, 24 * 3600] }
    #expect(fake.spawnCount == 1)
    #expect(checker.tainerRow == "Tainer 1.5.0 is available — Update…")
    clock.advance(by: 23 * 3600)
    for _ in 0..<1000 { await Task.yield() }
    #expect(fake.spawnCount == 1)
    clock.advance(by: 3600)
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

/// Opaque soft-amber pixels (R high, G mid-high, B low) in the top-right quarter (bitmap rows run top-down).
private func amberTopRight(_ px: [UInt8]) -> Int {
    let w = 90, h = 72
    var n = 0
    for y in 0..<(h / 2) { for x in (w / 2)..<w {
        let i = (y * w + x) * 4
        let r = Int(px[i]), g = Int(px[i + 1]), b = Int(px[i + 2])
        if px[i + 3] > 230, r > 220, g > 160, g < 215, b < 110 { n += 1 }
    } }
    return n
}

/// Opaque near-white pixels anywhere: the old neutral dot must be gone.
private func whitePixels(_ px: [UInt8]) -> Int {
    var n = 0
    for i in stride(from: 0, to: px.count, by: 4) where px[i + 3] > 230 && px[i] > 230 && px[i + 1] > 230 && px[i + 2] > 230 { n += 1 }
    return n
}

/// Opaque dark pixels in the top-right quarter: the ring around the amber dot.
private func darkRingTopRight(_ px: [UInt8]) -> Int {
    let w = 90, h = 72
    var n = 0
    for y in 0..<(h / 2) { for x in (w / 2)..<w {
        let i = (y * w + x) * 4
        if px[i + 3] > 200, px[i] > 90, px[i] < 200, px[i + 2] < 60, px[i + 1] < 140 { n += 1 }
    } }
    return n
}

/// Opaque pixels in the top-right corner block the dot occupies.
private func opaqueCorner(_ px: [UInt8], size: Int) -> Int {
    let w = 90
    var n = 0
    for y in 0..<size { for x in (w - size)..<w where px[(y * w + x) * 4 + 3] > 230 { n += 1 } }
    return n
}

@Test @MainActor func updateDotIsSoftAmberWithRingOnHealthyIcon() {
    let plain = pixels(BarIconRenderer.markImage(style: .full, pulseHigh: true))
    let badged = pixels(BarIconRenderer.markImage(style: .full, pulseHigh: true, updateBadge: true))
    #expect(amberTopRight(plain) == 0)
    #expect(amberTopRight(badged) > 40)
    #expect(darkRingTopRight(badged) > darkRingTopRight(plain))
    #expect(whitePixels(badged) == 0)
}

@Test @MainActor func updateDotOnIdleIconStaysMonochromeAndReadsLarger() {
    let idle = BarIconRenderer.markImage(style: .dimmed, pulseHigh: true, updateBadge: true)
    #expect(idle.isTemplate)
    let px = pixels(idle)
    #expect(px != pixels(BarIconRenderer.markImage(style: .dimmed, pulseHigh: true)))
    #expect(amberTopRight(px) == 0)
    var tinted = 0
    for i in stride(from: 0, to: px.count, by: 4) where px[i + 3] > 0 {
        if px[i] != px[i + 1] || px[i + 1] != px[i + 2] { tinted += 1 }
    }
    #expect(tinted == 0)
    // The template mark is drawn translucent, so the opaque corner pixels are the dot alone. A 7pt dot
    // (6pt body, 24px at 4x) covers ~450px; the 8pt template dot (28px body) covers ~615px.
    #expect(opaqueCorner(px, size: 32) > 540)
}

@Test @MainActor func updateDotNeverDrawsOnRecoveringOrUnreachableIcons() {
    for style in [BarIconStyle.pulsing, .error] {
        #expect(pixels(BarIconRenderer.markImage(style: style, pulseHigh: true, updateBadge: true))
                == pixels(BarIconRenderer.markImage(style: style, pulseHigh: true)))
    }
}

// MARK: - Fix round 1

/// The first spawn blocks until `release()`; every spawn then streams `line`.
final class GateRunner: ProcessRunner, @unchecked Sendable {
    private let lock = NSLock()
    private var spawns = 0
    private var open = false
    let line: String
    init(line: String) { self.line = line }
    var spawnCount: Int { lock.withLock { spawns } }
    func release() { lock.withLock { open = true } }

    func run(executable: String, arguments: [String], currentDirectory: URL?, onLine: @escaping @Sendable (String) async -> Void) async throws -> ProcessResult {
        let first = lock.withLock { () -> Bool in spawns += 1; return spawns == 1 }
        if first { while !lock.withLock({ open }) { await Task.yield() } }
        await onLine(line)
        return ProcessResult(status: 0, stderr: "")
    }
}

@MainActor
@Test func recheckDuringACheckRunsOnceMoreAfterIt() async {
    let fake = GateRunner(line: newCheckDoc.replacingOccurrences(of: "\n", with: ""))
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    let first = Task { await checker.check() }
    await settle { fake.spawnCount == 1 }
    await checker.check()
    await checker.check()
    #expect(fake.spawnCount == 1)
    fake.release()
    await first.value
    #expect(fake.spawnCount == 2)
    #expect(checker.tainerRow == "Tainer 1.5.0 is available — Update…")
}

@MainActor
@Test func podUpdatesWaitForASelfUpdate() async {
    let checker = UpdateChecker(runner: FakeRunner([]), clock: FakeClock(), locate: { "/bin/tainer" })
    #expect(checker.podUpdatesAllowed)
    checker.activity = .updatingTainer
    #expect(!checker.podUpdatesAllowed)

    let running = Pod(name: "blog", domain: "blog.tainer.me", state: .running, running: 2, total: 2)
    let locked = PodRowModel(pod: running, busy: false, update: PodUpdate(name: "blog", parts: "x"), updatesAllowed: false)
    #expect(locked.actions == [.open, .restart, .stop, .manage])

    let fake = PerArgRunner(blocking: "tainer")
    let runner = ActionRunner(state: StackState(), runner: fake, sink: FakeSink(), locate: { "/bin/tainer" })
    let selfUpdate = Task { await runner.runSelfUpdate(.tainer) }
    await settle { fake.arguments.count == 1 }
    await runner.run(.update, pod: "blog")
    #expect(fake.arguments.count == 1)
    selfUpdate.cancel()
    await selfUpdate.value
    await runner.run(.update, pod: "blog")
    #expect(fake.arguments.last == ["update", "blog", "--json"])
}

@Test func nullPodsStillParses() throws {
    let doc = #"{"v":1,"checkedAt":"2026-11-02T12:00:00.000Z","pods":null,"tainer":{"installed":"1.4.0","available":"1.5.0","updateAvailable":true,"notes":""}}"#
    let parsed = try #require(UpdateCheckDocument.parse([doc]))
    #expect(parsed.pods.isEmpty)
    #expect(parsed.tainer?.available == "1.5.0")
    #expect(UpdateCheckDocument.parse([#"{"v":1}"#]) == nil)
}

// MARK: - tain-123: check now, update core

@MainActor
@Test func checkNowRunsTheFreshCheckAndShowsItIsChecking() async {
    let fake = GateRunner(line: newCheckDoc.replacingOccurrences(of: "\n", with: ""))
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    #expect(!checker.isChecking)
    #expect(checker.tainerRow == nil)
    let run = Task { await checker.check() }
    await settle { fake.spawnCount == 1 }
    #expect(checker.isChecking)
    fake.release()
    await run.value
    #expect(!checker.isChecking)
    #expect(checker.tainerRow == "Tainer 1.5.0 is available — Update…")
    #expect(checker.hasUpdates)
}

@MainActor
@Test func checkNowUsesTheCheckThatAlwaysFetchesTheRelease() async {
    let fake = FakeRunner([.stream(lines: [newCheckDoc.replacingOccurrences(of: "\n", with: "")], status: 0)])
    let checker = UpdateChecker(runner: fake, clock: FakeClock(), locate: { "/bin/tainer" })
    await checker.check()
    // `update --check` skips the 24h release cache (cmd/tainer/updatecheck.go buildUpdateCheck).
    #expect(fake.arguments == [["update", "--check", "--json"]])
}

@Test func checkNowRowCopy() {
    #expect(UpdateCopy.checkNow == "Check for updates")
    #expect(UpdateCopy.checking == "Checking for updates…")
}

@MainActor
@Test func podNamedCoreUpdatesByFolderPath() async {
    let state = StackState()
    state.applyList([ListedProject(name: "core", domain: "core.tainer.me", path: "/p/core")])
    let fake = FakeRunner([.stream(lines: [podDoc], status: 0)])
    let runner = ActionRunner(state: state, runner: fake, sink: FakeSink(), locate: { "/bin/tainer" })
    await runner.run(.update, pod: "core")
    #expect(fake.arguments == [["update", "/p/core", "--json"]])
}

// MARK: - Check now closes the loop

private let upToDateDoc = #"{"v":1,"checkedAt":"2026-11-02T12:00:00.000Z","pods":[{"name":"blog","updateAvailable":false,"parts":"WordPress","roles":[]}],"tainer":{"installed":"1.5.6","available":"1.5.6","updateAvailable":false},"menu":{"installed":"1.5.6","available":"1.5.6","updateAvailable":false},"releaseCheck":"ok","podsCheck":"ok"}"#
private let offlineDoc = #"{"v":1,"checkedAt":"2026-11-02T12:00:00.000Z","pods":[],"tainer":{"installed":"1.5.6","available":"","updateAvailable":false},"menu":{"installed":"1.5.6","available":"","updateAvailable":false},"releaseCheck":"unavailable","podsCheck":"ok"}"#

@MainActor
private func checker(_ steps: [FakeRunner.Step], cli: String? = "/bin/tainer") -> UpdateChecker {
    UpdateChecker(runner: FakeRunner(steps), clock: FakeClock(), locate: { cli }, outcomeHold: .milliseconds(30))
}

@MainActor
@Test func checkNowSaysUpToDateBrieflyThenGoesIdle() async {
    let c = checker([.stream(lines: [upToDateDoc], status: 0)])
    await c.checkNow()
    #expect(c.checkOutcome == .upToDate)
    #expect(CheckRowModel(isChecking: c.isChecking, outcome: c.checkOutcome).title == "Up to date")
    await settle { c.checkOutcome == nil }
    #expect(CheckRowModel(isChecking: false, outcome: nil).title == "Check for updates")
}

@MainActor
@Test func checkNowWithUpdatesLeavesItToTheRows() async {
    let c = checker([.stream(lines: [newCheckDoc.replacingOccurrences(of: "\n", with: "")], status: 0)])
    await c.checkNow()
    #expect(c.checkOutcome == nil)
    #expect(c.hasUpdates)
}

@MainActor
@Test func checkNowSaysCouldntCheckWhenItCouldnt() async {
    for c in [
        checker([.stream(lines: [errDoc], status: 1)]),
        checker([.spawnFailure]),
        checker([], cli: nil),
        checker([.stream(lines: [offlineDoc], status: 0)]),
    ] {
        await c.checkNow()
        #expect(c.checkOutcome == .couldNotCheck)
        await settle { c.checkOutcome == nil }
    }
    #expect(CheckRowModel(isChecking: false, outcome: .couldNotCheck).title == "Couldn't check for updates")
}

@MainActor
@Test func backgroundChecksNeverShowAnOutcome() async {
    let c = checker([.stream(lines: [upToDateDoc], status: 0)])
    await c.check()
    #expect(c.checkOutcome == nil)
}

@Test func checkRowModel() {
    #expect(CheckRowModel(isChecking: true, outcome: .upToDate).title == "Checking for updates…")
    #expect(CheckRowModel(isChecking: false, outcome: .upToDate).symbol == "checkmark.circle")
    #expect(CheckRowModel(isChecking: false, outcome: .couldNotCheck).symbol == "exclamationmark.triangle")
    #expect(CheckRowModel(isChecking: false, outcome: nil).symbol == "arrow.triangle.2.circlepath")
}

@Test func offlineDocumentIsMarkedUnavailable() throws {
    #expect(try #require(UpdateCheckDocument.parse([offlineDoc])).releaseUnavailable)
    #expect(try #require(UpdateCheckDocument.parse([upToDateDoc])).releaseUnavailable == false)
}
