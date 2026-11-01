import Foundation

enum PodAction: Equatable {
    case open, restart, stop, start, manage, doctor
}

struct PodRowModel {
    let pod: Pod
    let busy: Bool

    var title: String { pod.domain.isEmpty ? pod.name : pod.domain }
    var tooltip: String { pod.name }
    /// The hover bubble only earns its place when the name isn't already on screen.
    var showsTooltip: Bool { tooltip != title || startHint != nil }

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

    /// Buttons shown in the row; empty while an operation is in flight.
    var actions: [PodAction] {
        if busy { return [] }
        switch pod.state {
        case .running, .partial: return [.open, .restart, .stop, .manage]
        case .stopped: return [.start, .manage]
        case .unknown: return [.manage]
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
}
