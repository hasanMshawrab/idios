import Foundation

/// Timestamp is a stored timestamp string and the instant it names, when the
/// string is one of the two shapes the daemon stores.
public struct Timestamp: Hashable, Sendable {
    public let raw: String
    public let date: Date?

    /// init keeps the string as it arrived and parses what it can.
    public init(_ raw: String) {
        self.raw = raw
        self.date = Timestamp.parse(raw)
    }
}

extension Timestamp {
    private static let utc: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
        return calendar
    }()

    // Process timestamps carry six fractional digits, Kubernetes timestamps are
    // stored verbatim and carry none. ISO8601DateFormatter reads only three
    // fractional digits, so the two shapes are split by hand.
    private static func parse(_ raw: String) -> Date? {
        guard raw.hasSuffix("Z") else { return nil }
        let body = raw.dropLast()
        let parts = body.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 1 || parts.count == 2 else { return nil }

        var nanosecond = 0
        if parts.count == 2 {
            guard parts[1].count == 6, let microsecond = digits(parts[1]) else { return nil }
            nanosecond = microsecond * 1000
        }

        let head = Array(parts[0])
        guard head.count == 19,
            head[4] == "-", head[7] == "-", head[10] == "T", head[13] == ":", head[16] == ":",
            let year = digits(head[0..<4]),
            let month = digits(head[5..<7]),
            let day = digits(head[8..<10]),
            let hour = digits(head[11..<13]),
            let minute = digits(head[14..<16]),
            let second = digits(head[17..<19])
        else { return nil }

        return utc.date(
            from: DateComponents(
                year: year, month: month, day: day,
                hour: hour, minute: minute, second: second, nanosecond: nanosecond))
    }

    private static func digits(_ characters: some Collection<Character>) -> Int? {
        guard characters.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        return Int(String(characters))
    }
}
