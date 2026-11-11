import SwiftUI

struct MenuContent: View {
    let state: StackState
    let updates: UpdateChecker
    let onAction: (PodAction, String?) -> Void
    let onQuit: () -> Void
    var onOpen: () async -> Void = {}
    var onUpdateTainer: () -> Void = {}
    var onUpdateMenu: () -> Void = {}
    var onUpdateAllPods: () -> Void = {}
    var onOpenProject: () -> Void = {}
    @Environment(\.colorScheme) private var scheme
    @Environment(\.openURL) private var openURL

    var body: some View {
        let palette = MenuPalette.forScheme(scheme)
        VStack(spacing: 0) {
            BrandHeader(palette: palette)
            StatusHeader(line: statusLine, palette: palette, busy: updates.activity != nil)
            updateRows(palette)
            divider(palette, vertical: 4)
            Text("PODS")
                .font(.system(size: 10, weight: .bold)).tracking(1.2)
                .foregroundStyle(palette.tertiaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14).padding(.top, 8).padding(.bottom, 4)
            podList(palette)
            divider(palette, vertical: 6)
            let dashboardHelp = DashboardGate.help(state.engine)
            MenuItem(symbol: "plus.circle", title: "New Pod", color: palette.menuItemText, palette: palette) {
                openURL(MenuLinks.newPod)
            }
            .help(dashboardHelp ?? "")
            .disabled(dashboardHelp != nil)
            MenuItem(symbol: "folder", title: OpenProjectCopy.menuItem, color: palette.menuItemText, palette: palette,
                     action: onOpenProject)
            MenuItem(symbol: "square.grid.2x2", title: "Open Control Dashboard", color: palette.teal, weight: .semibold, palette: palette) {
                openURL(MenuLinks.dashboard)
            }
            .help(dashboardHelp ?? "")
            .disabled(dashboardHelp != nil)
            let doctor = DoctorRowModel(run: state.doctor)
            MenuItem(symbol: doctor.symbol, title: doctor.title, color: palette.menuItemText, busy: doctor.showsSpinner,
                     palette: palette) {
                onAction(.doctor, nil)
            }
            .disabled(!doctor.enabled)
            MenuItem(symbol: "power", title: "Quit Tainer Menu", color: palette.secondaryText, palette: palette, action: onQuit)
            divider(palette, vertical: 4)
            footer(palette)
        }
        .padding(.vertical, 6)
        .frame(width: 340)
        .background(palette.background)
        .foregroundStyle(palette.text)
        .focusEffectDisabled()
        .task {
            state.resetPage()
            while !Task.isCancelled {
                await onOpen()
                try? await Task.sleep(for: .seconds(60))
            }
        }
    }

    @ViewBuilder private func podList(_ palette: MenuPalette) -> some View {
        if state.allPods.isEmpty {
            Text("No pods yet")
                .font(.system(size: 12)).foregroundStyle(palette.tertiaryText)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14).padding(.vertical, 8)
        } else {
            VStack(spacing: 0) {
                ForEach(state.visiblePods) { pod in
                    PodRow(model: PodRowModel(pod: pod, busy: state.busy.contains(pod.name), update: updates.pods[pod.name],
                                                    updatesAllowed: updates.podUpdatesAllowed), palette: palette, onAction: onAction)
                }
                if state.showsPager { pager(palette) }
                if updates.pods.count >= 2 {
                    MenuItem(symbol: "arrow.down.circle", title: UpdateCopy.updateAllPods, color: palette.link, palette: palette,
                             action: onUpdateAllPods)
                        .disabled(!updates.podUpdatesAllowed)
                        .padding(.top, 2)
                }
            }
        }
    }

    /// The header's counts give way to the update in flight.
    private var statusLine: (pill: String, counts: String) {
        let line = state.statusLine
        return (line.pill, updates.activity?.text ?? line.counts)
    }

    /// "Tainer <v> is available — Update…" / "Menu <v> …", only when there is one; notes as help.
    @ViewBuilder private func updateRows(_ palette: MenuPalette) -> some View {
        let busy = updates.activity != nil
        if let release = updates.tainerUpdate, let title = updates.tainerRow {
            MenuItem(symbol: "arrow.down.circle", title: title, color: palette.link, weight: .semibold, palette: palette,
                     action: onUpdateTainer)
                .help(release.notes)
                .disabled(busy)
        }
        if let release = updates.menuUpdate, let title = updates.menuRow {
            let help = release.needsTainerFirst
                ? [release.notes, UpdateCopy.needsTainerFirst(release.minTainer)].filter { !$0.isEmpty }.joined(separator: "\n")
                : release.notes
            MenuItem(symbol: "arrow.down.circle", title: title, color: palette.link, weight: .semibold, palette: palette,
                     action: onUpdateMenu)
                .help(help)
                .disabled(busy)
        }
    }

    private func pager(_ palette: MenuPalette) -> some View {
        VStack(spacing: 0) {
            Rectangle().fill(palette.divider).frame(height: 1).padding(.horizontal, 10).padding(.top, 4)
            HStack(spacing: 0) {
                PagerButton(symbol: "chevron.left", label: "Previous page", palette: palette) { state.previousPage() }
                    .disabled(!state.canGoPrevious)
                    .keyboardShortcut(.leftArrow, modifiers: [])
                Spacer(minLength: 8)
                Text(state.pageLabel)
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundStyle(palette.secondaryText)
                Spacer(minLength: 8)
                PagerButton(symbol: "chevron.right", label: "Next page", palette: palette) { state.nextPage() }
                    .disabled(!state.canGoNext)
                    .keyboardShortcut(.rightArrow, modifiers: [])
            }
            .padding(.horizontal, 10).padding(.top, 3)
        }
    }

    private func footer(_ palette: MenuPalette) -> some View {
        HStack(spacing: 8) {
            Button { openURL(MenuLinks.cyber5) } label: { Cyber5Mark(palette: palette) }
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
    var busy = false
    let palette: MenuPalette
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                if busy {
                    BrandSpinner(tint: color).frame(width: 16)
                } else {
                    Image(systemName: symbol).font(.system(size: 12, weight: .medium)).frame(width: 16)
                }
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

struct Cyber5Mark: View {
    let palette: MenuPalette
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        Group {
            if let logo = AppBundle.image(scheme == .dark ? "logo-cyber5-dark" : "logo-cyber5-light", height: 14) {
                Image(nsImage: logo)
            } else {
                Text("cyber5").font(.system(size: 11, weight: .semibold, design: .monospaced)).foregroundStyle(palette.text)
            }
        }
        .accessibilityLabel("Cyber5")
    }
}
