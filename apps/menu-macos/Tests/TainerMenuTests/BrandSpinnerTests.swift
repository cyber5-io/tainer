import AppKit
import SwiftUI
import Testing
@testable import TainerMenu

/// Renders a view at 4x into RGBA rows (top-down) of the given point size.
@MainActor
private func render(_ view: some View, width: CGFloat, height: CGFloat) -> (px: [UInt8], w: Int, h: Int)? {
    let renderer = ImageRenderer(content: view.frame(width: width, height: height))
    renderer.scale = 4
    guard let cg = renderer.cgImage else { return nil }
    let w = cg.width, h = cg.height
    var px = [UInt8](repeating: 0, count: w * h * 4)
    guard let ctx = CGContext(data: &px, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                              space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
    ctx.draw(cg, in: CGRect(x: 0, y: 0, width: w, height: h))
    return (px, w, h)
}

/// Mean alpha over a rectangle given in the mark's 45x36 grid.
private func meanAlpha(_ r: (px: [UInt8], w: Int, h: Int), grid rect: CGRect) -> Double {
    let sx = Double(r.w) / 45, sy = Double(r.h) / 36
    let x0 = Int((rect.minX + 1) * sx), x1 = Int((rect.maxX - 1) * sx)
    let y0 = Int((rect.minY + 1) * sy), y1 = Int((rect.maxY - 1) * sy)
    var sum = 0, n = 0
    for y in y0..<y1 { for x in x0..<x1 { sum += Int(r.px[(y * r.w + x) * 4 + 3]); n += 1 } }
    return n == 0 ? 0 : Double(sum) / Double(n) / 255
}

@Test func spinnerBarsPulseHalfACycleApart() {
    let start = BrandSpinner.barOpacities(phase: 0)
    #expect(start.top == 1)
    #expect(abs(start.bottom - 0.25) < 1e-9)
    let half = BrandSpinner.barOpacities(phase: 0.5)
    #expect(abs(half.top - 0.25) < 1e-9)
    #expect(half.bottom == 1)
    for p in stride(from: 0.0, to: 1.0, by: 0.05) {
        let o = BrandSpinner.barOpacities(phase: p)
        #expect((0.25...1).contains(o.top) && (0.25...1).contains(o.bottom))
        #expect(max(o.top, o.bottom) >= 0.6, "one bar is always bright")
    }
}

@Test func spinnerPhaseWrapsEachPeriod() {
    #expect(BrandSpinner.phase(at: 0) == 0)
    #expect(abs(BrandSpinner.phase(at: BrandSpinner.period * 2.25) - 0.25) < 1e-9)
    #expect((0..<1).contains(BrandSpinner.phase(at: -0.3)))
}

@Test @MainActor func spinnerFrameDrawsTheMarkWithTheBrightBarFollowingThePhase() throws {
    let a = try #require(render(BrandSpinnerFrame(phase: 0, tint: .black), width: 16, height: 12.8))
    for bracket in BrandMarkGeometry.brackets where bracket.width > 3 && bracket.height > 3 {
        #expect(meanAlpha(a, grid: bracket) > 0.4, "bracket \(bracket) drawn")
    }
    #expect(meanAlpha(a, grid: CGRect(x: 15, y: 16, width: 15, height: 4)) < 0.05, "gap between the bars stays clear")
    let top = BrandMarkGeometry.bars[0], bottom = BrandMarkGeometry.bars[1]
    #expect(meanAlpha(a, grid: top) > 0.9)
    #expect(meanAlpha(a, grid: bottom) < 0.35)
    let b = try #require(render(BrandSpinnerFrame(phase: 0.5, tint: .black), width: 16, height: 12.8))
    #expect(meanAlpha(b, grid: top) < 0.35)
    #expect(meanAlpha(b, grid: bottom) > 0.9)
}

@Test @MainActor func spinnerViewBuildsAtMenuSize() throws {
    let r = try #require(render(BrandSpinner(tint: .black), width: 16, height: 12.8))
    #expect(r.w == 64)
    let maxAlpha = stride(from: 3, to: r.px.count, by: 4).map { r.px[$0] }.max() ?? 0
    #expect(maxAlpha > 120, "max alpha \(maxAlpha)")
}

@Test func doctorRowSpinsOnlyWhileRunning() {
    #expect(DoctorRowModel(run: .running).showsSpinner)
    #expect(!DoctorRowModel(run: .idle).showsSpinner)
    #expect(!DoctorRowModel(run: .finished(.healthy)).showsSpinner)
}

/// The spinner pauses while the menu's panel is hidden: the reader follows the window in and out.
@Test @MainActor func visibilityReaderFollowsItsWindowInAndOut() async {
    var seen: [Bool] = []
    let tracker = WindowVisibilityReader.Tracker()
    tracker.onChange = { seen.append($0) }
    let panel = NSPanel(contentRect: NSRect(x: -10_000, y: -10_000, width: 20, height: 16),
                        styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
    panel.contentView?.addSubview(tracker)
    await settle { seen.last == false }
    panel.orderFrontRegardless()
    await settle { seen.last == true }
    panel.orderOut(nil)
    await settle { seen.last == false }
    panel.orderFrontRegardless()
    await settle { seen.last == true }
    panel.orderOut(nil)
    tracker.removeFromSuperview()
    await settle { seen.last == false }
}
