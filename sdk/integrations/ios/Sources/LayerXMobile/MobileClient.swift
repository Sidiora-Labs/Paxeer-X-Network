import Foundation
import LayerXSDK
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public final class LayerXMobileClient: @unchecked Sendable {
    private let configuration: PublishableConfiguration
    private let sessions: SessionTokenProvider
    private let session: URLSession
    private let telemetry: SDKTelemetry?

    public init(
        configuration: PublishableConfiguration,
        sessions: SessionTokenProvider,
        session: URLSession = .shared,
        telemetry: SDKTelemetry? = nil
    ) {
        self.configuration = configuration
        self.sessions = sessions
        self.session = session
        self.telemetry = telemetry
    }

    public convenience init(configuration: PublishableConfiguration, session: URLSession = .shared) throws {
        let provider = try BrokeredSessionTokenProvider(brokerURL: configuration.sessionBrokerURL, session: session)
        self.init(configuration: configuration, sessions: provider, session: session)
    }

    public func version() async throws -> JSONValue {
        try await authorized { try await $0.humanVersion() }
    }

    public func profile() async throws -> JSONValue {
        try await authorized { try await $0.humanProfileGet() }
    }

    public func activity(_ request: JSONValue = .emptyObject) async throws -> JSONValue {
        try await authorized { try await $0.humanActivityQuery(request) }
    }

    public func activityEntry(id: String) async throws -> JSONValue {
        let entry = try pathValue(id)
        return try await authorized { try await $0.humanActivityEntry(pathParameters: ["entry_id": entry]) }
    }

    public func journeys() async throws -> JSONValue {
        try await authorized { try await $0.humanJourneyList() }
    }

    public func journey(id: String) async throws -> JSONValue {
        let journey = try pathValue(id)
        return try await authorized { try await $0.humanJourneyGet(pathParameters: ["journey_id": journey]) }
    }

    public func quote(_ request: JSONValue) async throws -> JSONValue {
        try await authorized { try await $0.humanMoveQuote(request) }
    }

    public func commit(_ request: JSONValue, idempotencyKey key: IdempotencyKey) async throws -> JSONValue {
        try await authorized { try await $0.humanMoveCommit(request, idempotencyKey: key) }
    }

    public func openStream() async throws -> StreamCursor {
        let response = try await authorized { try await $0.humanStreamOpen() }
        guard let object = response.objectValue, case let .string(cursor)? = object["cursor"] else {
            throw MobileIntegrationError(.decodeFailure)
        }
        return try StreamCursor(cursor)
    }

    public func events(after cursor: StreamCursor) async throws -> StreamPage<JSONValue> {
        let response = try await authorized {
            try await $0.humanStreamNext(pathParameters: ["cursor": cursor.rawValue])
        }
        guard let object = response.objectValue,
              case let .array(untrusted)? = object["events"],
              case let .string(next)? = object["next_cursor"] else {
            throw MobileIntegrationError(.decodeFailure)
        }
        var previous = cursor
        var events: [StreamEvent<JSONValue>] = []
        events.reserveCapacity(untrusted.count)
        for value in untrusted {
            guard let entry = value.objectValue, case let .string(identifier)? = entry["cursor"] else {
                throw MobileIntegrationError(.decodeFailure)
            }
            let advanced = try StreamCursor(identifier)
            events.append(StreamEvent(eventID: identifier, previousCursor: previous, cursor: advanced, value: value))
            previous = advanced
        }
        return StreamPage(requestedCursor: cursor, events: events, nextCursor: try StreamCursor(next))
    }

    public func streamSource() -> StreamPageSource<JSONValue> {
        { cursor in try await self.events(after: cursor) }
    }

    private func authorized<Value>(_ body: (PlatformClient) async throws -> Value) async throws -> Value {
        do {
            return try await perform(body)
        } catch let error as PlatformSDKError where error.code == .capabilityRefusal {
            await sessions.invalidate()
            return try await perform(body)
        }
    }

    private func perform<Value>(_ body: (PlatformClient) async throws -> Value) async throws -> Value {
        let token = try await sessions.token()
        let accessToken = try token.accessToken()
        defer { accessToken.destroy() }
        let transport = try HumanHTTPTransport(
            baseURL: configuration.serviceURL,
            session: session,
            accessToken: accessToken
        )
        return try await body(PlatformClient(transport: transport, telemetry: telemetry))
    }

    private func pathValue(_ value: String) throws -> String {
        guard !value.isEmpty, value.utf8.count <= 255, !value.contains("\0"),
              !value.contains("/"), !value.contains("?"), !value.contains("#") else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        return value
    }
}
