import Foundation
import LayerXMobile

let arguments = CommandLine.arguments.dropFirst()
guard let target = arguments.first, arguments.count <= 2 else {
    FileHandle.standardError.write(Data("usage: layerx-ios-secret-scan <artifact-path> [declared-keys.json]\n".utf8))
    exit(64)
}

var exempt: Set<String> = []
if arguments.count == 2, let declared = arguments.dropFirst().first {
    do {
        exempt = try PublishableConfiguration(contentsOfJSONFile: URL(fileURLWithPath: declared)).exemptScannerValues
    } catch {
        FileHandle.standardError.write(Data("layerx-ios-secret-scan: declared keys refused\n".utf8))
        exit(65)
    }
}

let findings: [EmbeddedSecretFinding]
do {
    findings = try EmbeddedSecretDetector.scan(directoryAt: URL(fileURLWithPath: target), exempt: exempt)
} catch {
    FileHandle.standardError.write(Data("layerx-ios-secret-scan: cannot read \(target)\n".utf8))
    exit(66)
}

for finding in findings {
    FileHandle.standardError.write(Data("layerx-ios-secret-scan: \(finding)\n".utf8))
}

if findings.isEmpty {
    FileHandle.standardOutput.write(Data("layerx-ios-secret-scan: no embedded secret material in \(target)\n".utf8))
    exit(0)
}
exit(1)
