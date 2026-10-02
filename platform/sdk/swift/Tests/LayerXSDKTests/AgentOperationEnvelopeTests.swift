import Foundation
import XCTest
@testable import LayerXSDK

final class AgentOperationEnvelopeTests: XCTestCase {
    private struct Probe {
        let baseURL: URL
        let gatewayKeyID: String
        let gatewayKey: Data
        let tenant: String
        let sessionID: String
        let tokenID: Data
        let generation: UInt64
        let readAccount: JSONValue
        let programRead: JSONValue
        let approvalList: JSONValue
        let faucetClaim: JSONValue
    }

    private static let required = [
        "LAYERX_AGENT_GATEWAY_URL", "LAYERX_AGENT_GATEWAY_KEY_ID", "LAYERX_AGENT_GATEWAY_KEY",
        "LAYERX_AGENT_TENANT", "LAYERX_AGENT_SESSION_ID", "LAYERX_AGENT_TOKEN_ID", "LAYERX_AGENT_GENERATION",
        "LAYERX_AGENT_READ_ACCOUNT_REQUEST", "LAYERX_AGENT_PROGRAM_READ_REQUEST",
        "LAYERX_AGENT_APPROVAL_LIST_REQUEST", "LAYERX_AGENT_FAUCET_CLAIM_REQUEST",
    ]

    private func probe() throws -> Probe {
        let environment = ProcessInfo.processInfo.environment
        let missing = Self.required.filter { (environment[$0] ?? "").isEmpty }
        guard missing.isEmpty else {
            XCTFail("agent envelope probe refused: missing \(missing.joined(separator: ","))")
            throw PlatformSDKError(code: .invalidArgument, retry: .never)
        }
        func json(_ name: String) throws -> JSONValue {
            let value = try JSONDecoder().decode(JSONValue.self, from: Data(environment[name]!.utf8))
            guard value.objectValue != nil else { throw PlatformSDKError(code: .invalidArgument, retry: .never) }
            return value
        }
        guard let url = URL(string: environment["LAYERX_AGENT_GATEWAY_URL"]!),
              let token = Self.hexBytes(environment["LAYERX_AGENT_TOKEN_ID"]!),
              let generationText = environment["LAYERX_AGENT_GENERATION"],
              let generation = UInt64(generationText), String(generation) == generationText else {
            XCTFail("agent envelope probe refused: malformed gateway url, token id or generation")
            throw PlatformSDKError(code: .invalidArgument, retry: .never)
        }
        return Probe(
            baseURL: url, gatewayKeyID: environment["LAYERX_AGENT_GATEWAY_KEY_ID"]!,
            gatewayKey: Data(environment["LAYERX_AGENT_GATEWAY_KEY"]!.utf8),
            tenant: environment["LAYERX_AGENT_TENANT"]!, sessionID: environment["LAYERX_AGENT_SESSION_ID"]!,
            tokenID: token, generation: generation,
            readAccount: try json("LAYERX_AGENT_READ_ACCOUNT_REQUEST"),
            programRead: try json("LAYERX_AGENT_PROGRAM_READ_REQUEST"),
            approvalList: try json("LAYERX_AGENT_APPROVAL_LIST_REQUEST"),
            faucetClaim: try json("LAYERX_AGENT_FAUCET_CLAIM_REQUEST"))
    }

    private func transport(_ probe: Probe, generation: UInt64? = nil, withCredential: Bool = true) throws
        -> AgentEnvelopeTransport {
        let credential = withCredential
            ? try AgentSessionCredential(tenant: probe.tenant, sessionID: probe.sessionID, tokenID: probe.tokenID,
                generation: generation ?? probe.generation)
            : nil
        return try AgentEnvelopeTransport(baseURL: probe.baseURL,
            gatewayKey: LayerXKeyCredential(keyID: probe.gatewayKeyID, secret: probe.gatewayKey),
            credential: credential)
    }

    private func call(_ operation: PlatformOperation, _ request: JSONValue,
                      idempotencyKey: IdempotencyKey? = nil) -> TransportCall {
        TransportCall(operation: operation, request: request, pathParameters: [:], idempotencyKey: idempotencyKey)
    }

    func testSdkSwiftReadAccountProgramReadAndApprovalListThroughGateway() async throws {
        let probe = try probe()
        let agent = try transport(probe)
        for (operation, request) in [
            (PlatformOperation.agentReadAccount, probe.readAccount),
            (.agentProgramDiscover, probe.programRead),
            (.agentApprovalList, probe.approvalList),
        ] {
            let result = try await agent.sendEnvelope(call(operation, request))
            XCTAssertTrue(AgentEnvelopeTransport.validVerification(result.verificationStatus))
            XCTAssertFalse(result.requestID.isEmpty)
        }
    }

    func testSdkSwiftFaucetClaimIsRetiredUnavailableCapability() async throws {
        let probe = try probe()
        let key = try IdempotencyKey(String(UInt64.random(in: 1...UInt64.max), radix: 16)
            .padding(toLength: 64, withPad: "0", startingAt: 0))
        do {
            _ = try await transport(probe).sendEnvelope(call(.agentFaucetClaim, probe.faucetClaim, idempotencyKey: key))
            XCTFail("faucet.claim was accepted")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .unavailableCapability)
            XCTAssertEqual(error.retry, .never)
        }
    }

    func testSdkSwiftWrongGenerationIsRefusedBySessionAuthority() async throws {
        let probe = try probe()
        guard probe.generation < UInt64.max else { return XCTFail("generation has no successor") }
        do {
            _ = try await transport(probe, generation: probe.generation + 1)
                .sendEnvelope(call(.agentReadAccount, probe.readAccount))
            XCTFail("stale generation was accepted")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .policyRefusal)
        }
    }

    func testSdkSwiftRefusesCatalogueCallWithoutSessionCredential() async throws {
        let probe = try probe()
        do {
            _ = try await transport(probe, withCredential: false).sendEnvelope(call(.agentReadAccount, probe.readAccount))
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
