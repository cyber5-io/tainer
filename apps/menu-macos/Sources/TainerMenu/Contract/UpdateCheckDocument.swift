import Foundation

/// The `tainer` or `menu` object of `tainer update --check --json` (docs/api/cli-json-v1.md, `update`).
struct ReleaseInfo: Equatable, Decodable, Sendable {
    var installed: String
    var available: String
    var updateAvailable: Bool
    var notes: String
    var minTainer: String = ""
    var needsTainerFirst: Bool = false

    init(installed: String, available: String, updateAvailable: Bool, notes: String,
         minTainer: String = "", needsTainerFirst: Bool = false) {
        self.installed = installed
        self.available = available
        self.updateAvailable = updateAvailable
        self.notes = notes
        self.minTainer = minTainer
        self.needsTainerFirst = needsTainerFirst
    }

    private enum Key: String, CodingKey { case installed, available, updateAvailable, notes, minTainer, needsTainerFirst }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        installed = (try? c.decode(String.self, forKey: .installed)) ?? ""
        available = (try? c.decode(String.self, forKey: .available)) ?? ""
        updateAvailable = (try? c.decode(Bool.self, forKey: .updateAvailable)) ?? false
        notes = (try? c.decode(String.self, forKey: .notes)) ?? ""
        minTainer = (try? c.decode(String.self, forKey: .minTainer)) ?? ""
        needsTainerFirst = (try? c.decode(Bool.self, forKey: .needsTainerFirst)) ?? false
    }
}

/// A pod whose parts have an update.
struct PodUpdate: Equatable, Sendable {
    var name: String
    var parts: String
}

/// The single document of `tainer update --check --json`. Decoded tolerantly: an older CLI
/// without `tainer`/`menu` leaves them nil, and unknown keys are ignored.
struct UpdateCheckDocument: Equatable, Sendable {
    var tainer: ReleaseInfo?
    var menu: ReleaseInfo?
    /// Only pods with `updateAvailable` true, keyed by name.
    var pods: [String: PodUpdate]

    private struct Row: Decodable {
        var name: String
        var updateAvailable: Bool
        var parts: String

        private enum Key: String, CodingKey { case name, updateAvailable, parts }

        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: Key.self)
            name = try c.decode(String.self, forKey: .name)
            updateAvailable = (try? c.decode(Bool.self, forKey: .updateAvailable)) ?? false
            parts = (try? c.decode(String.self, forKey: .parts)) ?? ""
        }
    }

    private struct Doc: Decodable {
        var pods: [Row]
        var tainer: ReleaseInfo?
        var menu: ReleaseInfo?
    }

    /// nil for an error envelope, a malformed document, or no output at all.
    static func parse(_ lines: [String]) -> UpdateCheckDocument? {
        let text = lines.joined(separator: "\n")
        guard !text.isEmpty, let data = text.data(using: .utf8),
              let doc = try? JSONDecoder().decode(Doc.self, from: data)
        else { return nil }
        var pods: [String: PodUpdate] = [:]
        for row in doc.pods where row.updateAvailable {
            pods[row.name] = PodUpdate(name: row.name, parts: row.parts)
        }
        return UpdateCheckDocument(tainer: doc.tainer, menu: doc.menu, pods: pods)
    }
}

/// The pods `tainer update resume` will bring back, read from the CLI's resume file
/// (`$XDG_CONFIG_HOME/tainer/update-resume.json`, else `~/.config/tainer/update-resume.json`).
enum ResumeFile {
    private struct Doc: Decodable {
        struct Entry: Decodable { var name: String }
        var pods: [Entry]
    }

    static func path(env: [String: String]) -> URL {
        if let xdg = env["XDG_CONFIG_HOME"], !xdg.isEmpty {
            return URL(fileURLWithPath: xdg).appendingPathComponent("tainer/update-resume.json")
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".config/tainer/update-resume.json")
    }

    /// 0 when there is no file or it can't be read.
    static func pendingCount(env: [String: String] = ProcessInfo.processInfo.environment) -> Int {
        guard let data = try? Data(contentsOf: path(env: env)),
              let doc = try? JSONDecoder().decode(Doc.self, from: data)
        else { return 0 }
        return doc.pods.count
    }
}
