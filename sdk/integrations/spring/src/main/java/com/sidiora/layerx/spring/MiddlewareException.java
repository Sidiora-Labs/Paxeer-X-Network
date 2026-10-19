package com.sidiora.layerx.spring;

public final class MiddlewareException extends RuntimeException {
    public enum Code {
        INVALID_PAYMENT_REQUIRED("invalid-payment-required"),
        INVALID_PAYMENT_PAYLOAD("invalid-payment-payload"),
        REQUIREMENTS_MISMATCH("requirements-mismatch"),
        UNSUPPORTED_PAYMENT("unsupported-payment"),
        PAYMENT_PENDING("payment-pending"),
        PAYMENT_REFUSED("payment-refused"),
        VERIFICATION_FAILURE("verification-failure"),
        FULFILLMENT_CONFLICT("fulfillment-conflict"),
        INVALID_WEBHOOK("invalid-webhook"),
        WEBHOOK_REPLAY("webhook-replay"),
        MISSING_DECLARED_KEY("missing-declared-key"),
        INVALID_DECLARED_KEY("invalid-declared-key"),
        PUBLISHED_SECRET("published-secret"),
        DUPLICATE_HEADER("duplicate-header"),
        UNVERIFIABLE_BODY("unverifiable-body"),
        RECEIPT_NOT_BACKED("receipt-not-backed");

        private final String wire;

        Code(String wire) { this.wire = wire; }

        public String wire() { return wire; }
    }

    private final Code code;

    public MiddlewareException(Code code) {
        super(code.wire());
        this.code = code;
    }

    public Code code() { return code; }

    public static MiddlewareException of(Code code) { return new MiddlewareException(code); }
}
