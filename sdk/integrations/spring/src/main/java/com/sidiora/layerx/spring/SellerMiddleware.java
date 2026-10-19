package com.sidiora.layerx.spring;

import com.fasterxml.jackson.databind.JsonNode;
import com.sidiora.layerx.sdk.PlatformSdkException;
import com.sidiora.layerx.sdk.verify.LocalVerifier;
import java.io.IOException;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

public final class SellerMiddleware {
    public sealed interface SellerDecision
        permits PaymentRequiredDecision, PendingDecision, RefusedDecision, ReleasedDecision {
        int status();
    }

    public record PaymentRequiredDecision(X402.PaymentRequired body, String header) implements SellerDecision {
        @Override public int status() { return 402; }
    }

    public record PendingDecision() implements SellerDecision {
        @Override public int status() { return 202; }
    }

    public record RefusedDecision(X402.SettlementResponse settlement, String header) implements SellerDecision {
        @Override public int status() { return 402; }
    }

    public record ReleasedDecision(X402.SettlementResponse settlement, String header,
                                   LocalVerifier.ReceiptVerification verification, LayerXResource resource)
        implements SellerDecision {
        @Override public int status() { return 200; }
    }

    public sealed interface SettlementOutcome permits Pending, Refused, Settled {}

    public record Pending() implements SettlementOutcome {}

    public record Refused(String reason) implements SettlementOutcome {}

    public record Settled(byte[] canonicalReceipt, LocalVerifier.AuthorizedReceiptBatch authorizedBatch)
        implements SettlementOutcome {}

    public record SettlementRequest(String principal, X402.PaymentPayload payload,
                                    X402.PaymentRequirements requirements, String idempotencyKey,
                                    String requestDigest) {}

    @FunctionalInterface
    public interface PaymentAuthority {
        SettlementOutcome settle(SettlementRequest request);
    }

    @FunctionalInterface
    public interface AuthorizedBatchResolver {
        LocalVerifier.AuthorizedReceiptBatch resolve(byte[] canonicalReceipt);
    }

    public static final class ReceiptPayloadAuthority implements PaymentAuthority {
        private final AuthorizedBatchResolver authorizedBatches;

        public ReceiptPayloadAuthority(AuthorizedBatchResolver authorizedBatches) {
            this.authorizedBatches = Objects.requireNonNull(authorizedBatches, "authorizedBatches");
        }

        @Override
        public SettlementOutcome settle(SettlementRequest request) {
            X402.ReceiptEvidence evidence = X402.parseReceiptEvidence(request.payload().payload());
            byte[] canonicalReceipt =
                X402.decodeBase64(evidence.receipt(), MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
            String digest = X402.hex(X402.merkleLeafDigest(canonicalReceipt));
            if (!X402.constantTimeEquals(evidence.receiptDigest(), digest)) {
                throw MiddlewareException.of(MiddlewareException.Code.VERIFICATION_FAILURE);
            }
            return new Settled(canonicalReceipt, authorizedBatches.resolve(canonicalReceipt));
        }
    }

    public static AuthorizedBatchResolver staticAuthorizedBatches(LocalVerifier.AuthorizedReceiptBatch batch) {
        Objects.requireNonNull(batch, "batch");
        return canonicalReceipt -> batch;
    }

    private final X402.PaymentRequired required;
    private final PaymentAuthority authority;
    private final Fulfillments.FulfillmentRepository fulfillments;

    public SellerMiddleware(X402.PaymentRequired paymentRequired, PaymentAuthority authority,
                            Fulfillments.FulfillmentRepository fulfillments) {
        this.required = X402.parsePaymentRequired(
            Objects.requireNonNull(paymentRequired, "paymentRequired").toNode());
        this.authority = Objects.requireNonNull(authority, "authority");
        this.fulfillments = Objects.requireNonNull(fulfillments, "fulfillments");
    }

    public X402.PaymentRequired required() { return required; }

    public PaymentRequiredDecision paymentRequired() {
        return new PaymentRequiredDecision(required, X402.encodePaymentRequiredHeader(required));
    }

    public SellerDecision handle(String principal, String paymentHeader, ResourceRelease release) throws IOException {
        if (paymentHeader == null) return paymentRequired();
        if (!X402.bounded(principal, 512)) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_PAYMENT_PAYLOAD);
        }
        X402.PaymentPayload payload = X402.decodePaymentPayloadHeader(paymentHeader);
        X402.PaymentRequirements requirements = X402.matchRequirements(required, payload);
        byte[] requestDigestBytes =
            X402.sha256(X402.canonicalJson(payload.toNode()).getBytes(StandardCharsets.UTF_8));
        String requestDigest = X402.hex(requestDigestBytes);
        String idempotencyKey = X402.paymentIdempotencyKey(principal, requestDigestBytes);
        SettlementOutcome outcome = authority.settle(
            new SettlementRequest(principal, payload, requirements, idempotencyKey, requestDigest));
        if (outcome instanceof Pending) return new PendingDecision();
        if (outcome instanceof Refused refused) {
            X402.SettlementResponse settlement = X402.refusal(requirements, refused.reason());
            return new RefusedDecision(settlement, X402.encodeSettlementHeader(settlement));
        }
        Settled settled = (Settled) outcome;
        Fulfillments.ProposedFulfillment proposed = new Fulfillments.ProposedFulfillment(
            idempotencyKey, requestDigest, settled.canonicalReceipt(), settled.authorizedBatch());
        LocalVerifier.ReceiptVerification verification =
            verifyPaymentReceipt(proposed.canonicalReceipt(), proposed.authorizedBatch(), requirements);
        Fulfillments.StoredFulfillment stored = fulfillments.fulfill(proposed, release);
        if (!stored.idempotencyKey().equals(idempotencyKey) || !stored.requestDigest().equals(requestDigest)) {
            throw MiddlewareException.of(MiddlewareException.Code.FULFILLMENT_CONFLICT);
        }
        LocalVerifier.ReceiptVerification storedVerification =
            verifyPaymentReceipt(stored.canonicalReceipt(), stored.authorizedBatch(), requirements);
        if (!X402.constantTimeEquals(verification.receiptDigest(), storedVerification.receiptDigest())) {
            throw MiddlewareException.of(MiddlewareException.Code.FULFILLMENT_CONFLICT);
        }
        String receiptDigest = X402.hex(X402.merkleLeafDigest(stored.canonicalReceipt()));
        X402.ReceiptEvidence evidence = X402.ReceiptEvidence.sequencerSigned(
            X402.encodeBase64(stored.canonicalReceipt()), receiptDigest);
        Map<String, JsonNode> extensions = new LinkedHashMap<>();
        extensions.put("layerx", evidence.toNode());
        X402.SettlementResponse settlement = new X402.SettlementResponse(
            true,
            null,
            X402.hex(storedVerification.receipt().from()),
            "lxp:" + receiptDigest,
            requirements.network(),
            requirements.amount(),
            extensions);
        return new ReleasedDecision(settlement, X402.encodeSettlementHeader(settlement), storedVerification,
            stored.resource());
    }

    public static LocalVerifier.ReceiptVerification verifyPaymentReceipt(
            byte[] canonicalReceipt, LocalVerifier.AuthorizedReceiptBatch authorized,
            X402.PaymentRequirements requirements) {
        LocalVerifier.ReceiptVerification verified;
        try {
            verified = LocalVerifier.verifyReceipt(canonicalReceipt, authorized);
        } catch (PlatformSdkException error) {
            throw MiddlewareException.of(MiddlewareException.Code.VERIFICATION_FAILURE);
        }
        MiddlewareException.Code code = MiddlewareException.Code.VERIFICATION_FAILURE;
        if (!verified.receipt().amount().equals(new BigInteger(requirements.amount()))
                || !X402.constantTimeEquals(verified.receipt().asset(), X402.parseHex32(requirements.asset(), code))
                || !X402.constantTimeEquals(verified.receipt().to(), X402.parseHex32(requirements.payTo(), code))) {
            throw MiddlewareException.of(code);
        }
        return verified;
    }

    public static void assertReceiptBacked(ReleasedDecision decision, X402.PaymentRequirements requirements) {
        Map<String, JsonNode> extensions = decision.settlement().extensions();
        JsonNode layerx = extensions == null ? null : extensions.get("layerx");
        if (layerx == null || !layerx.isObject() || !layerx.has("receiptDigest")
                || !layerx.get("receiptDigest").isTextual()
                || !X402.isLowerHex32(layerx.get("receiptDigest").textValue())) {
            throw MiddlewareException.of(MiddlewareException.Code.RECEIPT_NOT_BACKED);
        }
        String receiptDigest = X402.hex(X402.merkleLeafDigest(decision.verification().canonicalBytes()));
        MiddlewareException.Code code = MiddlewareException.Code.RECEIPT_NOT_BACKED;
        if (decision.verification().level() != LocalVerifier.VerificationLevel.SEQUENCER_SIGNED
                || decision.verification().receipt().resultCode() != 0
                || !decision.settlement().success()
                || !decision.settlement().network().equals(requirements.network())
                || !requirements.amount().equals(decision.settlement().amount())
                || !decision.settlement().transaction().equals("lxp:" + receiptDigest)
                || !X402.constantTimeEquals(layerx.get("receiptDigest").textValue(), receiptDigest)
                || !decision.verification().receipt().amount().equals(new BigInteger(requirements.amount()))
                || !X402.constantTimeEquals(decision.verification().receipt().asset(),
                    X402.parseHex32(requirements.asset(), code))
                || !X402.constantTimeEquals(decision.verification().receipt().to(),
                    X402.parseHex32(requirements.payTo(), code))) {
            throw MiddlewareException.of(code);
        }
    }
}
