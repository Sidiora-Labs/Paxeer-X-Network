import Foundation

public struct StreamCursor: RawRepresentable, Hashable, Sendable, Codable, CustomStringConvertible {
    public let rawValue: String

    public init?(rawValue: String) {
        guard !rawValue.isEmpty, rawValue.utf8.count <= 512, !rawValue.contains("\0") else { return nil }
        self.rawValue = rawValue
    }

    public init(_ value: String) throws {
        guard let cursor = Self(rawValue: value) else {
            throw PlatformSDKError(code: .invalidArgument, retry: .never)
        }
        self = cursor
    }

    public var description: String { rawValue }
}

public struct StreamEvent<Value: Sendable>: Sendable {
    public let eventID: String
    public let previousCursor: StreamCursor
    public let cursor: StreamCursor
    public let value: Value

    public init(eventID: String, previousCursor: StreamCursor, cursor: StreamCursor, value: Value) {
        self.eventID = eventID
        self.previousCursor = previousCursor
        self.cursor = cursor
        self.value = value
    }
}

public struct StreamPage<Value: Sendable>: Sendable {
    public let requestedCursor: StreamCursor
    public let events: [StreamEvent<Value>]
    public let nextCursor: StreamCursor

    public init(requestedCursor: StreamCursor, events: [StreamEvent<Value>], nextCursor: StreamCursor) {
        self.requestedCursor = requestedCursor
        self.events = events
        self.nextCursor = nextCursor
    }
}

public typealias StreamPageSource<Value: Sendable> = @Sendable (StreamCursor) async throws -> StreamPage<Value>

public actor ResumableStream<Value: Sendable> {
    public private(set) var cursor: StreamCursor
    private var seenEventIDs: Set<String> = []

    public init(cursor: StreamCursor) {
        self.cursor = cursor
    }

    public func accept(_ page: StreamPage<Value>) throws -> [StreamEvent<Value>] {
        guard page.requestedCursor == cursor else {
            throw PlatformSDKError(code: .decodeFailure, retry: .never)
        }
        var expected = cursor
        var pageEventIDs: Set<String> = []
        var accepted: [StreamEvent<Value>] = []
        accepted.reserveCapacity(page.events.count)
        for event in page.events {
            guard !event.eventID.isEmpty,
                  event.previousCursor == expected,
                  event.cursor != event.previousCursor,
                  !seenEventIDs.contains(event.eventID),
                  pageEventIDs.insert(event.eventID).inserted else {
                throw PlatformSDKError(code: .decodeFailure, retry: .never)
            }
            accepted.append(event)
            expected = event.cursor
        }
        guard page.nextCursor == expected else {
            throw PlatformSDKError(code: .decodeFailure, retry: .never)
        }

        seenEventIDs.formUnion(pageEventIDs)
        cursor = page.nextCursor
        return accepted
    }

    public func next(from source: StreamPageSource<Value>) async throws -> [StreamEvent<Value>] {
        let requested = cursor
        let page = try await source(requested)
        return try accept(page)
    }

    public nonisolated func events(from source: @escaping StreamPageSource<Value>) -> AsyncThrowingStream<StreamEvent<Value>, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    while !Task.isCancelled {
                        for event in try await self.next(from: source) {
                            continuation.yield(event)
                        }
                    }
                    continuation.finish()
                } catch is CancellationError {
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

public struct HumanStreamSource: Sendable {
    private let client: PlatformClient

    public init(client: PlatformClient) {
        self.client = client
    }

    public func open() async throws -> StreamCursor {
        let response = try await client.humanStreamOpen()
        return try StreamCursor(try stringField("cursor", in: response))
    }

    public func next(after requested: StreamCursor) async throws -> StreamPage<JSONValue> {
        let response = try await client.humanStreamNext(pathParameters: ["cursor": requested.rawValue])
        guard let object = response.objectValue,
              case let .array(untrustedEvents)? = object["events"],
              case let .string(nextValue)? = object["next_cursor"] else { throw decodeFailure() }
        var previous = requested
        var events: [StreamEvent<JSONValue>] = []
        events.reserveCapacity(untrustedEvents.count)
        for value in untrustedEvents {
            let cursor = try StreamCursor(try stringField("cursor", in: value))
            events.append(StreamEvent(eventID: cursor.rawValue, previousCursor: previous, cursor: cursor, value: value))
            previous = cursor
        }
        return StreamPage(requestedCursor: requested, events: events, nextCursor: try StreamCursor(nextValue))
    }

    public func source() -> StreamPageSource<JSONValue> {
        { cursor in try await next(after: cursor) }
    }

    private func stringField(_ name: String, in value: JSONValue) throws -> String {
        guard let object = value.objectValue, case let .string(field)? = object[name] else { throw decodeFailure() }
        return field
    }

    private func decodeFailure() -> PlatformSDKError {
        PlatformSDKError(code: .decodeFailure, retry: .never)
    }
}
