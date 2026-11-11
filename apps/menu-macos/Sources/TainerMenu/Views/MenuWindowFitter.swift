import AppKit
import SwiftUI

/// Keeps the menu's window exactly as tall as its content. The menu-bar window grows when content
/// grows but keeps its tallest height when content shrinks (a short last pod page left an empty
/// block above the panel), so the height is applied explicitly, in both directions, with the top
/// edge held under the menu bar.
@MainActor
enum MenuWindowFitter {
    /// `current` resized to hold `contentHeight` plus the window's own chrome, top edge fixed.
    nonisolated static func fittedFrame(current: NSRect, contentHeight: CGFloat, chrome: CGFloat) -> NSRect {
        let height = (contentHeight + chrome).rounded(.up)
        return NSRect(x: current.minX, y: current.maxY - height, width: current.width, height: height)
    }

    static func fit(_ window: NSWindow, contentHeight: CGFloat) {
        guard contentHeight > 0 else { return }
        let chrome = window.frame.height - window.contentLayoutRect.height
        let target = fittedFrame(current: window.frame, contentHeight: contentHeight, chrome: chrome)
        guard abs(target.height - window.frame.height) >= 0.5 else { return }
        window.setFrame(target, display: true)
    }
}

/// Measures the content it is attached to and fits the hosting window to it whenever the height
/// changes or the window is shown again.
struct FitsMenuWindow: ViewModifier {
    @State private var height: CGFloat = 0

    func body(content: Content) -> some View {
        content
            .fixedSize(horizontal: false, vertical: true)
            .background(GeometryReader { proxy in
                Color.clear.preference(key: ContentHeightKey.self, value: proxy.size.height)
            })
            .onPreferenceChange(ContentHeightKey.self) { height = $0 }
            .background(WindowFitterView(height: height))
    }
}

private struct ContentHeightKey: PreferenceKey {
    static let defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

private struct WindowFitterView: NSViewRepresentable {
    let height: CGFloat

    func makeNSView(context: Context) -> Fitter { Fitter() }

    func updateNSView(_ fitter: Fitter, context: Context) {
        fitter.height = height
        fitter.scheduleFit()
    }

    final class Fitter: NSView {
        var height: CGFloat = 0
        private var observation: NSKeyValueObservation?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            observation = window?.observe(\.isVisible, options: [.new]) { [weak self] _, change in
                guard change.newValue == true else { return }
                DispatchQueue.main.async { self?.scheduleFit() }
            }
            scheduleFit()
        }

        /// Deferred: resizing the window inside a SwiftUI update pass would re-enter layout.
        func scheduleFit() {
            DispatchQueue.main.async { [weak self] in
                guard let self, let window = self.window else { return }
                MenuWindowFitter.fit(window, contentHeight: self.height)
            }
        }
    }
}

extension View {
    func fitsMenuWindow() -> some View { modifier(FitsMenuWindow()) }
}
