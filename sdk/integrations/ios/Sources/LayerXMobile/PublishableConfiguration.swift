import Foundation

public struct PublishableConfiguration: Sendable, Equatable {
    public static let serviceURLKey = "layerx.service_url"
    public static let sessionBrokerURLKey = "layerx.session_broker_url"
    public static let eventPublicKeyPrefix = "layerx.event_public_key."
    public static let eventMaximumAgeSecondsKey = "layerx.event_max_age_seconds"
    public static let requestTimeoutSecondsKey = "layerx.request_timeout_seconds"

    public let serviceURL: URL
    public let sessionBrokerURL: URL
    public let eventPublicKeys: [String: Data]
    public let eventMaximumAgeMilliseconds: Int64
    public let requestTimeoutSeconds: Double

    public init(declaredKeys: [String: String]) throws {
        var serviceURL: URL?
        var brokerURL: URL?
        var publicKeys: [String: Data] = [:]
        var maximumAgeSeconds: Int64 = 300
        var timeoutSeconds: Double = 30

        for (name, value) in declaredKeys {
            guard !EmbeddedSecretDetector.isSecretShapedName(name) else {
                throw MobileIntegrationError(.embeddedSecret)
            }
            switch name {
            case Self.serviceURLKey:
                serviceURL = try Self.endpoint(value)
            case Self.sessionBrokerURLKey:
                brokerURL = try Self.endpoint(value)
            case Self.eventMaximumAgeSecondsKey:
                maximumAgeSeconds = try Self.bounded(value, minimum: 1, maximum: 3_600)
            case Self.requestTimeoutSecondsKey:
                timeoutSeconds = Double(try Self.bounded(value, minimum: 1, maximum: 300))
            default:
                guard name.hasPrefix(Self.eventPublicKeyPrefix) else {
                    throw MobileIntegrationError(.invalidConfiguration)
                }
                let identifier = String(name.dropFirst(Self.eventPublicKeyPrefix.count))
                guard Self.isKeyIdentifier(identifier), publicKeys[identifier] == nil else {
                    throw MobileIntegrationError(.invalidConfiguration)
                }
                publicKeys[identifier] = try Self.publicKey(value)
            }
        }

        guard let resolvedService = serviceURL, let resolvedBroker = brokerURL, !publicKeys.isEmpty else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        self.serviceURL = resolvedService
        self.sessionBrokerURL = resolvedBroker
        self.eventPublicKeys = publicKeys
        self.eventMaximumAgeMilliseconds = maximumAgeSeconds * 1_000
        self.requestTimeoutSeconds = timeoutSeconds
    }

    public init(contentsOfJSONFile url: URL) throws {
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        guard data.count <= 262_144,
              let decoded = try? JSONDecoder().decode([String: String].self, from: data) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        try self.init(declaredKeys: decoded)
    }

    public init(environment: [String: String]) throws {
        var declared: [String: String] = [:]
        for (name, value) in environment {
            guard let key = Self.declaredKey(forEnvironmentVariable: name) else { continue }
            guard declared[key] == nil else {
                throw MobileIntegrationError(.invalidConfiguration)
            }
            declared[key] = value
        }
        if environment["LAYERX_EVENT_KEY_ID"] != nil || environment["LAYERX_EVENT_PUBLIC_KEY"] != nil {
            guard let identifier = environment["LAYERX_EVENT_KEY_ID"],
                  let publicKey = environment["LAYERX_EVENT_PUBLIC_KEY"],
                  Self.isKeyIdentifier(identifier) else {
                throw MobileIntegrationError(.invalidConfiguration)
            }
            let key = Self.eventPublicKeyPrefix + identifier
            guard declared[key] == nil else {
                throw MobileIntegrationError(.invalidConfiguration)
            }
            declared[key] = publicKey
        }
        try self.init(declaredKeys: declared)
    }

    public static func declaredKey(forEnvironmentVariable name: String) -> String? {
        let prefix = "LAYERX_"
        guard name.hasPrefix(prefix) else { return nil }
        let remainder = name.dropFirst(prefix.count).lowercased()
        switch remainder {
        case "service_url": return serviceURLKey
        case "session_broker_url": return sessionBrokerURLKey
        case "event_max_age_seconds": return eventMaximumAgeSecondsKey
        case "request_timeout_seconds": return requestTimeoutSecondsKey
        default:
            let keyPrefix = "event_public_key_"
            guard remainder.hasPrefix(keyPrefix) else { return nil }
            let identifier = String(remainder.dropFirst(keyPrefix.count))
            return isKeyIdentifier(identifier) ? eventPublicKeyPrefix + identifier : nil
        }
    }

    public var exemptScannerValues: Set<String> {
        var values: Set<String> = [serviceURL.absoluteString, sessionBrokerURL.absoluteString]
        for (_, key) in eventPublicKeys {
            values.insert(key.map { String(format: "%02x", $0) }.joined())
        }
        return values
    }

    private static func endpoint(_ value: String) throws -> URL {
        guard EmbeddedSecretDetector.classify(value) == nil,
              value.utf8.count <= 2_048,
              let url = URL(string: value),
              url.user == nil, url.password == nil, url.query == nil, url.fragment == nil,
              let host = url.host, !host.isEmpty,
              url.scheme == "https" || (url.scheme == "http" && isLoopback(host)) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        return url
    }

    private static func publicKey(_ value: String) throws -> Data {
        let characters = Array(value)
        guard characters.count == 64,
              characters.allSatisfy({ $0.isASCII && $0.isHexDigit && !$0.isUppercase }) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        var bytes = Data()
        bytes.reserveCapacity(32)
        for index in stride(from: 0, to: 64, by: 2) {
            guard let byte = UInt8(String(characters[index...index + 1]), radix: 16) else {
                throw MobileIntegrationError(.invalidConfiguration)
            }
            bytes.append(byte)
        }
        guard bytes.contains(where: { $0 != 0 }) else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        return bytes
    }

    private static func bounded(_ value: String, minimum: Int64, maximum: Int64) throws -> Int64 {
        guard EmbeddedSecretDetector.classify(value) == nil,
              !value.isEmpty, value.utf8.allSatisfy({ $0 >= 48 && $0 <= 57 }),
              let parsed = Int64(value), parsed >= minimum, parsed <= maximum else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        return parsed
    }

    private static func isKeyIdentifier(_ value: String) -> Bool {
        guard !value.isEmpty, value.utf8.count <= 64 else {
            return false
        }
        return value.allSatisfy { character in
            character.isASCII && (character.isLetter || character.isNumber || character == "." || character == "-" || character == "_")
        }
    }

    private static func isLoopback(_ host: String) -> Bool {
        let normalized = host.lowercased()
        return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1" || normalized == "[::1]"
    }
}
