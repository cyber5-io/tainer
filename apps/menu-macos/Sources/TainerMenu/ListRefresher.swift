import Foundation

/// Fetches every registered project with `tainer list --json` and merges it into the state.
/// Failures leave the previous list in place; the watch stream keeps owning live state.
@MainActor
final class ListRefresher {
    private let state: StackState
    private let runner: any ProcessRunner
    private let locate: () -> String?

    init(
        state: StackState,
        runner: any ProcessRunner = SubprocessRunner(),
        locate: @escaping () -> String? = { CLILocator.find() }
    ) {
        self.state = state
        self.runner = runner
        self.locate = locate
    }

    func refresh() async {
        guard let cli = locate() else { return }
        let collector = ListLineCollector()
        guard (try? await runner.run(executable: cli, arguments: ["list", "--json"], onLine: { await collector.add($0) })) != nil,
              let projects = ListDocument.parse(await collector.lines)
        else { return }
        state.applyList(projects)
    }
}

private actor ListLineCollector {
    private(set) var lines: [String] = []
    func add(_ line: String) { lines.append(line) }
}
