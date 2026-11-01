import AppKit
import Observation

@MainActor
@Observable
final class AppModel {
    let state = StackState()
    let pulse = PulseDriver()
    private var watch: WatchClient?

    private let actions: ActionRunner
    private let lister: ListRefresher
    private var listLoop: Task<Void, Never>?

    func refreshList() async { await lister.refresh() }

    func onAction(_ action: PodAction, _ pod: String?) {
        let actions = self.actions
        Task { await actions.run(action, pod: pod) }
    }

    init() {
        let state = self.state
        let pulse = self.pulse
        actions = ActionRunner(state: state)
        lister = ListRefresher(state: state)
        pulse.sync(state.engine)
        // The menu's own .task is unreliable inside MenuBarExtra windows, so the
        // registry merge runs from here: once at launch, then every minute.
        let lister = self.lister
        listLoop = Task {
            while !Task.isCancelled {
                await lister.refresh()
                try? await Task.sleep(for: .seconds(60))
            }
        }
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
        listLoop?.cancel()
        watch?.stop()
        pulse.stop()
        NSApplication.shared.terminate(nil)
    }
}
