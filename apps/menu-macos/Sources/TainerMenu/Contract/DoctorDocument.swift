import Foundation

/// The `doctor --fix --json` report document.
struct DoctorDocument: Decodable, Equatable {
    struct Check: Decodable, Equatable {
        var name: String
        var status: String
        var detail: String?
        var hint: String?
    }

    var checks: [Check]
    var fixed: [String]?
    var healthy: Bool?

    /// The document among the output lines, if any line is one.
    static func parse(_ lines: [String]) -> DoctorDocument? {
        for line in lines {
            guard let data = line.data(using: .utf8),
                  let doc = try? JSONDecoder().decode(DoctorDocument.self, from: data) else { continue }
            return doc
        }
        return nil
    }

    var outcome: DoctorOutcome {
        let failing = checks.first { $0.status == "fail" }
        if healthy == false || (healthy == nil && failing != nil) {
            return .failing(check: failing?.name ?? "", detail: failing?.detail ?? "")
        }
        let repaired = fixed?.count ?? 0
        return repaired > 0 ? .fixed(repaired) : .healthy
    }
}

/// What a finished doctor run found.
enum DoctorOutcome: Equatable {
    case healthy
    case fixed(Int)
    case failing(check: String, detail: String)
    /// The run itself failed (tainer missing, could not start, error envelope).
    case couldNotRun
}

/// The doctor row's state.
enum DoctorRun: Equatable {
    case idle
    case running
    case finished(DoctorOutcome)
}
