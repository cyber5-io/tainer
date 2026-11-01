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

@Test func shouldPulseOnlyWhileRecovering() {
    #expect(PulseDriver.shouldPulse(.recovering))
    #expect(!PulseDriver.shouldPulse(.healthy(runningPods: 1)))
    #expect(!PulseDriver.shouldPulse(.idle))
    #expect(!PulseDriver.shouldPulse(.unreachable))
}

@Test @MainActor func pulseTaskExistsOnlyWhileRecovering() {
    let pulse = PulseDriver()
    #expect(!pulse.isActive)
    pulse.sync(.recovering)
    #expect(pulse.isActive)
    pulse.sync(.healthy(runningPods: 2))
    #expect(!pulse.isActive)
    pulse.sync(.recovering)
    pulse.sync(.unreachable)
    #expect(!pulse.isActive)
    #expect(pulse.high)
}

import AppKit

@MainActor
private func hasRedPixel(_ image: NSImage) -> Bool {
    let w = 90, h = 72
    guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: w, pixelsHigh: h, bitsPerSample: 8, samplesPerPixel: 4,
                                     hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
          let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return false }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = ctx
    image.draw(in: NSRect(x: 0, y: 0, width: w, height: h))
    NSGraphicsContext.restoreGraphicsState()
    for x in 0..<w { for y in 0..<h {
        guard let c = rep.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB), c.alphaComponent > 0.9 else { continue }
        if c.redComponent > 1.5 * c.greenComponent && c.redComponent > 1.5 * c.blueComponent { return true }
    } }
    return false
}

@Test @MainActor func unreachableIconIsOneImageWithRedBadge() {
    let error = BarIconRenderer.markImage(style: .error, pulseHigh: true)
    #expect(hasRedPixel(error))
    #expect(!error.isTemplate)
    #expect(!hasRedPixel(BarIconRenderer.markImage(style: .full, pulseHigh: true)))
    #expect(!hasRedPixel(BarIconRenderer.markImage(style: .dimmed, pulseHigh: true)))
}

@Test func brandImagesLoadAtPointSize() throws {
    let expected: [(String, CGFloat, CGFloat)] = [
        ("logo-tainer-light", 132, 30), ("logo-tainer-dark", 132, 30),
        ("logo-cyber5-light", 120, 21.5), ("bar-mark", 22.5, 18),
    ]
    for (name, w, h) in expected {
        let image = try #require(AppBundle.image(name), "\(name) did not load")
        #expect(image.size == NSSize(width: w, height: h), "\(name) size \(image.size)")
    }
    let scaled = try #require(AppBundle.image("logo-tainer-light", height: 30))
    #expect(scaled.size.height == 30)
}
