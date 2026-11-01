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
    private(set) var listed: [ListedProject] = []
    private(set) var page = 1

    static let pageSize = 10

    private var engineHealthy = false

    /// Pill reflects stack health; an idle stack is still healthy (only the bar icon dims).
    var statusLine: (pill: String, counts: String) {
        switch engine {
        case .healthy, .idle:
            let counts = allPods.isEmpty ? "No pods yet" : "\(activeCount) of \(allPods.count) pods running"
            return ("HEALTHY", counts)
        case .recovering:
            return ("RECOVERING", hint ?? "Tainer is getting ready")
        case .unreachable:
            return ("UNREACHABLE", hint ?? "Tainer is not responding")
        }
    }

    /// A partial pod has containers up, so it counts as running.
    private var activeCount: Int { allPods.filter { $0.isActive }.count }

    /// Stream pods plus registered projects the stream has not reported (shown as stopped).
    var allPods: [Pod] {
        let known = Set(pods.map(\.name))
        let extra = listed.filter { !known.contains($0.name) }
            .map { Pod(name: $0.name, domain: $0.domain, state: .stopped, running: 0, total: 0) }
        return Self.ordered(pods + extra)
    }

    func applyList(_ projects: [ListedProject]) {
        listed = projects
    }

    var pageCount: Int { max(1, (allPods.count + Self.pageSize - 1) / Self.pageSize) }
    private var clampedPage: Int { min(max(page, 1), pageCount) }
    var showsPager: Bool { allPods.count > Self.pageSize }
    var canGoPrevious: Bool { clampedPage > 1 }
    var canGoNext: Bool { clampedPage < pageCount }

    var visiblePods: [Pod] {
        let all = allPods
        let start = (clampedPage - 1) * Self.pageSize
        return Array(all[min(start, all.count)..<min(start + Self.pageSize, all.count)])
    }

    var pageLabel: String {
        let total = allPods.count
        guard total > 0 else { return "0 of 0" }
        let start = (clampedPage - 1) * Self.pageSize
        return "\(start + 1)-\(min(start + Self.pageSize, total)) of \(total)"
    }

    func nextPage() { page = min(clampedPage + 1, pageCount) }
    func previousPage() { page = max(clampedPage - 1, 1) }
    func resetPage() { page = 1 }

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
        let running = activeCount
        engine = running > 0 ? .healthy(runningPods: running) : .idle
    }

    private static func ordered(_ pods: [Pod]) -> [Pod] {
        pods.sorted {
            let a = $0.isActive, b = $1.isActive
            return a != b ? a : $0.name < $1.name
        }
    }
}

extension Pod {
    var isActive: Bool { state == .running || state == .partial }
}
