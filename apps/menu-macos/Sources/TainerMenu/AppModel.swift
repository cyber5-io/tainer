import AppKit
import Observation

@MainActor
@Observable
final class AppModel {
    let state = StackState()
    private(set) var pulseHigh = true
    private var watch: WatchClient?
    private var pulse: Task<Void, Never>?

    /// Task 5 replaces this with the real command runner.
    var onAction: (PodAction, String?) -> Void = { _, _ in }

    init() {
        let state = self.state
        if let cli = CLILocator.find() {
            watch = WatchClient(executable: cli) { update in state.apply(update) }
            watch?.start()
        } else {
            state.apply(.unreachable)
        }
        pulse = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(650))
                guard let self else { return }
                if self.state.engine == .recovering { self.pulseHigh.toggle() } else { self.pulseHigh = true }
            }
        }
    }

    func quit() {
        watch?.stop()
        pulse?.cancel()
        NSApplication.shared.terminate(nil)
    }
}
