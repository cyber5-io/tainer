import Testing
@testable import TainerMenu

@Test func envOverrideWins() {
    let p = CLILocator.find(env: ["TAINER_CLI": "/x/tainer", "PATH": "/bin"], isExecutable: { _ in true })
    #expect(p == "/x/tainer")
}

@Test func envOverrideIgnoredWhenNotExecutable() {
    let p = CLILocator.find(env: ["TAINER_CLI": "/x/tainer"], isExecutable: { $0 == "/opt/tainer/bin/tainer" })
    #expect(p == "/opt/tainer/bin/tainer")
}

@Test func fallsBackToPath() {
    let p = CLILocator.find(env: ["PATH": "/a:/b"], isExecutable: { $0 == "/b/tainer" })
    #expect(p == "/b/tainer")
}

@Test func nothingFoundIsNil() {
    #expect(CLILocator.find(env: ["PATH": "/a"], isExecutable: { _ in false }) == nil)
}
