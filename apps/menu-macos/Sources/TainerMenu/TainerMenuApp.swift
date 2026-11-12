import SwiftUI

@main
enum Main {
    static func main() {
        switch LaunchMode(arguments: CommandLine.arguments) {
        case .loginItemStatus:
            print(SystemLoginItem().status.rawValue)
        case .unregisterLoginItem:
            let removed = LoginItemRegistrar.uninstall(remove: SystemLoginItem().removeForUninstall, flags: UserDefaults.standard)
            if !removed { exit(1) }
        case .app:
            TainerMenuApp.main()
        }
    }
}

struct TainerMenuApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        MenuBarExtra {
            MenuContent(state: model.state, updates: model.updates, bulk: model.bulk, onAction: { model.onAction($0, $1) }, onQuit: model.quit,
                        onOpen: model.refreshList, onUpdateTainer: model.updateTainer, onUpdateMenu: model.updateMenu,
                        onUpdateAllPods: model.updateAllPods, onOpenProject: model.openExistingProject,
                        onCheckForUpdates: model.checkForUpdates)
        } label: {
            BarIcon(health: model.state.engine, pulseHigh: model.pulse.high, hasUpdates: model.updates.hasUpdates)
        }
        .menuBarExtraStyle(.window)
    }
}
