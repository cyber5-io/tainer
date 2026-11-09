import Foundation
import Observation

/// Asks `tainer update --check --json` what has an update: shortly after launch, once a day
/// (the release itself is fetched at most once per 24h), and whenever the app asks after an update. Only checks; it never updates anything. A failed
/// check is silent and keeps the previous answer.
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

    @ObservationIgnored private let runner: any ProcessRunner
    @ObservationIgnored private let clock: any WatchClock
    @ObservationIgnored private let locate: () -> String?
    @ObservationIgnored private var checking = false
    @ObservationIgnored private var pendingRecheck = false
    @ObservationIgnored private var loop: Task<Void, Never>?

    init(
        runner: any ProcessRunner = SubprocessRunner(),
        clock: any WatchClock = SystemClock(),
        locate: @escaping () -> String? = { CLILocator.find() }
    ) {
        self.runner = runner
        self.clock = clock
        self.locate = locate
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

    /// One check at a time. A call while one is running asks for one more run once it finishes,
    /// so a re-check after an update is never lost.
    func check() async {
        if checking {
            pendingRecheck = true
            return
        }
        guard let cli = locate() else { return }
        checking = true
        defer { checking = false }
        repeat {
            pendingRecheck = false
            await runOnce(cli)
        } while pendingRecheck && !Task.isCancelled
    }

    private func runOnce(_ cli: String) async {
        let collector = CheckLineCollector()
        guard (try? await runner.run(executable: cli, arguments: ["update", "--check", "--json"], onLine: { await collector.add($0) })) != nil,
              let doc = UpdateCheckDocument.parse(await collector.lines)
        else { return }
        tainer = doc.tainer
        menu = doc.menu
        pods = doc.pods
    }
}

private actor CheckLineCollector {
    private(set) var lines: [String] = []
    func add(_ line: String) { lines.append(line) }
}
