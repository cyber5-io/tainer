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
}
