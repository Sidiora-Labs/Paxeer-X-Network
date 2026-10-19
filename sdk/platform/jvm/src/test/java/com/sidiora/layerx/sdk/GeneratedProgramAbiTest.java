package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.databind.DatabindException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.exc.MismatchedInputException;
import com.fasterxml.jackson.databind.exc.ValueInstantiationException;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.sidiora.layerx.sdk.GeneratedSchema.AgentModels.ProgramGuestAbi;
import com.sidiora.layerx.sdk.GeneratedSchema.AgentOperations.ProgramDiscoverResponse;
import com.sidiora.layerx.sdk.GeneratedSchema.AgentOperations.ProgramInterfaceResponse;
import java.util.List;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertSame;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

public final class GeneratedProgramAbiTest {
    private static final ObjectMapper JSON = new ObjectMapper();
    private static final String HEX32 = "11".repeat(32);
    private static final List<ProgramGuestAbi> ORDERED = List.of(ProgramGuestAbi.ABI_V1_VERSION,
        ProgramGuestAbi.ABI_V2_VERSION, ProgramGuestAbi.ABI_V3_VERSION, ProgramGuestAbi.ABI_V4_VERSION,
        ProgramGuestAbi.ABI_V5_VERSION);

    private static ObjectNode discovery(JsonNode abiVersion) {
        ObjectNode node = JSON.createObjectNode()
            .put("program_id", HEX32)
            .put("lifecycle", "active")
            .put("version", 1)
            .put("code_hash", "22".repeat(32));
        node.set("abi_version", abiVersion);
        return node.put("receipt_digest", "33".repeat(32))
            .put("deployment_receipt_digest", "44".repeat(32))
            .put("discovery_public_key", "55".repeat(32))
            .put("discovery_signature", "66".repeat(64))
            .put("state_root", "77".repeat(32))
            .put("observed_sequence", "9")
            .put("observed_at", "1700000000000")
            .put("valid_through", "1700000005000")
            .put("verification", "registry-receipt-and-current-head-verified");
    }

    private static ObjectNode programInterface(JsonNode abiVersion) {
        ObjectNode node = JSON.createObjectNode()
            .put("program_id", HEX32)
            .put("version", 1)
            .put("code_hash", "22".repeat(32));
        node.set("abi_version", abiVersion);
        node.put("interface", "0102")
            .put("interface_digest", "88".repeat(32))
            .put("receipt_digest", "44".repeat(32))
            .put("state_root", "77".repeat(32))
            .put("observed_sequence", "9")
            .put("observed_at", "1700000000000")
            .put("valid_through", "1700000005000");
        node.putObject("source").put("status", "unpublished");
        return node.put("verification", "deployment-interface-and-current-head-verified");
    }

    @Test
    void decodesEveryNamedAbiVersionAndSerializesItBackToTheNumber() throws Exception {
        for (int version = 1; version <= 5; version++) {
            ProgramGuestAbi expected = ORDERED.get(version - 1);
            JsonNode number = JSON.getNodeFactory().numberNode(version);

            ProgramDiscoverResponse discovered = JSON.treeToValue(discovery(number), ProgramDiscoverResponse.class);
            assertSame(expected, discovered.abi_version());
            assertEquals(version, JSON.valueToTree(discovered).get("abi_version").intValue());
            assertTrue(JSON.valueToTree(discovered).get("abi_version").isInt());

            ProgramInterfaceResponse described = JSON.treeToValue(programInterface(number), ProgramInterfaceResponse.class);
            assertSame(expected, described.abi_version());
            assertEquals("unpublished", described.source().status());
            assertEquals(version, JSON.valueToTree(described).get("abi_version").intValue());

            assertEquals(version, expected.wire());
            assertSame(expected, ProgramGuestAbi.fromWire(version));
            assertEquals(Integer.toString(version), JSON.writeValueAsString(expected));
            assertSame(expected, JSON.readValue(Integer.toString(version), ProgramGuestAbi.class));
        }
        assertEquals(ORDERED, List.of(ProgramGuestAbi.values()));
    }

    @Test
    void refusesUnnamedVersionsAndTheVariantLabel() {
        for (int version : new int[] {0, 6}) {
            IllegalArgumentException direct = assertThrows(IllegalArgumentException.class,
                () -> ProgramGuestAbi.fromWire(version));
            assertEquals("unknown schema variant", direct.getMessage());
            JsonNode number = JSON.getNodeFactory().numberNode(version);
            ValueInstantiationException discovered = assertThrows(ValueInstantiationException.class,
                () -> JSON.treeToValue(discovery(number), ProgramDiscoverResponse.class));
            assertInstanceOf(IllegalArgumentException.class, discovered.getCause());
            assertEquals("unknown schema variant", discovered.getCause().getMessage());
            ValueInstantiationException described = assertThrows(ValueInstantiationException.class,
                () -> JSON.treeToValue(programInterface(number), ProgramInterfaceResponse.class));
            assertEquals("unknown schema variant", described.getCause().getMessage());
        }
        JsonNode label = JSON.getNodeFactory().textNode("ABI_V5_VERSION");
        DatabindException discovered = assertThrows(DatabindException.class,
            () -> JSON.treeToValue(discovery(label), ProgramDiscoverResponse.class));
        assertInstanceOf(MismatchedInputException.class, discovered);
        DatabindException described = assertThrows(DatabindException.class,
            () -> JSON.treeToValue(programInterface(label), ProgramInterfaceResponse.class));
        assertInstanceOf(MismatchedInputException.class, described);
        assertThrows(MismatchedInputException.class,
            () -> JSON.readValue("\"ABI_V5_VERSION\"", ProgramGuestAbi.class));
    }
}
