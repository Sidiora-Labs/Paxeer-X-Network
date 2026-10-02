using System.Security.Cryptography.X509Certificates;
using System.Text.Json;
using LayerX.Sdk;
using Xunit;

namespace LayerX.Sdk.Tests;

[Trait("Category", "AgentOperationEnvelopeProbe")]
public sealed class AgentOperationEnvelopeTests
{
    private static readonly Dictionary<string, string> NoPathParameters = new(StringComparer.Ordinal);

    private static string Required(string name) =>
        Environment.GetEnvironmentVariable(name) is { Length: > 0 } value
            ? value
            : throw new InvalidOperationException($"required probe input {name} is absent");

    private static byte[] Hex32(string name)
    {
        var value = Required(name);
        if (value.Length != 64 || value.Any(character => !(character is >= '0' and <= '9' or >= 'a' and <= 'f')))
            throw new InvalidOperationException($"probe input {name} is not 64 lowercase hex characters");
        return Convert.FromHexString(value);
    }

    private static ulong Generation()
    {
        var value = Required("LAYERX_AGENT_ENVELOPE_GENERATION");
        if ((value != "0" && value[0] == '0') || value.Any(character => character is < '0' or > '9') ||
            !ulong.TryParse(value, out var generation))
            throw new InvalidOperationException("probe input LAYERX_AGENT_ENVELOPE_GENERATION is not a canonical u64");
        return generation;
    }

    private static JsonValue RequestFile(string name)
    {
        var request = JsonSerializer.Deserialize<JsonValue>(File.ReadAllBytes(Required(name)));
        return request is JsonValue.ObjectValue
            ? request
            : throw new InvalidOperationException($"probe input {name} does not hold a JSON object request");
    }

    private static AgentSessionCredential Credential(ulong generation) =>
        new(Required("LAYERX_AGENT_ENVELOPE_TENANT"), Hex32("LAYERX_AGENT_ENVELOPE_SESSION_ID"),
            Hex32("LAYERX_AGENT_ENVELOPE_TOKEN_ID"), generation);

    private static AgentEnvelopeTransport Transport(AgentSessionCredential? credential)
    {
        var endpoint = new Uri(Required("LAYERX_AGENT_ENVELOPE_ENDPOINT"), UriKind.Absolute);
        using var root = X509Certificate2.CreateFromPemFile(Required("LAYERX_AGENT_ENVELOPE_CA_PEM"));
        return new AgentEnvelopeTransport(endpoint, credential, trustedRoot: root);
    }

    private static TransportCall Read(PlatformOperation operation, JsonValue request) =>
        new(operation, request, NoPathParameters, null);

    private static void AssertSuccess(AgentEnvelopeResult result)
    {
        Assert.False(string.IsNullOrEmpty(result.RequestId));
        Assert.IsType<JsonValue.ObjectValue>(result.VerificationStatus);
        Assert.IsNotType<JsonValue.NullValue>(result.Value);
    }

    [Fact]
    public async Task AuthenticatedReadProgramReadAndApprovalListReachTheDaemonThroughTheGateway()
    {
        using var credential = Credential(Generation());
        using var transport = Transport(credential);
        AssertSuccess(await transport.SendEnvelopeAsync(Read(PlatformOperation.AgentReadAccount,
            RequestFile("LAYERX_AGENT_ENVELOPE_READ_ACCOUNT_REQUEST"))));
        AssertSuccess(await transport.SendEnvelopeAsync(Read(PlatformOperation.AgentProgramDiscover,
            RequestFile("LAYERX_AGENT_ENVELOPE_PROGRAM_READ_REQUEST"))));
        AssertSuccess(await transport.SendEnvelopeAsync(Read(PlatformOperation.AgentApprovalList,
            RequestFile("LAYERX_AGENT_ENVELOPE_APPROVAL_LIST_REQUEST"))));
    }

    [Fact]
    public async Task RetiredFaucetClaimReturnsTerminalUnavailableCapability()
    {
        using var credential = Credential(Generation());
        using var transport = Transport(credential);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(Read(PlatformOperation.AgentFaucetClaim, JsonValue.EmptyObject)));
        Assert.Equal(SdkErrorCode.UnavailableCapability, error.Code);
        Assert.Equal(RetryClass.Never, error.Retry);
        Assert.False(string.IsNullOrEmpty(error.RequestId));
    }

    [Fact]
    public async Task WrongGenerationIsRefusedByTheDaemonSessionAuthority()
    {
        var generation = Generation();
        using var credential = Credential(generation == ulong.MaxValue ? generation - 1 : generation + 1);
        using var transport = Transport(credential);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(Read(PlatformOperation.AgentReadAccount,
                RequestFile("LAYERX_AGENT_ENVELOPE_READ_ACCOUNT_REQUEST"))));
        Assert.Equal(SdkErrorCode.PolicyRefusal, error.Code);
        Assert.Equal(RetryClass.Never, error.Retry);
    }

    [Fact]
    public async Task CatalogueReadWithoutSessionCredentialIsRefusedBeforeSending()
    {
        using var transport = Transport(null);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(Read(PlatformOperation.AgentReadAccount,
                RequestFile("LAYERX_AGENT_ENVELOPE_READ_ACCOUNT_REQUEST"))));
        Assert.Equal(SdkErrorCode.CapabilityRefusal, error.Code);
    }

    [Fact]
    public async Task MutationWithoutIdempotencyKeyIsRefusedBeforeSending()
    {
        using var credential = Credential(Generation());
        using var transport = Transport(credential);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() =>
            transport.SendEnvelopeAsync(Read(PlatformOperation.AgentBudgetCreate, JsonValue.EmptyObject)));
        Assert.Equal(SdkErrorCode.IdempotencyRequired, error.Code);
    }
}
