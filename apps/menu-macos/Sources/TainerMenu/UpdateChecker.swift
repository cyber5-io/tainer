import Foundation
import Observation

/// Asks `tainer update --check --json` what has an update: shortly after launch, once a day, after
/// an update, and when the user picks "Check for updates". `update --check` always fetches the
/// release afresh (it skips the 24h cache). Only checks; it never updates anything. A failed check
/// is silent and keeps the previous answer.
@MainActor
@Observable
final class UpdateChecker {
    static let launchDelay: TimeInterval = 10
    static let interval: TimeInterval = 24 * 3600

    private(set) var tainer: ReleaseInfo?
    private(set) var menu: ReleaseInfo?
    private(set) var pods: [String: PodUpdate] = [:]
    /// Set while an update the user started is in flight; drives the status header.
    var activity: UpdateActivity?
    /// True while a check runs; drives the "Check for updates" row.
    private(set) var isChecking = false
    /// How the last "Check for updates" ended, shown on its row for `outcomeHold`. nil while
    /// idle, and when updates were found (the update rows say so).
    private(set) var checkOutcome: CheckOutcome?

    @ObservationIgnored private let runner: any ProcessRunner
    @ObservationIgnored private let clock: any WatchClock
    @ObservationIgnored private let locate: () -> String?
    @ObservationIgnored private var checking = false
    @ObservationIgnored private var pendingRecheck = false
    @ObservationIgnored private var loop: Task<Void, Never>?
    @ObservationIgnored private let outcomeHold: Duration
    @ObservationIgnored private var outcomeGeneration = 0

    init(
        runner: any ProcessRunner = SubprocessRunner(),
        clock: any WatchClock = SystemClock(),
        locate: @escaping () -> String? = { CLILocator.find() },
        outcomeHold: Duration = .seconds(4)
    ) {
        self.runner = runner
        self.clock = clock
        self.locate = locate
        self.outcomeHold = outcomeHold
    }

    var tainerUpdate: ReleaseInfo? { tainer.flatMap { $0.updateAvailable ? $0 : nil } }
    var menuUpdate: ReleaseInfo? { menu.flatMap { $0.updateAvailable ? $0 : nil } }
    var tainerRow: String? { tainerUpdate.map { UpdateCopy.tainerRow($0.available) } }
    var menuRow: String? { menuUpdate.map { UpdateCopy.menuRow($0.available) } }
    var hasUpdates: Bool { tainerUpdate != nil || menuUpdate != nil || !pods.isEmpty }
    /// Pod updates wait while tainer or the menu is updating.
    var podUpdatesAllowed: Bool { activity == nil }

    func start() {
        guard loop == nil else { return }
        let clock = self.clock
        loop = Task { [weak self] in
            guard (try? await clock.sleep(seconds: Self.launchDelay)) != nil else { return }
            while !Task.isCancelled {
                await self?.check()
                guard (try? await clock.sleep(seconds: Self.interval)) != nil else { return }
            }
        }
    }

    func stop() {
        loop?.cancel()
        loop = nil
    }

    /// "Check for updates": a check whose end is shown on the row for a moment, "Up to date" or
    /// "Couldn't check for updates", then idle again. Found updates show as the update rows instead.
    func checkNow() async {
        guard let succeeded = await runCheck() else { return }
        let outcome: CheckOutcome? = !succeeded ? .couldNotCheck : (hasUpdates ? nil : .upToDate)
        outcomeGeneration += 1
        checkOutcome = outcome
        guard outcome != nil else { return }
        let generation = outcomeGeneration, hold = outcomeHold
        Task { [weak self] in
            try? await Task.sleep(for: hold)
            guard let self, self.outcomeGeneration == generation else { return }
            self.checkOutcome = nil
        }
    }

    /// One check at a time. A call while one is running asks for one more run once it finishes,
    /// so a re-check after an update is never lost.
    func check() async { _ = await runCheck() }

    /// `check`, returning whether the last run got an answer, or nil when this call only queued
    /// a re-check.
    private func runCheck() async -> Bool? {
        if checking {
            pendingRecheck = true
            return nil
        }
        guard let cli = locate() else { return false }
        checking = true
        isChecking = true
        defer {
            checking = false
            isChecking = false
        }
        var succeeded = false
        repeat {
            pendingRecheck = false
            succeeded = await runOnce(cli)
        } while pendingRecheck && !Task.isCancelled
        return succeeded
    }

    /// False when the check didn't run, failed, or couldn't reach the release.
    private func runOnce(_ cli: String) async -> Bool {
        let collector = CheckLineCollector()
        guard (try? await runner.run(executable: cli, arguments: ["update", "--check", "--json"], onLine: { await collector.add($0) })) != nil,
              let doc = UpdateCheckDocument.parse(await collector.lines)
        else { return false }
        tainer = doc.tainer
        menu = doc.menu
        pods = doc.pods
        return !doc.releaseUnavailable
    }
}

private actor CheckLineCollector {
    private(set) var lines: [String] = []
    func add(_ line: String) { lines.append(line) }
}

enum CheckOutcome: Equatable, Sendable { case upToDate, couldNotCheck }
