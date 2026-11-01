import Foundation
import Testing
@testable import TainerMenu

/// Deterministic clock: sleepers resume only when a test advances time.
final class FakeClock: WatchClock, @unchecked Sendable {
    private struct Waiter {
        let id: Int
        let deadline: TimeInterval
        let duration: TimeInterval
        let continuation: CheckedContinuation<Void, any Error>
    }

    private let lock = NSLock()
    private var current: TimeInterval = 0
    private var nextID = 0
    private var waiters: [Waiter] = []
    private var requests: [TimeInterval] = []

    /// Every duration ever passed to `sleep`, in request order.
    var requested: [TimeInterval] { lock.withLock { requests } }

    /// Durations of backoff sleeps only (the escalation timer sleeps for 60).
    var backoffs: [TimeInterval] { requested.filter { $0 != WatchClient.escalationDelay } }

    /// Remaining time of the earliest pending backoff sleeper, if any.
    var pendingBackoff: TimeInterval? {
        lock.withLock {
            waiters.filter { $0.duration != WatchClient.escalationDelay }
                .map { $0.deadline - current }.min()
        }
    }

    func sleep(seconds: TimeInterval) async throws {
        try Task.checkCancellation()
        let id = lock.withLock { () -> Int in nextID += 1; requests.append(seconds); return nextID }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (c: CheckedContinuation<Void, any Error>) in
                lock.lock()
                if Task.isCancelled {
                    lock.unlock()
                    c.resume(throwing: CancellationError())
                } else {
                    waiters.append(Waiter(id: id, deadline: current + seconds, duration: seconds, continuation: c))
                    lock.unlock()
                }
            }
        } onCancel: {
            let hit = lock.withLock { () -> Waiter? in
                guard let i = waiters.firstIndex(where: { $0.id == id }) else { return nil }
                return waiters.remove(at: i)
            }
            hit?.continuation.resume(throwing: CancellationError())
        }
    }

    func advance(by seconds: TimeInterval) {
        let due = lock.withLock { () -> [Waiter] in
            current += seconds
            let ready = waiters.filter { $0.deadline <= current }.sorted { $0.deadline < $1.deadline }
            waiters.removeAll { $0.deadline <= current }
            return ready
        }
        for w in due { w.continuation.resume() }
    }
}

/// Scripted stand-in for the `tainer watch --json` child process.
final class FakeRunner: ProcessRunner, @unchecked Sendable {
    enum Step {
        case stream(lines: [String], stderr: String = "", status: Int32 = 1)
        case streamThenBlock(lines: [String])
        case spawnFailure
        case blockUntilCancelled
    }

    struct SpawnFailed: Error {}

    private let lock = NSLock()
    private var script: [Step]
    private var spawns = 0
    private var cancelled = 0
    private var args: [[String]] = []

    init(_ script: [Step]) { self.script = script }

    var spawnCount: Int { lock.withLock { spawns } }
    var terminations: Int { lock.withLock { cancelled } }
    var arguments: [[String]] { lock.withLock { args } }

    func run(executable: String, arguments: [String], onLine: @escaping @Sendable (String) async -> Void) async throws -> ProcessResult {
        let step = lock.withLock { () -> Step in
            spawns += 1
            args.append(arguments)
            return script.isEmpty ? .blockUntilCancelled : script.removeFirst()
        }
        switch step {
        case .spawnFailure:
            throw SpawnFailed()
        case let .stream(lines, stderr, status):
            for line in lines { await onLine(line) }
            return ProcessResult(status: status, stderr: stderr)
        case let .streamThenBlock(lines):
            for line in lines { await onLine(line) }
            return await blockUntilCancelled()
        case .blockUntilCancelled:
            return await blockUntilCancelled()
        }
    }

    private func blockUntilCancelled() async -> ProcessResult {
        await withTaskCancellationHandler {
            while !Task.isCancelled { await Task.yield() }
        } onCancel: {
            lock.withLock { cancelled += 1 }
        }
        return ProcessResult(status: 15, stderr: "")
    }
}

/// Yields until `condition` holds; no wall-clock waiting.
@MainActor
func settle(_ condition: @MainActor () -> Bool, sourceLocation: SourceLocation = #_sourceLocation) async {
    for _ in 0..<50_000 {
        if condition() { return }
        await Task.yield()
    }
    Issue.record("condition never became true", sourceLocation: sourceLocation)
}
