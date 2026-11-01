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
