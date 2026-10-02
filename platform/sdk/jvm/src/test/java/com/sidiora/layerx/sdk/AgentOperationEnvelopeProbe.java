package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.InputStream;
import java.net.URI;
import java.net.http.HttpClient;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.time.Duration;
import java.util.concurrent.CompletionException;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;

/** Probes the real unified gateway and agent daemon envelope route; every input is required. */
public final class AgentOperationEnvelopeProbe {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final com.fasterxml.jackson.databind.JavaType NODE = JSON.constructType(JsonNode.class);

    static String required(String name) {
        String value = System.getenv(name);
        if (value == null || value.isEmpty()) throw new IllegalStateException("missing required probe input " + name);
        return value;
    }

    static HttpClient client() throws Exception {
        var certificates = CertificateFactory.getInstance("X.509");
        var trust = KeyStore.getInstance(KeyStore.getDefaultType());
        trust.load(null, null);
        try (InputStream input = Files.newInputStream(Path.of(required("LAYERX_AGENT_PROBE_GATEWAY_CA")))) {
            int index = 0;
            for (var certificate : certificates.generateCertificates(input)) trust.setCertificateEntry("ca" + index++, certificate);
            if (index == 0) throw new IllegalStateException("LAYERX_AGENT_PROBE_GATEWAY_CA holds no certificate");
        }
        var factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
        factory.init(trust);
        var tls = SSLContext.getInstance("TLS");
        tls.init(null, factory.getTrustManagers(), null);
        return HttpClient.newBuilder().sslContext(tls).followRedirects(HttpClient.Redirect.NEVER)
            .version(HttpClient.Version.HTTP_1_1).build();
    }

    static HttpProductionTransport transport(String generation) throws Exception {
        URI gateway = URI.create(required("LAYERX_AGENT_PROBE_GATEWAY_URL"));
        if (!"https".equalsIgnoreCase(gateway.getScheme())) throw new IllegalStateException("LAYERX_AGENT_PROBE_GATEWAY_URL must be https");
        var admission = new HttpProductionTransport.LayerXKeyCredential(required("LAYERX_AGENT_PROBE_API_KEY_ID"),
            new SecretBytes(required("LAYERX_AGENT_PROBE_API_KEY").getBytes(StandardCharsets.US_ASCII)));
        var session = new HttpProductionTransport.AgentSessionCredential(required("LAYERX_AGENT_PROBE_TENANT"),
            required("LAYERX_AGENT_PROBE_SESSION_ID"),
            new SecretBytes(required("LAYERX_AGENT_PROBE_TOKEN_ID").getBytes(StandardCharsets.US_ASCII)), generation);
        return new HttpProductionTransport(client(), JSON, gateway, gateway, Duration.ofSeconds(30), admission, session);
    }

    static ObjectNode request(String name) throws Exception {
        JsonNode value = JSON.readTree(required(name));
        if (!(value instanceof ObjectNode object)) throw new IllegalStateException(name + " must be a JSON object");
        return object;
    }

    static JsonNode read(HttpProductionTransport transport, String operation, String requestInput) throws Exception {
        var call = new ProductionTransport.Call(OperationCatalog.agent(operation), request(requestInput), null, null);
        return transport.<JsonNode>call(call, NODE).toCompletableFuture().join();
    }

    static PlatformSdkException refusal(HttpProductionTransport transport, String operation, String requestInput) throws Exception {
        var call = new ProductionTransport.Call(OperationCatalog.agent(operation), request(requestInput), null, null);
        var failure = assertThrows(CompletionException.class,
            () -> transport.<JsonNode>call(call, NODE).toCompletableFuture().join());
        return assertInstanceOf(PlatformSdkException.class, failure.getCause());
    }

    static String staleGeneration() {
        String generation = required("LAYERX_AGENT_PROBE_GENERATION");
        if ("0".equals(generation)) throw new IllegalStateException("LAYERX_AGENT_PROBE_GENERATION must exceed 0 for the stale case");
        return new java.math.BigInteger(generation).subtract(java.math.BigInteger.ONE).toString();
    }

    @Test
    void authenticatedReadsProgramReadAndApprovalListUseTheEnvelopeRoute() throws Exception {
        var transport = transport(required("LAYERX_AGENT_PROBE_GENERATION"));
        assertNotNull(read(transport, "read.account", "LAYERX_AGENT_PROBE_READ_ACCOUNT_REQUEST"));
        assertNotNull(read(transport, "program.discover", "LAYERX_AGENT_PROBE_PROGRAM_DISCOVER_REQUEST"));
        assertNotNull(read(transport, "approval.list", "LAYERX_AGENT_PROBE_APPROVAL_LIST_REQUEST"));
    }

    @Test
    void staleGenerationIsRefusedBySessionControl() throws Exception {
        var error = refusal(transport(staleGeneration()), "read.account", "LAYERX_AGENT_PROBE_READ_ACCOUNT_REQUEST");
        assertEquals(SchemaErrors.AgentClass.POLICY_REFUSAL, error.agentClass());
    }

    @Test
    void retiredFaucetIsUnavailable() throws Exception {
        var error = refusal(transport(required("LAYERX_AGENT_PROBE_GENERATION")), "faucet.claim",
            "LAYERX_AGENT_PROBE_FAUCET_CLAIM_REQUEST");
        assertEquals(SchemaErrors.AgentClass.UNAVAILABLE_CAPABILITY, error.agentClass());
        assertEquals(SchemaErrors.AgentRetriability.TERMINAL, error.agentRetriability());
    }
}
