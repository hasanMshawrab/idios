import Foundation
import Testing

@testable import IdiosModel

private let utc: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
    return calendar
}()

private func instant(
    _ year: Int, _ month: Int, _ day: Int, _ hour: Int, _ minute: Int, _ second: Int,
    nanosecond: Int = 0
) -> Date? {
    utc.date(
        from: DateComponents(
            year: year, month: month, day: day, hour: hour, minute: minute, second: second,
            nanosecond: nanosecond))
}

@Test(
    arguments: [
        ("2026-08-27T14:03:11.482913Z", instant(2026, 8, 27, 14, 3, 11, nanosecond: 482_913_000)),
        ("2026-08-27T14:38:00Z", instant(2026, 8, 27, 14, 38, 0)),
        ("2026-08-27T14:38:00.482Z", nil),
        ("2026-08-27T14:03:11.482913", nil),
        ("", nil),
        ("not a timestamp", nil),
    ])
func onlyTheTwoStoredTimestampShapesParse(raw: String, want: Date?) {
    #expect(Timestamp(raw).date == want)
}
