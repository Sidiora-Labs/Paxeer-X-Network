import Foundation
import LayerXMobile
import LayerXSDK
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

private struct CapturedDelivery: Decodable {
    let body: String
    let headers: [String: String]
}

private actor HandledDeliveries {
    private var count = 0
    func record() { count += 1 }
    func value() -> Int { count }
}

private func require(_ condition: Bool, _ detail: String) throws {
    guard condition else { throw ConformanceFailure(detail: detail) }
}

private struct ConformanceFailure: Error {
    let detail: String
}

private func reject(
    _ operation: () async throws -> EventConsumeOutcome
) async throws {
    do {
        _ = try await operation()
        throw ConformanceFailure(detail: "invalid signed delivery was accepted")
    } catch let error as MobileIntegrationError {
        try require(error.code == .invalidEvent, "wrong signed delivery refusal")
    }
}

private func run() async throws {
    let arguments = Array(CommandLine.arguments.dropFirst())
    guard arguments.count == 8, arguments[0] == "--configuration",
          arguments[2] == "--capture", arguments[4] == "--ledger",
          arguments[6] == "--expect", ["processed", "duplicate"].contains(arguments[7]) else {
        throw ConformanceFailure(detail: "usage: --configuration public.json --capture delivery.json --ledger private.json --expect processed|duplicate")
    }
    let configuration = try PublishableConfiguration(contentsOfJSONFile: URL(fileURLWithPath: arguments[1]))
    let captureBytes = try Data(contentsOf: URL(fileURLWithPath: arguments[3]))
    try require(captureBytes.count <= 2 * 1024 * 1024, "captured delivery exceeds bound")
    let captured = try JSONDecoder().decode(CapturedDelivery.self, from: captureBytes)
    guard let body = Data(base64Encoded: captured.body), !body.isEmpty else {
        throw ConformanceFailure(detail: "capture requires actual signed delivery body")
    }
    let mobile = try LayerXMobile(configuration: configuration,
        deliveryStoreURL: URL(fileURLWithPath: arguments[5]))
    let handled = HandledDeliveries()
    let handle: (JSONValue, String) async throws -> Void = { _, _ in await handled.record() }

    var tampered = body
    tampered[tampered.startIndex] ^= 1
    try await reject { try await mobile.consume(rawBody: tampered, headerFields: captured.headers, handle: handle) }
    let envelope = try EventEnvelopeHeaders(fields: captured.headers)
    var missing = captured.headers
    missing = missing.filter { $0.key.lowercased() != EventEnvelopeHeaders.signatureHeader.lowercased() }
    try await reject { try await mobile.consume(rawBody: body, headerFields: missing, handle: handle) }
    var duplicate = captured.headers
    duplicate[EventEnvelopeHeaders.idHeader.lowercased()] = envelope.id
    if !captured.headers.keys.contains(EventEnvelopeHeaders.idHeader.lowercased()) {
        try await reject { try await mobile.consume(rawBody: body, headerFields: duplicate, handle: handle) }
    } else {
        duplicate[EventEnvelopeHeaders.idHeader.uppercased()] = envelope.id
        try await reject { try await mobile.consume(rawBody: body, headerFields: duplicate, handle: handle) }
    }
    var wrongSignature = captured.headers.filter {
        $0.key.lowercased() != EventEnvelopeHeaders.signatureHeader.lowercased()
    }
    wrongSignature[EventEnvelopeHeaders.signatureHeader] = "v1=invalid"
    try await reject { try await mobile.consume(rawBody: body, headerFields: wrongSignature, handle: handle) }
    try require(await handled.value() == 0, "refused delivery reached handler")
    let expected: EventConsumeOutcome = arguments[7] == "processed" ? .processed : .duplicate
    let outcome = try await mobile.consume(rawBody: body, headerFields: captured.headers, handle: handle)
    try require(outcome == expected, "unexpected initial durable delivery outcome")
    let replay = try await mobile.consume(rawBody: body, headerFields: captured.headers, handle: handle)
    try require(replay == .duplicate, "same signed event was not deduplicated")
    try require(await handled.value() == (expected == .processed ? 1 : 0), "replay reached handler")
    let reopened = try LayerXMobile(configuration: configuration,
        deliveryStoreURL: URL(fileURLWithPath: arguments[5]))
    let retained = try await reopened.consume(rawBody: body, headerFields: captured.headers, handle: handle)
    try require(retained == .duplicate, "reopened default delivery store lost replay record")
    try require(await handled.value() == (expected == .processed ? 1 : 0), "reopen replay reached handler")
    print("PAXEER_X_IOS_WEBHOOK outcome=\(expected.rawValue) tamper=rejected missing=rejected duplicate-header=rejected malformed-signature=rejected replay=duplicate reopen=duplicate handled=\(await handled.value())")
}

do {
    try await run()
} catch let error as ConformanceFailure {
    FileHandle.standardError.write(Data("iOS webhook conformance: \(error.detail)\n".utf8))
    exit(1)
} catch let error as MobileIntegrationError {
    FileHandle.standardError.write(Data("iOS webhook conformance: \(error.code.rawValue)\n".utf8))
    exit(1)
} catch {
    FileHandle.standardError.write(Data("iOS webhook conformance: capture or transport refused\n".utf8))
    exit(1)
}
