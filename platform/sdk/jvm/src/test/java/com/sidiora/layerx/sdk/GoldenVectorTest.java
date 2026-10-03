package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sidiora.layerx.sdk.verify.LocalVerifier;
import org.junit.jupiter.api.Test;
import java.io.IOException;
import java.math.BigInteger;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.List;
import static org.junit.jupiter.api.Assertions.*;

public final class GoldenVectorTest {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final Path REPO_ROOT = Paths.get(System.getProperty("layerx.repo.root", "../../.."));

    @Test
    void testAgentApiVersion() throws Exception {
        Path goldenRoot = REPO_ROOT.resolve("agent/schema/agent-api/golden");
        byte[] request = hexDecode(readHex(goldenRoot.resolve("version-request.hex")));
        byte[] response = hexDecode(readHex(goldenRoot.resolve("version-response.hex")));
        assertNotNull(request);
        assertNotNull(response);
        assertEquals(19, request.length);
        assertEquals(23, response.length);
    }

    @Test
    void testCodecValidVectors() throws Exception {
        Path vectorPath = REPO_ROOT.resolve("tests/vectors/codec/valid.lxv");
        assertTrue(Files.isRegularFile(vectorPath));
        List<String> lines = Files.readAllLines(vectorPath);
        int verified = 0;
        for (String line : lines) {
            if (line.startsWith("#") || line.trim().isEmpty()) continue;
            String[] parts = line.split("\\|");
            assertEquals(5, parts.length, line);
            String kind = parts[0];
            String hex = parts[2];
            if (kind.equals("u64")) {
                byte[] bytes = hexDecode(hex);
                assertNotNull(bytes);
                assertEquals(8, bytes.length);
                assertEquals(parts[4], java.util.HexFormat.of().formatHex(
                    MessageDigest.getInstance("SHA-256").digest(bytes)));
                verified++;
            }
        }
        assertTrue(verified > 0, "No valid codec vectors verified");
    }

    @Test
    void testCodecAdversarialVectorsAreExplicit() throws Exception {
        Path vectorPath = REPO_ROOT.resolve("tests/vectors/codec/adversarial.lxv");
        assertTrue(Files.isRegularFile(vectorPath));
        int verified = 0;
        for (String line : Files.readAllLines(vectorPath)) {
            if (line.startsWith("#") || line.isBlank()) continue;
            String[] parts = line.split("\\|");
            assertEquals(5, parts.length, line);
            assertTrue(Integer.parseInt(parts[3]) < 0, line);
            assertFalse(parts[2].isEmpty(), line);
            verified++;
        }
        assertTrue(verified > 0);
    }

    @Test
    void testProtocolAmountBounds() throws Exception {
        assertThrows(PlatformSdkException.class, () -> 
            ProtocolAmount.of(BigInteger.valueOf(-1)));
        assertThrows(PlatformSdkException.class, () -> 
            ProtocolAmount.of(ProtocolAmount.MAX_VALUE.add(BigInteger.ONE)));
        
        ProtocolAmount zero = ProtocolAmount.of(BigInteger.ZERO);
        assertEquals("0", zero.toString());
        
        ProtocolAmount max = ProtocolAmount.of(ProtocolAmount.MAX_VALUE);
        assertTrue(max.toString().length() > 30);
        assertThrows(PlatformSdkException.class, () -> SchemaTypes.protocolInteger(JSON.readTree("1.0")));
        assertThrows(PlatformSdkException.class, () -> SchemaTypes.protocolInteger(JSON.readTree("1")));
        assertEquals(BigInteger.TEN, SchemaTypes.protocolInteger(JSON.readTree("\"10\"")));
    }

    @Test
    void testIdempotencyKeyValidation() {
        assertThrows(PlatformSdkException.class, () -> new IdempotencyKey(""));
        assertThrows(PlatformSdkException.class, () -> new IdempotencyKey(null));
        assertThrows(PlatformSdkException.class, () -> new IdempotencyKey("a\0b"));
        assertThrows(PlatformSdkException.class, () -> new IdempotencyKey("a".repeat(256)));
        
        IdempotencyKey valid = new IdempotencyKey("test-key-123");
        assertEquals("test-key-123", valid.toString());
    }

    @Test
    void testSecretBytesZeroization() {
        byte[] original = "secret-data".getBytes();
        SecretBytes secret = new SecretBytes(original);
        original[0] = (byte) 'X';
        
        secret.use(bytes -> {
            assertEquals('s', (char) bytes[0]);
            return null;
        });
        
        secret.close();
        assertTrue(secret.isDestroyed());
        assertThrows(PlatformSdkException.class, () -> secret.use(b -> null));
        assertEquals("[REDACTED]", secret.toString());
    }

    @Test
    void testResumableStreamCursorValidation() {
        assertThrows(PlatformSdkException.class, () -> 
            new ResumableStream.Cursor(""));
        assertThrows(PlatformSdkException.class, () -> 
            new ResumableStream.Cursor(null));
        assertThrows(PlatformSdkException.class, () -> 
            new ResumableStream.Cursor("a\0b"));
        assertThrows(PlatformSdkException.class, () -> 
            new ResumableStream.Cursor("x".repeat(513)));
        
        ResumableStream.Cursor valid = new ResumableStream.Cursor("cursor-abc-123");
        assertEquals("cursor-abc-123", valid.toString());
    }

    @Test
    void testStreamRefusesGapAndRepeatedPositionWithoutAdvancing() {
        var start = new ResumableStream.Cursor("position-0");
        var middle = new ResumableStream.Cursor("position-1");
        var end = new ResumableStream.Cursor("position-2");
        var stream = new ResumableStream<String>(start);
        var first = new ResumableStream.Event<>("event-1", start, middle, "first");
        var gap = new ResumableStream.Event<>("event-2", start, end, "second");
        assertThrows(PlatformSdkException.class,
            () -> stream.accept(new ResumableStream.Page<>(start, List.of(first, gap), end)));
        assertEquals(start, stream.cursor());
        assertEquals(List.of(first), stream.accept(new ResumableStream.Page<>(start, List.of(first), middle)));
        var unchanged = new ResumableStream.Event<>("event-3", middle, middle, "repeat-position");
        assertThrows(PlatformSdkException.class,
            () -> stream.accept(new ResumableStream.Page<>(middle, List.of(unchanged), middle)));
        assertEquals(middle, stream.cursor());
        var duplicate = new ResumableStream.Event<>("event-1", middle, end, "duplicate");
        assertThrows(PlatformSdkException.class,
            () -> stream.accept(new ResumableStream.Page<>(middle, List.of(duplicate), end)));
        assertEquals(middle, stream.cursor());
    }

    @Test
    void testMerkleProofDepthCalculation() {
        byte[] leaf = new byte[32];
        byte[] root = sha256("LXP/v1/merkle-leaf\0".getBytes(java.nio.charset.StandardCharsets.UTF_8), leaf);
        
        LocalVerifier.MerkleProof singleLeaf = new LocalVerifier.MerkleProof(0, 1, List.of());
        assertDoesNotThrow(() -> LocalVerifier.verifyMerkleInclusion(leaf, singleLeaf, root));
        
        LocalVerifier.MerkleProof twoLeaves = new LocalVerifier.MerkleProof(0, 2, List.of(new byte[32]));
        assertThrows(PlatformSdkException.class, () -> 
            LocalVerifier.verifyMerkleInclusion(leaf, twoLeaves, root));
    }

    @Test
    void testTypedSchemaContracts() {
        var operation = GeneratedSchema.HumanOperations.MOVE_QUOTE;
        assertEquals(OperationCatalog.Plane.HUMAN, operation.plane());
        assertEquals(GeneratedSchema.HumanOperations.MoveQuoteRequest.class, operation.requestType());
        var money = new GeneratedSchema.HumanModels.Money(ProtocolAmount.parse("42"), "LXP");
        var request = new GeneratedSchema.HumanOperations.MoveQuoteRequest("source", "destination", money);
        assertEquals(new BigInteger("42"), request.money().amount().value());

        var floating = JSON.createObjectNode();
        floating.put("source", "source").put("destination", "destination");
        floating.putObject("money").put("amount", 42.0).put("currency", "LXP");
        assertThrows(IllegalArgumentException.class,
            () -> JSON.convertValue(floating, GeneratedSchema.HumanOperations.MoveQuoteRequest.class));

        assertEquals(OperationCatalog.AGENT_ERROR_CLASSES,
            java.util.Arrays.stream(SchemaErrors.AgentClass.values()).map(SchemaErrors.AgentClass::wire)
                .collect(java.util.stream.Collectors.toUnmodifiableSet()));
        assertEquals(OperationCatalog.HUMAN_ERROR_CODES,
            java.util.Arrays.stream(SchemaErrors.HumanCode.values()).map(SchemaErrors.HumanCode::wire)
                .collect(java.util.stream.Collectors.toUnmodifiableSet()));
    }

    @Test
    void testMoneyDoesNotCoerceNumericJson() throws Exception {
        assertEquals(ProtocolAmount.of(BigInteger.TEN), JSON.readValue("\"10\"", ProtocolAmount.class));
        for (String encoded : List.of("10", "10.0", "true", "\"01\"", "\"-1\"")) {
            assertThrows(Exception.class, () -> JSON.readValue(encoded, ProtocolAmount.class));
        }
        assertEquals("\"10\"", JSON.writeValueAsString(ProtocolAmount.of(BigInteger.TEN)));
    }

    @Test
    void testGeneratedCredentialDiagnosticsRedactTokens() {
        var credential = new GeneratedSchema.AgentModels.AgentHttpCredential(
            "tenant", "session", "credential-token", BigInteger.ONE);
        assertEquals("[REDACTED]", credential.toString());
        assertFalse(credential.toString().contains(credential.token_id()));
    }

    @Test
    void testGeneratedEnvelopePreservesMapsAndNullableSchemaFields() throws Exception {
        var encoded = JSON.readValue("{\"version\":1,\"request_id\":\"1\",\"operation\":\"session.open\",\"request\":{},\"credential\":null,\"idempotency_key\":null}",
            com.fasterxml.jackson.databind.node.ObjectNode.class);
        var envelope = JSON.treeToValue(encoded, GeneratedSchema.AgentModels.AgentHttpEnvelope.class);
        assertTrue(envelope.request().isObject());
        assertNull(envelope.credential());
        assertNull(envelope.idempotency_key());
        assertTrue(JSON.valueToTree(envelope).path("request").isObject());
        assertEquals("[REDACTED]", envelope.toString());
        for (String version : List.of("1.0", "\"1\"", "256", "-1")) {
            encoded.set("version", JSON.readTree(version));
            assertThrows(Exception.class,
                () -> JSON.treeToValue(encoded, GeneratedSchema.AgentModels.AgentHttpEnvelope.class));
        }
    }

    @Test
    void testGeneratedTimestampUsesSchemaDecimalStrings() throws Exception {
        var body = JSON.createObjectNode();
        body.set("approval_id", JSON.createObjectNode());
        body.set("tenant", JSON.createObjectNode());
        body.set("held_activity", JSON.createObjectNode());
        body.set("canonical_bytes_digest", JSON.createObjectNode());
        body.set("hold_reason", JSON.createObjectNode());
        body.put("created_at", "18446744073709551615");
        body.put("expires_at", "18446744073709551615");
        body.set("state", JSON.createObjectNode());
        var record = JSON.treeToValue(body, GeneratedSchema.AgentModels.ApprovalRecord.class);
        assertEquals(new BigInteger("18446744073709551615"), record.created_at());
        assertTrue(JSON.valueToTree(record).path("created_at").isTextual());
        for (String encoded : List.of("10", "10.0", "\"01\"", "\"18446744073709551616\"")) {
            body.set("created_at", JSON.readTree(encoded));
            assertThrows(Exception.class,
                () -> JSON.treeToValue(body, GeneratedSchema.AgentModels.ApprovalRecord.class));
        }
    }

    @Test
    void testPlatformSdkMetadata() {
        var metadata = PlatformSdk.platform_sdk_jvm();
        assertNotNull(metadata);
        assertEquals("com.sidiora.layerx:layerx-sdk", metadata.get("name"));
        assertEquals("0.1.0", metadata.get("version"));
        assertEquals(1, metadata.get("contractMajor"));
        assertTrue((Integer) metadata.get("agentOperations") > 0);
        assertTrue((Integer) metadata.get("humanOperations") > 0);
    }

    @Test
    void testOperationCatalogIntegrity() {
        assertFalse(OperationCatalog.AGENT_OPERATIONS.isEmpty());
        assertFalse(OperationCatalog.HUMAN_ROUTES.isEmpty());
        assertFalse(OperationCatalog.HUMAN_ERROR_CODES.isEmpty());
        
        for (String operation : OperationCatalog.AGENT_OPERATIONS) {
            assertNotNull(operation);
            assertFalse(operation.isEmpty());
        }
        
        for (var entry : OperationCatalog.HUMAN_ROUTES.entrySet()) {
            assertNotNull(entry.getKey());
            assertNotNull(entry.getValue());
            assertNotNull(entry.getValue().method());
            assertNotNull(entry.getValue().path());
        }
    }

    private static String readHex(Path path) throws IOException {
        return Files.readString(path).trim();
    }

    private static byte[] hexDecode(String hex) {
        assertEquals(0, hex.length() & 1, "hex must have even length");
        int len = hex.length();
        byte[] data = new byte[len / 2];
        for (int i = 0; i < len; i += 2) {
            data[i / 2] = (byte) ((Character.digit(hex.charAt(i), 16) << 4)
                + Character.digit(hex.charAt(i + 1), 16));
        }
        return data;
    }

    private static byte[] sha256(byte[]... parts) {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            for (byte[] part : parts) digest.update(part);
            return digest.digest();
        } catch (java.security.GeneralSecurityException impossible) {
            throw new AssertionError(impossible);
        }
    }
}
