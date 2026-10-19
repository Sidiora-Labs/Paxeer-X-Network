import Foundation
import LayerXSDK

public struct MobileReceiptEvidence: Sendable {
    public let canonicalReceipt: Data
    public let authorizedBatch: AuthorizedReceiptBatch

    public init(canonicalReceipt: Data, authorizedBatch: AuthorizedReceiptBatch) {
        self.canonicalReceipt = canonicalReceipt
        self.authorizedBatch = authorizedBatch
    }
}

public struct MobileSettlementExpectation: Sendable, Equatable {
    public let asset: Data
    public let recipient: Data
    public let amount: String

    public init(asset: Data, recipient: Data, amount: String) throws {
        guard asset.count == 32, recipient.count == 32,
              !amount.isEmpty, amount.utf8.allSatisfy({ $0 >= 48 && $0 <= 57 }),
              amount == "0" || amount.first != "0" else {
            throw MobileIntegrationError(.invalidConfiguration)
        }
        self.asset = asset
        self.recipient = recipient
        self.amount = amount
    }
}

public enum MobileSettlementState: Sendable, Equatable {
    case pending(reference: String)
    case verified(level: String, receiptDigest: String)
    case refused(code: String)
}

public protocol MobileReceiptResolver: Sendable {
    func resolve(receiptReference: String) async throws -> MobileReceiptEvidence
}

public struct ReceiptGate: Sendable {
    private let receipts: MobileReceiptResolver

    public init(receipts: MobileReceiptResolver) {
        self.receipts = receipts
    }

    public func settle(
        receiptReference: String,
        expecting expectation: MobileSettlementExpectation
    ) async throws -> MobileSettlementState {
        guard !receiptReference.isEmpty, receiptReference.utf8.count <= 512, !receiptReference.contains("\0") else {
            throw MobileIntegrationError(.decodeFailure)
        }
        let evidence = try await receipts.resolve(receiptReference: receiptReference)
        return try await verify(evidence: evidence, expecting: expectation)
    }

    public func verify(
        evidence: MobileReceiptEvidence,
        expecting expectation: MobileSettlementExpectation
    ) async throws -> MobileSettlementState {
        let verification: ReceiptVerification
        do {
            verification = try await LocalVerifier.verifyReceipt(evidence.canonicalReceipt, authorized: evidence.authorizedBatch)
        } catch {
            throw MobileIntegrationError(.verificationFailure)
        }
        guard verification.receipt.asset == expectation.asset,
              verification.receipt.to == expectation.recipient,
              decimalString(verification.receipt.amount) == expectation.amount else {
            throw MobileIntegrationError(.verificationFailure)
        }
        return .verified(level: verification.level, receiptDigest: hexadecimal(verification.receiptDigest))
    }

    public func project(journey: JSONValue, expecting expectation: MobileSettlementExpectation) async throws -> MobileSettlementState {
        guard let object = journey.objectValue, case let .string(state)? = object["state"] else {
            throw MobileIntegrationError(.decodeFailure)
        }
        switch state {
        case "settled", "completed":
            guard case let .string(reference)? = object["receipt_ref"] else {
                throw MobileIntegrationError(.decodeFailure)
            }
            return try await settle(receiptReference: reference, expecting: expectation)
        case "failed", "expired", "refused":
            return .refused(code: state)
        default:
            guard case let .string(reference)? = object["journey_id"] else {
                throw MobileIntegrationError(.decodeFailure)
            }
            return .pending(reference: reference)
        }
    }
}

public func decimalString(_ value: UInt128Value) -> String {
    var high = value.high
    var low = value.low
    if high == 0 && low == 0 { return "0" }
    var digits: [UInt8] = []
    while high != 0 || low != 0 {
        let quotientHigh = high / 10
        let remainderHigh = high % 10
        let (quotientLow, remainder) = UInt64(10).dividingFullWidth((high: remainderHigh, low: low))
        digits.append(UInt8(48 + remainder))
        high = quotientHigh
        low = quotientLow
    }
    return String(decoding: digits.reversed(), as: UTF8.self)
}

public func hexadecimal(_ bytes: Data) -> String {
    bytes.map { String(format: "%02x", $0) }.joined()
}
