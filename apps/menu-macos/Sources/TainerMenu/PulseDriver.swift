import Observation

/// Owns the recovering-icon pulse; the timer task exists only while pulsing is wanted.
@MainActor
@Observable
final class PulseDriver {
    private(set) var high = true
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private let interval: Duration

    init(interval: Duration = .milliseconds(650)) { self.interval = interval }

    var isActive: Bool { task != nil }

    nonisolated static func shouldPulse(_ health: BarHealth) -> Bool { health == .recovering }

    func sync(_ health: BarHealth) {
        if Self.shouldPulse(health) { start() } else { stop() }
    }

    func start() {
        guard task == nil else { return }
        task = Task { [weak self, interval] in
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard !Task.isCancelled, let self else { return }
                self.high.toggle()
            }
        }
    }

    func stop() {
        task?.cancel()
        task = nil
        high = true
    }
}
