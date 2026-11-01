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

struct PodRow: View {
    let model: PodRowModel
    let palette: MenuPalette
    let onAction: (PodAction, String?) -> Void
    @Environment(\.openURL) private var openURL
    @State private var hovering = false

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
        .onHover { hovering = $0 }
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
