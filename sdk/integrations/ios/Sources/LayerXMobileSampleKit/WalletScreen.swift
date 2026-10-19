#if canImport(SwiftUI)
import Foundation
import LayerXMobile
import LayerXSDK
import SwiftUI

public struct WalletScreen: View {
    private let model: WalletModel
    private let quoteRequest: JSONValue
    private let expectation: MobileSettlementExpectation

    @State private var snapshot = WalletSnapshot()
    @State private var busy = false

    public init(model: WalletModel, quoteRequest: JSONValue, expectation: MobileSettlementExpectation) {
        self.model = model
        self.quoteRequest = quoteRequest
        self.expectation = expectation
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("LayerX Wallet").font(.title2).bold()
            LabeledRow(label: "Service", value: snapshot.serviceVersion.isEmpty ? "—" : snapshot.serviceVersion)
            LabeledRow(label: "Account", value: snapshot.displayName.isEmpty ? "—" : snapshot.displayName)
            LabeledRow(label: "Activity", value: "\(snapshot.activityCount)")
            LabeledRow(label: "Settlement", value: settlementText)
            if let refusal = snapshot.refusal {
                Text(refusal).font(.footnote).foregroundColor(.red)
            }
            Button(action: pay) {
                Text(busy ? "Working…" : "Pay and verify")
            }
            .disabled(busy)
            if !snapshot.deliveries.isEmpty {
                Text("Verified deliveries").font(.headline)
                ForEach(snapshot.deliveries.suffix(8), id: \.self) { delivery in
                    Text(delivery).font(.caption).lineLimit(1)
                }
            }
            Spacer()
        }
        .padding(20)
        .task {
            snapshot = await model.refresh()
        }
    }

    private var settlementText: String {
        switch snapshot.settlement {
        case .none: return "—"
        case let .some(.pending(reference)): return "pending \(reference)"
        case let .some(.verified(level, digest)): return "\(level) \(String(digest.prefix(16)))"
        case let .some(.refused(code)): return "refused \(code)"
        }
    }

    private func pay() {
        busy = true
        Task { @MainActor in
            defer { busy = false }
            guard let key = IdempotencyKey(rawValue: UUID().uuidString) else {
                snapshot = await model.current()
                return
            }
            snapshot = await model.pay(quote: quoteRequest, expecting: expectation, idempotencyKey: key)
        }
    }
}

private struct LabeledRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack {
            Text(label).font(.subheadline).foregroundColor(.secondary)
            Spacer()
            Text(value).font(.subheadline).multilineTextAlignment(.trailing)
        }
    }
}
#endif
