import Foundation
import LayerXSDK
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

let settledStates: Set<String> = ["done", "done-finalised", "refused"]
let completedStates: Set<String> = ["done", "done-finalised"]

func fail(_ reason: String) -> Never {
    FileHandle.standardError.write(Data("first-payment-swift: \(reason)\n".utf8))
    exit(1)
}

func required(_ name: String) -> String {
    guard let value = ProcessInfo.processInfo.environment[name], !value.isEmpty else {
        fail("missing \(name)")
    }
    return value
}

func emit(_ value: [String: JSONValue]) {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    guard let encoded = try? encoder.encode(JSONValue.object(value)) else {
        fail("could not encode the report")
    }
    FileHandle.standardOutput.write(encoded)
    FileHandle.standardOutput.write(Data("\n".utf8))
}

func state(of journey: JSONValue) -> String {
    journey.objectValue?["state"]?.stringValue ?? ""
}

func receipts(in journey: JSONValue) -> [JSONValue] {
    guard case let .array(evidence)? = journey.objectValue?["evidence"] else { return [] }
    return evidence.compactMap { entry in
        guard entry.objectValue?["class"]?.stringValue == "layerx-receipt",
              let identifier = entry.objectValue?["evidence_id"]?.stringValue else { return nil }
        return .string(identifier)
    }
}

@main
struct FirstPayment {
    static func main() async {
        guard let serviceURL = URL(string: required("LAYERX_API_URL")) else {
            fail("invalid LAYERX_API_URL")
        }
        let apiToken = required("LAYERX_API_TOKEN")
        let source = required("LAYERX_SOURCE")
        let destination = required("LAYERX_DESTINATION")
        let money = JSONValue.object([
            "amount": .string(required("LAYERX_AMOUNT")),
            "currency": .string(required("LAYERX_CURRENCY")),
        ])
        guard let paymentKey = IdempotencyKey(rawValue: required("LAYERX_PAYMENT_KEY")) else {
            fail("invalid LAYERX_PAYMENT_KEY")
        }

        do {
            // layerx:begin integration
            let token = try AccessToken(Data(apiToken.utf8))
            let layerx = PlatformClient(transport: try HumanHTTPTransport(baseURL: serviceURL, accessToken: token))
            let quote = try await layerx.humanMoveQuote(.object(["source": .string(source), "destination": .string(destination), "money": money]))
            guard let quoteID = quote.objectValue?["quote_id"]?.stringValue else { fail("move quote omitted quote_id") }
            var journey = try await layerx.humanMoveCommit(.object(["quote_id": .string(quoteID)]), idempotencyKey: paymentKey)
            // layerx:end integration

            guard let journeyID = journey.objectValue?["journey_id"]?.stringValue else {
                fail("commit omitted journey_id")
            }
            var attempt = 0
            while attempt < 40 && !settledStates.contains(state(of: journey)) {
                try await Task.sleep(nanoseconds: 250_000_000)
                journey = try await layerx.humanJourneyGet(pathParameters: ["journey_id": journeyID])
                attempt += 1
            }
            token.destroy()

            var report: [String: JSONValue] = [
                "journey_id": .string(journeyID),
                "state": .string(state(of: journey)),
                "receipts": .array(receipts(in: journey)),
            ]
            if let refusal = journey.objectValue?["refusal"]?.objectValue {
                report["refused_by"] = refusal["refused_by"] ?? .null
                report["money_left"] = refusal["money_left"] ?? .null
            }
            emit(report)
            exit(completedStates.contains(state(of: journey)) ? 0 : 2)
        } catch let error as PlatformSDKError {
            fail("refused: \(error.code.rawValue)")
        } catch {
            fail("refused: transport-failure")
        }
    }
}
