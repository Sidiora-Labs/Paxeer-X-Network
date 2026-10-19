package com.sidiora.layerx.android;

import com.fasterxml.jackson.databind.JsonNode;
import com.sidiora.layerx.sdk.ProtocolAmount;
import com.sidiora.layerx.sdk.verify.LocalVerifier;
import java.math.BigInteger;
import java.security.MessageDigest;
import java.util.Objects;

/** Settlement is believed only after the device itself verifies the protocol receipt. */
public final class ReceiptGate {
    public record Evidence(byte[] canonicalReceipt, LocalVerifier.AuthorizedReceiptBatch authorizedBatch) {
        public Evidence {
            Objects.requireNonNull(canonicalReceipt, "canonicalReceipt");
            Objects.requireNonNull(authorizedBatch, "authorizedBatch");
        }
    }

    public record Expectation(byte[] asset, byte[] recipient, ProtocolAmount amount) {
        public Expectation {
            if (asset == null || asset.length != 32 || recipient == null || recipient.length != 32
                    || amount == null) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
            }
            asset = asset.clone();
            recipient = recipient.clone();
        }
    }

    public sealed interface State permits Pending, Verified, Refused {}
    public record Pending(String reference) implements State {}
    public record Verified(String level, String receiptDigest) implements State {}
    public record Refused(String code) implements State {}

    @FunctionalInterface public interface ReceiptResolver {
        Evidence resolve(String receiptReference);
    }

    private final ReceiptResolver receipts;

    public ReceiptGate(ReceiptResolver receipts) {
        this.receipts = Objects.requireNonNull(receipts, "receipts");
    }

    public State settle(String receiptReference, Expectation expectation) {
        if (receiptReference == null || receiptReference.isEmpty() || receiptReference.length() > 512
                || receiptReference.indexOf('\0') >= 0) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        return verify(receipts.resolve(receiptReference), expectation);
    }

    public State verify(Evidence evidence, Expectation expectation) {
        Objects.requireNonNull(evidence, "evidence");
        Objects.requireNonNull(expectation, "expectation");
        LocalVerifier.ReceiptVerification verification;
        try {
            verification = LocalVerifier.verifyReceipt(evidence.canonicalReceipt(), evidence.authorizedBatch());
        } catch (RuntimeException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.VERIFICATION_FAILURE);
        }
        LocalVerifier.ProtocolReceipt receipt = verification.receipt();
        BigInteger expected = expectation.amount().value();
        if (!MessageDigest.isEqual(receipt.asset(), expectation.asset())
                || !MessageDigest.isEqual(receipt.to(), expectation.recipient())
                || !expected.equals(receipt.amount())) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.VERIFICATION_FAILURE);
        }
        return new Verified(verification.level().wire(),
            PublishableConfiguration.hexadecimal(verification.receiptDigest()));
    }

    public State project(JsonNode journey, Expectation expectation) {
        Objects.requireNonNull(journey, "journey");
        if (!journey.isObject() || !journey.path("state").isTextual()) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
        String state = journey.path("state").textValue();
        return switch (state) {
            case "settled", "completed" -> {
                if (!journey.path("receipt_ref").isTextual()) {
                    throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
                }
                yield settle(journey.path("receipt_ref").textValue(), expectation);
            }
            case "failed", "expired", "refused" -> new Refused(state);
            default -> {
                if (!journey.path("journey_id").isTextual()) {
                    throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
                }
                yield new Pending(journey.path("journey_id").textValue());
            }
        };
    }
}
