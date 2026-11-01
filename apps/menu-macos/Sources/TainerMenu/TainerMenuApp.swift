import SwiftUI

@main
struct TainerMenuApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        MenuBarExtra {
            MenuContent(state: model.state, onAction: { model.onAction($0, $1) }, onQuit: model.quit)
        } label: {
            BarIcon(health: model.state.engine, pulseHigh: model.pulse.high)
        }
        .menuBarExtraStyle(.window)
    }
}
