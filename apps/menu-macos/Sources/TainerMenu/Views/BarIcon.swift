import AppKit
import SwiftUI

/// Draws the bar mark for each state. The source PNG is black-on-alpha, so the mark is tinted at
/// draw time; every state except healthy is a template image so it adapts to light, dark and
/// tinted menu bars. The unreachable badge is a separate coloured layer (see `BarIcon`).
@MainActor
enum BarIconRenderer {
    static let size = NSSize(width: 22.5, height: 18)
    static let badgeSize = NSSize(width: 9, height: 9)

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
                (style == .full ? NSColor(MenuPalette.brandTeal) : NSColor.black).set()
                r.fill(using: .sourceIn)
                return true
            }
            tinted.draw(in: rect, from: .zero, operation: .sourceOver, fraction: alpha)
            return true
        }
        image.isTemplate = style != .full
        return image
    }

    static func badgeImage() -> NSImage {
        let image = NSImage(size: badgeSize, flipped: false) { rect in
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
            return true
        }
        image.isTemplate = false
        return image
    }
}

struct BarIcon: View {
    let health: BarHealth
    let pulseHigh: Bool

    var body: some View {
        let style = BarIconStyle(health)
        ZStack(alignment: .bottomTrailing) {
            Image(nsImage: BarIconRenderer.markImage(style: style, pulseHigh: pulseHigh))
            if style == .error {
                Image(nsImage: BarIconRenderer.badgeImage())
            }
        }
        .accessibilityLabel("Tainer")
    }
}
