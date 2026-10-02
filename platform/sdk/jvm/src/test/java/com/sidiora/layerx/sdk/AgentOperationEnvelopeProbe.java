package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.io.InputStream;
import java.math.BigInteger;
import java.net.URI;
import java.net.http.HttpClient;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyStore;
import java.security.cert.CertificateFactory;
import java.time.Duration;
import java.util.LinkedHashSet;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.CompletionException;
import javax.net.ssl.SSLContext;
import javax.net.ssl.TrustManagerFactory;

/** Probes the real unified gateway and agent daemon envelope route from the harness case file. */
public final class AgentOperationEnvelopeProbe {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final JavaType NODE = JSON.constructType(JsonNode.class);
    private static final Map<String, String> OPERATIONS = Map.of(
        "read", "read.account", "program_read", "program.interface", "approval_list", "approval.list");
    private static final Set<String> DECODE_CASES = Set.of("read_decode_failure", "mutation_decode_unknown");

    private AgentOperationEnvelopeProbe() {}

    public static void main(String[] arguments) {
        try {
            int passed = run();
            System.out.println("PAXEER_X_AGENT_ENVELOPE_CASES=" + passed);
            System.out.flush();
        } catch (Throwable failure) {
            System.err.println("agent envelope probe failed: " + failure.getClass().getName()
                + (failure instanceof PlatformSdkException error ? " " + error.code() + " " + error.agentClass() : "")
                + (failure instanceof IllegalStateException ? " " + failure.getMessage() : ""));
            System.exit(1);
        }
    }

    static int run() throws Exception {
        String caseFile = System.getenv("PAXEER_X_AGENT_ENVELOPE_CASE");
        check(caseFile != null && !caseFile.isEmpty(), "missing PAXEER_X_AGENT_ENVELOPE_CASE");
        JsonNode config = JSON.readTree(Files.readAllBytes(Path.of(caseFile)));
        check(config != null && config.isObject(), "case file must be a JSON object");
        check("read".equals(text(config, "phase")), "the JVM probe implements only the read phase");
        JsonNode credential = JSON.readTree(Files.readAllBytes(Path.of(text(config, "credential_file"))));
        check(credential != null && credential.isObject() && credential.size() == 4, "credential coordinates");
        String generation = text(credential, "generation");
        JsonNode requests = config.get("requests");
        check(requests != null && requests.isObject(), "requests must be a JSON object");
        JsonNode cases = config.get("cases");
        check(cases != null && cases.isArray() && !cases.isEmpty(), "cases must be a non-empty array");
        Path responses = Path.of(text(config, "response_dir"));
        check(Files.isDirectory(responses), "response_dir must exist");

        var transport = transport(config, credential, generation);
        var seen = new LinkedHashSet<String>();
        for (JsonNode entry : cases) {
            check(entry.isTextual() && seen.add(entry.textValue()), "case ids must be unique strings");
            String id = entry.textValue();
            if (DECODE_CASES.contains(id)) {
                decodeCase(config, credential, generation, requests, id, responses);
                System.out.println("PAXEER_X_AGENT_ENVELOPE_CASE " + id + " passed");
                continue;
            }
            String operation = OPERATIONS.get(id);
            check(operation != null, "unsupported case " + id);
            JsonNode provisioned = requests.get(id);
            check(provisioned != null && provisioned.isObject(), "provisioned request lacks case " + id);
            check(operation.equals(text(provisioned, "operation")), "case " + id + " operation must be " + operation);
            check(provisioned.get("idempotency_key") == null || provisioned.get("idempotency_key").isNull(),
                "case " + id + " is non-mutating and carries no idempotency_key");
            var reply = read(transport, operation, request(provisioned, "request"));
            check(reply.status() == 200, "case " + id + " status " + reply.status());
            check(reply.requestId().matches("0|[1-9][0-9]{0,19}") && new BigInteger(reply.requestId()).bitLength() <= 64,
                "case " + id + " request_id is not a canonical unsigned 64-bit decimal");
            check(reply.requestId().equals(reply.body().path("request_id").textValue()), "case " + id + " request_id not preserved");
            check(reply.value() != null && !reply.value().isNull(), "case " + id + " returned no value");
            JsonNode sent = reply.body().get("value");
            JsonNode expected = sent instanceof ObjectNode object ? SchemaTypes.canonicalBody(object) : sent;
            check(expected != null && expected.equals(reply.value()), "case " + id + " value not preserved");
            JsonNode verification = reply.verificationStatus();
            check(verification != null && verification.equals(reply.body().get("verification_status"))
                && ("achieved".equals(verification.path("state").textValue())
                    || "unverified".equals(verification.path("state").textValue())),
                "case " + id + " verification_status not preserved");
            ObjectNode record = JSON.createObjectNode();
            record.put("status", reply.status());
            record.set("body", reply.body());
            Files.write(responses.resolve(id + ".json"), JSON.writeValueAsBytes(record));
            System.out.println("PAXEER_X_AGENT_ENVELOPE_CASE " + id + " passed");
        }

        BigInteger current = new BigInteger(generation);
        check(current.signum() > 0, "credential generation must exceed 0 for the stale case");
        var stale = refusal(transport(config, credential, current.subtract(BigInteger.ONE).toString()),
            "read.account", request(requests.path("read"), "request"));
        check(stale.agentClass() == SchemaErrors.AgentClass.POLICY_REFUSAL, "stale generation was not a policy refusal");
        var faucet = refusal(transport, "faucet.claim", JSON.createObjectNode());
        check(faucet.agentClass() == SchemaErrors.AgentClass.UNAVAILABLE_CAPABILITY
            && faucet.agentRetriability() == SchemaErrors.AgentRetriability.TERMINAL, "faucet.claim was not terminal unavailable");
        return seen.size();
    }

    static void decodeCase(JsonNode config, JsonNode credential, String generation, JsonNode requests, String id,
                           Path responses) throws Exception {
        JsonNode provisioned = requests.get(id);
        check(provisioned != null && provisioned.isObject(), "provisioned request lacks case " + id);
        var operation = OperationCatalog.agent(text(provisioned, "operation"));
        boolean mutating = OperationCatalog.requiresIdempotency(operation);
        check(mutating == "mutation_decode_unknown".equals(id), "case " + id + " operation mutability");
        IdempotencyKey key = null;
        if (mutating) {
            String value = text(provisioned, "idempotency_key");
            check(value.matches("[0-9a-f]{64}"), "case " + id + " idempotency_key must be 64 lowercase hex");
            key = new IdempotencyKey(value);
        } else {
            check(provisioned.get("idempotency_key") == null || provisioned.get("idempotency_key").isNull(),
                "case " + id + " is non-mutating and carries no idempotency_key");
        }
        String endpoint = provisioned.has("endpoint") ? text(provisioned, "endpoint") : text(config, "endpoint");
        var target = transport(config, endpoint, credential, generation);
        var call = new ProductionTransport.Call(operation, request(provisioned, "request"), null, key);
        PlatformSdkException error = null;
        try {
            target.callAgentReply(call).toCompletableFuture().join();
        } catch (CompletionException failure) {
            check(failure.getCause() instanceof PlatformSdkException, "case " + id + " failed outside the SDK error contract");
            error = (PlatformSdkException) failure.getCause();
        }
        check(error != null, "case " + id + " decoded a reply");
        if (mutating) {
            check(error.code() == PlatformSdkException.Code.UNKNOWN_OUTCOME
                && error.retry() == PlatformSdkException.Retry.UNKNOWN_OUTCOME, "case " + id + " was not an unknown outcome");
        } else {
            check(error.code() == PlatformSdkException.Code.DECODE_FAILURE
                && error.retry() == PlatformSdkException.Retry.NEVER, "case " + id + " was not a never-retry decode failure");
        }
        ObjectNode record = JSON.createObjectNode();
        record.put("sdk_error", error.code().wire());
        record.put("retry", error.retry().wire());
        Files.write(responses.resolve(id + ".json"), JSON.writeValueAsBytes(record));
    }

    static HttpProductionTransport transport(JsonNode config, JsonNode credential, String generation) throws Exception {
        return transport(config, text(config, "endpoint"), credential, generation);
    }

    static HttpProductionTransport transport(JsonNode config, String target, JsonNode credential, String generation)
            throws Exception {
        URI endpoint = URI.create(target);
        check("https".equalsIgnoreCase(endpoint.getScheme()), "endpoint must be https");
        String key = Files.readString(Path.of(text(config, "gateway_api_key_file")), StandardCharsets.US_ASCII).strip();
        int separator = key.indexOf(':');
        check(separator > 0 && key.indexOf(':', separator + 1) < 0, "gateway api key must be <id>:<secret>");
        var admission = new HttpProductionTransport.LayerXKeyCredential(key.substring(0, separator),
            new SecretBytes(key.substring(separator + 1).getBytes(StandardCharsets.US_ASCII)));
        var session = new HttpProductionTransport.AgentSessionCredential(text(credential, "tenant"),
            text(credential, "session_id"),
            new SecretBytes(text(credential, "token_id").getBytes(StandardCharsets.US_ASCII)), generation);
        return new HttpProductionTransport(client(text(config, "ca_pem")), JSON, endpoint, endpoint,
            Duration.ofSeconds(30), admission, session);
    }

    static HttpClient client(String caPem) throws Exception {
        var trust = KeyStore.getInstance(KeyStore.getDefaultType());
        trust.load(null, null);
        try (InputStream input = Files.newInputStream(Path.of(caPem))) {
            int index = 0;
            for (var certificate : CertificateFactory.getInstance("X.509").generateCertificates(input)) {
                trust.setCertificateEntry("ca" + index++, certificate);
            }
            check(index > 0, "ca_pem holds no certificate");
        }
        var factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm());
        factory.init(trust);
        var tls = SSLContext.getInstance("TLS");
        tls.init(null, factory.getTrustManagers(), null);
        return HttpClient.newBuilder().sslContext(tls).followRedirects(HttpClient.Redirect.NEVER)
            .version(HttpClient.Version.HTTP_1_1).build();
    }

    static ObjectNode request(JsonNode provisioned, String field) {
        JsonNode value = provisioned.get(field);
        check(value instanceof ObjectNode, "provisioned request lacks " + field);
        return (ObjectNode) value;
    }

    static HttpProductionTransport.AgentReply read(HttpProductionTransport transport, String operation, ObjectNode request) {
        var call = new ProductionTransport.Call(OperationCatalog.agent(operation), request, null, null);
        return transport.callAgentReply(call).toCompletableFuture().join();
    }

    static PlatformSdkException refusal(HttpProductionTransport transport, String operation, ObjectNode request) {
        var call = new ProductionTransport.Call(OperationCatalog.agent(operation), request, null, null);
        try {
            transport.<JsonNode>call(call, NODE).toCompletableFuture().join();
        } catch (CompletionException failure) {
            check(failure.getCause() instanceof PlatformSdkException, operation + " failed outside the SDK error contract");
            return (PlatformSdkException) failure.getCause();
        }
        throw new IllegalStateException(operation + " was not refused");
    }

    static String text(JsonNode object, String field) {
        JsonNode value = object.get(field);
        check(value != null && value.isTextual() && !value.textValue().isEmpty(), "missing " + field);
        return value.textValue();
    }

    static void check(boolean condition, String message) {
        if (!condition) throw new IllegalStateException(message);
    }
}
