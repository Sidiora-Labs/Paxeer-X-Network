package com.sidiora.layerx.spring;

import java.util.Objects;

public record LayerXResource(String contentType, byte[] body) {
    public LayerXResource {
        Objects.requireNonNull(contentType, "contentType");
        Objects.requireNonNull(body, "body");
        body = body.clone();
    }

    @Override
    public byte[] body() { return body.clone(); }
}
