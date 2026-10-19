package com.sidiora.layerx.spring;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.util.Objects;
import org.springframework.web.filter.OncePerRequestFilter;

public final class LayerXPaymentGateFilter extends OncePerRequestFilter {
    private final LayerXDeclaredConfig config;
    private final SellerMiddleware seller;
    private final LayerXResourceHandler resources;

    public LayerXPaymentGateFilter(LayerXDeclaredConfig config, SellerMiddleware seller,
                                   LayerXResourceHandler resources) {
        this.config = Objects.requireNonNull(config, "config");
        this.seller = Objects.requireNonNull(seller, "seller");
        this.resources = Objects.requireNonNull(resources, "resources");
    }

    @Override
    protected boolean shouldNotFilter(HttpServletRequest request) {
        return !Http.matchesPrefix(Http.path(request), config.protectedPath());
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response, FilterChain chain)
            throws ServletException, IOException {
        SellerMiddleware.SellerDecision decision;
        try {
            decision = seller.handle(config.principal(),
                Http.singleHeader(request, X402.PAYMENT_SIGNATURE_HEADER),
                () -> resources.release(request));
        } catch (MiddlewareException error) {
            Http.writeError(response, paymentErrorStatus(error.code()), error.code().wire());
            return;
        }
        if (decision instanceof SellerMiddleware.PaymentRequiredDecision required) {
            response.setHeader(X402.PAYMENT_REQUIRED_HEADER, required.header());
            Http.writeJson(response, required.status(), X402.canonicalJson(required.body().toNode()));
            return;
        }
        if (decision instanceof SellerMiddleware.PendingDecision pending) {
            response.setHeader("retry-after", "1");
            Http.writeEmpty(response, pending.status());
            return;
        }
        if (decision instanceof SellerMiddleware.RefusedDecision refused) {
            response.setHeader(X402.PAYMENT_RESPONSE_HEADER, refused.header());
            Http.writeEmpty(response, refused.status());
            return;
        }
        SellerMiddleware.ReleasedDecision released = (SellerMiddleware.ReleasedDecision) decision;
        try {
            SellerMiddleware.assertReceiptBacked(released, config.requirements());
        } catch (MiddlewareException error) {
            Http.writeError(response, 500, error.code().wire());
            return;
        }
        byte[] body = released.resource().body();
        request.setAttribute("layerx.receiptDigest", X402.hex(released.verification().receiptDigest()));
        request.setAttribute("layerx.transaction", released.settlement().transaction());
        request.setAttribute("layerx.verificationLevel", released.verification().level().wire());
        response.setStatus(released.status());
        response.setHeader(X402.PAYMENT_RESPONSE_HEADER, released.header());
        response.setHeader("layerx-receipt-digest", X402.hex(released.verification().receiptDigest()));
        response.setHeader("layerx-transaction", released.settlement().transaction());
        response.setContentType(released.resource().contentType());
        response.setContentLength(body.length);
        response.getOutputStream().write(body);
    }

    static int paymentErrorStatus(MiddlewareException.Code code) {
        if (code == MiddlewareException.Code.PAYMENT_PENDING) return 202;
        if (code == MiddlewareException.Code.FULFILLMENT_CONFLICT) return 409;
        if (code == MiddlewareException.Code.DUPLICATE_HEADER) return 400;
        return 402;
    }
}
