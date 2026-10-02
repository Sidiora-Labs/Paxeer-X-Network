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
private val decodeCases = setOf("read_decode_failure", "mutation_decode_unknown")

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

private fun transport(config: JsonNode, credential: JsonNode, generation: String): HttpProductionTransport =
    transport(config, config.text("endpoint"), credential, generation)

private fun transport(config: JsonNode, target: String, credential: JsonNode, generation: String): HttpProductionTransport {
    val endpoint = URI.create(target)
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

private fun request(provisioned: JsonNode, field: String): ObjectNode =
    provisioned.get(field) as? ObjectNode ?: throw IllegalStateException("provisioned request lacks $field")

private fun read(transport: HttpProductionTransport, operation: String, request: ObjectNode): JsonNode =
    transport.call<JsonNode>(ProductionTransport.Call(agentOperation(operation), request, null, null), node)
        .toCompletableFuture().join()

private fun reply(transport: HttpProductionTransport, operation: String, request: ObjectNode): HttpProductionTransport.AgentReply =
    transport.callAgentReply(ProductionTransport.Call(agentOperation(operation), request, null, null))
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

private fun decodeCase(config: JsonNode, credential: JsonNode, generation: String, requests: JsonNode, id: String, responses: Path) {
    val provisioned = requests.get(id)?.takeIf { it.isObject } ?: throw IllegalStateException("provisioned request lacks case $id")
    val operation = agentOperation(provisioned.text("operation"))
    val mutating = OperationCatalog.requiresIdempotency(operation)
    check(mutating == (id == "mutation_decode_unknown")) { "case $id operation mutability" }
    val key = if (mutating) {
        val value = provisioned.text("idempotency_key")
        check(Regex("[0-9a-f]{64}").matches(value)) { "case $id idempotency_key must be 64 lowercase hex" }
        IdempotencyKey(value)
    } else {
        check(provisioned.get("idempotency_key")?.isNull ?: true) { "case $id is non-mutating and carries no idempotency_key" }
        null
    }
    val endpoint = if (provisioned.has("endpoint")) provisioned.text("endpoint") else config.text("endpoint")
    val target = transport(config, endpoint, credential, generation)
    val call = ProductionTransport.Call(operation, request(provisioned, "request"), null, key)
    val error = try {
        target.callAgentReply(call).toCompletableFuture().join()
        null
    } catch (failure: CompletionException) {
        failure.cause as? PlatformSdkException ?: throw IllegalStateException("case $id failed outside the SDK error contract")
    } ?: throw IllegalStateException("case $id decoded a reply")
    if (mutating) {
        check(error.code() == PlatformSdkException.Code.UNKNOWN_OUTCOME &&
            error.retry() == PlatformSdkException.Retry.UNKNOWN_OUTCOME) { "case $id was not an unknown outcome" }
    } else {
        check(error.code() == PlatformSdkException.Code.DECODE_FAILURE &&
            error.retry() == PlatformSdkException.Retry.NEVER) { "case $id was not a never-retry decode failure" }
    }
    val record = json.createObjectNode()
    record.put("sdk_error", error.code().wire())
    record.put("retry", error.retry().wire())
    Files.write(responses.resolve("$id.json"), json.writeValueAsBytes(record))
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
        if (id in decodeCases) {
            decodeCase(config, credential, generation, requests, id, responses)
            println("PAXEER_X_AGENT_ENVELOPE_CASE $id passed")
            continue
        }
        val operation = operations[id] ?: throw IllegalStateException("unsupported case $id")
        val provisioned = requests.get(id)?.takeIf { it.isObject } ?: throw IllegalStateException("provisioned request lacks case $id")
        check(provisioned.text("operation") == operation) { "case $id operation must be $operation" }
        check(provisioned.get("idempotency_key")?.isNull ?: true) { "case $id is non-mutating and carries no idempotency_key" }
        val result = reply(transport, operation, request(provisioned, "request"))
        check(result.status() == 200) { "case $id status ${result.status()}" }
        check(Regex("0|[1-9][0-9]{0,19}").matches(result.requestId()) && BigInteger(result.requestId()).bitLength() <= 64) {
            "case $id request_id is not a canonical unsigned 64-bit decimal"
        }
        check(result.requestId() == result.body().path("request_id").textValue()) { "case $id request_id not preserved" }
        val value = result.value()
        check(value != null && !value.isNull) { "case $id returned no value" }
        val sent = result.body().get("value")
        val expected = if (sent is ObjectNode) SchemaTypes.canonicalBody(sent) else sent
        check(expected != null && expected == value) { "case $id value not preserved" }
        val verification = result.verificationStatus()
        check(verification != null && verification == result.body().get("verification_status") &&
            verification.path("state").textValue() in setOf("achieved", "unverified")) { "case $id verification_status not preserved" }
        val record = json.createObjectNode()
        record.put("status", result.status())
        record.set<JsonNode>("body", result.body())
        Files.write(responses.resolve("$id.json"), json.writeValueAsBytes(record))
        println("PAXEER_X_AGENT_ENVELOPE_CASE $id passed")
    }

    val current = BigInteger(generation)
    check(current.signum() > 0) { "credential generation must exceed 0 for the stale case" }
    val stale = refusal(transport(config, credential, current.subtract(BigInteger.ONE).toString()),
        "read.account", request(requests.path("read"), "request"))
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
