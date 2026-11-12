import Foundation
import Testing
@testable import TainerMenu

/// tain-132: the menu app is its own login item (SMAppService), so Login Items shows
/// "Tainer Menu" with its icon instead of the signing team.

final class FakeLoginItem: LoginItemService, @unchecked Sendable {
    var status: LoginItemStatus
    var statusAfterRegister: LoginItemStatus
    var registerError: Error?
    private(set) var registers = 0
    private(set) var settingsOpened = 0

    init(_ status: LoginItemStatus, after: LoginItemStatus = .enabled) {
        self.status = status
        self.statusAfterRegister = after
    }

    func register() throws {
        registers += 1
        if let registerError { throw registerError }
        status = statusAfterRegister
    }

    func openLoginItemsSettings() { settingsOpened += 1 }
}

final class MemoryDefaults: FlagStore, @unchecked Sendable {
    var flags: [String: Bool] = [:]
    private(set) var flushes = 0
    func bool(forKey key: String) -> Bool { flags[key] ?? false }
    func set(_ value: Bool, forKey key: String) { flags[key] = value }
    func flush() { flushes += 1 }
}

// Observed on macOS (probe app, 2026-11-12): never registered reads notFound; registered reads
// enabled; after unregister it reads notRegistered; deleting the bundle and putting it back at
// the same path keeps enabled.

/// The installed app's registrar.
private func registrar(_ item: FakeLoginItem, _ flags: MemoryDefaults) -> LoginItemRegistrar {
    LoginItemRegistrar(service: item, flags: flags, bundlePath: "/Applications/Tainer Menu.app")
}

@Test func firstLaunchRegistersOnce() {
    let item = FakeLoginItem(.notFound)
    let flags = MemoryDefaults()
    #expect(registrar(item, flags).ensure() == .enabled)
    #expect(item.registers == 1)
    // Next launch: already enabled, nothing to do.
    #expect(registrar(item, flags).ensure() == .enabled)
    #expect(item.registers == 1)
}

@Test func anEnabledItemIsLeftAlone() {
    let item = FakeLoginItem(.enabled)
    _ = registrar(item, MemoryDefaults()).ensure()
    #expect(item.registers == 0)
    #expect(item.settingsOpened == 0)
}

@Test func aUserWhoRemovedTheItemIsNotOverruled() {
    // Registered on an earlier launch, then removed in System Settings: stays removed.
    let item = FakeLoginItem(.notRegistered)
    let flags = MemoryDefaults()
    flags.set(true, forKey: LoginItemRegistrar.registeredKey)
    #expect(registrar(item, flags).ensure() == .notRegistered)
    #expect(item.registers == 0)
}

@Test func notRegisteredWithoutOurFlagRegisters() {
    // Unregistered before this app ever registered it (e.g. the uninstaller ran, which also
    // clears our flags): register.
    let item = FakeLoginItem(.notRegistered)
    #expect(registrar(item, MemoryDefaults()).ensure() == .enabled)
    #expect(item.registers == 1)
}

@Test func aRemovedItemStaysRemovedHoweverMacOSReportsIt() {
    // Once the item was ours, a missing record means the user removed it (macOS may report
    // that as notRegistered or notFound): never re-added. doctor's hint covers recovery.
    for status in [LoginItemStatus.notFound, .notRegistered] {
        let item = FakeLoginItem(status)
        let flags = MemoryDefaults()
        flags.set(true, forKey: LoginItemRegistrar.registeredKey)
        #expect(registrar(item, flags).ensure() == status)
        #expect(item.registers == 0)
        #expect(item.settingsOpened == 0)
    }
}

@Test func anItemSeenEnabledIsOurs() {
    // Added by hand in System Settings: later removing it must stick too.
    let item = FakeLoginItem(.enabled)
    let flags = MemoryDefaults()
    _ = registrar(item, flags).ensure()
    #expect(flags.bool(forKey: LoginItemRegistrar.registeredKey))
    item.status = .notRegistered
    _ = registrar(item, flags).ensure()
    #expect(item.registers == 0)
}

@Test func onlyACopyInApplicationsRegisters() {
    for path in ["/Volumes/Tainer/Tainer Menu.app", "/Users/me/Downloads/Tainer Menu.app",
                 "/Users/me/Applications/Tainer Menu.app", "/ApplicationsX/Tainer Menu.app"] {
        let item = FakeLoginItem(.notFound)
        let flags = MemoryDefaults()
        #expect(LoginItemRegistrar(service: item, flags: flags, bundlePath: path).ensure() == .notFound)
        #expect(item.registers == 0, "\(path)")
        #expect(!flags.bool(forKey: LoginItemRegistrar.registeredKey))
    }
    let installed = FakeLoginItem(.notFound)
    _ = LoginItemRegistrar(service: installed, flags: MemoryDefaults(), bundlePath: "/Applications/Tainer Menu.app").ensure()
    #expect(installed.registers == 1)
}

@Test func uninstallClearsTheFlagsEvenWhenUnregisterFails() {
    struct Boom: Error {}
    for fails in [false, true] {
        let flags = MemoryDefaults()
        flags.set(true, forKey: LoginItemRegistrar.registeredKey)
        flags.set(true, forKey: LoginItemRegistrar.approvalAskedKey)
        let ok = LoginItemRegistrar.uninstall(remove: { if fails { throw Boom() } }, flags: flags)
        #expect(ok == !fails)
        #expect(!flags.bool(forKey: LoginItemRegistrar.registeredKey))
        #expect(!flags.bool(forKey: LoginItemRegistrar.approvalAskedKey))
        #expect(flags.flushes == 1)
    }
    // After an uninstall, a reinstall registers again.
    let flags = MemoryDefaults()
    let item = FakeLoginItem(.notFound)
    _ = registrar(item, flags).ensure()
    _ = LoginItemRegistrar.uninstall(remove: { item.status = .notRegistered }, flags: flags)
    #expect(registrar(item, flags).ensure() == .enabled)
    #expect(item.registers == 2)
}

@Test func approvalOpensSettingsOnlyOnce() {
    let item = FakeLoginItem(.notFound, after: .requiresApproval)
    let flags = MemoryDefaults()
    #expect(registrar(item, flags).ensure() == .requiresApproval)
    #expect(item.settingsOpened == 1)
    // Still waiting for approval on the next launches: never asked again.
    _ = registrar(item, flags).ensure()
    _ = registrar(item, flags).ensure()
    #expect(item.settingsOpened == 1)
    #expect(item.registers == 1)
}

@Test func aFailedRegistrationIsRetriedNextLaunch() {
    struct Boom: Error {}
    let item = FakeLoginItem(.notFound)
    item.registerError = Boom()
    let flags = MemoryDefaults()
    #expect(registrar(item, flags).ensure() == .notFound)
    #expect(!flags.bool(forKey: LoginItemRegistrar.registeredKey))
    item.registerError = nil
    #expect(registrar(item, flags).ensure() == .enabled)
    #expect(item.registers == 2)
}

@Test func statusQueryIsItsOwnLaunchMode() {
    #expect(LaunchMode(arguments: ["/x/TainerMenu"]) == .app)
    #expect(LaunchMode(arguments: ["/x/TainerMenu", "--login-item-status"]) == .loginItemStatus)
    #expect(LaunchMode(arguments: ["/x/TainerMenu", "-NSDocumentRevisionsDebugMode", "YES"]) == .app)
}

@Test func statusWordsMatchWhatDoctorReads() {
    #expect(LoginItemStatus.enabled.rawValue == "enabled")
    #expect(LoginItemStatus.requiresApproval.rawValue == "requiresApproval")
    #expect(LoginItemStatus.notRegistered.rawValue == "notRegistered")
    #expect(LoginItemStatus.notFound.rawValue == "notFound")
}

/// Quit means quit for now: the login item stays for the next login. The only unregister
/// call is the uninstaller's `--unregister-login-item` path; the app itself never removes it.
@Test func onlyTheUninstallPathUnregistersTheLoginItem() throws {
    let sources = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .appendingPathComponent("Sources")
    let files = try #require(FileManager.default.enumerator(at: sources, includingPropertiesForKeys: nil))
    var calls: [String] = []
    var checked = 0
    for case let url as URL in files where url.pathExtension == "swift" {
        let text = try String(contentsOf: url, encoding: .utf8)
        for line in text.split(separator: "\n") where line.contains("unregister(") || line.contains("removeForUninstall") {
            calls.append("\(url.lastPathComponent): \(line.trimmingCharacters(in: .whitespaces))")
        }
        checked += 1
    }
    #expect(checked > 10)
    #expect(calls.sorted() == [
        "LoginItem.swift: func removeForUninstall() throws { try service.unregister() }",
        "TainerMenuApp.swift: let removed = LoginItemRegistrar.uninstall(remove: SystemLoginItem().removeForUninstall, flags: UserDefaults.standard)",
    ], "\(calls)")
}

@Test func uninstallModeIsItsOwnLaunchMode() {
    #expect(LaunchMode(arguments: ["/x/TainerMenu", "--unregister-login-item"]) == .unregisterLoginItem)
}
