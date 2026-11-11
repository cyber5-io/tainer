import AppKit

/// The Open Existing Project picker: a project folder or the tainer.yaml inside one. Picking a
/// file is enabled only for that exact name; every other file stays greyed out.
@MainActor
enum OpenProjectPanel {
    nonisolated static let manifestName = "tainer.yaml"

    /// Asks for a project and returns its folder, or nil when cancelled. The menu window closes
    /// as the panel takes focus (MenuBarExtra windows dismiss when they lose key status).
    static func choose() -> URL? {
        let panel = NSOpenPanel()
        let filter = ManifestFilter()
        panel.delegate = filter
        panel.canChooseDirectories = true
        panel.canChooseFiles = true
        panel.allowsMultipleSelection = false
        panel.prompt = "Open"
        panel.message = OpenProjectCopy.panelMessage
        NSApp.activate()
        let response = withExtendedLifetime(filter) { panel.runModal() }
        guard response == .OK, let url = panel.url else { return nil }
        return projectFolder(for: url)
    }

    /// A folder is itself; a chosen tainer.yaml is the folder that holds it; anything else is nil.
    nonisolated static func projectFolder(for url: URL) -> URL? {
        if isDirectory(url) { return url }
        return url.lastPathComponent == manifestName ? url.deletingLastPathComponent() : nil
    }

    nonisolated static func isSelectable(_ url: URL) -> Bool {
        isDirectory(url) || url.lastPathComponent == manifestName
    }

    nonisolated private static func isDirectory(_ url: URL) -> Bool {
        (try? url.resourceValues(forKeys: [.isDirectoryKey]))?.isDirectory == true
    }
}

private final class ManifestFilter: NSObject, NSOpenSavePanelDelegate {
    func panel(_ sender: Any, shouldEnable url: URL) -> Bool { OpenProjectPanel.isSelectable(url) }
}
