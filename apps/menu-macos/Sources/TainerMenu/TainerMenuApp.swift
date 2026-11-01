import SwiftUI

@main
struct TainerMenuApp: App {
    var body: some Scene {
        MenuBarExtra {
            Text("Tainer is starting up")
                .padding()
        } label: {
            Image("bar-mark", bundle: AppBundle.resources)
        }
        .menuBarExtraStyle(.window)
    }
}
