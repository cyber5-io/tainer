import AppKit
import Foundation

enum AppBundle {
    static let resourceBundleName = "TainerMenu_TainerMenu.bundle"

    /// SwiftPM's generated `Bundle.module` only looks at the .app root and then at the
    /// build machine's `.build` path (fatalError if neither exists), so the assembled
    /// app keeps its resource bundle in Contents/Resources and resolves it here first.
    static let resources: Bundle = {
        if let url = Bundle.main.resourceURL?.appendingPathComponent(resourceBundleName),
           let bundle = Bundle(url: url) {
            return bundle
        }
        return Bundle.module
    }()

    /// Loads a `name@2x.png` brand raster at point size (pixels / 2), scaled to `height` when given.
    /// SwiftUI's `Image(_:bundle:)` does not find loose @2x files in a SwiftPM resource bundle.
    static func image(_ name: String, height: CGFloat? = nil) -> NSImage? {
        guard let url = resources.url(forResource: "\(name)@2x", withExtension: "png"),
              let image = NSImage(contentsOf: url),
              let rep = image.representations.first, rep.pixelsWide > 0, rep.pixelsHigh > 0
        else { return nil }
        let pointSize = NSSize(width: CGFloat(rep.pixelsWide) / 2, height: CGFloat(rep.pixelsHigh) / 2)
        let scale = height.map { $0 / pointSize.height } ?? 1
        image.size = NSSize(width: pointSize.width * scale, height: pointSize.height * scale)
        return image
    }
}
