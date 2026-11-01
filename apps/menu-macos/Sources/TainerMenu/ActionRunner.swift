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
    private var doctorRunning = false

    init(
        state: StackState,
        runner: any ProcessRunner = SubprocessRunner(),
        sink: any NotificationSink = SystemNotificationSink(),
        locate: @escaping () -> String? = { CLILocator.find() }
    ) {
        self.state = state
        self.runner = runner
        self.sink = sink
        self.locate = locate
    }

    func run(_ action: PodAction, pod: String?) async {
        let label: String
        let arguments: [String]
        switch (action, pod) {
        case (.doctor, _):
            guard !doctorRunning else { return }
            label = "doctor"
            arguments = ["doctor", "--fix", "--json"]
        case (.start, let pod?), (.stop, let pod?), (.restart, let pod?):
            guard !state.busy.contains(pod) else { return }
            label = pod
            arguments = [command(action), pod, "--json"]
        default:
            return
        }

        if action == .doctor { doctorRunning = true } else { state.busy.insert(label) }
        defer {
            if action == .doctor { doctorRunning = false } else { state.busy.remove(label) }
        }

        if let message = await failure(arguments, isDoctor: action == .doctor) {
            await sink.post("\(label): \(message)")
        }
    }

    private func command(_ action: PodAction) -> String {
        switch action {
        case .start: "start"
        case .stop: "stop"
        default: "restart"
        }
    }

    private static func isJSONDocument(_ line: String) -> Bool {
        guard let data = line.data(using: .utf8) else { return false }
        return (try? JSONSerialization.jsonObject(with: data)) is [String: Any]
    }

    /// A failure message, or nil on success. A top-level error object is always a failure, and
    /// so is a non-zero exit; the one exemption is doctor, whose report document accompanies exit 1.
    private func failure(_ arguments: [String], isDoctor: Bool) async -> String? {
        guard let cli = locate() else { return "tainer was not found" }
        let output = LineCollector()
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
            let reportAllowed = isDoctor && lines.contains(where: Self.isJSONDocument)
            if !reportAllowed {
                let tail = result.stderr.trimmingCharacters(in: .whitespacesAndNewlines)
                return tail.isEmpty ? "tainer exited with status \(result.status)" : String(tail.suffix(200))
            }
        }
        return nil
    }
}

private actor LineCollector {
    private(set) var lines: [String] = []
    func add(_ line: String) { lines.append(line) }
}
