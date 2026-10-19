import Foundation
import LayerXSDK
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

private let maximumBrokerResponseBytes = 64 * 1024
private let maximumSessionLifetimeMilliseconds: Int64 = 24 * 60 * 60 * 1_000
private let refreshMarginMilliseconds: Int64 = 30_000

public struct EphemeralSessionToken: Sendable, CustomStringConvertible {
    private let bytes: Data
    public let issuedAtMilliseconds: Int64
    public let expiresAtMilliseconds: Int64

    init(bytes: Data, issuedAtMilliseconds: Int64, expiresAtMilliseconds: Int64) throws {
        guard !bytes.isEmpty, bytes.count <= 4_096,
              let value = String(data: bytes, encoding: .utf8),
              !value.contains("\0"), !value.contains("\r"), !value.contains("\n"),
              EmbeddedSecretDetector.providerCredentialRule(value) == nil,
              issuedAtMilliseconds > 0,
              expiresAtMilliseconds > issuedAtMilliseconds,
              expiresAtMilliseconds - issuedAtMilliseconds <= maximumSessionLifetimeMilliseconds else {
            throw MobileIntegrationError(.invalidSession)
        }
        self.bytes = bytes
        self.issuedAtMilliseconds = issuedAtMilliseconds
        self.expiresAtMilliseconds = expiresAtMilliseconds
    }

    public func isUsable(atMilliseconds now: Int64) -> Bool {
        now + refreshMarginMilliseconds < expiresAtMilliseconds
    }

    public func accessToken() throws -> AccessToken {
        do {
            return try AccessToken(bytes)
        } catch {
            throw MobileIntegrationError(.invalidSession)
        }
    }

    public var description: String { "[REDACTED]" }
}

public protocol SessionTokenProvider: Sendable {
    func token() async throws -> EphemeralSessionToken
    func invalidate() async
}

public final class BrokeredSessionTokenProvider: SessionTokenProvider, @unchecked Sendable {
    private static let forbiddenHeaderNames: Set<String> = [
        "authorization", "proxy-authorization", "x-api-key", "api-key", "x-auth-token",
        "x-access-token", "x-secret", "x-signature",
    ]

    private let brokerURL: URL
    private let session: URLSession
    private let audience: String
    private let now: @Sendable () -> Int64
    private let lock = NSLock()
    private var cached: EphemeralSessionToken?

    public init(
        brokerURL: URL,
        session: URLSession = .shared,
        audience: String = "layerx-human-api",
        now: (@Sendable () -> Int64)? = nil
    ) throws {
        guard brokerURL.user == nil, brokerURL.password == nil, brokerURL.query == nil, brokerURL.fragment == nil,
              let host = brokerURL.host, !host.isEmpty,
              brokerURL.scheme == "https" || (brokerURL.scheme == "http" && Self.isLoopback(host)),
              !audience.isEmpty, audience.utf8.count <= 128,
              audience.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "." || $0 == "_") }) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        try Self.rejectEmbeddedCredentials(in: session)
        self.brokerURL = brokerURL
        self.session = session
        self.audience = audience
        self.now = now ?? { Int64(Date().timeIntervalSince1970 * 1_000) }
    }

    public func token() async throws -> EphemeralSessionToken {
        let timestamp = now()
        lock.lock()
        let current = cached
        lock.unlock()
        if let current, current.isUsable(atMilliseconds: timestamp) {
            return current
        }
        let issued = try await request(atMilliseconds: timestamp)
        lock.lock()
        cached = issued
        lock.unlock()
        return issued
    }

    public func invalidate() async {
        lock.lock()
        cached = nil
        lock.unlock()
    }

    private func request(atMilliseconds timestamp: Int64) async throws -> EphemeralSessionToken {
        var request = URLRequest(url: brokerURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("layerx-ios/0.1.0", forHTTPHeaderField: "User-Agent")
        request.httpBody = try JSONEncoder().encode(BrokerRequest(audience: audience))

        let (data, response) = try await perform(request)
        guard let http = response as? HTTPURLResponse, (200..<300).contains(http.statusCode),
              data.count <= maximumBrokerResponseBytes,
              let decoded = try? JSONDecoder().decode(BrokerResponse.self, from: data) else {
            throw MobileIntegrationError(.invalidSession)
        }
        let issuedAt = decoded.issuedAtMilliseconds ?? timestamp
        return try EphemeralSessionToken(
            bytes: Data(decoded.sessionToken.utf8),
            issuedAtMilliseconds: issuedAt,
            expiresAtMilliseconds: decoded.expiresAtMilliseconds
        )
    }

    private func perform(_ request: URLRequest) async throws -> (Data, URLResponse) {
        let box = SessionTaskBox()
        return try await withTaskCancellationHandler(operation: {
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
                box.install(task)
                task.resume()
            }
        }, onCancel: {
            box.cancel()
        })
    }

    private static func rejectEmbeddedCredentials(in session: URLSession) throws {
        guard let headers = session.configuration.httpAdditionalHeaders else { return }
        for (name, value) in headers {
            let headerName = (name as? String ?? String(describing: name)).lowercased()
            if forbiddenHeaderNames.contains(headerName) || EmbeddedSecretDetector.isSecretShapedName(headerName) {
                throw MobileIntegrationError(.embeddedSecret)
            }
            if let text = value as? String, EmbeddedSecretDetector.providerCredentialRule(text) != nil {
                throw MobileIntegrationError(.embeddedSecret)
            }
        }
    }

    private static func isLoopback(_ host: String) -> Bool {
        let normalized = host.lowercased()
        return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1" || normalized == "[::1]"
    }
}

private struct BrokerRequest: Encodable {
    let audience: String
}

private struct BrokerResponse: Decodable {
    let sessionToken: String
    let expiresAtMilliseconds: Int64
    let issuedAtMilliseconds: Int64?

    enum CodingKeys: String, CodingKey {
        case sessionToken = "session_token"
        case expiresAtMilliseconds = "expires_at_ms"
        case issuedAtMilliseconds = "issued_at_ms"
    }
}

private final class SessionTaskBox: @unchecked Sendable {
    private let lock = NSLock()
    private var task: URLSessionTask?
    private var cancelled = false

    func install(_ task: URLSessionTask) {
        lock.lock()
        self.task = task
        let shouldCancel = cancelled
        lock.unlock()
        if shouldCancel { task.cancel() }
    }

    func cancel() {
        lock.lock()
        cancelled = true
        let task = task
        lock.unlock()
        task?.cancel()
    }
}
