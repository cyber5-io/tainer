import Foundation
import UserNotifications

/// Where failure banners go.
protocol NotificationSink: Sendable {
    func post(_ message: String) async
}

/// Presents banners even while the menu window (the app's foreground state) is open.
private final class ForegroundBannerDelegate: NSObject, UNUserNotificationCenterDelegate {
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner]
    }
}

/// Best-effort banners. Without an app bundle (dev binary, `swift test`) there is no
/// notification center, so the message goes to stderr; a denied permission is a no-op.
final class SystemNotificationSink: NotificationSink, @unchecked Sendable {
    private let lock = NSLock()
    private var authorizationRequested = false
    private let delegate = ForegroundBannerDelegate()

    func post(_ message: String) async {
        guard Bundle.main.bundleIdentifier != nil else {
            FileHandle.standardError.write(Data("tainer-menu: \(message)\n".utf8))
            return
        }
        let center = UNUserNotificationCenter.current()
        let first = lock.withLock { () -> Bool in
            defer { authorizationRequested = true }
            return !authorizationRequested
        }
        if first {
            center.delegate = delegate
            guard (try? await center.requestAuthorization(options: [.alert])) == true else { return }
        }
        let content = UNMutableNotificationContent()
        content.title = "Tainer"
        content.body = message
        try? await center.add(UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil))
    }
}

/// Runs pod actions through the CLI. State changes arrive on the watch stream; this only
/// toggles `busy` and reports failures.
@MainActor
final class ActionRunner {
    private let state: StackState
    private let runner: any ProcessRunner
    private let sink: any NotificationSink
    private let locate: () -> String?
    private let doctorResultHold: Duration
    private var doctorGeneration = 0
    private var selfUpdateRunning = false
    private var openingFolders: Set<String> = []

    /// Words the CLI always reads as update targets, never as a project name.
    static let reservedUpdateTargets: Set<String> = ["tainer", "menu", "resume"]

    init(
        state: StackState,
        runner: any ProcessRunner = SubprocessRunner(),
        sink: any NotificationSink = SystemNotificationSink(),
        locate: @escaping () -> String? = { CLILocator.find() },
        doctorResultHold: Duration = .seconds(8)
    ) {
        self.state = state
        self.runner = runner
        self.sink = sink
        self.locate = locate
        self.doctorResultHold = doctorResultHold
    }

    func run(_ action: PodAction, pod: String?) async {
        let label: String
        let arguments: [String]
        switch (action, pod) {
        case (.doctor, _):
            await runDoctor()
            return
        case (.start, let pod?), (.stop, let pod?), (.restart, let pod?):
            guard !state.busy.contains(pod) else { return }
            label = pod
            arguments = [command(action), pod, "--json"]
        case (.update, let pod?):
            guard !state.busy.contains(pod), !selfUpdateRunning else { return }
            label = pod
            guard let target = updateTarget(pod) else {
                await sink.post("\(pod): update it from its folder with tainer update ./\(pod)")
                return
            }
            arguments = ["update", target, "--json"]
        default:
            return
        }

        state.busy.insert(label)
        defer { state.busy.remove(label) }

        if let message = await failure(arguments, isDoctor: false) {
            await sink.post("\(label): \(message)")
        }
    }

    /// `doctor --fix --json`. The row shows the run and then its outcome for `doctorResultHold`;
    /// a failing check, or a run that could not execute, also goes to the notification sink.
    private func runDoctor() async {
        guard state.doctor != .running else { return }
        state.doctor = .running
        doctorGeneration += 1
        let generation = doctorGeneration
        let output = LineCollector()
        let outcome: DoctorOutcome
        if let message = await failure(["doctor", "--fix", "--json"], isDoctor: true, into: output) {
            await sink.post("doctor: \(message)")
            outcome = .couldNotRun
        } else {
            // A clean exit is a healthy run even if its document can't be read.
            outcome = DoctorDocument.parse(await output.lines)?.outcome ?? .healthy
            if case .failing(let check, let detail) = outcome {
                await sink.post(DoctorCopy.failureBanner(check: check, detail: detail))
            }
        }
        state.doctor = .finished(outcome)
        let hold = doctorResultHold
        Task { [weak self] in
            try? await Task.sleep(for: hold)
            guard let self, self.doctorGeneration == generation, case .finished = self.state.doctor else { return }
            self.state.doctor = .idle
        }
    }

    /// `tainer start --plain` run inside `folder`, the way a user starts a project from its
    /// folder. The pod then appears on the watch stream; only a failure is reported. A folder
    /// without tainer.yaml is refused here rather than initialised.
    func openProject(at folder: URL) async {
        let label = folder.lastPathComponent
        guard FileManager.default.fileExists(atPath: folder.appendingPathComponent("tainer.yaml").path) else {
            await sink.post(OpenProjectCopy.notAProject)
            return
        }
        guard openingFolders.insert(folder.path).inserted else { return }
        defer { openingFolders.remove(folder.path) }
        guard let cli = locate() else {
            await sink.post("\(label): tainer was not found")
            return
        }
        let output = LineCollector()
        let result: ProcessResult
        do {
            result = try await runner.run(executable: cli, arguments: ["start", "--plain"], currentDirectory: folder) {
                await output.add($0)
            }
        } catch {
            await sink.post("\(label): could not run tainer")
            return
        }
        guard result.status != 0 else { return }
        await sink.post("\(label): \(Self.firstErrorLine(stderr: result.stderr, stdout: await output.lines, status: result.status))")
    }

    /// Lines a start prints on success too; never the reason it failed.
    nonisolated static let startNoticePrefixes = [
        "couldn't update ~/.ssh/config",
        "renamed html/ to app/",
        "ignoring leftover html/",
    ]

    /// What a failed plain-text run said: the CLI's own error line ("tainer: <message>", prefix
    /// removed), else the first stderr line that isn't a known notice, else the last stdout line
    /// that isn't one, else the first stderr line, else the exit status.
    nonisolated static func firstErrorLine(stderr: String, stdout: [String], status: Int32) -> String {
        let trimmed = { (s: Substring) in s.trimmingCharacters(in: .whitespaces) }
        let errLines = stderr.split(whereSeparator: \.isNewline).map(trimmed).filter { !$0.isEmpty }
        let outLines = stdout.map { trimmed(Substring($0)) }.filter { !$0.isEmpty }
        let isNotice = { (line: String) in startNoticePrefixes.contains { line.hasPrefix($0) } }
        let pick: String
        if let line = errLines.first(where: { $0.hasPrefix("tainer: ") }) {
            pick = String(line.dropFirst("tainer: ".count))
        } else if let line = errLines.first(where: { !isNotice($0) }) {
            pick = line
        } else if let line = outLines.last(where: { !isNotice($0) }) {
            pick = line
        } else if let line = errLines.first ?? outLines.last {
            pick = line
        } else {
            return "tainer exited with status \(status)"
        }
        return String(pick.prefix(200))
    }

    /// A pod named like an update target is updated by its folder path, which the CLI never
    /// mistakes for a target; nil when that path isn't known.
    private func updateTarget(_ pod: String) -> String? {
        guard Self.reservedUpdateTargets.contains(pod) else { return pod }
        let path = state.listed.first(where: { $0.name == pod })?.path ?? ""
        return path.isEmpty ? nil : path
    }

    enum SelfUpdateTarget: String { case tainer, menu }

    /// `tainer update tainer|menu --yes --json`: download, verify, open Installer. The installer may
    /// relaunch this app mid-run; the CLI and postinstall bring the pods back either way.
    func runSelfUpdate(_ target: SelfUpdateTarget) async {
        guard !selfUpdateRunning else { return }
        selfUpdateRunning = true
        defer { selfUpdateRunning = false }
        let output = LineCollector()
        if let message = await failure(["update", target.rawValue, "--yes", "--json"], isDoctor: false, into: output) {
            await sink.post("\(target.rawValue): \(message)")
            return
        }
        for pod in SelfUpdateDocument.failed(await output.lines) {
            await sink.post("\(pod.name) didn't come back: \(pod.reason)")
        }
    }

    /// The launch-time safety net: brings back pods an interrupted update left stopped, if any.
    /// Its output is ignored.
    func runResume() async {
        guard let cli = locate() else { return }
        _ = try? await runner.run(executable: cli, arguments: ["update", "resume"]) { _ in }
    }

    private func command(_ action: PodAction) -> String {
        switch action {
        case .start: "start"
        case .stop: "stop"
        default: "restart"
        }
    }

    /// A failure message, or nil on success. A top-level error object is always a failure, and
    /// so is a non-zero exit; the one exemption is doctor, whose report document accompanies exit 1.
    private func failure(_ arguments: [String], isDoctor: Bool, into output: LineCollector = LineCollector()) async -> String? {
        guard let cli = locate() else { return "tainer was not found" }
        let result: ProcessResult
        do {
            result = try await runner.run(executable: cli, arguments: arguments) { await output.add($0) }
        } catch {
            return "could not run tainer"
        }
        let lines = await output.lines
        for line in lines {
            if case .errorEnvelope(_, let message) = StreamLine.parse(line) {
                return message.isEmpty ? "the command failed" : message
            }
        }
        if result.status != 0 {
            let reportAllowed = isDoctor && DoctorDocument.parse(lines) != nil
            if !reportAllowed {
                let tail = result.stderr.trimmingCharacters(in: .whitespacesAndNewlines)
                return tail.isEmpty ? "tainer exited with status \(result.status)" : String(tail.suffix(200))
            }
        }
        return nil
    }
}

/// The `failed` list of `update tainer|menu --json`.
enum SelfUpdateDocument {
    struct Failed: Decodable, Equatable { var name: String; var reason: String }
    private struct Doc: Decodable { var failed: [Failed]? }

    static func failed(_ lines: [String]) -> [Failed] {
        let text = lines.joined(separator: "\n")
        guard let data = text.data(using: .utf8), let doc = try? JSONDecoder().decode(Doc.self, from: data) else { return [] }
        return doc.failed ?? []
    }
}

private actor LineCollector {
    private(set) var lines: [String] = []
    func add(_ line: String) { lines.append(line) }
}
