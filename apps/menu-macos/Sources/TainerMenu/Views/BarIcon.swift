import AppKit
import SwiftUI

/// Draws the bar mark for each state. The source PNG is black-on-alpha, so the mark is tinted at
/// draw time; template rendering keeps dimmed/pulsing states adaptive in light and dark bars.
@MainActor
enum BarIconRenderer {
    private static let mark: NSImage? = {
        guard let url = AppBundle.resources.url(forResource: "bar-mark@2x", withExtension: "png") else { return nil }
        return NSImage(contentsOf: url)
    }()

    static func image(style: BarIconStyle, pulseHigh: Bool) -> NSImage {
        let size = NSSize(width: 22.5, height: 18)
        let image = NSImage(size: size, flipped: false) { rect in
            guard let mark else { return false }
            let alpha: CGFloat
            switch style {
            case .full: alpha = 1
            case .dimmed: alpha = 0.45
            case .pulsing: alpha = pulseHigh ? 1 : 0.3
            case .error: alpha = 1
            }
            let color: NSColor = style == .full ? NSColor(MenuPalette.brandTeal) : (style == .error ? .labelColor : .black)
            let tinted = NSImage(size: rect.size, flipped: false) { r in
                mark.draw(in: r)
                color.set()
                r.fill(using: .sourceIn)
                return true
            }
            tinted.draw(in: rect, from: .zero, operation: .sourceOver, fraction: alpha)
            if style == .error {
                let badge = NSRect(x: rect.maxX - 9, y: 0, width: 9, height: 9)
                NSColor.systemRed.setFill()
                NSBezierPath(ovalIn: badge).fill()
                NSColor.white.setStroke()
                let x = NSBezierPath()
                x.lineWidth = 1.4
                x.lineCapStyle = .round
                x.move(to: NSPoint(x: badge.minX + 2.8, y: badge.minY + 2.8))
                x.line(to: NSPoint(x: badge.maxX - 2.8, y: badge.maxY - 2.8))
                x.move(to: NSPoint(x: badge.minX + 2.8, y: badge.maxY - 2.8))
                x.line(to: NSPoint(x: badge.maxX - 2.8, y: badge.minY + 2.8))
                x.stroke()
            }
            return true
        }
        // Teal and the red badge must keep their colour; the mono states adapt to the bar.
        image.isTemplate = style == .dimmed || style == .pulsing
        return image
    }
}

struct BarIcon: View {
    let health: BarHealth
    let pulseHigh: Bool

    var body: some View {
        Image(nsImage: BarIconRenderer.image(style: BarIconStyle(health), pulseHigh: pulseHigh))
            .accessibilityLabel("Tainer")
    }
}
