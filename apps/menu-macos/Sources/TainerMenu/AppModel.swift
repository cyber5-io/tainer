import AppKit
import Observation

@MainActor
@Observable
final class AppModel {
    let state = StackState()
    let pulse = PulseDriver()
    private var watch: WatchClient?

    /// Task 5 replaces this with the real command runner.
    var onAction: (PodAction, String?) -> Void = { _, _ in }

    init() {
        let state = self.state
        let pulse = self.pulse
        pulse.sync(state.engine)
        if let cli = CLILocator.find() {
            watch = WatchClient(executable: cli) { update in
                state.apply(update)
                pulse.sync(state.engine)
            }
            watch?.start()
        } else {
            state.apply(.unreachable)
            pulse.sync(state.engine)
        }
    }

    func quit() {
        watch?.stop()
        pulse.stop()
        NSApplication.shared.terminate(nil)
    }
}
