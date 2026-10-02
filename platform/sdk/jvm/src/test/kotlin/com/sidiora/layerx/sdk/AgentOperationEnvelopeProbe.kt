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
import kotlin.system.exitProcess

private val json = ObjectMapper()
private val node = json.constructType(JsonNode::class.java)
private val operations = mapOf("read" to "read.account", "program_read" to "program.interface", "approval_list" to "approval.list")

private fun JsonNode.text(field: String): String =
    get(field)?.takeIf { it.isTextual && it.textValue().isNotEmpty() }?.textValue()
        ?: throw IllegalStateException("missing $field")

private fun client(caPem: String): HttpClient {
    val trust = KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null, null) }
    val certificates = Files.newInputStream(Path.of(caPem)).use { CertificateFactory.getInstance("X.509").generateCertificates(it) }
    check(certificates.isNotEmpty()) { "ca_pem holds no certificate" }
    certificates.forEachIndexed { index, certificate -> trust.setCertificateEntry("ca$index", certificate) }
    val factory = TrustManagerFactory.getInstance(TrustManagerFactory.getDefaultAlgorithm()).apply { init(trust) }
    val tls = SSLContext.getInstance("TLS").apply { init(null, factory.trustManagers, null) }
    return HttpClient.newBuilder().sslContext(tls).followRedirects(HttpClient.Redirect.NEVER)
        .version(HttpClient.Version.HTTP_1_1).build()
}

private fun transport(config: JsonNode, credential: JsonNode, generation: String): HttpProductionTransport {
    val endpoint = URI.create(config.text("endpoint"))
    check(endpoint.scheme.equals("https", ignoreCase = true)) { "endpoint must be https" }
    val key = Files.readString(Path.of(config.text("gateway_api_key_file")), Charsets.US_ASCII).trim()
    val separator = key.indexOf(':')
    check(separator > 0 && key.indexOf(':', separator + 1) < 0) { "gateway api key must be <id>:<secret>" }
    val admission = HttpProductionTransport.LayerXKeyCredential(
        key.substring(0, separator), SecretBytes(key.substring(separator + 1).toByteArray(Charsets.US_ASCII)))
    val session = HttpProductionTransport.AgentSessionCredential(
        credential.text("tenant"), credential.text("session_id"),
        SecretBytes(credential.text("token_id").toByteArray(Charsets.US_ASCII)), generation)
    return HttpProductionTransport(client(config.text("ca_pem")), json, endpoint, endpoint, Duration.ofSeconds(30), admission, session)
}

private fun request(requests: JsonNode, operation: String): ObjectNode =
    requests.get(operation) as? ObjectNode ?: throw IllegalStateException("provisioned request lacks $operation")

private fun read(transport: HttpProductionTransport, operation: String, request: ObjectNode): JsonNode =
    transport.call<JsonNode>(ProductionTransport.Call(agentOperation(operation), request, null, null), node)
        .toCompletableFuture().join()

private fun refusal(transport: HttpProductionTransport, operation: String, request: ObjectNode): PlatformSdkException {
    try {
        read(transport, operation, request)
    } catch (failure: CompletionException) {
        return failure.cause as? PlatformSdkException
            ?: throw IllegalStateException("$operation failed outside the SDK error contract")
    }
    throw IllegalStateException("$operation was not refused")
}

private fun run(): Int {
    val caseFile = System.getenv("PAXEER_X_AGENT_ENVELOPE_CASE")?.takeIf { it.isNotEmpty() }
        ?: throw IllegalStateException("missing PAXEER_X_AGENT_ENVELOPE_CASE")
    val config = json.readTree(Files.readAllBytes(Path.of(caseFile)))
    check(config != null && config.isObject) { "case file must be a JSON object" }
    check(config.text("phase") == "read") { "the JVM probe implements only the read phase" }
    val credential = json.readTree(Files.readAllBytes(Path.of(config.text("credential_file"))))
    check(credential != null && credential.isObject && credential.size() == 4) { "credential coordinates" }
    val generation = credential.text("generation")
    val requests = config.get("requests")
    check(requests != null && requests.isObject) { "requests must be a JSON object" }
    val cases = config.get("cases")
    check(cases != null && cases.isArray && !cases.isEmpty) { "cases must be a non-empty array" }
    val responses = Path.of(config.text("response_dir"))
    check(Files.isDirectory(responses)) { "response_dir must exist" }

    val transport = transport(config, credential, generation)
    val seen = linkedSetOf<String>()
    for (entry in cases) {
        check(entry.isTextual && seen.add(entry.textValue())) { "case ids must be unique strings" }
        val id = entry.textValue()
        val operation = operations[id] ?: throw IllegalStateException("unsupported case $id")
        val value = read(transport, operation, request(requests, operation))
        check(!value.isNull) { "case $id returned no value" }
        Files.write(responses.resolve("$id.json"), json.writeValueAsBytes(value))
        println("PAXEER_X_AGENT_ENVELOPE_CASE $id passed")
    }

    val current = BigInteger(generation)
    check(current.signum() > 0) { "credential generation must exceed 0 for the stale case" }
    val stale = refusal(transport(config, credential, current.subtract(BigInteger.ONE).toString()),
        "read.account", request(requests, "read.account"))
    check(stale.agentClass() == SchemaErrors.AgentClass.POLICY_REFUSAL) { "stale generation was not a policy refusal" }
    val faucet = refusal(transport, "faucet.claim", json.createObjectNode())
    check(faucet.agentClass() == SchemaErrors.AgentClass.UNAVAILABLE_CAPABILITY &&
        faucet.agentRetriability() == SchemaErrors.AgentRetriability.TERMINAL) { "faucet.claim was not terminal unavailable" }
    return seen.size
}

fun main() {
    val passed = try {
        run()
    } catch (failure: Throwable) {
        val detail = when (failure) {
            is PlatformSdkException -> " ${failure.code()} ${failure.agentClass()}"
            is IllegalStateException -> " ${failure.message}"
            else -> ""
        }
        System.err.println("agent envelope probe failed: ${failure.javaClass.name}$detail")
        exitProcess(1)
    }
    println("PAXEER_X_AGENT_ENVELOPE_CASES=$passed")
    System.out.flush()
}
