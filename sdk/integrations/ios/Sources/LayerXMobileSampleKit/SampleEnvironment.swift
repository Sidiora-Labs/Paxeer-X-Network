import Foundation
import LayerXMobile
import LayerXSDK
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public enum SampleEnvironment {
    public static func configuration(from environment: [String: String] = ProcessInfo.processInfo.environment) throws -> PublishableConfiguration {
        try PublishableConfiguration(environment: environment)
    }

    #if canImport(Darwin)
    public static func configuration(bundle: Bundle) throws -> PublishableConfiguration {
        guard let raw = bundle.object(forInfoDictionaryKey: "LayerX") as? [String: Any] else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        var declared: [String: String] = [:]
        for (name, value) in raw {
            guard let text = value as? String else {
                throw MobileIntegrationError(.invalidConfiguration)
            }
            declared[name] = text
        }
        return try PublishableConfiguration(declaredKeys: declared)
    }
    #endif

    public static func session(timeoutSeconds: Double) -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = timeoutSeconds
        configuration.httpAdditionalHeaders = nil
        configuration.httpCookieAcceptPolicy = .onlyFromMainDocumentDomain
        return URLSession(configuration: configuration)
    }
}

public struct RelayReceiptResolver: MobileReceiptResolver, @unchecked Sendable {
    private let relayURL: URL
    private let session: URLSession

    public init(relayURL: URL, session: URLSession) throws {
        guard relayURL.user == nil, relayURL.password == nil, relayURL.query == nil, relayURL.fragment == nil,
              let host = relayURL.host, !host.isEmpty,
              relayURL.scheme == "https" || (relayURL.scheme == "http" && Self.isLoopback(host)) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        self.relayURL = relayURL
        self.session = session
    }

    public func resolve(receiptReference: String) async throws -> MobileReceiptEvidence {
        var request = URLRequest(url: relayURL.appendingPathComponent(receiptReference))
        request.httpMethod = "GET"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("layerx-ios/0.1.0", forHTTPHeaderField: "User-Agent")
        let (data, response) = try await perform(request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode),
              data.count <= 4 * 1024 * 1024,
              let payload = try? JSONDecoder().decode(RelayReceiptPayload.self, from: data),
              let canonical = Data(base64Encoded: payload.canonicalReceiptBase64) else {
            throw MobileIntegrationError(.decodeFailure)
        }
        return MobileReceiptEvidence(
            canonicalReceipt: canonical,
            authorizedBatch: AuthorizedReceiptBatch(
                batchID: try Self.hex32(payload.authorizedBatch.batchID),
                asset: try Self.hex32(payload.authorizedBatch.asset),
                previousStateRoot: try Self.hex32(payload.authorizedBatch.previousStateRoot),
                resultingStateRoot: try Self.hex32(payload.authorizedBatch.resultingStateRoot),
                sequencerPublicKey: try Self.hex32(payload.authorizedBatch.sequencerPublicKey)
            )
        )
    }

    private func perform(_ request: URLRequest) async throws -> (Data, URLResponse) {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<(Data, URLResponse), Error>) in
            let task = session.dataTask(with: request) { data, response, error in
                if error != nil {
                    continuation.resume(throwing: MobileIntegrationError(.transportFailure))
                    return
                }
                guard let data, let response else {
                    continuation.resume(throwing: MobileIntegrationError(.transportFailure))
                    return
                }
                continuation.resume(returning: (data, response))
            }
            task.resume()
        }
    }

    public static func hex32(_ value: String) throws -> Data {
        let characters = Array(value)
        guard characters.count == 64, characters.allSatisfy({ $0.isASCII && $0.isHexDigit }) else {
            throw MobileIntegrationError(.decodeFailure)
        }
        var bytes = Data()
        bytes.reserveCapacity(32)
        for index in stride(from: 0, to: 64, by: 2) {
            guard let byte = UInt8(String(characters[index...index + 1]), radix: 16) else {
                throw MobileIntegrationError(.decodeFailure)
            }
            bytes.append(byte)
        }
        return bytes
    }

    private static func isLoopback(_ host: String) -> Bool {
        let normalized = host.lowercased()
        return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1" || normalized == "[::1]"
    }
}

private struct RelayReceiptPayload: Decodable {
    struct Batch: Decodable {
        let batchID: String
        let asset: String
        let previousStateRoot: String
        let resultingStateRoot: String
        let sequencerPublicKey: String

        enum CodingKeys: String, CodingKey {
            case batchID = "batch_id"
            case asset
            case previousStateRoot = "previous_state_root"
            case resultingStateRoot = "resulting_state_root"
            case sequencerPublicKey = "sequencer_public_key"
        }
    }

    let canonicalReceiptBase64: String
    let authorizedBatch: Batch

    enum CodingKeys: String, CodingKey {
        case canonicalReceiptBase64 = "canonical_receipt_base64"
        case authorizedBatch = "authorized_batch"
    }
}
