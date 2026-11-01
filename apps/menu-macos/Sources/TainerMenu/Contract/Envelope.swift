import Foundation

/// RFC 3339 UTC timestamps with exactly three fractional digits (docs/api/cli-json-v1.md §1).
enum ContractDate {
    private static func formatter(fractional: Bool) -> ISO8601DateFormatter {
        let f = ISO8601DateFormatter()
        f.formatOptions = fractional ? [.withInternetDateTime, .withFractionalSeconds] : [.withInternetDateTime]
        return f
    }

    static func parse(_ s: String) -> Date? {
        formatter(fractional: true).date(from: s) ?? formatter(fractional: false).date(from: s)
    }

    static func decode(from decoder: any Decoder) throws -> Date {
        let container = try decoder.singleValueContainer()
        let raw = try container.decode(String.self)
        guard let date = parse(raw) else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "bad timestamp \(raw)")
        }
        return date
    }
}

/// One NDJSON line from `tainer watch --json`.
enum StreamLine: Equatable {
    case event(Event)
    case errorEnvelope(code: String, message: String)
    case invalid

    private struct ErrorEnvelope: Decodable {
        struct Body: Decodable { var code: String; var message: String }
        var error: Body?
    }

    static func parse(_ line: String) -> StreamLine {
        guard let data = line.data(using: .utf8) else { return .invalid }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { try ContractDate.decode(from: $0) }
        guard let probe = try? decoder.decode(ErrorEnvelope.self, from: data) else { return .invalid }
        if let body = probe.error {
            return .errorEnvelope(code: body.code, message: body.message)
        }
        guard let event = try? decoder.decode(Event.self, from: data) else { return .invalid }
        return .event(event)
    }
}
