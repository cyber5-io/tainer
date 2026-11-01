import AppKit
import SwiftUI

/// Draws the bar mark for each state. The source PNG is black-on-alpha, so the mark is tinted at
/// draw time; the dimmed and pulsing states are template images so they adapt to light, dark and
/// tinted menu bars; healthy and unreachable are coloured, single non-template images.
@MainActor
enum BarIconRenderer {
    static let size = NSSize(width: 22.5, height: 18)

    private static let mark: NSImage? = {
        guard let url = AppBundle.resources.url(forResource: "bar-mark@2x", withExtension: "png") else { return nil }
        return NSImage(contentsOf: url)
    }()

    static func markImage(style: BarIconStyle, pulseHigh: Bool) -> NSImage {
        let image = NSImage(size: size, flipped: false) { rect in
            guard let mark else { return false }
            let alpha: CGFloat
            switch style {
            case .full, .error: alpha = 1
            case .dimmed: alpha = 0.45
            case .pulsing: alpha = pulseHigh ? 1 : 0.3
            }
            let tinted = NSImage(size: rect.size, flipped: false) { r in
                mark.draw(in: r)
                tint(style).set()
                r.fill(using: .sourceIn)
                return true
            }
            tinted.draw(in: rect, from: .zero, operation: .sourceOver, fraction: alpha)
            if style == .error { drawBadge(in: NSRect(x: rect.maxX - 9, y: 0, width: 9, height: 9)) }
            return true
        }
        image.isTemplate = style == .dimmed || style == .pulsing
        return image
    }

    /// Healthy is teal; unreachable is a neutral grey legible on light and dark bars (the composite
    /// is not a template, so the red badge keeps its colour); the rest are black templates.
    private static func tint(_ style: BarIconStyle) -> NSColor {
        switch style {
        case .full: NSColor(MenuPalette.brandTeal)
        case .error: NSColor(white: 0.55, alpha: 1)
        case .dimmed, .pulsing: .black
        }
    }

    private static func drawBadge(in rect: NSRect) {
        NSColor.systemRed.setFill()
        NSBezierPath(ovalIn: rect).fill()
        NSColor.white.setStroke()
        let x = NSBezierPath()
        x.lineWidth = 1.4
        x.lineCapStyle = .round
        x.move(to: NSPoint(x: rect.minX + 2.8, y: rect.minY + 2.8))
        x.line(to: NSPoint(x: rect.maxX - 2.8, y: rect.maxY - 2.8))
        x.move(to: NSPoint(x: rect.minX + 2.8, y: rect.maxY - 2.8))
        x.line(to: NSPoint(x: rect.maxX - 2.8, y: rect.minY + 2.8))
        x.stroke()
    }
}

struct BarIcon: View {
    let health: BarHealth
    let pulseHigh: Bool

    var body: some View {
        Image(nsImage: BarIconRenderer.markImage(style: BarIconStyle(health), pulseHigh: pulseHigh))
            .accessibilityLabel("Tainer")
    }
}
