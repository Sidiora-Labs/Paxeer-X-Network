import Foundation
import XCTest
@testable import LayerXSDK

final class AgentOperationEnvelopeTests: XCTestCase {
    private struct CaseFile {
        let endpoint: URL
        let certificateAuthority: AgentCertificateAuthority
        let gatewayKeyID: String
        let gatewayKey: Data
        let credential: [String: JSONValue]
        let requests: [String: JSONValue]
        let cases: [String]
        let phase: String
        let responseDir: URL
    }

    private static let fileKeys: Set<String> = [
        "endpoint", "server_name", "ca_pem", "ca_der", "gateway_api_key_file", "program_bearer_file",
        "credential_file", "requests", "operations", "cases", "phase", "state_file", "response_dir",
    ]
    private static let readCases = ["read": "read.account", "program_read": "program.interface",
                                    "approval_list": "approval.list"]

    private static func refused(_ reason: String) -> PlatformSDKError {
        XCTFail("agent envelope probe refused: " + reason)
        return PlatformSDKError(code: .invalidArgument, retry: .never)
    }

    private static func object(_ data: Data, _ name: String) throws -> [String: JSONValue] {
        guard let value = try? JSONDecoder().decode(JSONValue.self, from: data), let object = value.objectValue else {
            throw refused(name + " is not a JSON object")
        }
        return object
    }

    private static func path(_ object: [String: JSONValue], _ key: String) throws -> URL {
        guard let text = object[key]?.stringValue, text.hasPrefix("/") else { throw refused(key + " is not an absolute path") }
        return URL(fileURLWithPath: text)
    }

    private func caseFile() throws -> CaseFile {
        guard let raw = ProcessInfo.processInfo.environment["PAXEER_X_AGENT_ENVELOPE_CASE"], raw.hasPrefix("/") else {
            throw Self.refused("PAXEER_X_AGENT_ENVELOPE_CASE is absent")
        }
        let file = try Self.object(Data(contentsOf: URL(fileURLWithPath: raw)), "case file")
        guard Set(file.keys) == Self.fileKeys else { throw Self.refused("case file keys") }
        guard let endpointText = file["endpoint"]?.stringValue, let endpoint = URL(string: endpointText),
              endpoint.scheme == "https", endpoint.path == AgentEnvelopeTransport.routePath,
              file["server_name"]?.stringValue == endpoint.host else {
            throw Self.refused("endpoint is not the https agent envelope route")
        }
        let authority = try AgentCertificateAuthority(pemFile: Self.path(file, "ca_pem"))
        guard try Data(contentsOf: Self.path(file, "ca_der")) == authority.der else {
            throw Self.refused("ca_pem and ca_der name different authorities")
        }
        let keyLine = try String(contentsOf: Self.path(file, "gateway_api_key_file"), encoding: .utf8)
        guard keyLine.hasSuffix("\n"), keyLine.filter({ $0 == "\n" }).count == 1 else {
            throw Self.refused("gateway_api_key_file is not one line")
        }
        let key = keyLine.dropLast().split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        guard key.count == 2 else { throw Self.refused("gateway_api_key_file is not <key_id>:<secret>") }
        let credential = try Self.object(Data(contentsOf: Self.path(file, "credential_file")), "credential_file")
        guard Set(credential.keys) == ["tenant", "session_id", "token_id", "generation"] else {
            throw Self.refused("credential_file keys")
        }
        guard let requests = file["requests"]?.objectValue, case let .array(caseValues)? = file["cases"],
              let phase = file["phase"]?.stringValue else {
            throw Self.refused("requests, cases or phase")
        }
        let cases = caseValues.compactMap(\.stringValue)
        guard cases.count == caseValues.count, Set(cases).count == cases.count else { throw Self.refused("cases") }
        return CaseFile(
            endpoint: endpoint, certificateAuthority: authority, gatewayKeyID: String(key[0]),
            gatewayKey: Data(key[1].utf8), credential: credential, requests: requests, cases: cases, phase: phase,
            responseDir: try Self.path(file, "response_dir"))
    }

    private func transport(_ file: CaseFile, generation: UInt64? = nil, withCredential: Bool = true) throws
        -> AgentEnvelopeTransport {
        var credential: AgentSessionCredential?
        if withCredential {
            guard let tenant = file.credential["tenant"]?.stringValue,
                  let session = file.credential["session_id"]?.stringValue,
                  let token = file.credential["token_id"]?.stringValue.flatMap(Self.hexBytes),
                  let generationText = file.credential["generation"]?.stringValue,
                  let parsed = UInt64(generationText), String(parsed) == generationText else {
                throw Self.refused("credential_file coordinates")
            }
            credential = try AgentSessionCredential(tenant: tenant, sessionID: session, tokenID: token,
                generation: generation ?? parsed)
        }
        return try AgentEnvelopeTransport(baseURL: file.endpoint,
            gatewayKey: LayerXKeyCredential(keyID: file.gatewayKeyID, secret: file.gatewayKey),
            credential: credential, certificateAuthority: file.certificateAuthority)
    }

    private func call(_ operation: PlatformOperation, _ request: JSONValue,
                      idempotencyKey: IdempotencyKey? = nil) -> TransportCall {
        TransportCall(operation: operation, request: request, pathParameters: [:], idempotencyKey: idempotencyKey)
    }

    private func runCase(_ id: String, _ file: CaseFile, _ agent: AgentEnvelopeTransport) async throws {
        guard let expected = Self.readCases[id], let entry = file.requests[id]?.objectValue,
              entry["operation"]?.stringValue == expected,
              Set(entry.keys) == ["operation", "request"] || Set(entry.keys) == ["operation", "request", "idempotency_key"],
              let request = entry["request"], request.objectValue != nil,
              let operation = PlatformOperation(rawValue: "agent:" + expected) else {
            throw Self.refused("requests." + id)
        }
        let key = try entry["idempotency_key"]?.stringValue.map { try IdempotencyKey($0) }
        let exchanged = try await agent.exchange(call(operation, request, idempotencyKey: key))
        let body = try JSONDecoder().decode(JSONValue.self, from: exchanged.body)
        let record = try JSONEncoder().encode(JSONValue.object([
            "status": .integer(Int64(exchanged.status)), "body": body,
        ]))
        guard FileManager.default.createFile(atPath: file.responseDir.appendingPathComponent(id + ".json").path,
            contents: record, attributes: [.posixPermissions: 0o600]) else {
            throw Self.refused("response_dir is not writable")
        }
        let result = try AgentEnvelopeTransport.decodeResponse(status: exchanged.status, data: exchanged.body,
            requestID: exchanged.requestID)
        XCTAssertTrue(AgentEnvelopeTransport.validVerification(result.verificationStatus))
        XCTAssertEqual(result.requestID, exchanged.requestID)
    }

    func testAgentOperationEnvelopeProcessCases() async throws {
        var passed = 0
        defer {
            print("PAXEER_X_AGENT_ENVELOPE_CASES=\(passed)")
            fflush(stdout)
        }
        let file = try caseFile()
        guard file.phase == "read", file.cases == ["read", "program_read", "approval_list"] else {
            throw Self.refused("swift probe serves only the read phase cases")
        }
        let agent = try transport(file)
        for id in file.cases {
            do {
                try await runCase(id, file, agent)
                print("PAXEER_X_AGENT_ENVELOPE_CASE \(id) passed")
                fflush(stdout)
                passed += 1
            } catch {
                XCTFail("case \(id) failed: \(error)")
            }
        }
    }

    func testSdkSwiftFaucetClaimIsRetiredUnavailableCapability() async throws {
        let file = try caseFile()
        do {
            _ = try await transport(file).sendEnvelope(call(.agentFaucetClaim, .emptyObject))
            XCTFail("faucet.claim was accepted")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .unavailableCapability)
            XCTAssertEqual(error.retry, .never)
        }
    }

    func testSdkSwiftWrongGenerationIsRefusedBySessionAuthority() async throws {
        let file = try caseFile()
        guard let request = file.requests["read"]?.objectValue?["request"],
              let generation = file.credential["generation"]?.stringValue.flatMap({ UInt64($0) }) else {
            throw Self.refused("requests.read or credential generation")
        }
        guard generation < UInt64.max else { return XCTFail("generation has no successor") }
        do {
            _ = try await transport(file, generation: generation + 1).sendEnvelope(call(.agentReadAccount, request))
            XCTFail("stale generation was accepted")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .policyRefusal)
        }
    }

    func testSdkSwiftRefusesCatalogueCallWithoutSessionCredential() async throws {
        let file = try caseFile()
        guard let request = file.requests["read"]?.objectValue?["request"] else { throw Self.refused("requests.read") }
        do {
            _ = try await transport(file, withCredential: false).sendEnvelope(call(.agentReadAccount, request))
            XCTFail("catalogue call was sent without a session credential")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .capabilityRefusal)
        }
    }

    private static func hexBytes(_ text: String) -> Data? {
        guard AgentEnvelopeTransport.lowerHex32(text) else { return nil }
        var bytes = Data()
        var index = text.startIndex
        while index < text.endIndex {
            let next = text.index(index, offsetBy: 2)
            guard let byte = UInt8(text[index..<next], radix: 16) else { return nil }
            bytes.append(byte)
            index = next
        }
        return bytes
    }
}
