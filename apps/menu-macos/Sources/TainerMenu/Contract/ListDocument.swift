import Foundation

/// One entry of `tainer list --json`.
struct ListedProject: Equatable, Decodable {
    var name: String
    var domain: String

    init(name: String, domain: String) {
        self.name = name
        self.domain = domain
    }

    private enum Key: String, CodingKey { case name, domain }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        name = try c.decode(String.self, forKey: .name)
        domain = (try? c.decode(String.self, forKey: .domain)) ?? ""
    }
}

/// The single `{"v":1,"projects":[...]}` document of `tainer list --json`.
enum ListDocument {
    private struct Doc: Decodable { var projects: [ListedProject] }

    /// nil for an error envelope, a malformed document, or no output at all.
    static func parse(_ lines: [String]) -> [ListedProject]? {
        let text = lines.joined(separator: "\n")
        guard let data = text.data(using: .utf8), !text.isEmpty else { return nil }
        return (try? JSONDecoder().decode(Doc.self, from: data))?.projects
    }
}
