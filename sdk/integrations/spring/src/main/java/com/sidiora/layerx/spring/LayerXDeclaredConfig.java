package com.sidiora.layerx.spring;

import com.sidiora.layerx.sdk.verify.LocalVerifier;
import java.util.Map;

public record LayerXDeclaredConfig(String principal, String protectedPath, X402.PaymentRequired paymentRequired,
                                   X402.PaymentRequirements requirements,
                                   LocalVerifier.AuthorizedReceiptBatch authorizedBatch, String webhookPath,
                                   Map<String, byte[]> webhookPublicKeys, long webhookMaximumAgeMs,
                                   long webhookLeaseMs) {
    public LayerXDeclaredConfig { webhookPublicKeys = Map.copyOf(webhookPublicKeys); }
}
