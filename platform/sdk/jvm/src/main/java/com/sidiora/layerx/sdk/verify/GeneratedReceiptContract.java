// Code generated from platform/sdk/generators/receipt.kvx. DO NOT EDIT.

package com.sidiora.layerx.sdk.verify;

import java.util.List;

public final class GeneratedReceiptContract {
    private GeneratedReceiptContract() {}
    public static final int PROGRAMS_MODULE_ID = 9;
    public static final long PROGRAM_OUTCOME_V1 = 0x50524731L;
    public static final long PROGRAM_OUTCOME_V2 = 0x50524732L;
    public static final long PROGRAM_OUTCOME_V3 = 0x50524733L;
    public static final long PROGRAM_OUTCOME_V4 = 0x50524734L;

    public enum ReceiptCheck {
        DECODE("decode"),
        CANONICAL_ENCODING("canonical-encoding"),
        RECEIPT_SHAPE("receipt-shape"),
        MISSING_SIGNATURE("missing-signature"),
        PROTOCOL_VERSION("protocol-version"),
        RESULT_CODE("result-code"),
        OPERATION("operation"),
        ACTIVITY_ID("activity-id"),
        GLOBAL_SEQUENCE("global-sequence"),
        MODULE_ID("module-id"),
        MODULE_VERSION("module-version"),
        TIMESTAMP("timestamp"),
        BATCH_ID("batch-id"),
        ASSET("asset"),
        PREVIOUS_STATE_ROOT("previous-state-root"),
        RESULTING_STATE_ROOT("resulting-state-root"),
        DEBIT_BALANCE("debit-balance"),
        CREDIT_BALANCE("credit-balance"),
        PROGRAM_OUTCOME("program-outcome"),
        SEQUENCER_SIGNATURE("sequencer-signature");
        private final String wire;
        ReceiptCheck(String wire) { this.wire = wire; }
        public String wire() { return wire; }
    }

    public static final List<ReceiptCheck> REQUIRED_NONZERO_CHECKS = List.of(
        ReceiptCheck.GLOBAL_SEQUENCE,
        ReceiptCheck.MODULE_ID,
        ReceiptCheck.MODULE_VERSION,
        ReceiptCheck.TIMESTAMP,
        ReceiptCheck.ACTIVITY_ID,
        ReceiptCheck.RESULTING_STATE_ROOT);

    public static final int PROGRAM_ABI_V1 = 1;
    public static final int PROGRAM_ABI_V2 = 2;
    public static final int PROGRAM_ABI_V3 = 3;
    public static final int PROGRAM_ABI_V4 = 4;
    public static boolean supportsProgramGuestAbi(int version) {
        return switch (version) {
            case PROGRAM_ABI_V1, PROGRAM_ABI_V2, PROGRAM_ABI_V3, PROGRAM_ABI_V4 -> true;
            default -> false;
        };
    }
}
