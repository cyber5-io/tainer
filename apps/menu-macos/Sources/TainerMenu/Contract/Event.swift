import Foundation

/// Engine health. Unknown strings are kept, never rejected.
enum HealthState: Equatable, Decodable {
    case healthy, recovering, failed
    case unknown(String)

    init(from decoder: any Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        switch raw {
        case "healthy": self = .healthy
        case "recovering": self = .recovering
        case "failed": self = .failed
        default: self = .unknown(raw)
        }
    }
}

enum PodState: Equatable, Decodable {
    case running, stopped, partial
    case unknown(String)

    init(from decoder: any Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        switch raw {
        case "running": self = .running
        case "stopped": self = .stopped
        case "partial": self = .partial
        default: self = .unknown(raw)
        }
    }
}

struct EngineState: Equatable, Decodable {
    var state: HealthState
    var since: Date
    var detail: String
}

struct Pod: Equatable, Identifiable, Decodable {
    var name: String
    var domain: String
    var state: PodState
    var running: Int
    var total: Int

    var id: String { name }
}

struct Progress: Equatable, Decodable {
    var op: String
    var pod: String
    var step: String
    var message: String
    var percent: Int
    var done: Bool
}

enum Event: Equatable, Decodable {
    case hello(engine: EngineState, pods: [Pod])
    case engine(EngineState)
    case pod(Pod)
    case progress(Progress)
    case unknown

    private enum Key: String, CodingKey { case type, data }
    private struct Hello: Decodable { var engine: EngineState; var pods: [Pod] }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        switch try c.decode(String.self, forKey: .type) {
        case "hello":
            let h = try c.decode(Hello.self, forKey: .data)
            self = .hello(engine: h.engine, pods: h.pods)
        case "engine": self = .engine(try c.decode(EngineState.self, forKey: .data))
        case "pod": self = .pod(try c.decode(Pod.self, forKey: .data))
        case "progress": self = .progress(try c.decode(Progress.self, forKey: .data))
        default: self = .unknown
        }
    }
}
