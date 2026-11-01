import SwiftUI

struct MenuPalette {
    let background: Color
    let text: Color
    let secondaryText: Color
    let tertiaryText: Color
    let countsText: Color
    let icon: Color
    let iconHover: Color
    let hoverFill: Color
    let divider: Color
    let chipFill: Color
    let chipText: Color
    let menuItemText: Color
    let teal: Color
    let link: Color
    let stoppedIcon: Color
    let red: Color = Color(red: 1.0, green: 0.27, blue: 0.23)

    static let brandTeal = Color(red: 0, green: 0.831, blue: 0.667)

    static let dark = MenuPalette(
        background: Color(hex: 0x1D222B), text: Color(hex: 0xF2F5F9), secondaryText: Color(hex: 0x8A94A6),
        tertiaryText: Color(hex: 0x6B7686), countsText: Color(hex: 0xB9C2CF), icon: Color(hex: 0x8A94A6),
        iconHover: Color(hex: 0xF2F5F9), hoverFill: Color.white.opacity(0.06), divider: Color(hex: 0x2A3140),
        chipFill: Color.white.opacity(0.07), chipText: Color(hex: 0x8A94A6), menuItemText: Color(hex: 0xD7DEE8),
        teal: Color(hex: 0x00D4AA), link: Color(hex: 0x4E9EF4), stoppedIcon: Color(hex: 0x55606E))

    static let light = MenuPalette(
        background: Color(hex: 0xF4F5F7), text: Color(hex: 0x1B2430), secondaryText: Color(hex: 0x707D90),
        tertiaryText: Color(hex: 0x8A94A6), countsText: Color(hex: 0x445061), icon: Color(hex: 0x5E6B7E),
        iconHover: Color(hex: 0x1B2430), hoverFill: Color.black.opacity(0.05), divider: Color(hex: 0xE2E5EA),
        chipFill: Color.black.opacity(0.06), chipText: Color(hex: 0x5E6B7E), menuItemText: Color(hex: 0x2A3442),
        teal: Color(hex: 0x089E80), link: Color(hex: 0x2563EB), stoppedIcon: Color(hex: 0x8A94A6))

    static func forScheme(_ scheme: ColorScheme) -> MenuPalette { scheme == .dark ? .dark : .light }

    func tone(_ tone: StatusPillTone, dark: Bool) -> (fg: Color, fill: Color, stroke: Color) {
        switch tone {
        case .healthy:
            return dark
                ? (teal, Color(hex: 0x00D4AA).opacity(0.14), Color(hex: 0x00D4AA).opacity(0.3))
                : (Color(hex: 0x067A64), Color(hex: 0x00B894).opacity(0.13), Color(hex: 0x00B894).opacity(0.35))
        case .recovering:
            let o = Color(hex: dark ? 0xFF6B35 : 0xC2410C)
            return (o, Color(hex: 0xFF6B35).opacity(0.14), Color(hex: 0xFF6B35).opacity(0.35))
        case .unreachable:
            let r = Color(hex: dark ? 0xFF6A5F : 0xC0302A)
            return (r, Color(hex: 0xFF453A).opacity(0.14), Color(hex: 0xFF453A).opacity(0.35))
        }
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }
}

extension PodAction {
    var symbol: String {
        switch self {
        case .open: "arrow.up.right"
        case .restart: "arrow.clockwise"
        case .stop: "stop.fill"
        case .start: "play.fill"
        case .manage: "slider.horizontal.3"
        case .doctor: "waveform.path.ecg"
        }
    }

    func label(pod: String) -> String {
        switch self {
        case .open: "Open \(pod) in browser"
        case .restart: "Restart \(pod)"
        case .stop: "Stop \(pod)"
        case .start: "Start \(pod)"
        case .manage: "Manage \(pod)"
        case .doctor: "Run Doctor"
        }
    }
}
