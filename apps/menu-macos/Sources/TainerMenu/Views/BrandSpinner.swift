import AppKit
import SwiftUI

/// The bar mark's shapes in its 45x36 artwork grid: two square brackets and the equals bars.
enum BrandMarkGeometry {
    static let grid = CGSize(width: 45, height: 36)

    static let brackets: [CGRect] = [
        CGRect(x: 1.5, y: 1, width: 4, height: 34), CGRect(x: 1.5, y: 1, width: 13.5, height: 3),
        CGRect(x: 1.5, y: 32, width: 13.5, height: 3),
        CGRect(x: 39.5, y: 1, width: 4, height: 34), CGRect(x: 30, y: 1, width: 13.5, height: 3),
        CGRect(x: 30, y: 32, width: 13.5, height: 3),
    ]

    static let bars: [CGRect] = [
        CGRect(x: 12, y: 11.5, width: 21, height: 4), CGRect(x: 12, y: 20.5, width: 21, height: 4),
    ]
}

/// tainer's own busy indicator: the bar mark with its equals bars pulsing out of step, the in-menu
/// cousin of the recovering bar icon.
struct BrandSpinner: View {
    var tint: Color = .secondary
    var width: CGFloat = 16
    nonisolated static let period: TimeInterval = 1.1
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var onScreen = true

    var body: some View {
        // A hidden window keeps its timelines running, and the menu's panel is hidden rather
        // than torn down when it closes, so the pulse pauses while its window is hidden.
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: reduceMotion || !onScreen)) { timeline in
            let phase = reduceMotion ? 0.25 : Self.phase(at: timeline.date.timeIntervalSinceReferenceDate)
            BrandSpinnerFrame(phase: phase, tint: tint)
        }
        .frame(width: width, height: width * BrandMarkGeometry.grid.height / BrandMarkGeometry.grid.width)
        .background(WindowVisibilityReader(visible: $onScreen))
        .accessibilityHidden(true)
    }

    nonisolated static func phase(at time: TimeInterval) -> Double {
        let p = time.truncatingRemainder(dividingBy: period) / period
        return p < 0 ? p + 1 : p
    }

    /// Opacity of the top and bottom bars at a phase in [0, 1): each swings between 0.25 and 1,
    /// half a cycle apart, so one bar is always bright.
    nonisolated static func barOpacities(phase: Double) -> (top: Double, bottom: Double) {
        func swing(_ p: Double) -> Double { 0.625 + 0.375 * cos(2 * .pi * p) }
        return (swing(phase), swing(phase + 0.5))
    }
}

/// One still frame of the spinner; separate so tests can render a chosen phase.
struct BrandSpinnerFrame: View {
    let phase: Double
    let tint: Color

    var body: some View {
        Canvas { context, size in
            let sx = size.width / BrandMarkGeometry.grid.width, sy = size.height / BrandMarkGeometry.grid.height
            func scaled(_ r: CGRect) -> CGRect { CGRect(x: r.minX * sx, y: r.minY * sy, width: r.width * sx, height: r.height * sy) }
            var brackets = Path()
            for r in BrandMarkGeometry.brackets { brackets.addRect(scaled(r)) }
            context.fill(brackets, with: .color(tint.opacity(0.55)))
            let o = BrandSpinner.barOpacities(phase: phase)
            for (rect, opacity) in zip(BrandMarkGeometry.bars, [o.top, o.bottom]) {
                let r = scaled(rect)
                context.fill(Path(roundedRect: r, cornerRadius: r.height / 2), with: .color(tint.opacity(opacity)))
            }
        }
    }
}

/// Reports whether the window hosting it is shown, following the window's `isVisible` (which
/// changes when the window is ordered in or out). Occlusion is not used: it can read as hidden for
/// a window that is on screen, which would freeze a spinner the user is looking at.
struct WindowVisibilityReader: NSViewRepresentable {
    @Binding var visible: Bool

    func makeNSView(context: Context) -> Tracker {
        let tracker = Tracker()
        tracker.onChange = { visible = $0 }
        return tracker
    }

    func updateNSView(_ tracker: Tracker, context: Context) {
        tracker.onChange = { visible = $0 }
    }

    final class Tracker: NSView {
        var onChange: ((Bool) -> Void)?
        private var observation: NSKeyValueObservation?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            observation = window?.observe(\.isVisible, options: [.initial, .new]) { [weak self] _, change in
                let shown = change.newValue ?? false
                // Deferred so the binding never changes in the middle of a view update.
                DispatchQueue.main.async { self?.onChange?(shown) }
            }
            if window == nil {
                DispatchQueue.main.async { [weak self] in self?.onChange?(false) }
            }
        }
    }
}
