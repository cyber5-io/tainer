import Observation

/// The four menu-bar icon states.
enum BarHealth: Equatable {
    case healthy(runningPods: Int)
    case idle
    case recovering
    case unreachable
}

/// Everything the watch client can tell the UI.
enum StackUpdate: Sendable {
    case event(Event)
    case recovering(hint: String?)
    case unreachable
}

@MainActor
@Observable
final class StackState {
    private(set) var engine: BarHealth = .recovering
    private(set) var pods: [Pod] = []
    private(set) var hint: String?
    var busy: Set<String> = []

    private var engineHealthy = false

    var statusLine: (pill: String, counts: String) {
        switch engine {
        case .healthy(let running):
            return ("Running", "\(running) of \(pods.count) pods running")
        case .idle:
            return ("Idle", pods.isEmpty ? "No pods yet" : "0 of \(pods.count) pods running")
        case .recovering:
            return ("Recovering", hint ?? "Tainer is getting ready")
        case .unreachable:
            return ("Unreachable", hint ?? "Tainer is not responding")
        }
    }

    func apply(_ update: StackUpdate) {
        switch update {
        case .event(let event):
            apply(event)
        case .recovering(let hint):
            self.hint = hint
            if engine != .unreachable { engine = .recovering }
            engineHealthy = false
        case .unreachable:
            engine = .unreachable
            engineHealthy = false
        }
    }

    private func apply(_ event: Event) {
        switch event {
        case .hello(let engine, let pods):
            hint = nil
            self.pods = Self.ordered(pods)
            setEngine(engine.state)
        case .engine(let engine):
            hint = nil
            setEngine(engine.state)
        case .pod(let pod):
            var next = pods.filter { $0.name != pod.name }
            next.append(pod)
            pods = Self.ordered(next)
            refreshBar()
        case .progress, .unknown:
            break
        }
    }

    private func setEngine(_ health: HealthState) {
        switch health {
        case .healthy:
            engineHealthy = true
            refreshBar()
        case .failed:
            engineHealthy = false
            engine = .unreachable
        case .recovering, .unknown:
            engineHealthy = false
            engine = .recovering
        }
    }

    private func refreshBar() {
        guard engineHealthy else { return }
        let running = pods.filter { $0.state == .running }.count
        engine = running > 0 ? .healthy(runningPods: running) : .idle
    }

    private static func ordered(_ pods: [Pod]) -> [Pod] {
        pods.sorted {
            let a = $0.state == .running, b = $1.state == .running
            return a != b ? a : $0.name < $1.name
        }
    }
}
