import Foundation
import Testing
@testable import TainerMenu

private let t0 = Date(timeIntervalSince1970: 1_793_448_000) // 2026-10-31T12:00:00Z

private let helloLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"hello","data":{"engine":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"},"pods":[{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2}]}}"#
private let helloEmptyLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"hello","data":{"engine":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"},"pods":[]}}"#
private let engineLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"engine","data":{"state":"healthy","since":"2026-10-31T12:00:00.000Z","detail":"ok"}}"#
private let podLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"pod","data":{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2}}"#
private let progressLine = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"progress","data":{"op":"start","pod":"blog","step":"pull","percent":-1,"message":"pulling","done":false}}"#
private let endedLine = #"{"v":1,"error":{"code":"watch-ended","message":"the event stream ended"}}"#

private let blog = Pod(name: "blog", domain: "blog.tainer.me", state: .partial, running: 1, total: 2)

@Test func helloWithPods() {
    let engine = EngineState(state: .healthy, since: t0, detail: "ok")
    #expect(StreamLine.parse(helloLine) == .event(.hello(engine: engine, pods: [blog])))
}

@Test func helloWithEmptyPods() {
    let engine = EngineState(state: .healthy, since: t0, detail: "ok")
    #expect(StreamLine.parse(helloEmptyLine) == .event(.hello(engine: engine, pods: [])))
}

@Test func engineEvent() {
    #expect(StreamLine.parse(engineLine) == .event(.engine(EngineState(state: .healthy, since: t0, detail: "ok"))))
}

@Test func podEvent() {
    #expect(StreamLine.parse(podLine) == .event(.pod(blog)))
}

@Test func progressWithUnknownPercent() {
    let p = Progress(op: "start", pod: "blog", step: "pull", message: "pulling", percent: -1, done: false)
    #expect(StreamLine.parse(progressLine) == .event(.progress(p)))
}

@Test func watchEndedEnvelope() {
    #expect(StreamLine.parse(endedLine) == .errorEnvelope(code: "watch-ended", message: "the event stream ended"))
}

@Test func unknownTypeIsUnknownEvent() {
    let line = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"volume","data":{"x":1}}"#
    #expect(StreamLine.parse(line) == .event(.unknown))
}

@Test func unknownPodStateKeepsRawValue() {
    let line = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"pod","data":{"name":"blog","domain":"d","state":"hibernating","running":0,"total":2}}"#
    guard case .event(.pod(let pod)) = StreamLine.parse(line) else { Issue.record("not a pod event"); return }
    #expect(pod.state == .unknown("hibernating"))
}

@Test func unknownEngineStateKeepsRawValue() {
    let line = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"engine","data":{"state":"degraded","since":"2026-10-31T12:00:00.000Z","detail":""}}"#
    guard case .event(.engine(let e)) = StreamLine.parse(line) else { Issue.record("not an engine event"); return }
    #expect(e.state == .unknown("degraded"))
}

@Test func unknownFieldsAreIgnored() {
    let line = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"pod","extra":true,"data":{"name":"blog","domain":"blog.tainer.me","state":"partial","running":1,"total":2,"size":"nano"}}"#
    #expect(StreamLine.parse(line) == .event(.pod(blog)))
}

@Test func truncatedLineIsInvalid() {
    #expect(StreamLine.parse(String(podLine.dropLast(20))) == .invalid)
    #expect(StreamLine.parse("") == .invalid)
    #expect(StreamLine.parse("not json") == .invalid)
}

@Test func millisecondTimestampParsesToExactDate() throws {
    let d = try #require(ContractDate.parse("2026-10-31T12:00:00.123Z"))
    #expect(abs(d.timeIntervalSince1970 - 1_793_448_000.123) < 0.0005)
    #expect(ContractDate.parse("2026-10-31T12:00:00Z") == t0)
}

@Test func podIdentityIsName() {
    #expect(blog.id == "blog")
}

@Test func errorWithoutMessageIsStillAnEnvelope() {
    #expect(StreamLine.parse(#"{"v":1,"error":{"code":"watch-ended"}}"#) == .errorEnvelope(code: "watch-ended", message: ""))
}

@Test func emptyErrorObjectIsStillAnEnvelope() {
    #expect(StreamLine.parse(#"{"v":1,"error":{}}"#) == .errorEnvelope(code: "", message: ""))
}

@Test func errorWithNonStringFieldsDefaultsToEmpty() {
    #expect(StreamLine.parse(#"{"v":1,"error":{"code":7,"message":null}}"#) == .errorEnvelope(code: "", message: ""))
}

@Test func errorObjectWinsOverEventShape() {
    let line = #"{"v":1,"ts":"2026-10-31T12:00:00.000Z","type":"pod","data":{"name":"blog","domain":"d","state":"running","running":1,"total":1},"error":{"code":"x","message":"y"}}"#
    #expect(StreamLine.parse(line) == .errorEnvelope(code: "x", message: "y"))
}
