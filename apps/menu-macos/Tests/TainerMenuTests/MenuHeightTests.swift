import AppKit
import SwiftUI
import Testing
@testable import TainerMenu

/// tain-122: the menu window must follow the page's height, shrinking on a short last page and
/// growing back on a full one.

@MainActor
private func pagedState(_ count: Int) -> StackState {
    let s = StackState()
    s.applyList((0..<count).map { ListedProject(name: String(format: "p%02d", $0), domain: String(format: "p%02d.tainer.me", $0)) })
    return s
}

/// The menu hosted and laid out, with its on-open work (which resets to page 1) already run.
@MainActor
private func menu(_ state: StackState) async -> NSHostingView<MenuContent> {
    let view = NSHostingView(rootView: MenuContent(state: state, updates: UpdateChecker(), onAction: { _, _ in }, onQuit: {}))
    view.frame.size = view.fittingSize
    view.layoutSubtreeIfNeeded()
    for _ in 0..<50 { await Task.yield() }
    return view
}

@Test @MainActor func menuIdealHeightFollowsThePage() async {
    let state = pagedState(12)
    let view = await menu(state)
    let full = view.fittingSize.height
    state.nextPage()
    view.layoutSubtreeIfNeeded()
    let short = view.fittingSize.height
    #expect(state.visiblePods.count == 2)
    // Eight fewer rows: nothing pins the list to the tallest page it has shown.
    #expect(full - short > 8 * 20, "full \(full) short \(short)")
    state.previousPage()
    view.layoutSubtreeIfNeeded()
    #expect(view.fittingSize.height == full)
}

@Test func fittedFrameKeepsTheTopEdge() {
    let tall = NSRect(x: 100, y: 300, width: 340, height: 600)
    let short = MenuWindowFitter.fittedFrame(current: tall, contentHeight: 200, chrome: 0)
    #expect(short == NSRect(x: 100, y: 700, width: 340, height: 200))
    #expect(MenuWindowFitter.fittedFrame(current: short, contentHeight: 600, chrome: 0) == tall)
    // Window chrome (frame minus content) is kept on top of the content height.
    #expect(MenuWindowFitter.fittedFrame(current: tall, contentHeight: 200, chrome: 12).height == 212)
    #expect(MenuWindowFitter.fittedFrame(current: tall, contentHeight: 200, chrome: 12).maxY == tall.maxY)
}

/// End to end: the menu's own fitter (attached inside MenuContent) resizes a window that, like the
/// menu-bar window, does not follow its content on its own.
@Test @MainActor func menuFitsAWindowThatKeepsItsTallestHeight() async {
    let state = pagedState(12)
    let host = await menu(state)
    let gauge = await menu(state)
    host.sizingOptions = []
    let full = gauge.fittingSize.height
    let panel = NSPanel(contentRect: NSRect(x: -10_000, y: -10_000, width: 340, height: full),
                        styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
    panel.contentView = host
    panel.orderFrontRegardless()
    let top = panel.frame.maxY

    state.nextPage()
    gauge.layoutSubtreeIfNeeded()
    let short = gauge.fittingSize.height
    #expect(state.visiblePods.count == 2)
    #expect(short < full - 160)
    await settle { panel.frame.height == short }
    #expect(panel.frame.maxY == top, "top edge stays under the menu bar")

    state.previousPage()
    await settle { panel.frame.height == full }
    #expect(panel.frame.maxY == top)

    // Without the fitter the same window stays at its tallest height.
    let bare = NSHostingView(rootView: Text("x").frame(width: 340, height: 50))
    bare.sizingOptions = []
    let stuck = NSPanel(contentRect: NSRect(x: -10_000, y: -10_000, width: 340, height: full),
                        styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
    stuck.contentView = bare
    stuck.orderFrontRegardless()
    for _ in 0..<200 { await Task.yield() }
    #expect(stuck.frame.height == full)
    stuck.close()
    panel.close()
}
