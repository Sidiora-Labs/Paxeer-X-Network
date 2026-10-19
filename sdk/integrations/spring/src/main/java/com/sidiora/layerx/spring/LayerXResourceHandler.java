package com.sidiora.layerx.spring;

import jakarta.servlet.http.HttpServletRequest;
import java.io.IOException;

@FunctionalInterface
public interface LayerXResourceHandler {
    LayerXResource release(HttpServletRequest request) throws IOException;
}
