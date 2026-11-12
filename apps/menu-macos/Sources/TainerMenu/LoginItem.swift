import AppKit
import ServiceManagement

/// The menu app's own login item. SMAppService.mainApp makes Login Items show
/// "Tainer Menu" with its icon; the old bare launchd plist showed the signing team.
enum LoginItemStatus: String, Equatable, Sendable {
    case enabled, requiresApproval, notRegistered, notFound
}

protocol LoginItemService: AnyObject {
    var status: LoginItemStatus { get }
    func register() throws
    func openLoginItemsSettings()
}

final class SystemLoginItem: LoginItemService {
    private let service = SMAppService.mainApp

    var status: LoginItemStatus {
        switch service.status {
        case .enabled: .enabled
        case .requiresApproval: .requiresApproval
        case .notFound: .notFound
        case .notRegistered: .notRegistered
        @unknown default: .notRegistered
        }
    }

    func register() throws { try service.register() }

    func openLoginItemsSettings() { SMAppService.openSystemSettingsLoginItems() }

    /// Only for `--unregister-login-item`, which the uninstaller runs before deleting the app.
    func removeForUninstall() throws { try service.unregister() }
}

protocol FlagStore: AnyObject {
    func bool(forKey key: String) -> Bool
    func set(_ value: Bool, forKey key: String)
    /// Writes pending changes out now (the uninstall mode exits right after).
    func flush()
}

extension UserDefaults: FlagStore {
    func flush() { synchronize() }
}

/// Registers the login item so the menu opens at login, once: `loginItemRegistered` records
/// that the item was ours at some point (registered here, or seen enabled after the user
/// added it by hand), and from then on its removal is respected whatever status macOS then
/// reports (notRegistered or notFound). Only the uninstaller's mode clears it, so a reinstall
/// registers again. Registration happens only from a copy under /Applications, never from a
/// disk image or Downloads, which would leave a login item pointing at a path that goes away.
/// When macOS wants approval, System Settings opens once, never on every launch. Quitting
/// never unregisters.
struct LoginItemRegistrar {
    static let registeredKey = "loginItemRegistered"
    static let approvalAskedKey = "loginItemApprovalAsked"

    let service: LoginItemService
    let flags: FlagStore
    /// Where the running app lives (Bundle.main.bundlePath in the app).
    let bundlePath: String

    var isInstalled: Bool { bundlePath.hasPrefix("/Applications/") }

    @discardableResult
    func ensure() -> LoginItemStatus {
        switch service.status {
        case .enabled:
            flags.set(true, forKey: Self.registeredKey)
        case .notFound, .notRegistered:
            if !flags.bool(forKey: Self.registeredKey), isInstalled {
                registerOnce()
            }
        default:
            break
        }
        let status = service.status
        if status == .requiresApproval, !flags.bool(forKey: Self.approvalAskedKey) {
            flags.set(true, forKey: Self.approvalAskedKey)
            service.openLoginItemsSettings()
        }
        return status
    }

    private func registerOnce() {
        if (try? service.register()) != nil {
            flags.set(true, forKey: Self.registeredKey)
        }
    }

    static func forget(_ flags: FlagStore) {
        flags.set(false, forKey: registeredKey)
        flags.set(false, forKey: approvalAskedKey)
    }

    /// The uninstaller's mode: remove the login item and forget having registered it, even if
    /// the removal fails, so a later reinstall registers again. True when the removal worked.
    static func uninstall(remove: () throws -> Void, flags: FlagStore) -> Bool {
        let removed = (try? remove()) != nil
        forget(flags)
        flags.flush()
        return removed
    }
}

/// Two command-line modes that never start the menu. SMAppService.mainApp answers only for
/// the bundle it runs from, so other tools ask the app itself:
/// `--login-item-status` prints the status for `tainer doctor`, and `--unregister-login-item`
/// removes the item for the uninstaller.
enum LaunchMode: Equatable {
    case app, loginItemStatus, unregisterLoginItem

    init(arguments: [String]) {
        let args = arguments.dropFirst()
        if args.contains("--login-item-status") {
            self = .loginItemStatus
        } else if args.contains("--unregister-login-item") {
            self = .unregisterLoginItem
        } else {
            self = .app
        }
    }
}
