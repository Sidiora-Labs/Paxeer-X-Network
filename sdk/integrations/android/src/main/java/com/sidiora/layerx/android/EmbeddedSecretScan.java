package com.sidiora.layerx.android;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;
import java.util.Set;

/** CI entry point that fails a build whose Android artifact carries credential material. */
public final class EmbeddedSecretScan {
    private EmbeddedSecretScan() {}

    public static void main(String[] arguments) {
        if (arguments.length < 1 || arguments.length > 2) {
            System.err.println("usage: layerx-android-secret-scan <artifact-path> [declared-keys.json]");
            System.exit(64);
            return;
        }
        Set<String> exempt = Set.of();
        if (arguments.length == 2) {
            try {
                exempt = PublishableConfiguration.ofJsonFile(Path.of(arguments[1])).exemptScannerValues();
            } catch (MobileIntegrationException error) {
                System.err.println("layerx-android-secret-scan: declared keys refused");
                System.exit(65);
                return;
            }
        }
        List<EmbeddedSecretDetector.Finding> findings;
        try {
            findings = EmbeddedSecretDetector.scanArtifact(Path.of(arguments[0]), exempt);
        } catch (IOException | MobileIntegrationException error) {
            System.err.println("layerx-android-secret-scan: cannot read " + arguments[0]);
            System.exit(66);
            return;
        }
        for (EmbeddedSecretDetector.Finding finding : findings) {
            System.err.println("layerx-android-secret-scan: " + finding);
        }
        if (findings.isEmpty()) {
            System.out.println("layerx-android-secret-scan: no embedded secret material in " + arguments[0]);
            System.exit(0);
            return;
        }
        System.exit(1);
    }
}
