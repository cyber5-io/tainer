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
    state.apply(.event(.pod(Pod(name: "ops", domain: "ops.tainer.me", state: .running, running: 2, total: 2))))
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: [podDoc], status: 0)])
    let runner = makeRunner(fake, state: state, sink: sink)
    let before = state.pods
    #expect(before.count == 1)
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
    let report = #"{"v":1,"checks":[{"name":"daemon","status":"ok"},{"name":"dns","status":"fail","detail":"resolver missing"}],"fixed":[],"healthy":false}"#
    let state = StackState()
    let fake = FakeRunner([.stream(lines: [report], status: 1)])
    await makeRunner(fake, state: state, sink: sink).run(.doctor, pod: nil)
    #expect(sink.posted == ["doctor: the dns check failed — resolver missing"])
    #expect(state.doctor == .finished(.failing(check: "dns", detail: "resolver missing")))
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

/// Blocks any spawn whose argv mentions `blockedArg` until cancelled; everything else succeeds at once.
final class PerArgRunner: ProcessRunner, @unchecked Sendable {
    private let lock = NSLock()
    private var spawned: [[String]] = []
    let blockedArg: String
    init(blocking blockedArg: String) { self.blockedArg = blockedArg }
    var arguments: [[String]] { lock.withLock { spawned } }

    func run(executable: String, arguments: [String], currentDirectory: URL?, onLine: @escaping @Sendable (String) async -> Void) async throws -> ProcessResult {
        lock.withLock { spawned.append(arguments) }
        if arguments.contains(blockedArg) {
            while !Task.isCancelled { await Task.yield() }
            return ProcessResult(status: 15, stderr: "")
        }
        await onLine(podDoc)
        return ProcessResult(status: 0, stderr: "")
    }
}

@MainActor
@Test func concurrentActionOnSamePodRefused() async {
    let state = StackState()
    let fake = PerArgRunner(blocking: "ops")
    let runner = ActionRunner(state: state, runner: fake, sink: FakeSink(), locate: { "/bin/tainer" })
    let first = Task { await runner.run(.restart, pod: "ops") }
    await settle { fake.arguments.count == 1 }
    await runner.run(.stop, pod: "ops")
    #expect(fake.arguments.count == 1)
    await runner.run(.stop, pod: "other")
    #expect(fake.arguments.count == 2)
    #expect(state.busy == ["ops"])
    first.cancel()
    await first.value
    #expect(state.busy.isEmpty)
}

@MainActor
@Test func doctorResetsAfterRunAndConcurrentDoctorRefused() async {
    let fake = PerArgRunner(blocking: "doctor")
    let runner = ActionRunner(state: StackState(), runner: fake, sink: FakeSink(), locate: { "/bin/tainer" })
    let first = Task { await runner.run(.doctor, pod: nil) }
    await settle { fake.arguments.count == 1 }
    await runner.run(.doctor, pod: nil)
    #expect(fake.arguments.count == 1)
    first.cancel()
    await first.value

    let seq = FakeRunner([.stream(lines: [podDoc], status: 0), .stream(lines: [podDoc], status: 0)])
    let again = makeRunner(seq)
    await again.run(.doctor, pod: nil)
    await again.run(.doctor, pod: nil)
    #expect(seq.spawnCount == 2)
}

@MainActor
@Test func nonZeroExitWithPlainTextStdoutIsAFailure() async {
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: ["Usage: tainer stop [pod]"], stderr: "unknown flag: --json", status: 1)])
    await makeRunner(fake, sink: sink).run(.stop, pod: "ops")
    #expect(sink.posted == ["ops: unknown flag: --json"])
}

@MainActor
@Test func nonZeroExitWithPlainTextStdoutFailsDoctorToo() async {
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: ["Usage: tainer doctor"], status: 1)])
    await makeRunner(fake, sink: sink).run(.doctor, pod: nil)
    #expect(sink.posted == ["doctor: tainer exited with status 1"])
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

// MARK: - Doctor feedback (tain-101)

@MainActor
@Test func doctorDocumentOutcomes() {
    func outcome(_ json: String) -> DoctorOutcome? { DoctorDocument.parse(["noise", json])?.outcome }
    #expect(outcome(#"{"v":1,"checks":[{"name":"daemon","status":"ok"}],"fixed":[],"healthy":true}"#) == .healthy)
    #expect(outcome(#"{"v":1,"checks":[{"name":"pods","status":"ok","fixed":true}],"fixed":["pods","dns"],"healthy":true}"#) == .fixed(2))
    #expect(outcome(#"{"v":1,"checks":[{"name":"daemon","status":"ok"},{"name":"router","status":"fail","detail":"not answering"},{"name":"tls","status":"fail"}],"healthy":false}"#)
        == .failing(check: "router", detail: "not answering"))
    #expect(outcome(#"{"v":1,"checks":[{"name":"edge","status":"warn"}],"healthy":true}"#) == .healthy)
    // Without a "healthy" field, a failing check still decides.
    #expect(outcome(#"{"v":1,"checks":[{"name":"agent","status":"fail"}]}"#) == .failing(check: "agent", detail: ""))
    #expect(DoctorDocument.parse(["not json", #"{"v":1,"name":"ops"}"#]) == nil)
}

@MainActor
@Test func doctorShowsRunningThenOutcomeThenReverts() async {
    let state = StackState()
    let sink = FakeSink()
    let fixedDoc = #"{"v":1,"checks":[{"name":"pods","status":"ok","fixed":true}],"fixed":["pods"],"healthy":true}"#
    let fake = FakeRunner([.streamThenBlock(lines: [fixedDoc])])
    let runner = ActionRunner(state: state, runner: fake, sink: sink, locate: { "/bin/tainer" }, doctorResultHold: .milliseconds(1))
    let task = Task { await runner.run(.doctor, pod: nil) }
    await settle { state.doctor == .running }
    #expect(DoctorRowModel(run: state.doctor).enabled == false)
    #expect(DoctorRowModel(run: state.doctor).title == "Running doctor…")
    task.cancel()
    await task.value
    // A cancelled child exits non-zero, but its report document was read: still a report.
    #expect(state.doctor == .finished(.fixed(1)))
    #expect(sink.posted.isEmpty)
    for _ in 0..<200 where state.doctor != .idle { try? await Task.sleep(for: .milliseconds(5)) }
    #expect(state.doctor == .idle)
}

@MainActor
@Test func doctorHealthyRunPostsNothing() async {
    let state = StackState()
    let sink = FakeSink()
    let doc = #"{"v":1,"checks":[{"name":"daemon","status":"ok"}],"fixed":[],"healthy":true}"#
    await makeRunner(FakeRunner([.stream(lines: [doc], status: 0)]), state: state, sink: sink).run(.doctor, pod: nil)
    #expect(state.doctor == .finished(.healthy))
    #expect(sink.posted.isEmpty)
}

@MainActor
@Test func doctorThatCannotRunSaysSo() async {
    let state = StackState()
    let sink = FakeSink()
    await makeRunner(FakeRunner([]), state: state, sink: sink, cli: nil).run(.doctor, pod: nil)
    #expect(state.doctor == .finished(.couldNotRun))
    #expect(sink.posted == ["doctor: tainer was not found"])
}

@Test func doctorRowCopy() {
    #expect(DoctorRowModel(run: .idle).title == "Run Doctor — check & heal")
    #expect(DoctorRowModel(run: .idle).enabled)
    #expect(DoctorRowModel(run: .finished(.healthy)).title == "Everything looks healthy")
    #expect(DoctorRowModel(run: .finished(.fixed(1))).title == "Fixed: 1 issue")
    #expect(DoctorRowModel(run: .finished(.fixed(3))).title == "Fixed: 3 issues")
    #expect(DoctorRowModel(run: .finished(.failing(check: "dns", detail: ""))).title == "Needs attention: dns")
    #expect(DoctorRowModel(run: .finished(.fixed(1))).enabled)
    #expect(DoctorCopy.failureBanner(check: "tls", detail: "") == "doctor: the tls check failed")
}

// MARK: - Open existing project (tain-102)

private func projectFolder(withManifest: Bool) throws -> URL {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("tainer-menu-\(UUID().uuidString)/shop")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    if withManifest { try Data("version: 2\n".utf8).write(to: dir.appendingPathComponent("tainer.yaml")) }
    return dir
}

@MainActor
@Test func openFolderWithoutManifestIsRefusedWithoutRunning() async throws {
    let folder = try projectFolder(withManifest: false)
    let sink = FakeSink()
    let fake = FakeRunner([])
    await makeRunner(fake, sink: sink).openProject(at: folder)
    #expect(fake.spawnCount == 0)
    #expect(sink.posted == ["That folder isn't a tainer project yet — create one from the dashboard"])
}

@MainActor
@Test func openProjectStartsPlainInsideTheFolder() async throws {
    let folder = try projectFolder(withManifest: true)
    let sink = FakeSink()
    let fake = FakeRunner([.stream(lines: ["starting shop...", "shop started   (pod 3, 4.1s)"], status: 0)])
    await makeRunner(fake, sink: sink).openProject(at: folder)
    #expect(fake.arguments == [["start", "--plain"]])
    #expect(fake.directories == [folder])
    #expect(sink.posted.isEmpty)
}

@MainActor
@Test func openProjectFailureSurfacesTheFirstErrorLine() async throws {
    let folder = try projectFolder(withManifest: true)
    let sink = FakeSink()
    let stderr = "tainer: tainer.yaml is version 1 — run tainer migrate first\nmore detail\n"
    let fake = FakeRunner([.stream(lines: ["starting shop..."], stderr: stderr, status: 1)])
    await makeRunner(fake, sink: sink).openProject(at: folder)
    #expect(sink.posted == ["shop: tainer.yaml is version 1 — run tainer migrate first"])
}

@MainActor
@Test func openProjectSpawnFailureAndMissingCLIReport() async throws {
    let folder = try projectFolder(withManifest: true)
    let sink = FakeSink()
    await makeRunner(FakeRunner([.spawnFailure]), sink: sink).openProject(at: folder)
    let missing = FakeRunner([])
    await makeRunner(missing, sink: sink, cli: nil).openProject(at: folder)
    #expect(missing.spawnCount == 0)
    #expect(sink.posted == ["shop: could not run tainer", "shop: tainer was not found"])
}

@MainActor
@Test func openingTheSameFolderTwiceRunsOnce() async throws {
    let folder = try projectFolder(withManifest: true)
    let fake = FakeRunner([.blockUntilCancelled])
    let runner = makeRunner(fake)
    let first = Task { await runner.openProject(at: folder) }
    await settle { fake.spawnCount == 1 }
    await runner.openProject(at: folder)
    #expect(fake.spawnCount == 1)
    first.cancel()
    await first.value
}

@Test func firstErrorLineFallbacks() {
    #expect(ActionRunner.firstErrorLine(stderr: "\n  tainer: port 80 is in use\n", stdout: [], status: 1) == "port 80 is in use")
    #expect(ActionRunner.firstErrorLine(stderr: "", stdout: ["starting shop...", "pod failed to start", ""], status: 1) == "pod failed to start")
    #expect(ActionRunner.firstErrorLine(stderr: "", stdout: [], status: 2) == "tainer exited with status 2")
}

// MARK: - Review round: pretty-printed doctor report, start notices

/// The CLI's own doctor golden (pretty-printed, as the real binary prints it) with the "v":1 the
/// golden omits, split into output lines the way the runner delivers them.
private func goldenDoctorLines(healthy: Bool) throws -> [String] {
    let golden = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("../../cmd/tainer/testdata/json/doctor.golden").standardized
    var text = try String(contentsOf: golden, encoding: .utf8)
    #expect(text.hasPrefix("{\n"))
    text = "{\n  \"v\": 1,\n" + text.dropFirst(2)
    if healthy {
        text = text.replacingOccurrences(of: "\"status\": \"fail\"", with: "\"status\": \"ok\"")
            .replacingOccurrences(of: "\"healthy\": false", with: "\"fixed\": [\"pods\"],\n  \"healthy\": true")
    }
    let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
    #expect(lines.count > 5)
    return lines
}

@MainActor
@Test func prettyPrintedFailingDoctorReportIsRead() async throws {
    let lines = try goldenDoctorLines(healthy: false)
    #expect(DoctorDocument.parse(lines)?.outcome == .failing(check: "pods", detail: "blog down"))
    let state = StackState()
    let sink = FakeSink()
    await makeRunner(FakeRunner([.stream(lines: lines, status: 1)]), state: state, sink: sink).run(.doctor, pod: nil)
    #expect(state.doctor == .finished(.failing(check: "pods", detail: "blog down")))
    #expect(sink.posted == ["doctor: the pods check failed — blog down"])
}

@MainActor
@Test func prettyPrintedHealthyDoctorReportCountsFixes() async throws {
    let lines = try goldenDoctorLines(healthy: true)
    let state = StackState()
    let sink = FakeSink()
    await makeRunner(FakeRunner([.stream(lines: lines, status: 0)]), state: state, sink: sink).run(.doctor, pod: nil)
    #expect(state.doctor == .finished(.fixed(1)))
    #expect(sink.posted.isEmpty)
}

@Test func startNoticeDoesNotShadowTheRealError() {
    let notice = "couldn't update ~/.ssh/config (not writable) — tainer ssh still works; fix with: sudo chown $USER ~/.ssh/config"
    #expect(ActionRunner.firstErrorLine(stderr: "\(notice)\ntainer: port 443 is in use\n", stdout: [], status: 1) == "port 443 is in use")
    #expect(ActionRunner.firstErrorLine(stderr: "\(notice)\nrenamed html/ to app/\nsomething broke\n", stdout: [], status: 1) == "something broke")
    #expect(ActionRunner.firstErrorLine(stderr: "", stdout: ["starting shop...", "pod failed", "ignoring leftover html/ (app/ exists)"], status: 1) == "pod failed")
    // Only notices: the first one is still better than a bare status.
    #expect(ActionRunner.firstErrorLine(stderr: "\(notice)\n", stdout: [], status: 1) == notice)
}
