import Testing
import Foundation
@testable import TainerMenu

private func pod(_ name: String, _ state: PodState) -> Pod {
    Pod(name: name, domain: "\(name).tainer.me", state: state, running: 1, total: 1)
}

@Test func runningRowActions() {
    #expect(PodRowModel(pod: pod("ops", .running), busy: false).actions == [.open, .restart, .stop, .manage])
    #expect(PodRowModel(pod: pod("ops", .partial), busy: false).actions == [.open, .restart, .stop, .manage])
}

@Test func stoppedRowActions() {
    #expect(PodRowModel(pod: pod("ops", .stopped), busy: false).actions == [.start, .manage])
    #expect(PodRowModel(pod: pod("ops", .unknown("x")), busy: false).actions == [.manage])
}

@Test func busyRowShowsSpinnerAndDisablesActions() {
    let busy = PodRowModel(pod: pod("ops", .running), busy: true)
    #expect(busy.showsSpinner)
    #expect(busy.actions.isEmpty)
    #expect(!PodRowModel(pod: pod("ops", .running), busy: false).showsSpinner)
}

@Test func rowTintFollowsActivity() {
    #expect(PodRowModel(pod: pod("a", .running), busy: false).isActive)
    #expect(PodRowModel(pod: pod("a", .partial), busy: false).isActive)
    #expect(!PodRowModel(pod: pod("a", .stopped), busy: false).isActive)
}

@Test func statusPillTextAndToneForEachHealthState() {
    #expect(StatusPillTone(pill: "HEALTHY") == .healthy)
    #expect(StatusPillTone(pill: "RECOVERING") == .recovering)
    #expect(StatusPillTone(pill: "UNREACHABLE") == .unreachable)
}

@Test @MainActor func statusPillFromStackState() {
    let state = StackState()
    #expect(StatusPillTone(pill: state.statusLine.pill) == .recovering)
    state.apply(.unreachable)
    #expect(StatusPillTone(pill: state.statusLine.pill) == .unreachable)
}

@Test func countSegmentsEmphasiseNumbers() {
    let segs = CountSegments.parse("2 of 3 pods running")
    #expect(segs == [.init("2", true), .init(" of ", false), .init("3", true), .init(" pods running", false)])
    #expect(CountSegments.parse("No pods yet") == [.init("No pods yet", false)])
}

@Test func manageURLForPod() {
    #expect(MenuLinks.manage(pod: "ops").absoluteString == "https://my.tainer.me/pods/ops")
    #expect(MenuLinks.manage(pod: "my pod").absoluteString == "https://my.tainer.me/pods/my%20pod")
    #expect(MenuLinks.dashboard.absoluteString == "https://my.tainer.me")
    #expect(MenuLinks.cyber5.absoluteString == "https://cyber5.io/go/tainerapp")
    #expect(MenuLinks.tainer.absoluteString == "https://tainer.dev/go/tainerapp")
    #expect(MenuLinks.browse(domain: "ops.tainer.me").absoluteString == "https://ops.tainer.me")
}

@Test func barIconStyleForEachHealthState() {
    #expect(BarIconStyle(.healthy(runningPods: 2)) == .full)
    #expect(BarIconStyle(.idle) == .dimmed)
    #expect(BarIconStyle(.recovering) == .pulsing)
    #expect(BarIconStyle(.unreachable) == .error)
}

@Test func podListScrollsBeyondTwelveRows() {
    #expect(!PodListLayout.scrolls(rowCount: 12))
    #expect(PodListLayout.scrolls(rowCount: 13))
}
