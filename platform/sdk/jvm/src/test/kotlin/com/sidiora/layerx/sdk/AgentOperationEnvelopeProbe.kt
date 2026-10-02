package com.sidiora.layerx.sdk

import com.fasterxml.jackson.databind.JsonNode
import com.fasterxml.jackson.databind.ObjectMapper
import com.fasterxml.jackson.databind.node.ObjectNode
import java.math.BigInteger
import java.net.URI
import java.net.http.HttpClient
import java.nio.file.Files
import java.nio.file.Path
import java.security.KeyStore
import java.security.cert.CertificateFactory
import java.time.Duration
import java.util.concurrent.CompletionException
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManagerFactory
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertInstanceOf
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

/** Kotlin probe of the real unified gateway and agent daemon envelope route; every input is required. */
class KotlinAgentOperationEnvelopeProbe {
    private val json = ObjectMapper()
    private val node = json.constructType(JsonNode::class.java)

    private fun required(name: String): String =
        System.getenv(name)?.takeIf { it.isNotEmpty() } ?: throw IllegalStateException("missing required probe input $name")

    private fun client(): HttpClient {
        val trust = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null, null) }
        val certificates = Files.newInputStream(Path.of(required("LAYERX_AGENT_PROBE_GATEWAY_CA"))).use {
            CertificateFactory.getInstance("X.509").generateCertificates(it)
        }
        check(certificates.isNotEmpty()) { "LAYERX_AGENT_PROBE_GATEWAY_CA holds no certificate" }
        certificates.forEachIndexed { index, certificate -> trust.setCertificateEntry("ca$index", certificate) }
        val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(trust) }
        val tls = SSLContext.getInstance("TLS").apply { init(null, factory.trustManagers, null) }
        return HttpClient.newBuilder().sslContext(tls).followRedirects(HttpClient.Redirect.NEVER)
            .version(HttpClient.Version.HTTP_1_1).build()
    }

    private fun transport(generation: String): HttpProductionTransport {
        val gateway = URI.create(required("LAYERX_AGENT_PROBE_GATEWAY_URL"))
        check(gateway.scheme.equals("https", ignoreCase = true)) { "LAYERX_AGENT_PROBE_GATEWAY_URL must be https" }
        val admission = HttpProductionTransport.LayerXKeyCredential(
            required("LAYERX_AGENT_PROBE_API_KEY_ID"),
            SecretBytes(required("LAYERX_AGENT_PROBE_API_KEY").toByteArray(Charsets.US_ASCII)),
        )
        val session = HttpProductionTransport.AgentSessionCredential(
            required("LAYERX_AGENT_PROBE_TENANT"),
            required("LAYERX_AGENT_PROBE_SESSION_ID"),
            SecretBytes(required("LAYERX_AGENT_PROBE_TOKEN_ID").toByteArray(Charsets.US_ASCII)),
            generation,
        )
        return HttpProductionTransport(client(), json, gateway, gateway, Duration.ofSeconds(30), admission, session)
    }

    private fun request(name: String): ObjectNode =
        json.readTree(required(name)) as? ObjectNode ?: throw IllegalStateException("$name must be a JSON object")

    private fun read(transport: HttpProductionTransport, operation: String, input: String): JsonNode =
        transport.call<JsonNode>(ProductionTransport.Call(agentOperation(operation), request(input), null, null), node)
            .toCompletableFuture().join()

    private fun refusal(transport: HttpProductionTransport, operation: String, input: String): PlatformSdkException {
        val call = ProductionTransport.Call(agentOperation(operation), request(input), null, null)
        val failure = assertThrows(CompletionException::class.java) {
            transport.call<JsonNode>(call, node).toCompletableFuture().join()
        }
        return assertInstanceOf(PlatformSdkException::class.java, failure.cause)
    }

    @Test
    fun authenticatedReadsProgramReadAndApprovalListUseTheEnvelopeRoute() {
        val transport = transport(required("LAYERX_AGENT_PROBE_GENERATION"))
        assertNotNull(read(transport, "read.account", "LAYERX_AGENT_PROBE_READ_ACCOUNT_REQUEST"))
        assertNotNull(read(transport, "program.discover", "LAYERX_AGENT_PROBE_PROGRAM_DISCOVER_REQUEST"))
        assertNotNull(read(transport, "approval.list", "LAYERX_AGENT_PROBE_APPROVAL_LIST_REQUEST"))
    }

    @Test
    fun staleGenerationIsRefusedBySessionControl() {
        val generation = BigInteger(required("LAYERX_AGENT_PROBE_GENERATION"))
        check(generation.signum() > 0) { "LAYERX_AGENT_PROBE_GENERATION must exceed 0 for the stale case" }
        val error = refusal(transport(generation.subtract(BigInteger.ONE).toString()), "read.account",
            "LAYERX_AGENT_PROBE_READ_ACCOUNT_REQUEST")
        assertEquals(SchemaErrors.AgentClass.POLICY_REFUSAL, error.agentClass())
    }

    @Test
    fun retiredFaucetIsUnavailable() {
        val error = refusal(transport(required("LAYERX_AGENT_PROBE_GENERATION")), "faucet.claim",
            "LAYERX_AGENT_PROBE_FAUCET_CLAIM_REQUEST")
        assertEquals(SchemaErrors.AgentClass.UNAVAILABLE_CAPABILITY, error.agentClass())
        assertEquals(SchemaErrors.AgentRetriability.TERMINAL, error.agentRetriability())
    }
}
