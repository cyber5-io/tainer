import AppKit
import Observation

@MainActor
@Observable
final class AppModel {
    let state = StackState()
    let pulse = PulseDriver()
    private var watch: WatchClient?

    private let actions: ActionRunner

    func onAction(_ action: PodAction, _ pod: String?) {
        let actions = self.actions
        Task { await actions.run(action, pod: pod) }
    }

    init() {
        let state = self.state
        let pulse = self.pulse
        actions = ActionRunner(state: state)
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
