import Foundation

enum CLILocator {
    static let installedPath = "/opt/tainer/bin/tainer"

    /// `TAINER_CLI` override, then the installed pkg path, then `tainer` on PATH.
    static func find(
        env: [String: String] = ProcessInfo.processInfo.environment,
        isExecutable: (String) -> Bool = { FileManager.default.isExecutableFile(atPath: $0) }
    ) -> String? {
        if let override = env["TAINER_CLI"], !override.isEmpty, isExecutable(override) {
            return override
        }
        if isExecutable(installedPath) { return installedPath }
        for dir in (env["PATH"] ?? "").split(separator: ":") {
            let candidate = "\(dir)/tainer"
            if isExecutable(candidate) { return candidate }
        }
        return nil
    }
}
