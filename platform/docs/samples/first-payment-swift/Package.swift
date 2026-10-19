// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "LayerXFirstPayment",
    platforms: [.iOS(.v15), .macOS(.v12)],
    dependencies: [
        .package(path: "../../../../sdk/platform/swift")
    ],
    targets: [
        .executableTarget(
            name: "FirstPayment",
            dependencies: ["LayerXSDK"],
            path: "Sources/FirstPayment"
        )
    ]
)
