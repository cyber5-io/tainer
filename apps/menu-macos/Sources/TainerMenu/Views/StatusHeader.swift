import SwiftUI

struct BrandHeader: View {
    let palette: MenuPalette
    @Environment(\.colorScheme) private var scheme

    static var versionText: String {
        (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String) ?? "dev"
    }

    var body: some View {
        HStack(spacing: 10) {
            Group {
                if let logo = AppBundle.image(scheme == .dark ? "logo-tainer-dark" : "logo-tainer-light", height: 30) {
                    Image(nsImage: logo)
                } else {
                    Text("tainer").font(.system(size: 20, weight: .semibold, design: .rounded))
                }
            }
            .accessibilityLabel("tainer")
            Spacer()
            Text(Self.versionText)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(palette.chipText)
                .padding(.horizontal, 8).padding(.vertical, 2)
                .background(Capsule().fill(palette.chipFill))
        }
        .padding(.horizontal, 14).padding(.top, 8).padding(.bottom, 10)
    }
}

struct StatusHeader: View {
    let line: (pill: String, counts: String)
    let palette: MenuPalette
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let tone = palette.tone(StatusPillTone(pill: line.pill), dark: scheme == .dark)
        HStack(spacing: 9) {
            HStack(spacing: 5) {
                Circle().fill(tone.fg).frame(width: 6, height: 6)
                Text(line.pill)
                    .font(.system(size: 10, weight: .bold))
                    .tracking(0.6)
                    .foregroundStyle(tone.fg)
            }
            .padding(.horizontal, 9).padding(.vertical, 2.5)
            .background(Capsule().fill(tone.fill))
            .overlay(Capsule().stroke(tone.stroke, lineWidth: 1))
            counts
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 14).padding(.top, 2).padding(.bottom, 8)
    }

    private var counts: some View {
        CountSegments.parse(line.counts).reduce(Text("")) { acc, seg in
            acc + Text(seg.text)
                .fontWeight(seg.emphasised ? .bold : .regular)
                .foregroundColor(seg.emphasised ? palette.text : palette.countsText)
        }
        .font(.system(size: 12.5))
        .lineLimit(1)
    }
}
