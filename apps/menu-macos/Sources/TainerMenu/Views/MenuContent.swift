import SwiftUI

struct MenuContent: View {
    let state: StackState
    let onAction: (PodAction, String?) -> Void
    let onQuit: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL

    var body: some View {
        let palette = MenuPalette.forScheme(scheme)
        VStack(spacing: 0) {
            BrandHeader(palette: palette)
            StatusHeader(line: state.statusLine, palette: palette)
            divider(palette, vertical: 4)
            Text("PODS")
                .font(.system(size: 10, weight: .bold)).tracking(1.2)
                .foregroundStyle(palette.tertiaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14).padding(.top, 8).padding(.bottom, 4)
            podList(palette)
            divider(palette, vertical: 6)
            MenuItem(symbol: "square.grid.2x2", title: "Open Control Dashboard", color: palette.teal, weight: .semibold, palette: palette) {
                openURL(MenuLinks.dashboard)
            }
            MenuItem(symbol: "waveform.path.ecg", title: "Run Doctor — check & heal", color: palette.menuItemText, palette: palette) {
                onAction(.doctor, nil)
            }
            MenuItem(symbol: "power", title: "Quit Tainer Menu", color: palette.secondaryText, palette: palette, action: onQuit)
            divider(palette, vertical: 4)
            footer(palette)
        }
        .padding(.vertical, 6)
        .frame(width: 340)
        .background(palette.background)
        .foregroundStyle(palette.text)
    }

    @ViewBuilder private func podList(_ palette: MenuPalette) -> some View {
        if state.pods.isEmpty {
            Text("No pods yet")
                .font(.system(size: 12)).foregroundStyle(palette.tertiaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14).padding(.vertical, 8)
        } else {
            let rows = VStack(spacing: 0) {
                ForEach(state.pods) { pod in
                    PodRow(model: PodRowModel(pod: pod, busy: state.busy.contains(pod.name)), palette: palette, onAction: onAction)
                }
            }
            if PodListLayout.scrolls(rowCount: state.pods.count) {
                ScrollView { rows }.frame(maxHeight: PodListLayout.rowHeight * Double(PodListLayout.maxVisibleRows))
            } else {
                rows
            }
        }
    }

    private func footer(_ palette: MenuPalette) -> some View {
        HStack(spacing: 8) {
            Button { openURL(MenuLinks.cyber5) } label: { Cyber5Mark(palette: palette) }
                .buttonStyle(.plain)
            Button { openURL(MenuLinks.tainer) } label: {
                Text("tainer is a Cyber5 product").font(.system(size: 10.5)).foregroundStyle(palette.tertiaryText)
            }
            .buttonStyle(.plain)
            Spacer()
        }
        .padding(.horizontal, 14).padding(.top, 8).padding(.bottom, 6)
    }

    private func divider(_ palette: MenuPalette, vertical: CGFloat) -> some View {
        Rectangle().fill(palette.divider).frame(height: 1).padding(.horizontal, 10).padding(.vertical, vertical)
    }
}

struct MenuItem: View {
    let symbol: String
    let title: String
    let color: Color
    var weight: Font.Weight = .medium
    let palette: MenuPalette
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: symbol).font(.system(size: 12, weight: .medium)).frame(width: 16)
                Text(title).font(.system(size: 13, weight: weight))
                Spacer(minLength: 0)
            }
            .foregroundStyle(color)
            .padding(.horizontal, 8).padding(.vertical, 8)
            .background(RoundedRectangle(cornerRadius: 8).fill(hovering ? palette.hoverFill : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 6)
        .onHover { hovering = $0 }
    }
}

/// There is no dark cyber5 raster: light mode shows the full lockup; dark mode keeps the
/// coloured mark (cropped from the lockup) and sets the wordmark as light text.
struct Cyber5Mark: View {
    let palette: MenuPalette
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        let logo = Image("logo-cyber5-light", bundle: AppBundle.resources).resizable()
        Group {
            if scheme == .dark {
                HStack(spacing: 4) {
                    logo.scaledToFill().frame(width: 15, height: 14, alignment: .leading).clipped()
                    Text("cyber5").font(.system(size: 11, weight: .semibold, design: .monospaced)).foregroundStyle(palette.text)
                }
            } else {
                logo.scaledToFit().frame(height: 14)
            }
        }
        .accessibilityLabel("Cyber5")
    }
}
