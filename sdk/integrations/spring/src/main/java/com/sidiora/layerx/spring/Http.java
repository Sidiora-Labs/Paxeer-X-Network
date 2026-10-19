package com.sidiora.layerx.spring;

import jakarta.servlet.ServletInputStream;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Enumeration;

final class Http {
    private Http() {}

    static String path(HttpServletRequest request) {
        String uri = request.getRequestURI();
        String context = request.getContextPath();
        if (context != null && !context.isEmpty() && uri.startsWith(context)) {
            uri = uri.substring(context.length());
        }
        return uri.isEmpty() ? "/" : uri;
    }

    static boolean matchesPrefix(String path, String mount) {
        return "/".equals(mount) || path.equals(mount) || path.startsWith(mount + "/");
    }

    static String singleHeader(HttpServletRequest request, String name) {
        Enumeration<String> values = request.getHeaders(name);
        if (values == null || !values.hasMoreElements()) return null;
        String value = values.nextElement();
        if (values.hasMoreElements()) {
            throw MiddlewareException.of(MiddlewareException.Code.DUPLICATE_HEADER);
        }
        return value;
    }

    static byte[] readBody(HttpServletRequest request, int limit) throws IOException {
        ByteArrayOutputStream buffer = new ByteArrayOutputStream();
        byte[] chunk = new byte[8192];
        try (ServletInputStream stream = request.getInputStream()) {
            int read = stream.read(chunk);
            while (read >= 0) {
                if (buffer.size() + read > limit) {
                    throw MiddlewareException.of(MiddlewareException.Code.INVALID_WEBHOOK);
                }
                buffer.write(chunk, 0, read);
                read = stream.read(chunk);
            }
        }
        return buffer.toByteArray();
    }

    static void writeJson(HttpServletResponse response, int status, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        response.setStatus(status);
        response.setContentType("application/json");
        response.setContentLength(bytes.length);
        response.getOutputStream().write(bytes);
    }

    static void writeError(HttpServletResponse response, int status, String code) throws IOException {
        writeJson(response, status, "{\"error\":\"" + code + "\"}");
    }

    static void writeEmpty(HttpServletResponse response, int status) throws IOException {
        response.setStatus(status);
        response.setContentLength(0);
        response.getOutputStream().flush();
    }
}
