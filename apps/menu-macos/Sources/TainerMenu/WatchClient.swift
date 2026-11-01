import Foundation

struct ProcessResult: Sendable, Equatable {
    var status: Int32
    var stderr: String
}

/// Runs a child to completion, feeding each stdout line to `onLine`.
/// Cancelling the calling task must terminate the child. Throws only if the child cannot be spawned.
protocol ProcessRunner: Sendable {
    func run(executable: String, arguments: [String], onLine: @Sendable (String) async -> Void) async throws -> ProcessResult
}

protocol WatchClock: Sendable {
    func sleep(seconds: TimeInterval) async throws
}

struct SystemClock: WatchClock {
    func sleep(seconds: TimeInterval) async throws {
        try await Task.sleep(for: .seconds(seconds))
    }
}

/// Keeps one `tainer watch --json` child alive and turns its output into `StackUpdate`s.
final class WatchClient: @unchecked Sendable {
    typealias Sink = @MainActor @Sendable (StackUpdate) -> Void

    static let escalationDelay: TimeInterval = 60
    static let initialBackoff: TimeInterval = 1
    static let maxBackoff: TimeInterval = 30
    static let oldCLIHint = "update tainer"

    private let executable: String
    private let runner: any ProcessRunner
    private let clock: any WatchClock
    private let sink: Sink

    private let lock = NSLock()
    private var loop: Task<Void, Never>?
    private var escalation: Task<Void, Never>?
    private var stopped = false
    private var epoch = 0

    init(executable: String, runner: any ProcessRunner = SubprocessRunner(), clock: any WatchClock = SystemClock(), sink: @escaping Sink) {
        self.executable = executable
        self.runner = runner
        self.clock = clock
        self.sink = sink
    }

    func start() {
        lock.lock()
        defer { lock.unlock() }
        guard loop == nil, !stopped else { return }
        loop = Task { await self.run() }
    }

    /// Terminates the child and halts the respawn loop for good.
    func stop() {
        lock.lock()
        stopped = true
        epoch += 1
        let l = loop, e = escalation
        escalation = nil
        lock.unlock()
        l?.cancel()
        e?.cancel()
    }

    private var isStopped: Bool { lock.withLock { stopped } || Task.isCancelled }

    private final class Stream: @unchecked Sendable {
        private let lock = NSLock()
        private var _hello = false, _lines = false
        var gotHello: Bool { lock.withLock { _hello } }
        var gotLines: Bool { lock.withLock { _lines } }
        func sawLine() { lock.withLock { _lines = true } }
        func sawHello() { lock.withLock { _hello = true } }
    }

    private func run() async {
        var backoff = Self.initialBackoff
        while !isStopped {
            let stream = Stream()
            var hint: String?
            do {
                let result = try await runner.run(executable: executable, arguments: ["watch", "--json"]) { line in
                    await self.handle(line, stream)
                }
                if !stream.gotLines, result.status != 0, !result.stderr.isEmpty {
                    hint = Self.oldCLIHint
                }
            } catch {
                if isStopped { return }
            }
            if isStopped { return }

            if stream.gotHello { backoff = Self.initialBackoff }
            await beginEpisodeIfNeeded(hint: hint)
            do { try await clock.sleep(seconds: backoff) } catch { return }
            backoff = min(backoff * 2, Self.maxBackoff)
        }
    }

    private func handle(_ line: String, _ stream: Stream) async {
        switch StreamLine.parse(line) {
        case .invalid:
            return
        case .errorEnvelope:
            stream.sawLine()
        case .event(let event):
            stream.sawLine()
            if case .hello = event {
                stream.sawHello()
                endEpisode()
            }
            await deliver(.event(event))
        }
    }

    /// Applies on the main actor, dropping the update if the client was stopped
    /// or (for `epoch`-tagged updates) a hello superseded it in the meantime.
    private func deliver(_ update: StackUpdate, epoch expected: Int? = nil) async {
        await MainActor.run {
            let ok = lock.withLock { !stopped && (expected == nil || expected == epoch) }
            if ok { sink(update) }
        }
    }

    var currentEpoch: Int { lock.withLock { epoch } }

    func fireEscalation(epoch: Int) async {
        await deliver(.unreachable, epoch: epoch)
    }

    private func endEpisode() {
        let e = lock.withLock { () -> Task<Void, Never>? in
            epoch += 1
            defer { escalation = nil }
            return escalation
        }
        e?.cancel()
    }

    private func beginEpisodeIfNeeded(hint: String?) async {
        let began = lock.withLock { () -> Bool in
            guard escalation == nil, !stopped else { return false }
            let captured = epoch
            escalation = Task {
                do { try await self.clock.sleep(seconds: Self.escalationDelay) } catch { return }
                await self.fireEscalation(epoch: captured)
            }
            return true
        }
        if began { await deliver(.recovering(hint: hint)) }
    }
}

/// Foundation.Process-backed runner.
struct SubprocessRunner: ProcessRunner {
    private final class Handle: @unchecked Sendable {
        let process: Process
        init(_ p: Process) { process = p }
        func terminate() { if process.isRunning { process.terminate() } }
    }

    /// Only the tail of stderr is kept; a watch child can run for hours.
    static let stderrLimit = 4096

    func run(executable: String, arguments: [String], onLine: @Sendable (String) async -> Void) async throws -> ProcessResult {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: executable)
        process.arguments = arguments
        let out = Pipe(), err = Pipe()
        process.standardOutput = out
        process.standardError = err
        process.standardInput = FileHandle.nullDevice
        try process.run()
        let handle = Handle(process)

        return await withTaskCancellationHandler {
            let stderrTask = Task.detached { () -> Data in
                var tail = Data()
                let reader = err.fileHandleForReading
                while case let chunk = reader.availableData, !chunk.isEmpty {
                    tail.append(chunk)
                    if tail.count > Self.stderrLimit { tail = tail.suffix(Self.stderrLimit) }
                }
                return tail
            }
            do {
                for try await line in out.fileHandleForReading.bytes.lines { await onLine(line) }
            } catch {}
            let stderrData = await stderrTask.value
            let status = await Task.detached { () -> Int32 in
                process.waitUntilExit()
                return process.terminationStatus
            }.value
            return ProcessResult(status: status, stderr: String(decoding: stderrData, as: UTF8.self))
        } onCancel: {
            handle.terminate()
        }
    }
}
