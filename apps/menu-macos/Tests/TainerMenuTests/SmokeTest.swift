import Foundation
import Testing
@testable import TainerMenu

@Test func testTargetRuns() {
    #expect(true)
}

@Test func brandResourcesAreBundled() {
    for name in ["logo-tainer-light", "logo-tainer-dark", "logo-cyber5-light", "bar-mark"] {
        #expect(Bundle.module.url(forResource: "\(name)@2x", withExtension: "png") != nil, "missing \(name)")
    }
}

@Test func appBundleResolvesResources() {
    #expect(AppBundle.resources.url(forResource: "bar-mark@2x", withExtension: "png") != nil)
}
