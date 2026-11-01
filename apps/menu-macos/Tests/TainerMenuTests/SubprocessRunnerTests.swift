import Foundation
import Testing
@testable import TainerMenu

private final class Lines: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [String] = []
    func add(_ s: String) { lock.withLock { items.append(s) } }
    var all: [String] { lock.withLock { items } }
}

@Test func subprocessRunnerStreamsLinesAndReportsExit() async throws {
    let lines = Lines()
    let result = try await SubprocessRunner().run(executable: "/bin/sh", arguments: ["-c", "echo a; echo b; echo oops >&2; exit 3"]) { lines.add($0) }
    #expect(lines.all == ["a", "b"])
    #expect(result.status == 3)
    #expect(result.stderr.contains("oops"))
}

@Test func subprocessRunnerSpawnFailureThrows() async {
    await #expect(throws: (any Error).self) {
        _ = try await SubprocessRunner().run(executable: "/nonexistent/tainer", arguments: []) { _ in }
    }
}

@Test func subprocessRunnerTerminatesChildOnCancel() async {
    let task = Task { try await SubprocessRunner().run(executable: "/bin/sleep", arguments: ["600"]) { _ in } }
    for _ in 0..<2000 { await Task.yield() }
    task.cancel()
    let result = try? await task.value
    #expect(result?.status == 15)
}

/// A long-lived silent child must not stall other children's stdout: Foundation's
/// FileHandle.AsyncBytes serialises every read on one shared IO queue, so the
/// runner has to read pipes itself. This deadlocked the menu's list refresh
/// whenever the watch stream was connected.
@Test func quickChildCompletesWhileSilentChildStreams() async throws {
    let idle = Task { try await SubprocessRunner().run(executable: "/bin/sh", arguments: ["-c", "sleep 30"]) { _ in } }
    try await Task.sleep(for: .milliseconds(150))
    let lines = Lines()
    let quick = Task { try await SubprocessRunner().run(executable: "/bin/echo", arguments: ["pong"]) { lines.add($0) } }
    let winner = await withTaskGroup(of: Bool.self) { group in
        group.addTask { (try? await quick.value) != nil }
        group.addTask { try? await Task.sleep(for: .seconds(5)); return false }
        let first = await group.next() ?? false
        group.cancelAll()
        return first
    }
    idle.cancel()
    _ = try? await idle.value
    #expect(winner, "quick child stalled behind the idle child's stdout read")
    #expect(lines.all == ["pong"])
}
