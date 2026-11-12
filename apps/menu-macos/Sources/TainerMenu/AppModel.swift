import AppKit
import Observation

@MainActor
@Observable
final class AppModel {
    let state = StackState()
    let pulse = PulseDriver()
    let updates = UpdateChecker()
    let bulk = BulkUpdate()
    private var watch: WatchClient?

    private let actions: ActionRunner
    private let lister: ListRefresher
    private var listLoop: Task<Void, Never>?

    func refreshList() async { await lister.refresh() }

    func onAction(_ action: PodAction, _ pod: String?) {
        let actions = self.actions
        if action == .update, let pod {
            updatePods([pod])
            return
        }
        Task { await actions.run(action, pod: pod) }
    }

    /// Asks for a project folder (or its tainer.yaml), then starts it from there.
    func openExistingProject() {
        guard let folder = OpenProjectPanel.choose() else { return }
        let actions = self.actions
        Task { await actions.openProject(at: folder) }
    }

    private var runningPods: [String] { state.allPods.filter(\.isActive).map(\.name) }

    /// Warn when pods are running (Update now / Later), then run `update tainer`. The installer
    /// may relaunch this app before the command returns; the new process picks up the resume.
    func updateTainer() {
        guard updates.activity == nil, !bulk.isRunning else { return }
        let running = state.podsATainerUpdateRestarts
        if !running.isEmpty, !Confirm.ask(UpdateCopy.restartWarning(running), ok: "Update now", cancel: "Later") { return }
        startSelfUpdate(.tainer, activity: .updatingTainer)
    }

    /// A menu update that needs a newer tainer goes through the tainer update instead.
    func updateMenu() {
        guard updates.activity == nil, !bulk.isRunning else { return }
        if updates.menuUpdate?.needsTainerFirst == true {
            updateTainer()
            return
        }
        startSelfUpdate(.menu, activity: .updatingMenu)
    }

    /// "Check for updates": a fresh check now instead of waiting for the daily one.
    func checkForUpdates() {
        let updates = self.updates
        Task { await updates.checkNow() }
    }

    /// "Update all pods": one pod at a time, in the order the menu lists them, so the row can show
    /// the run and the list the pod being updated. A click while a run is in flight does nothing.
    func updateAllPods() {
        guard !bulk.isRunning, updates.podUpdatesAllowed else { return }
        let names = BulkUpdate.order(updates.pods.keys.sorted(), listed: state.allPods.map(\.name))
        guard !names.isEmpty, confirmRestarts(names) else { return }
        let actions = self.actions, updates = self.updates, bulk = self.bulk
        Task {
            if await bulk.run(names, update: { await actions.perform(.update, pod: $0) }) {
                await updates.check()
            }
        }
    }

    private func startSelfUpdate(_ target: ActionRunner.SelfUpdateTarget, activity: UpdateActivity) {
        let actions = self.actions, updates = self.updates
        updates.activity = activity
        Task {
            await actions.runSelfUpdate(target)
            updates.activity = nil
            await updates.check()
        }
    }

    /// Running pods restart while they update, so ask first (Update / Cancel).
    private func confirmRestarts(_ names: [String]) -> Bool {
        let running = names.filter(Set(runningPods).contains)
        return running.isEmpty || Confirm.ask(UpdateCopy.podsWarning(running), ok: "Update", cancel: "Cancel")
    }

    /// One pod's update arrow; then check again. Not while "Update all pods" runs.
    private func updatePods(_ names: [String]) {
        guard !names.isEmpty, updates.podUpdatesAllowed, !bulk.isRunning, confirmRestarts(names) else { return }
        let actions = self.actions, updates = self.updates
        Task {
            for name in names { await actions.run(.update, pod: name) }
            await updates.check()
        }
    }

    init() {
        LoginItemRegistrar(service: SystemLoginItem(), flags: UserDefaults.standard, bundlePath: Bundle.main.bundlePath).ensure()
        let state = self.state
        let pulse = self.pulse
        actions = ActionRunner(state: state)
        lister = ListRefresher(state: state)
        pulse.sync(state.engine)
        // Safety net for an update that was interrupted: bring back the pods it stopped.
        let updates = self.updates
        let pending = ResumeFile.pendingCount()
        if pending > 0 { updates.activity = .restarting(pending) }
        let actions = self.actions
        Task {
            await actions.runResume()
            if case .restarting = updates.activity { updates.activity = nil }
        }
        updates.start()
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
        updates.stop()
        watch?.stop()
        pulse.stop()
        NSApplication.shared.terminate(nil)
    }
}

/// A two-button alert; true when the first (confirming) button was chosen.
@MainActor
enum Confirm {
    static func ask(_ message: String, ok: String, cancel: String) -> Bool {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: ok)
        alert.addButton(withTitle: cancel)
        NSApp.activate()
        return alert.runModal() == .alertFirstButtonReturn
    }
}
