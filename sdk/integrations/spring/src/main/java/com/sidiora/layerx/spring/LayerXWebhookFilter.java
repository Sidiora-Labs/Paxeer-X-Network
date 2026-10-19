package com.sidiora.layerx.spring;

import jakarta.servlet.FilterChain;
import jakarta.servlet.ServletException;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.util.Objects;
import org.springframework.web.filter.OncePerRequestFilter;

public final class LayerXWebhookFilter extends OncePerRequestFilter {
    private final LayerXDeclaredConfig config;
    private final Webhooks.VerifiedWebhookConsumer consumer;
    private final LayerXWebhookEventHandler events;

    public LayerXWebhookFilter(LayerXDeclaredConfig config, Webhooks.VerifiedWebhookConsumer consumer,
                               LayerXWebhookEventHandler events) {
        this.config = Objects.requireNonNull(config, "config");
        this.consumer = Objects.requireNonNull(consumer, "consumer");
        this.events = Objects.requireNonNull(events, "events");
    }

    @Override
    protected boolean shouldNotFilter(HttpServletRequest request) {
        return !config.webhookPath().equals(Http.path(request));
    }

    @Override
    protected void doFilterInternal(HttpServletRequest request, HttpServletResponse response, FilterChain chain)
            throws ServletException, IOException {
        if (!"POST".equalsIgnoreCase(request.getMethod())) {
            Http.writeError(response, 405, "method-not-allowed");
            return;
        }
        try {
            Webhooks.RequestHeaders headers = headers(request);
            byte[] rawBody = Http.readBody(request, Webhooks.MAXIMUM_WEBHOOK_BYTES);
            Webhooks.ConsumeResult outcome = consumer.consume(rawBody, headers, events);
            if (outcome == Webhooks.ConsumeResult.PROCESSED) {
                Http.writeEmpty(response, 204);
                return;
            }
            if (outcome == Webhooks.ConsumeResult.DUPLICATE) {
                Http.writeJson(response, 200, "{\"outcome\":\"duplicate\"}");
                return;
            }
            response.setHeader("retry-after", "1");
            Http.writeJson(response, 409, "{\"outcome\":\"processing\"}");
        } catch (MiddlewareException error) {
            if (error.code() == MiddlewareException.Code.INVALID_WEBHOOK) {
                Http.writeError(response, 401, error.code().wire());
                return;
            }
            if (error.code() == MiddlewareException.Code.WEBHOOK_REPLAY) {
                Http.writeError(response, 409, error.code().wire());
                return;
            }
            if (error.code() == MiddlewareException.Code.DUPLICATE_HEADER) {
                Http.writeError(response, 400, error.code().wire());
                return;
            }
            throw error;
        }
    }

    private static Webhooks.RequestHeaders headers(HttpServletRequest request) {
        String id = Http.singleHeader(request, Webhooks.ID_HEADER);
        String timestamp = Http.singleHeader(request, Webhooks.TIMESTAMP_HEADER);
        String keyId = Http.singleHeader(request, Webhooks.KEY_HEADER);
        String signature = Http.singleHeader(request, Webhooks.SIGNATURE_HEADER);
        if (id == null || timestamp == null || keyId == null || signature == null) {
            throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
        }
        return new Webhooks.RequestHeaders(id, timestamp, keyId, signature);
    }
}
