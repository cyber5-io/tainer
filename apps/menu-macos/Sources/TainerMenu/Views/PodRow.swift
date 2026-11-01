import SwiftUI

struct IconButton: View {
    let symbol: String
    let label: String
    var tint: Color?
    let palette: MenuPalette
    let action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(hovering ? (tint ?? palette.iconHover) : (tint ?? palette.icon))
                .frame(width: 24, height: 24)
                .background(RoundedRectangle(cornerRadius: 6).fill(hovering ? palette.hoverFill.opacity(2) : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(label)
        .accessibilityLabel(label)
        .onHover { hovering = $0 }
    }
}

/// Pager chevron: a bigger, bordered sibling of IconButton so the control reads as one.
struct PagerButton: View {
    let symbol: String
    let label: String
    let palette: MenuPalette
    let action: () -> Void
    @Environment(\.isEnabled) private var enabled
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 12, weight: .bold))
                .foregroundStyle(enabled ? (hovering ? palette.iconHover : palette.icon) : palette.stoppedIcon.opacity(0.5))
                .frame(width: 44, height: 24)
                .background(RoundedRectangle(cornerRadius: 6).fill(hovering && enabled ? palette.hoverFill.opacity(2) : palette.chipFill))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(label)
        .accessibilityLabel(label)
        .onHover { hovering = $0 && enabled }
    }
}

struct PodRow: View {
    let model: PodRowModel
    let palette: MenuPalette
    let onAction: (PodAction, String?) -> Void
    @Environment(\.openURL) private var openURL
    @State private var hovering = false
    @State private var tipVisible = false
    @State private var tipTask: Task<Void, Never>?

    var body: some View {
        let pod = model.pod
        HStack(spacing: 10) {
            Image(systemName: "shippingbox")
                .font(.system(size: 12, weight: .regular))
                .foregroundStyle(model.isActive ? palette.teal : palette.stoppedIcon)
                .frame(width: 14)
            Text(model.title)
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(model.isActive ? palette.text : palette.countsText)
                .lineLimit(1)
                .truncationMode(.middle)
            Spacer(minLength: 4)
            if model.showsSpinner {
                ProgressView().controlSize(.small).scaleEffect(0.7).frame(width: 24, height: 24)
                    .accessibilityLabel("Working on \(pod.name)")
            } else {
                ForEach(model.actions, id: \.self) { action in
                    IconButton(symbol: action.symbol, label: action.label(pod: pod.name),
                               tint: tint(action), palette: palette) { perform(action) }
                }
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(RoundedRectangle(cornerRadius: 8).fill(hovering ? palette.hoverFill : .clear))
        .opacity(model.isActive ? 1 : 0.75)
        .padding(.horizontal, 6)
        .help(model.tooltip)
        // .help() tooltips rarely surface inside menu-bar panels, so draw our own bubble.
        .overlay(alignment: .topLeading) {
            if tipVisible {
                Text(model.tooltip)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(palette.text)
                    .padding(.horizontal, 8).padding(.vertical, 4)
                    .background(
                        RoundedRectangle(cornerRadius: 6)
                            .fill(palette.background)
                            .shadow(color: .black.opacity(0.35), radius: 6, y: 2)
                    )
                    .overlay(RoundedRectangle(cornerRadius: 6).stroke(palette.divider))
                    .fixedSize()
                    .offset(x: 30, y: -20)
                    .allowsHitTesting(false)
                    .transition(.opacity)
            }
        }
        .zIndex(tipVisible ? 1 : 0)
        .onHover { over in
            hovering = over
            tipTask?.cancel()
            if over && model.showsTooltip {
                tipTask = Task {
                    try? await Task.sleep(for: .milliseconds(600))
                    if !Task.isCancelled { withAnimation(.easeIn(duration: 0.12)) { tipVisible = true } }
                }
            } else {
                tipVisible = false
            }
        }
    }

    private func tint(_ action: PodAction) -> Color? {
        switch action {
        case .manage: palette.teal
        case .start: palette.teal
        default: nil
        }
    }

    private func perform(_ action: PodAction) {
        switch action {
        case .open: openURL(MenuLinks.browse(domain: model.pod.domain))
        case .manage: openURL(MenuLinks.manage(pod: model.pod.name))
        default: onAction(action, model.pod.name)
        }
    }
}
