import Foundation

enum PodAction: Equatable {
    case open, restart, stop, start, manage, doctor, update
}

struct PodRowModel {
    let pod: Pod
    let busy: Bool
    var update: PodUpdate? = nil
    /// False while tainer or the menu is updating: the update button is left out.
    var updatesAllowed = true

    var title: String { pod.domain.isEmpty ? pod.name : pod.domain }
    var tooltip: String { pod.name }
    /// The hover bubble only earns its place when the name isn't already on screen.
    var showsTooltip: Bool { tooltip != title || startHint != nil || updateHelp != nil }

    var hasUpdate: Bool { update != nil }
    var updateHelp: String? { update.map { UpdateCopy.podHelp(parts: $0.parts) } }

    /// A stopped pod with its parts in place restarts fast and shows a filled box;
    /// one that exists only in the registry needs its first full setup (outline box).
    var symbol: String {
        pod.state == .stopped && pod.total > 0 ? "shippingbox.fill" : "shippingbox"
    }

    var startHint: String? {
        guard pod.state == .stopped else { return nil }
        return pod.total > 0 ? "Starts in seconds" : "First start takes a little longer"
    }
    var isActive: Bool { pod.isActive }
    var showsSpinner: Bool { busy }

    /// Buttons shown in the row; empty while an operation is in flight. A pod with an update
    /// leads with the update button.
    var actions: [PodAction] {
        if busy { return [] }
        let base: [PodAction]
        switch pod.state {
        case .running, .partial: base = [.open, .restart, .stop, .manage]
        case .stopped: base = [.start, .manage]
        case .unknown: base = [.manage]
        }
        return hasUpdate && updatesAllowed ? [.update] + base : base
    }
}

/// Exact update wording shared by the menu rows, dialogs and header.
enum UpdateCopy {
    static func tainerRow(_ version: String) -> String { "Tainer \(version) is available — Update…" }
    static func menuRow(_ version: String) -> String { "Menu \(version) is available — Update…" }
    static func restartWarning(_ running: [String]) -> String {
        "Updating tainer restarts your running pods (\(running.joined(separator: ", ")))."
    }
    static func podWarning(_ name: String) -> String { "\(name) restarts while it updates." }
    static func podsWarning(_ names: [String]) -> String {
        names.count == 1 ? podWarning(names[0]) : "\(names.joined(separator: ", ")) restart while they update."
    }
    static func podHelp(parts: String) -> String {
        parts.isEmpty ? "Update available" : "Update available: \(parts)"
    }
    static func needsTainerFirst(_ minTainer: String) -> String {
        "Needs tainer \(minTainer) first — this updates tainer."
    }
    static let updateAllPods = "Update all pods"
}

/// What the status header says while an update is in flight.
enum UpdateActivity: Equatable {
    case updatingTainer
    case updatingMenu
    case restarting(Int)

    var text: String {
        switch self {
        case .updatingTainer: "Updating tainer…"
        case .updatingMenu: "Updating the menu…"
        case .restarting(let n): "Restarting \(n) \(n == 1 ? "pod" : "pods")…"
        }
    }
}

enum StatusPillTone: Equatable {
    case healthy, recovering, unreachable

    init(pill: String) {
        switch pill {
        case "HEALTHY": self = .healthy
        case "UNREACHABLE": self = .unreachable
        default: self = .recovering
        }
    }
}

enum CountSegments {
    struct Segment: Equatable {
        let text: String
        let emphasised: Bool
        init(_ text: String, _ emphasised: Bool) { self.text = text; self.emphasised = emphasised }
    }

    /// Splits digit runs out so the view can embolden them.
    static func parse(_ line: String) -> [Segment] {
        var out: [Segment] = []
        var current = ""
        var digits = false
        for ch in line {
            let isDigit = ch.isASCII && ch.isNumber
            if !current.isEmpty && isDigit != digits {
                out.append(Segment(current, digits))
                current = ""
            }
            digits = isDigit
            current.append(ch)
        }
        if !current.isEmpty { out.append(Segment(current, digits)) }
        return out
    }
}

enum MenuLinks {
    static let dashboard = URL(string: "https://my.tainer.me")!
    static let newPod = URL(string: "https://my.tainer.me/new")!
    static let cyber5 = URL(string: "https://cyber5.io/go/tainerapp")!
    static let tainer = URL(string: "https://tainer.dev/go/tainerapp")!

    static func manage(pod: String) -> URL {
        var c = URLComponents(string: "https://my.tainer.me")!
        c.path = "/pods/\(pod)"
        return c.url!
    }

    static func browse(domain: String) -> URL {
        URL(string: "https://\(domain)")!
    }
}

enum BarIconStyle: Equatable {
    case full, dimmed, pulsing, error

    init(_ health: BarHealth) {
        switch health {
        case .healthy: self = .full
        case .idle: self = .dimmed
        case .recovering: self = .pulsing
        case .unreachable: self = .error
        }
    }

    /// The update dot rides only on the healthy and idle icons; recovering and unreachable
    /// keep the icon to themselves (health wins).
    func showsUpdateBadge(hasUpdates: Bool) -> Bool {
        hasUpdates && (self == .full || self == .dimmed)
    }
}
