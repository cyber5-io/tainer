import Foundation
import Observation

/// "Update all pods": updates the pods one at a time (one `tainer update <pod>` each), so the menu
/// can show the run, the pod actually being updated, and how the run ended.
@MainActor
@Observable
final class BulkUpdate {
    /// The pod whose update is running now.
    private(set) var current: String?
    private(set) var isRunning = false
    /// How the last run ended, shown on its row for `outcomeHold`.
    private(set) var outcome: BulkOutcome?

    @ObservationIgnored private let outcomeHold: Duration
    @ObservationIgnored private var generation = 0

    init(outcomeHold: Duration = .seconds(4)) { self.outcomeHold = outcomeHold }

    /// Updates each pod in turn. A call while a run is in flight does nothing and returns false.
    @discardableResult
    func run(_ names: [String], update: (String) async -> Bool) async -> Bool {
        guard !isRunning else { return false }
        isRunning = true
        generation += 1
        outcome = nil
        var updated = 0, failed = 0
        for name in names {
            current = name
            if await update(name) { updated += 1 } else { failed += 1 }
        }
        current = nil
        isRunning = false
        outcome = BulkOutcome(updated: updated, failed: failed)
        let mine = generation, hold = outcomeHold
        Task { [weak self] in
            try? await Task.sleep(for: hold)
            guard let self, self.generation == mine, !self.isRunning else { return }
            self.outcome = nil
        }
        return true
    }

    /// `names` in the order the menu lists them, so the run walks down the visible list; names
    /// not in the list follow in their own order.
    nonisolated static func order(_ names: [String], listed: [String]) -> [String] {
        let wanted = Set(names)
        let inList = listed.filter(wanted.contains)
        let seen = Set(inList)
        return inList + names.filter { !seen.contains($0) }
    }
}

struct BulkOutcome: Equatable, Sendable {
    let updated: Int
    let failed: Int
}
