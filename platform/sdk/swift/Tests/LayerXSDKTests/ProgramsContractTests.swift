import Crypto
import Foundation
import XCTest
@testable import LayerXSDK

final class ProgramsContractTests: XCTestCase {
    private final class ProgramTransport: PlatformTransport, @unchecked Sendable {
        let response: JSONValue
        init(_ response: JSONValue) { self.response = response }
        func send(_ call: TransportCall) async throws -> JSONValue {
            throw PlatformSDKError(code: .unavailableCapability, retry: .never)
        }
        func sendProgram(_ call: ProgramTransportCall) async throws -> JSONValue { response }
    }

    func testProgramsClientRequiresIndependentNonzeroSequencerPin() throws {
        let client = PlatformClient(transport: ProgramTransport(.emptyObject))
        XCTAssertThrowsError(try ProgramsClient(client: client, sequencerPublicKey: Data(repeating: 0, count: 32)))
        XCTAssertNoThrow(try ProgramsClient(client: client, sequencerPublicKey: Data(repeating: 1, count: 32)))
    }

    func testPendingReceiptMayOmitRetainedBytesButMustBindExpectedActivity() async throws {
        let key = String(repeating: "a", count: 64)
        let activity = Data(repeating: 0x11, count: 32)
        let value: JSONValue = .object([
            "state": .string("unknown"), "activity_id": .string(activity.hexString),
            "idempotency_key": .string(key),
        ])
        let programs = try ProgramsClient(client: PlatformClient(transport: ProgramTransport(value)),
            sequencerPublicKey: Data(repeating: 1, count: 32))
        let pending = try await programs.receipt(idempotencyKey: IdempotencyKey(key), expectedActivityID: activity,
            verificationLevel: "sequencer-signed")
        XCTAssertTrue(pending.isUnknown)
        XCTAssertNil(pending.retainedSignedActivity)
        do {
            _ = try await programs.receipt(idempotencyKey: IdempotencyKey(key),
                expectedActivityID: Data(repeating: 0x12, count: 32), verificationLevel: "sequencer-signed")
            XCTFail("mismatched activity selector was accepted")
        } catch let error as PlatformSDKError {
            XCTAssertEqual(error.code, .verificationFailure)
        }
    }

    func testOperationValueVerificationStatusMatrixIsExact() {
        let achieved: JSONValue = .object(["state": .string("Achieved"), "level": .string("SequencerSigned")])
        let discovery: JSONValue = .object(["state": .string("Unverified"), "requested": .string("SequencerSigned"),
            "achieved": .string("Unverified"),
            "reason": .string("server_side_receipt_verification_only")])
        let pending: JSONValue = .object(["state": .string("Unverified"), "requested": .string("SequencerSigned"),
            "achieved": .string("Unverified"),
            "reason": .string("receipt_pending")])
        let oldUnverified: JSONValue = .object(["state": .string("Unverified"), "level": .string("SequencerSigned"),
            "reason": .string("server_side_receipt_verification_only")])
        let unknown: JSONValue = .object(["state": .string("unknown")])
        let inFlight: JSONValue = .object(["state": .string("pending")])
        let terminal: JSONValue = .object(["state": .string("executed")])
        XCTAssertTrue(AgentHTTPTransport.validVerification("program.discover", value: .emptyObject, status: discovery))
        XCTAssertFalse(AgentHTTPTransport.validVerification("program.discover", value: .emptyObject, status: achieved))
        XCTAssertFalse(AgentHTTPTransport.validVerification("program.discover", value: .emptyObject, status: oldUnverified))
        XCTAssertTrue(AgentHTTPTransport.validVerification("program.receipt", value: unknown, status: pending))
        XCTAssertTrue(AgentHTTPTransport.validVerification("program.activity", value: inFlight, status: pending))
        XCTAssertFalse(AgentHTTPTransport.validVerification("program.receipt", value: unknown, status: achieved))
        XCTAssertFalse(AgentHTTPTransport.validVerification("program.call", value: inFlight, status: achieved))
        XCTAssertTrue(AgentHTTPTransport.validVerification("program.call", value: terminal, status: achieved))
        XCTAssertTrue(AgentHTTPTransport.validVerification("program.simulate", value: .emptyObject, status: achieved))
        XCTAssertFalse(AgentHTTPTransport.validVerification("program.simulate", value: .emptyObject, status: discovery))
    }

    func testTransferSetV1AndV2ProduceTheSameCanonicalKernelRoot() throws {
        let v1 = transferAuthorization(version: 1)
        let v2 = transferAuthorization(version: 2)
        let rootV1 = try ProgramsWireTestSupport.authorizationRoot(v1)
        let rootV2 = try ProgramsWireTestSupport.authorizationRoot(v2)
        XCTAssertEqual(rootV1, rootV2)
        XCTAssertTrue(rootV1.contains(where: { $0 != 0 }))
        var mutated = v2; mutated[mutated.count - 33] ^= 1
        XCTAssertNotEqual(try ProgramsWireTestSupport.authorizationRoot(mutated), rootV2)
    }

    func testOccupancyV1V2V3AndAggregateBindings() throws {
        let asset = Data(repeating: 0x66, count: 32)
        for version in 1...3 {
            let binding = try ProgramsWireTestSupport.occupancyBinding(emptyOccupancy(version: version), asset: asset)
            XCTAssertEqual(binding.0, UInt128Value(high: 0, low: 0))
            XCTAssertEqual(binding.1, UInt128Value(high: 0, low: 0))
            XCTAssertEqual(binding.2, Data(repeating: 0, count: 32))
        }
        let evidence = chargedOccupancy()
        let binding = try ProgramsWireTestSupport.occupancyBinding(evidence, asset: asset)
        XCTAssertEqual(binding.0, UInt128Value(high: 0, low: 3))
        XCTAssertEqual(binding.1, UInt128Value(high: 0, low: 6))
        XCTAssertTrue(binding.2.contains(where: { $0 != 0 }))
        var mutated = evidence
        let declaredFeeLowByte = Data("LXP/storage-occupancy-settlement/v3\0".utf8).count + 8 + 4 + 7 * 8 + 16 + 15
        mutated[declaredFeeLowByte] ^= 1
        XCTAssertThrowsError(try ProgramsWireTestSupport.occupancyBinding(mutated, asset: asset))
    }

    func testNamedGuestABIPolicyAndNativeCodecBounds() throws {
        let fixture = try nativeCallFixture()
        let payload = try nativeTransportHex(fixture["payload_hex"] as? String)
        let native = try NativeProgramCall.decode(payload)
        for abi in [ProgramGuestABI.v1, .v2, .v3, .v4] {
            XCTAssertEqual(abi.capabilityEncoding, abi == .v1 ? 1 : 2)
            XCTAssertEqual(abi.accountProfile2Supported, abi != .v1)
            let call = nativeCall(native, abi: abi.rawValue)
            XCTAssertEqual(try NativeProgramCall.decode(call.encode()).guestABI, abi.rawValue)
        }
        for abi: UInt16 in [0, 5, UInt16.max] {
            XCTAssertNil(ProgramGuestABI(rawValue: abi))
            XCTAssertThrowsError(try nativeCall(native, abi: abi).encode())
        }
        XCTAssertThrowsError(try nativeCall(native, abi: 4, capacity: 1_048_577).encode())
        var trailing = payload; trailing.append(0)
        XCTAssertThrowsError(try NativeProgramCall.decode(trailing))
    }

    func testNamedGuestABILifecycleCodecsRetainWasmAndInterfaceBounds() throws {
        let fixture = try fixtureDocument("native-program-deploy-v3.json")
        let payload = try nativeTransportHex(fixture["payload_hex"] as? String)
        _ = try NativeProgramDeploy.decode(payload)
        let length = payload[100..<104].reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
        let wasm = Data(payload.suffix(Int(length)))
        let program = Data(payload.prefix(32)), hash = Data(payload[68..<100])
        let authority = Data(payload[36..<68])
        for abi: UInt16 in [1, 2, 3, 4] {
            let deploy = try NativeProgramDeploy(programID: program, guestABI: abi, policy: 1,
                authority: authority, newHash: hash, wasm: wasm)
            XCTAssertEqual(try NativeProgramDeploy.decode(deploy.encode()).encode(), deploy.encode())
            let upgrade = try NativeProgramUpgrade(programID: program, guestABI: abi,
                oldHash: hash, newHash: hash, wasm: wasm)
            XCTAssertEqual(try NativeProgramUpgrade.decode(upgrade.encode()).encode(), upgrade.encode())
        }
        for abi: UInt16 in [0, 5, UInt16.max] {
            XCTAssertThrowsError(try NativeProgramDeploy(programID: program, guestABI: abi, policy: 1,
                authority: authority, newHash: hash, wasm: wasm))
            XCTAssertThrowsError(try NativeProgramUpgrade(programID: program, guestABI: abi,
                oldHash: hash, newHash: hash, wasm: wasm))
        }
        XCTAssertThrowsError(try NativeProgramDeploy(programID: program, guestABI: 4, policy: 1,
            authority: authority, newHash: hash, programInterface: Data(repeating: 1, count: 953), wasm: wasm))
        XCTAssertThrowsError(try NativeProgramUpgrade(programID: program, guestABI: 4,
            oldHash: hash, newHash: Data(repeating: 0, count: 32), wasm: wasm))
    }

    func testNativeCallUsesExactSignedBinaryAndRecoverySelectors() throws {
        let fixture = try nativeCallFixture()
        let payload = try nativeTransportHex(fixture["payload_hex"] as? String)
        let signed = try nativeTransportHex(fixture["signed_activity_hex"] as? String)
        let key = try XCTUnwrap(fixture["idempotency_key_hex"] as? String)
        let activity = try XCTUnwrap(fixture["activity_id_hex"] as? String)
        let token = try AccessToken(Data("fixture-bearer".utf8)); defer { token.destroy() }
        let transport = try AgentHTTPTransport(baseURL: XCTUnwrap(URL(string: "http://127.0.0.1:8080/agent/v1")), accessToken: token)
        let body: JSONValue = .object(["payload": .string(payload.hexString), "signed_activity": .string(signed.hexString)])
        for operation in ["program.simulate", "program.call"] {
            let http = try transport.programRequest(.init(operation: operation, request: body,
                idempotencyKey: operation == "program.call" ? IdempotencyKey(key) : nil))
            XCTAssertEqual(http.url?.path, operation == "program.call" ? "/v1/programs/call" : "/v1/programs/simulate")
            XCTAssertEqual(http.httpMethod, "POST")
            XCTAssertEqual(http.httpBody, signed)
            XCTAssertEqual(http.value(forHTTPHeaderField: "Content-Type"), "application/octet-stream")
        }
        XCTAssertThrowsError(try transport.programRequest(.init(operation: "program.call", request: body)))
        XCTAssertThrowsError(try transport.programRequest(.init(operation: "program.call", request: body,
            idempotencyKey: IdempotencyKey(String(repeating: "a", count: 64)))))
        let recovery: JSONValue = .object(["idempotency_key": .string(key), "expected_activity_id": .string(activity),
            "requested_verification_level": .string("sequencer-signed")])
        let http = try transport.programRequest(.init(operation: "program.receipt", request: recovery,
            pathParameters: ["idempotency_key": key]))
        XCTAssertEqual(http.url?.path, "/v1/programs/receipts/by-idempotency/\(key)")
        XCTAssertEqual(http.httpMethod, "GET")
        XCTAssertEqual(try JSONDecoder().decode(JSONValue.self, from: XCTUnwrap(http.httpBody)), recovery)
        for operation in ["program.discover", "program.interface"] {
            let program = String(repeating: "1", count: 64)
            let request: JSONValue = .object(["program_id": .string(program),
                "requested_verification_level": .string("sequencer-signed")])
            let read = try transport.programRequest(.init(operation: operation, request: request,
                pathParameters: ["program_id": program]))
            XCTAssertEqual(read.httpMethod, "GET")
            XCTAssertEqual(read.url?.path, "/v1/programs/registry/\(program)" + (operation == "program.interface" ? "/interface" : ""))
            XCTAssertEqual(try JSONDecoder().decode(JSONValue.self, from: XCTUnwrap(read.httpBody)), request)
        }
    }

    func testSignedDiscoveryRequiresExactCanonicalProofAndIndependentPin() throws {
        let signer = try Curve25519.Signing.PrivateKey(rawRepresentation: Data(repeating: 0x42, count: 32))
        let pin = signer.publicKey.rawRepresentation
        let program = Data(repeating: 0x11, count: 32).hexString
        let achieved: JSONValue = .object(["state": .string("Achieved"), "level": .string("SequencerSigned")])
        for abi: UInt16 in [1, 2, 3, 4] {
            let object = try discoveryDocument(abi: abi, signer: signer)
            let verified = try XCTUnwrap(verifiedDiscovery(.object(object), programID: program,
                interface: false, now: 150, pinnedKey: pin).discovery)
            XCTAssertEqual(verified.abiVersion, abi)
            XCTAssertEqual(verified.verification, "sequencer-signed")
            XCTAssertTrue(AgentHTTPTransport.validVerification("program.discover", value: .object(object), status: achieved))
            XCTAssertFalse(AgentHTTPTransport.validVerification("program.interface", value: .object(object), status: achieved))
            XCTAssertThrowsError(try verifiedDiscovery(.object(object), programID: program,
                interface: false, now: 201, pinnedKey: pin))
            XCTAssertThrowsError(try verifiedDiscovery(.object(object), programID: program,
                interface: false, now: 150, pinnedKey: Data(repeating: 0x43, count: 32)))
            let mutations: [(String, JSONValue)] = [
                ("code_hash", .string(Data(repeating: 0x21, count: 32).hexString)),
                ("state_root", .string(Data(repeating: 0x51, count: 32).hexString)),
                ("observed_sequence", .string("8")), ("version", .integer(2)),
                ("discovery_signature", .string(Data(repeating: 0, count: 64).hexString)),
                ("unexpected", .integer(1))
            ]
            for (field, replacement) in mutations {
                var changed = object; changed[field] = replacement
                XCTAssertThrowsError(try verifiedDiscovery(.object(changed), programID: program,
                    interface: false, now: 150, pinnedKey: pin), field)
            }
            var halfProof = object; halfProof.removeValue(forKey: "discovery_signature")
            XCTAssertThrowsError(try verifiedDiscovery(.object(halfProof), programID: program,
                interface: false, now: 150, pinnedKey: pin))
            var unsigned = halfProof; unsigned.removeValue(forKey: "discovery_public_key")
            if abi == 1 || abi == 2 {
                XCTAssertEqual(try verifiedDiscovery(.object(unsigned), programID: program,
                    interface: false, now: 150, pinnedKey: pin).discovery?.verification,
                    "server-side-receipt-verification-only")
            } else {
                XCTAssertThrowsError(try verifiedDiscovery(.object(unsigned), programID: program,
                    interface: false, now: 150, pinnedKey: pin))
            }
        }
    }

    func testHigherABIInterfaceRequiresTheSameSignedDiscoveryHead() throws {
        let signer = try Curve25519.Signing.PrivateKey(rawRepresentation: Data(repeating: 0x42, count: 32))
        let pin = signer.publicKey.rawRepresentation
        for abi: UInt16 in [3, 4] {
            let document = try discoveryDocument(abi: abi, signer: signer)
            let program = try XCTUnwrap(document["program_id"]?.stringValue)
            let discovery = try XCTUnwrap(verifiedDiscovery(.object(document), programID: program,
                interface: false, now: 150, pinnedKey: pin).discovery)
            var object = document
            for field in ["lifecycle", "discovery_signature", "discovery_public_key", "deployment_receipt_digest"] {
                object.removeValue(forKey: field)
            }
            let bytes = try canonicalInterface(abi: abi)
            object["interface"] = .string(bytes.hexString)
            object["interface_digest"] = .string(Data(SHA256.hash(data: bytes)).hexString)
            object["receipt_digest"] = document["deployment_receipt_digest"]
            object["source"] = .object(["status": .string("unpublished")])
            object["verification"] = .string("deployment-interface-and-current-head-verified")
            let value = try XCTUnwrap(verifiedDiscovery(.object(object), programID: program,
                interface: true, now: 150, pinnedKey: pin).interface)
            XCTAssertNoThrow(try bindProgramInterface(value, discovery: discovery))
            XCTAssertEqual(value.verification, "server-side-receipt-verification-only")
            let mutations: [(String, JSONValue)] = [
                ("code_hash", .string(Data(repeating: 0x21, count: 32).hexString)),
                ("state_root", .string(Data(repeating: 0x51, count: 32).hexString)),
                ("receipt_digest", .string(Data(repeating: 0x61, count: 32).hexString)),
                ("observed_sequence", .string("8")), ("version", .integer(2)),
                ("abi_version", .integer(abi == 3 ? 4 : 3)), ("valid_through", .string("201"))
            ]
            for (field, replacement) in mutations {
                var changed = object; changed[field] = replacement
                let mismatch = try XCTUnwrap(verifiedDiscovery(.object(changed), programID: program,
                    interface: true, now: 150, pinnedKey: pin).interface)
                XCTAssertThrowsError(try bindProgramInterface(mismatch, discovery: discovery), field)
            }
            var oversized = object; oversized["interface"] = .string(Data(repeating: 1, count: 953).hexString)
            XCTAssertThrowsError(try verifiedDiscovery(.object(oversized), programID: program,
                interface: true, now: 150, pinnedKey: pin))
            var digestMismatch = object; digestMismatch["interface_digest"] = .string(Data(repeating: 0, count: 32).hexString)
            XCTAssertThrowsError(try verifiedDiscovery(.object(digestMismatch), programID: program,
                interface: true, now: 150, pinnedKey: pin))
        }
    }

    func testNativeGuestAdmissionDoesNotWidenSignedExecutionReceiptABI() async throws {
        let fixture = try fixtureDocument("receipt-programs-executed-v4.json")
        let batch = try XCTUnwrap(fixture["authorized_batch"] as? [String: Any])
        let authority = AuthorizedReceiptBatch(batchID: try nativeTransportHex(batch["batch_id_hex"] as? String),
            asset: try nativeTransportHex(batch["asset_hex"] as? String),
            previousStateRoot: try nativeTransportHex(batch["previous_state_root_hex"] as? String),
            resultingStateRoot: try nativeTransportHex(batch["resulting_state_root_hex"] as? String),
            sequencerPublicKey: try nativeTransportHex(batch["sequencer_public_key_hex"] as? String))
        let receipt = try nativeTransportHex(fixture["canonical_receipt_hex"] as? String)
        var activity = Data("LXP/v1/activity-id\0".utf8)
        activity.append(try nativeTransportHex(fixture["signed_activity_hex"] as? String))
        let expectedActivity = Data(SHA256.hash(data: activity))
        let terminal = try nativeTransportHex(fixture["terminal_payload_hex"] as? String)
        let graph = try nativeTransportHex(fixture["call_graph_hex"] as? String)
        for abi: UInt16 in [3, 4] {
            do {
                _ = try await ProgramsClient.verifyReceipt(receipt, authorized: authority,
                    expectedActivityID: expectedActivity, expectedGuestABIVersion: abi,
                    terminalPayload: terminal, callGraph: graph, protocolVersion: 3)
                XCTFail("native guest admission widened execution receipt ABI")
            } catch let error as PlatformSDKError {
                XCTAssertEqual(error.code, .invalidArgument)
            }
        }
    }

    private func nativeCallFixture() throws -> [String: Any] {
        try fixtureDocument("native-program-call-v3.json")
    }

    private func fixtureDocument(_ name: String) throws -> [String: Any] {
        var root = URL(fileURLWithPath: #filePath)
        for _ in 0..<6 { root.deleteLastPathComponent() }
        let bytes = try Data(contentsOf: root.appendingPathComponent("platform/sdk/conformance/fixtures").appendingPathComponent(name))
        return try XCTUnwrap(JSONSerialization.jsonObject(with: bytes) as? [String: Any])
    }

    private func canonicalInterface(abi: UInt16) throws -> Data {
        let fixture = try fixtureDocument("native-program-deploy-v3.json")
        let payload = try nativeTransportHex(fixture["payload_hex"] as? String)
        _ = try NativeProgramDeploy.decode(payload)
        let interfaceLength = payload[104..<108].reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
        var encoded = Data(payload[108..<108 + Int(interfaceLength)])
        let domainBytes = Data("LayerX/program-interface/v1\0".utf8).count
        encoded.replaceSubrange(domainBytes..<domainBytes + 32, with: Data(repeating: 0x22, count: 32))
        encoded.replaceSubrange(domainBytes + 32..<domainBytes + 34, with: word(abi.bigEndian))
        return encoded
    }

    private func nativeCall(_ value: NativeProgramCall, abi: UInt16, capacity: UInt32? = nil) -> NativeProgramCall {
        NativeProgramCall(programID: value.programID, guestABI: abi, entrypoint: value.entrypoint,
            calldata: value.calldata, capabilities: value.capabilities, accessDeclaration: value.accessDeclaration,
            responseCapacity: capacity ?? value.responseCapacity, resources: value.resources)
    }

    private func discoveryDocument(abi: UInt16, signer: Curve25519.Signing.PrivateKey) throws -> [String: JSONValue] {
        let program = Data(repeating: 0x11, count: 32), code = Data(repeating: 0x22, count: 32)
        let state = Data(repeating: 0x55, count: 32)
        var proof = Data("LayerX/program-discovery-proof/v1\0".utf8)
        proof.append(program); proof.append(1); proof.append(be32(1)); proof.append(code)
        proof.append(word(abi.bigEndian)); proof.append(be64(7)); proof.append(be64(100)); proof.append(be64(200)); proof.append(state)
        let digest = Data(SHA256.hash(data: proof))
        return ["program_id": .string(program.hexString), "lifecycle": .string("active"), "version": .integer(1),
            "code_hash": .string(code.hexString), "abi_version": .integer(Int64(abi)),
            "receipt_digest": .string(digest.hexString), "deployment_receipt_digest": .string(Data(repeating: 0x66, count: 32).hexString),
            "state_root": .string(state.hexString), "observed_sequence": .string("7"),
            "observed_at": .string("100"), "valid_through": .string("200"),
            "verification": .string("registry-receipt-and-current-head-verified"),
            "discovery_public_key": .string(signer.publicKey.rawRepresentation.hexString),
            "discovery_signature": .string(try signer.signature(for: digest).hexString)]
    }

    private func transferAuthorization(version: Int) -> Data {
        let program = Data(repeating: 1, count: 32); let principal = Data(repeating: 2, count: 32)
        let asset = Data(repeating: 4, count: 32); let destination = Data(repeating: 5, count: 32)
        var encoded = Data("LayerX/programs/402LXP/transfer-set/v\(version)\0".utf8)
        encoded.append(program); encoded.append(principal); encoded.append(Data(repeating: 3, count: 32))
        encoded.append(Data(repeating: 0, count: 9))
        var events = Data("LayerX/programs/events/v1\0".utf8); events.append(be32(0))
        encoded.append(be32(UInt32(events.count))); encoded.append(events); encoded.append(be64(0)); encoded.append(be64(1))
        encoded.append(Data(repeating: 0, count: 9))
        if version == 2 { encoded.append(1); encoded.append(principal) }
        encoded.append(asset); encoded.append(destination); encoded.append(be128(7)); encoded.append(program)
        return encoded
    }

    private func emptyOccupancy(version: Int) -> Data {
        var encoded = Data("LXP/storage-occupancy-settlement/v\(version)\0".utf8); encoded.append(be64(1))
        if version > 1 { encoded.append(be32(1)) }
        for value in 1...7 { encoded.append(be64(UInt64(value))) }
        if version == 3 {
            encoded.append(Data(repeating: 0, count: 16 * 4)); encoded.append(be32(0))
        } else {
            encoded.append(Data(repeating: 0, count: 16 * 2)); encoded.append(be64(0))
        }
        return encoded
    }

    private func chargedOccupancy() -> Data {
        let program = Data(repeating: 0x11, count: 32); let payer = Data(repeating: 0x77, count: 32)
        var encoded = Data("LXP/storage-occupancy-settlement/v3\0".utf8); encoded.append(be64(2)); encoded.append(be32(1))
        for value: UInt64 in [0, 0, 0, 0, 0, 0, 2] { encoded.append(be64(value)) }
        encoded.append(be128(3)); encoded.append(be128(6)); encoded.append(be128(6)); encoded.append(be128(0)); encoded.append(be32(1))
        encoded.append(65); encoded.append(program); encoded.append(0); encoded.append(payer)
        encoded.append(payer); encoded.append(program); encoded.append(Data(repeating: 0x88, count: 32))
        encoded.append(be64(1)); encoded.append(be64(2)); encoded.append(be64(3)); encoded.append(be64(3))
        encoded.append(be128(3)); encoded.append(be64(2)); encoded.append(be128(6)); encoded.append(be128(0))
        encoded.append(be128(6)); encoded.append(be128(0)); encoded.append(1); encoded.append(be128(0))
        encoded.append(be64(3)); encoded.append(be64(2)); encoded.append(be128(0)); encoded.append(Data(repeating: 0x99, count: 32))
        return encoded
    }

    private func be32(_ value: UInt32) -> Data { word(value.bigEndian) }
    private func be64(_ value: UInt64) -> Data { word(value.bigEndian) }
    private func be128(_ value: UInt64) -> Data { Data(repeating: 0, count: 8) + be64(value) }
    private func word<T>(_ value: T) -> Data { var copy = value; return withUnsafeBytes(of: &copy) { Data($0) } }
}

private extension Data {
    var hexString: String { map { String(format: "%02x", $0) }.joined() }
}
