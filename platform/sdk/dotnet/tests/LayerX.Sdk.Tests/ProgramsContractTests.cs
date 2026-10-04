using System.Buffers.Binary;
using System.Numerics;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using LayerX.Sdk;
using Org.BouncyCastle.Crypto.Parameters;
using Org.BouncyCastle.Crypto.Signers;
using Xunit;

namespace LayerX.Sdk.Tests;

public sealed class ProgramsContractTests
{
    private sealed class ProgramTransport(JsonValue response) : IPlatformTransport
    {
        public Task<JsonValue> SendAsync(TransportCall call, CancellationToken cancellationToken = default) =>
            Task.FromException<JsonValue>(new PlatformSdkException(SdkErrorCode.UnavailableCapability, RetryClass.Never));
        public Task<JsonValue> SendProgramAsync(ProgramTransportCall call, CancellationToken cancellationToken = default) =>
            Task.FromResult(response);
    }

    [Fact]
    public void ProgramsClientRequiresIndependentNonzeroSequencerPin()
    {
        var client = new PlatformClient(new ProgramTransport(JsonValue.EmptyObject));
        Assert.Throws<PlatformSdkException>(() => new ProgramsClient(client, new byte[32]));
        _ = new ProgramsClient(client, Enumerable.Repeat((byte)1, 32).ToArray());
    }

    [Fact]
    public async Task PendingReceiptMayOmitRetainedBytesButMustBindExpectedActivity()
    {
        var key = new string('a', 64); var activity = Enumerable.Repeat((byte)0x11, 32).ToArray();
        var value = JsonValue.Object(new Dictionary<string, JsonValue>
        {
            ["state"] = JsonValue.String("unknown"),
            ["activity_id"] = JsonValue.String(Convert.ToHexString(activity).ToLowerInvariant()),
            ["idempotency_key"] = JsonValue.String(key),
        });
        var programs = new ProgramsClient(new PlatformClient(new ProgramTransport(value)),
            Enumerable.Repeat((byte)1, 32).ToArray());
        var pending = await programs.ReceiptAsync(new IdempotencyKey(key), activity, "sequencer-signed");
        Assert.True(pending.IsUnknown); Assert.Null(pending.RetainedSignedActivity);
        var error = await Assert.ThrowsAsync<PlatformSdkException>(() => programs.ReceiptAsync(new IdempotencyKey(key),
            Enumerable.Repeat((byte)0x12, 32).ToArray(), "sequencer-signed"));
        Assert.Equal(SdkErrorCode.VerificationFailure, error.Code);
    }

    [Fact]
    public async Task DiscoveryClientReturnsBoundedTypedResponseWithHonestVerificationLevel()
    {
        var program = new string('1', 64); var now = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        var value = JsonValue.Object(new Dictionary<string, JsonValue>
        {
            ["program_id"] = JsonValue.String(program), ["lifecycle"] = JsonValue.String("active"),
            ["version"] = JsonValue.Integer(7), ["code_hash"] = JsonValue.String(new string('2', 64)),
            ["abi_version"] = JsonValue.Integer(2), ["receipt_digest"] = JsonValue.String(new string('3', 64)),
            ["state_root"] = JsonValue.String(new string('4', 64)), ["observed_sequence"] = JsonValue.String("9"),
            ["observed_at"] = JsonValue.String(now.ToString()), ["valid_through"] = JsonValue.String((now + 60_000).ToString()),
            ["verification"] = JsonValue.String("registry-receipt-and-current-head-verified"),
        });
        var programs = new ProgramsClient(new PlatformClient(new ProgramTransport(value)),
            Enumerable.Repeat((byte)1, 32).ToArray());

        var discovered = await programs.DiscoverAsync(Convert.FromHexString(program), "sequencer-signed");
        Assert.Equal(ProgramLifecycle.Active, discovered.Lifecycle);
        Assert.Equal((uint)7, discovered.Version); Assert.Equal((ulong)9, discovered.ObservedSequence);
        Assert.Equal("server-side-receipt-verification-only", discovered.Verification);
    }

    [Fact]
    public void OperationValueVerificationStatusMatrixIsExact()
    {
        var achieved = Status("Achieved", null); var discovery = Status("Unverified", "server_side_receipt_verification_only");
        var pending = Status("Unverified", "receipt_pending");
        var unknown = JsonValue.Object(new Dictionary<string, JsonValue> { ["state"] = JsonValue.String("unknown") });
        var inFlight = JsonValue.Object(new Dictionary<string, JsonValue> { ["state"] = JsonValue.String("pending") });
        var terminal = JsonValue.Object(new Dictionary<string, JsonValue> { ["state"] = JsonValue.String("executed") });
        var oldUnverified = JsonValue.Object(new Dictionary<string, JsonValue>
        {
            ["state"] = JsonValue.String("Unverified"), ["level"] = JsonValue.String("SequencerSigned"),
            ["reason"] = JsonValue.String("server_side_receipt_verification_only"),
        });
        Assert.True(TransportStatus("program.discover", JsonValue.EmptyObject, discovery));
        Assert.False(TransportStatus("program.discover", JsonValue.EmptyObject, achieved));
        Assert.False(TransportStatus("program.discover", JsonValue.EmptyObject, oldUnverified));
        Assert.True(TransportStatus("program.receipt", unknown, pending));
        Assert.True(TransportStatus("program.activity", inFlight, pending));
        Assert.False(TransportStatus("program.receipt", unknown, achieved));
        Assert.False(TransportStatus("program.call", inFlight, achieved));
        Assert.True(TransportStatus("program.call", terminal, achieved));
        Assert.True(TransportStatus("program.simulate", JsonValue.EmptyObject, achieved));
        Assert.False(TransportStatus("program.simulate", JsonValue.EmptyObject, discovery));
    }

    [Fact]
    public void TransferSetV1AndV2ProduceTheSameCanonicalKernelRoot()
    {
        var v1 = TransferAuthorization(1); var v2 = TransferAuthorization(2);
        var rootV1 = (byte[])Invoke("DecodeAuthorizationRoot", v1)!;
        var rootV2 = (byte[])Invoke("DecodeAuthorizationRoot", v2)!;
        Assert.Equal(rootV1, rootV2); Assert.Contains(rootV1, value => value != 0);
        var mutated = v2.ToArray(); mutated[^33] ^= 1;
        Assert.NotEqual(rootV2, (byte[])Invoke("DecodeAuthorizationRoot", mutated)!);
    }

    [Fact]
    public void OccupancyV1V2V3AndAggregateBindingsAreExact()
    {
        var asset = Enumerable.Repeat((byte)0x66, 32).ToArray();
        for (var version = 1; version <= 3; version++)
        {
            var binding = Invoke("DecodeOccupancySettlement", EmptyOccupancy(version))!;
            Assert.Equal(BigInteger.Zero, Property<BigInteger>(binding, "ByteBatches"));
            Assert.Equal(BigInteger.Zero, Property<BigInteger>(binding, "FeeUnits"));
            Assert.Equal(new byte[32], (byte[])Invoke("OccupancyTransferRoot", binding, asset)!);
        }
        var evidence = ChargedOccupancy(); var charged = Invoke("DecodeOccupancySettlement", evidence)!;
        Assert.Equal(new BigInteger(3), Property<BigInteger>(charged, "ByteBatches"));
        Assert.Equal(new BigInteger(6), Property<BigInteger>(charged, "FeeUnits"));
        Assert.Contains((byte[])Invoke("OccupancyTransferRoot", charged, asset)!, value => value != 0);
        var mutated = evidence.ToArray();
        var declaredFeeLowByte = Encoding.UTF8.GetByteCount("LXP/storage-occupancy-settlement/v3\0") + 8 + 4 + 7 * 8 + 16 + 15;
        mutated[declaredFeeLowByte] ^= 1;
        var exception = Assert.Throws<TargetInvocationException>(() => Invoke("DecodeOccupancySettlement", mutated));
        Assert.IsType<InvalidDataException>(exception.InnerException);
    }

    [Theory]
    [InlineData("native-program-call-v3.json")]
    [InlineData("native-program-call-v4.json")]
    public async Task NativeSignedFixturesUseCanonicalBinaryGatewayTransport(string name)
    {
        using var document = Fixture(name);
        var vector = document.RootElement;
        var payload = FixtureBytes(vector, "payload_hex");
        var signed = FixtureBytes(vector, "signed_activity_hex");
        var native = NativeProgramCall.Decode(payload);
        var call = new ProgramCall(native, new ProtocolAmount(vector.GetProperty("fee_limit").GetString()!), signed);
        var encoded = Assert.IsType<JsonValue.ObjectValue>(Invoke("Encode", call));
        var key = vector.GetProperty("idempotency_key_hex").GetString()!;
        using var token = new AccessToken(Encoding.ASCII.GetBytes("fixture-bearer"));
        var transport = new AgentHttpTransport(new Uri("http://127.0.0.1:8080"), accessToken: token);
        var build = typeof(AgentHttpTransport).GetMethod("ProgramRequest", BindingFlags.Instance | BindingFlags.NonPublic)!;
        foreach (var operation in new[] { "program.call", "program.simulate" })
        {
            var request = new ProgramTransportCall(operation, encoded, new Dictionary<string, string>(),
                operation == "program.call" ? new IdempotencyKey(key) : null);
            using var http = Assert.IsType<HttpRequestMessage>(build.Invoke(transport, [request]));
            Assert.Equal(operation == "program.call" ? "/v1/programs/call" : "/v1/programs/simulate", http.RequestUri!.AbsolutePath);
            Assert.Equal(HttpMethod.Post, http.Method);
            Assert.Equal("application/octet-stream", http.Content!.Headers.ContentType!.MediaType);
            Assert.Equal(signed, await http.Content.ReadAsByteArrayAsync());
            if (operation == "program.call") Assert.Equal(key, Assert.Single(http.Headers.GetValues("Idempotency-Key")));
            else Assert.False(http.Headers.Contains("Idempotency-Key"));
            var changed = native with { ResponseCapacity = native.ResponseCapacity + 1 };
            var changedCall = new ProgramCall(changed, call.Budget.FeeLimit, signed);
            Assert.Throws<TargetInvocationException>(() => Invoke("DecodeSignedCall", changedCall));
        }
        foreach (var abi in new ushort[] { 0, 5, ushort.MaxValue })
            Assert.Throws<ArgumentException>(() => (native with { GuestAbi = abi }).Encode());
        foreach (var abi in new ushort[] { 3, 4 })
        {
            var changed = native with { GuestAbi = abi };
            Assert.Equal(abi, NativeProgramCall.Decode(changed.Encode()).GuestAbi);
            Assert.Throws<TargetInvocationException>(() => Invoke("DecodeSignedCall", new ProgramCall(changed, call.Budget.FeeLimit, signed)));
        }
    }

    [Theory]
    [InlineData("native-program-deploy-v4.json", 1)]
    [InlineData("native-program-upgrade-v4.json", 2)]
    public void NativeLifecycleFixturesPreserveSignedBindingAcrossNamedAbiPolicies(string name, int ordinal)
    {
        using var document = Fixture(name);
        var vector = document.RootElement;
        var payload = FixtureBytes(vector, "payload_hex");
        INativeProgramLifecycle operation = ordinal == 1 ? NativeProgramDeploy.Decode(payload) : NativeProgramUpgrade.Decode(payload);
        Assert.Equal((ushort)2, BinaryPrimitives.ReadUInt16BigEndian(payload.AsSpan(32)));
        Assert.Equal(payload, operation.Encode());
        var request = new NativeProgramLifecycleRequest(operation, FixtureBytes(vector, "signed_activity_hex"));
        Assert.Equal(FixtureBytes(vector, "activity_id_hex"), request.ActivityId);
        foreach (var abi in new ushort[] { 3, 4 })
        {
            var changed = payload.ToArray(); BinaryPrimitives.WriteUInt16BigEndian(changed.AsSpan(32), abi);
            INativeProgramLifecycle updated = ordinal == 1 ? NativeProgramDeploy.Decode(changed) : NativeProgramUpgrade.Decode(changed);
            Assert.Equal(changed, updated.Encode());
            Assert.Throws<ArgumentException>(() => new NativeProgramLifecycleRequest(updated, FixtureBytes(vector, "signed_activity_hex")));
        }
        foreach (var abi in new ushort[] { 0, 5, ushort.MaxValue })
        {
            var changed = payload.ToArray(); BinaryPrimitives.WriteUInt16BigEndian(changed.AsSpan(32), abi);
            Assert.Throws<ArgumentException>(() => ordinal == 1 ? (INativeProgramLifecycle)NativeProgramDeploy.Decode(changed) : NativeProgramUpgrade.Decode(changed));
        }
    }

    [Fact]
    public void SignedDiscoveryHeadsBindEveryCanonicalFieldToTheIndependentPin()
    {
        var signer = new Ed25519PrivateKeyParameters(Enumerable.Repeat((byte)0x42, 32).ToArray(), 0);
        var pin = signer.GeneratePublicKey().GetEncoded();
        var programs = new ProgramsClient(new PlatformClient(new AgentHttpTransport(new Uri("http://127.0.0.1:8080"))), pin);
        var verify = typeof(ProgramsClient).GetMethod("VerifyDiscovery", BindingFlags.Instance | BindingFlags.NonPublic)!;
        foreach (var abi in new ushort[] { 1, 2, 3, 4 })
        {
            var head = SignedHead(abi, signer);
            ProgramDiscovery Verify(IReadOnlyDictionary<string, JsonValue> value) => Assert.IsType<ProgramDiscovery>(
                verify.Invoke(programs, [JsonValue.Object(value), new string('1', 64), false, 2000UL]));
            var accepted = Verify(head);
            Assert.Equal(abi, accepted.AbiVersion);
            Assert.Equal("sequencer-signed-discovery-head", accepted.Verification);
            Assert.Equal(Repeat(0x44), accepted.DeploymentReceiptDigest);
            foreach (var (field, changed) in new (string, JsonValue)[]
            {
                ("version", JsonValue.Integer(4)), ("code_hash", JsonValue.String(new string('9', 64))),
                ("abi_version", JsonValue.Integer(abi == 4 ? 3 : 4)),
                ("observed_sequence", JsonValue.String("78")), ("observed_at", JsonValue.String("1001")),
                ("valid_through", JsonValue.String("9999")), ("state_root", JsonValue.String(new string('9', 64))),
                ("receipt_digest", JsonValue.String(new string('9', 64))),
                ("discovery_public_key", JsonValue.String(new string('9', 64))),
                ("discovery_signature", JsonValue.String(new string('9', 128))),
                ("abi_version", JsonValue.Integer(5)), ("unexpected", JsonValue.String("extra")),
            })
            {
                var altered = new Dictionary<string, JsonValue>(head) { [field] = changed };
                Assert.IsType<PlatformSdkException>(Assert.Throws<TargetInvocationException>(() => Verify(altered)).InnerException);
            }
            foreach (var field in new[] { "discovery_public_key", "discovery_signature", "deployment_receipt_digest" })
            {
                var altered = new Dictionary<string, JsonValue>(head); altered.Remove(field);
                Assert.IsType<PlatformSdkException>(Assert.Throws<TargetInvocationException>(() => Verify(altered)).InnerException);
            }
            if (abi is 3 or 4)
            {
                var unsigned = new Dictionary<string, JsonValue>(head);
                unsigned.Remove("discovery_public_key"); unsigned.Remove("discovery_signature");
                Assert.IsType<PlatformSdkException>(Assert.Throws<TargetInvocationException>(() => Verify(unsigned)).InnerException);
            }
        }
    }

    [Fact]
    public async Task ExecutedNativeReceiptVerifiesLocallyAndRefusesChangedBindings()
    {
        using var document = Fixture("receipt-programs-executed-v4.json");
        var vector = document.RootElement; var batch = vector.GetProperty("authorized_batch");
        var authority = new AuthorizedReceiptBatch(FixtureBytes(batch, "batch_id_hex"), FixtureBytes(batch, "asset_hex"),
            FixtureBytes(batch, "previous_state_root_hex"), FixtureBytes(batch, "resulting_state_root_hex"), FixtureBytes(batch, "sequencer_public_key_hex"));
        var receipt = FixtureBytes(vector, "canonical_receipt_hex"); var terminal = FixtureBytes(vector, "terminal_payload_hex");
        var graph = FixtureBytes(vector, "call_graph_hex");
        var activity = SHA256.HashData(Encoding.UTF8.GetBytes("LXP/v1/activity-id\0").Concat(FixtureBytes(vector, "signed_activity_hex")).ToArray());
        var verified = await ProgramsClient.VerifyReceiptAsync(receipt, authority, activity, 2, terminal, graph, protocolVersion: 3);
        Assert.Equal(FixtureBytes(vector, "receipt_digest_hex"), verified.ReceiptDigest);
        Assert.Equal("sequencer-signed", verified.Level);
        foreach (var target in new[] { "receipt", "terminal", "graph", "activity", "pin" })
        {
            byte[] Changed(byte[] bytes) { var changed = bytes.ToArray(); changed[^1] ^= 1; return changed; }
            await Assert.ThrowsAsync<PlatformSdkException>(async () => await ProgramsClient.VerifyReceiptAsync(
                target == "receipt" ? Changed(receipt) : receipt,
                target == "pin" ? authority with { SequencerPublicKey = Changed(authority.SequencerPublicKey) } : authority,
                target == "activity" ? Changed(activity) : activity, 2,
                target == "terminal" ? Changed(terminal) : terminal,
                target == "graph" ? Changed(graph) : graph, protocolVersion: 3));
        }
    }

    [Fact]
    public void NativeDeploymentInterfaceHeaderBindsTheActualCodeAndAbi()
    {
        using var document = Fixture("native-program-deploy-v3.json");
        var payload = FixtureBytes(document.RootElement, "payload_hex");
        var length = checked((int)BinaryPrimitives.ReadUInt32BigEndian(payload.AsSpan(104)));
        var encoded = payload.AsSpan(108, length).ToArray(); var codeHash = payload[68..100];
        var abi = BinaryPrimitives.ReadUInt16BigEndian(payload.AsSpan(32));
        Assert.True((bool)Invoke("InterfaceHeaderBound", encoded, codeHash, abi)!);
        Assert.False((bool)Invoke("InterfaceHeaderBound", encoded, Repeat(0x99), abi)!);
        foreach (var wrongAbi in new ushort[] { 0, 1, 3, 4, 5, ushort.MaxValue })
            Assert.False((bool)Invoke("InterfaceHeaderBound", encoded, codeHash, wrongAbi)!);
        for (var size = 0; size < Encoding.UTF8.GetByteCount("LayerX/program-interface/v1\0") + 36; size++)
            Assert.False((bool)Invoke("InterfaceHeaderBound", encoded[..size], codeHash, abi)!);
        var changed = encoded.ToArray(); changed[0] ^= 1;
        Assert.False((bool)Invoke("InterfaceHeaderBound", changed, codeHash, abi)!);
    }

    [Fact]
    public async Task GenuineTerminalV5CorpusVerifiesEveryOutcomeAndRetainsLegacyRefusals()
    {
        var path = Environment.GetEnvironmentVariable("PAXEER_X_PROGRAM_TERMINAL_V5_CORPUS");
        Assert.False(string.IsNullOrEmpty(path), "genuine native ABI3/4 terminal-v5 corpus required");
        var file = new FileInfo(path!);
        Assert.True(file.Exists && file.LinkTarget is null && file.Length is > 0 and <= 16_777_216);
        using var corpus = JsonDocument.Parse(File.ReadAllText(path!));
        var root = corpus.RootElement;
        var revision = Environment.GetEnvironmentVariable("PAXEER_X_MAINLINE");
        Assert.False(string.IsNullOrEmpty(revision), "published candidate revision required");
        Assert.Equal(revision, root.GetProperty("source_revision").GetString());
        var pin = FixtureBytes(root, "trusted_sequencer_public_key_hex");
        Assert.Equal(32, pin.Length); Assert.Contains(pin, value => value != 0);
        Assert.Equal(Convert.FromHexString("b4f05aee172965774743f4cd7de4c3621c9e36fd77af7139aafec25eb3fb3360"), pin);
        var programs = new ProgramsClient(new PlatformClient(new AgentHttpTransport(new Uri("http://127.0.0.1:8080"))), pin, protocolVersion: 3);
        var method = typeof(ProgramsClient).GetMethod("VerifyExecutionAsync", BindingFlags.Instance | BindingFlags.NonPublic)!;
        var expected = new HashSet<string>(new[] { 3, 4 }.SelectMany(abi =>
            new[] { "success", "failure", "resource", "callback", "settlement" }.Select(kind => $"{abi}:{kind}")));
        var present = new HashSet<string>();
        foreach (var row in root.GetProperty("cases").EnumerateArray())
        {
            var abi = row.GetProperty("guest_abi").GetUInt16(); var kind = row.GetProperty("outcome").GetString();
            var selector = $"{abi}:{kind}";
            Assert.Contains(selector, expected); Assert.True(present.Add(selector), "duplicate native corpus case");
            Assert.Equal(pin, FixtureBytes(row, "sequencer_public_key_hex"));
            var signed = FixtureBytes(row, "signed_activity_hex");
            var call = NativeCallFromSignedActivity(signed);
            var native = Assert.IsType<NativeProgramCall>(call.NativeCall);
            Assert.Equal(abi, native.GuestAbi);
            Assert.Equal(FixtureBytes(row, "native_call_payload_hex"), native.Encode());
            Assert.Equal(FixtureBytes(row, "program_id_hex"), call.ProgramId);
            var binding = Invoke("DecodeSignedCall", call)!;
            var activity = Property<byte[]>(binding, "ActivityId");
            var program = call.ProgramId;
            var map = await ProjectVerifiedNativeExecution(row, pin, activity, program, abi, Property<byte[]>(binding, "IdempotencyKey"));
            var rawKind = (byte)(kind == "success" ? 1 : kind == "resource" ? 3 : 2);
            Task<VerifiedProgramExecution> Check(IReadOnlyDictionary<string, JsonValue> value, byte[]? wantedActivity = null) =>
                Assert.IsAssignableFrom<Task<VerifiedProgramExecution>>(method.Invoke(programs,
                    [value, kind == "success" ? "executed" : "refused", value.ContainsKey("idempotency_key"), program,
                        wantedActivity ?? activity, null, CancellationToken.None]));
            var verified = await Check(map);
            Assert.Equal(abi, verified.Receipt.Receipt.ProgramOutcome!.AbiVersion);
            Assert.Equal(rawKind, verified.Receipt.Receipt.ProgramOutcome.TerminalKind);
            Assert.Equal("sequencer-signed", verified.Receipt.Level);
            if (kind is "callback" or "settlement")
            {
                var unwrapped = Assert.IsType<byte[]>(Invoke("UnwrapAppliedTerminal", verified.TerminalPayload, verified.Receipt.Receipt.ProgramOutcome));
                var inner = Property<byte[]>(Invoke("UnwrapTerminal", unwrapped)!, "Inner");
                Assert.True(inner.AsSpan().StartsWith(Encoding.UTF8.GetBytes($"LXP/programs/{kind}-failure/v1\0")));
            }
            _ = Invoke("VerifyRequestedAbi", call, verified);
            var crossedCall = new ProgramCall(native with { GuestAbi = (ushort)(abi == 3 ? 4 : 3) }, call.Budget.FeeLimit, signed);
            Assert.IsType<PlatformSdkException>(Assert.Throws<TargetInvocationException>(() => Invoke("VerifyRequestedAbi", crossedCall, verified)).InnerException);
            Assert.Equal(FixtureBytes(row, "canonical_receipt_hex"), Convert.FromHexString(Assert.IsType<JsonValue.StringValue>(map["receipt"]).Value));
            Assert.Equal(FixtureBytes(row, "terminal_payload_hex"), verified.TerminalPayload);
            Assert.Equal(FixtureBytes(row, "call_graph_hex"), verified.CallGraph);
            foreach (var wrongAbi in new ushort[] { 0, 1, 2, 5, ushort.MaxValue, (ushort)(abi == 3 ? 4 : 3) })
                await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { ["guest_abi_version"] = JsonValue.Integer(wrongAbi) }));
            foreach (var field in new[] { "receipt", "terminal_payload", "call_graph", "activity_id", "receipt_digest", "state_root" })
            {
                var bytes = Convert.FromHexString(Assert.IsType<JsonValue.StringValue>(map[field]).Value);
                bytes[^1] ^= 1;
                await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { [field] = JsonValue.String(Convert.ToHexString(bytes).ToLowerInvariant()) }));
            }
            var usage = new Dictionary<string, JsonValue>(Assert.IsType<JsonValue.ObjectValue>(map["usage"]).Value);
            foreach (var field in new[] { "cpu_fuel", "memory_bytes", "storage_read_bytes", "storage_write_bytes", "output_bytes", "fee_units" })
            {
                var changed = new Dictionary<string, JsonValue>(usage) { [field] = JsonValue.String((BigInteger.Parse(Assert.IsType<JsonValue.StringValue>(usage[field]).Value) + 1).ToString()) };
                await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { ["usage"] = JsonValue.Object(changed) }));
            }
            var authority = Assert.IsType<JsonValue.ObjectValue>(map["authority"]).Value;
            foreach (var field in new[] { "batch_id", "asset", "previous_state_root", "resulting_state_root", "sequencer_public_key" })
            {
                var bytes = Convert.FromHexString(Assert.IsType<JsonValue.StringValue>(authority[field]).Value); bytes[0] ^= 1;
                var changed = new Dictionary<string, JsonValue>(authority) { [field] = JsonValue.String(Convert.ToHexString(bytes).ToLowerInvariant()) };
                await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { ["authority"] = JsonValue.Object(changed) }));
            }
            var wrongActivity = activity.ToArray(); wrongActivity[0] ^= 1;
            await Assert.ThrowsAsync<PlatformSdkException>(() => Check(map, wrongActivity));
            foreach (var length in new[] { 0, 1, verified.TerminalPayload.Length - 1 })
                await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { ["terminal_payload"] = JsonValue.String(Convert.ToHexString(verified.TerminalPayload[..length]).ToLowerInvariant()) }));
            await Assert.ThrowsAsync<PlatformSdkException>(() => Check(new Dictionary<string, JsonValue>(map) { ["terminal_payload"] = JsonValue.String(Convert.ToHexString(verified.TerminalPayload).ToLowerInvariant() + "00") }));
            if (kind is "success" or "failure" or "resource")
            {
                var terminal = verified.TerminalPayload;
                var domain = Encoding.UTF8.GetBytes("LXP/program-execution/v5\0");
                var offset = terminal.AsSpan().IndexOf(domain); Assert.True(offset >= 0);
                var outcome = Assert.IsType<JsonValue.ObjectValue>(map["outcome"]).Value;
                void RefuseTerminal(byte[] changed, ProgramReceiptOutcome receipt) => Assert.IsType<PlatformSdkException>(
                    Assert.Throws<TargetInvocationException>(() => Invoke("VerifyTerminal", changed, verified.CallGraph, program, outcome,
                        (ushort)3, receipt with { TerminalPayloadRoot = SHA256.HashData(changed) })).InnerException);
                var historical = terminal.ToArray(); historical[offset + domain.Length - 2] = (byte)'4';
                RefuseTerminal(historical, verified.Receipt.Receipt.ProgramOutcome!);
                var wrongProfile = terminal.ToArray(); wrongProfile[offset + domain.Length - 2] = (byte)'6';
                RefuseTerminal(wrongProfile, verified.Receipt.Receipt.ProgramOutcome!);
                foreach (var receiptAbi in new ushort[] { 0, 1, 2, 5, ushort.MaxValue, (ushort)(abi == 3 ? 4 : 3) })
                    RefuseTerminal(terminal, verified.Receipt.Receipt.ProgramOutcome! with { AbiVersion = receiptAbi });
                var abiOffset = CandidateTerminalAbiOffset(terminal, offset + domain.Length);
                foreach (var embeddedAbi in new ushort[] { 0, 1, 2, 5, ushort.MaxValue })
                {
                    var changed = terminal.ToArray(); BinaryPrimitives.WriteUInt16BigEndian(changed.AsSpan(abiOffset), embeddedAbi);
                    RefuseTerminal(changed, verified.Receipt.Receipt.ProgramOutcome! with { AbiVersion = embeddedAbi });
                }
                foreach (var index in new[] { offset + domain.Length + 5, abiOffset - 32 })
                {
                    var changed = terminal.ToArray(); changed[index] ^= 1;
                    RefuseTerminal(changed, verified.Receipt.Receipt.ProgramOutcome!);
                }
                foreach (var index in new[] { 0, terminal.Length - 1 })
                {
                    var changed = terminal.ToArray(); changed[index] ^= 1;
                    RefuseTerminal(changed, verified.Receipt.Receipt.ProgramOutcome!);
                }
            }
        }
        Assert.True(expected.SetEquals(present), "all ten genuine native ABI3/4 outcomes required");
    }

    private static ProgramCall NativeCallFromSignedActivity(byte[] signed)
    {
        Assert.True(signed.Length >= 5); Assert.Equal((ushort)3, BinaryPrimitives.ReadUInt16BigEndian(signed));
        Assert.Equal((ushort)0x1001, BinaryPrimitives.ReadUInt16BigEndian(signed.AsSpan(2))); Assert.Equal((byte)12, signed[4]);
        var offset = 5; byte[]? payload = null; BigInteger fee = 0;
        for (byte tag = 1; tag <= 12; tag++)
        {
            Assert.Equal(tag, signed[offset++]);
            var length = tag switch { 1 => 2, 2 or 3 => 4, 6 => 8, 7 or 9 => 16, _ => -1 };
            if (length < 0) { length = checked((int)BinaryPrimitives.ReadUInt32BigEndian(signed.AsSpan(offset))); offset += 4; }
            var bytes = signed.AsSpan(offset, length).ToArray(); offset += length;
            if (tag == 9) fee = new BigInteger(bytes, true, true);
            if (tag == 11) payload = bytes;
        }
        Assert.Equal(signed.Length, offset);
        return new ProgramCall(NativeProgramCall.Decode(payload!), new ProtocolAmount(fee.ToString()), signed);
    }

    private static AuthorizedReceiptBatch NativeBatch(JsonElement row, byte[] pin)
    {
        var batch = row.GetProperty("authorized_batch");
        Assert.Equal(pin, FixtureBytes(batch, "sequencer_public_key_hex"));
        return new(FixtureBytes(batch, "batch_id_hex"), FixtureBytes(batch, "asset_hex"),
            FixtureBytes(batch, "previous_state_root_hex"), FixtureBytes(batch, "resulting_state_root_hex"), pin);
    }

    private static async Task<IReadOnlyDictionary<string, JsonValue>> ProjectVerifiedNativeExecution(JsonElement row,
        byte[] pin, byte[] activity, byte[] program, ushort abi, byte[] idempotency)
    {
        var canonical = FixtureBytes(row, "canonical_receipt_hex");
        var terminal = FixtureBytes(row, "terminal_payload_hex"); var graph = FixtureBytes(row, "call_graph_hex");
        var batch = NativeBatch(row, pin);
        var verified = await ProgramsClient.VerifyReceiptAsync(canonical, batch, activity, abi, terminal, graph, protocolVersion: 3);
        var receipt = verified.Receipt; var outcome = receipt.ProgramOutcome!;
        JsonValue Hex(byte[] bytes) => JsonValue.String(Convert.ToHexString(bytes).ToLowerInvariant());
        JsonValue Number(ulong value) => JsonValue.String(value.ToString(System.Globalization.CultureInfo.InvariantCulture));
        var fee = ((new BigInteger(outcome.FeeUnits.High) << 64) + outcome.FeeUnits.Low).ToString(System.Globalization.CultureInfo.InvariantCulture);
        return new Dictionary<string, JsonValue>
        {
            ["state"] = JsonValue.String(outcome.TerminalKind == 1 ? "executed" : "refused"),
            ["activity_id"] = Hex(receipt.ActivityId), ["program_id"] = Hex(program),
            ["guest_abi_version"] = JsonValue.Integer(outcome.AbiVersion), ["module_version"] = JsonValue.Integer(receipt.ModuleVersion),
            ["batch_id"] = Hex(receipt.BatchId), ["global_sequence"] = Number(receipt.GlobalSequence),
            ["result_code"] = JsonValue.Integer(receipt.ResultCode), ["state_root"] = Hex(receipt.ResultingStateRoot),
            ["receipt"] = Hex(verified.CanonicalBytes), ["receipt_digest"] = Hex(verified.ReceiptDigest),
            ["terminal_payload"] = Hex(terminal), ["call_graph"] = Hex(graph), ["idempotency_key"] = Hex(idempotency),
            ["authority"] = JsonValue.Object(new Dictionary<string, JsonValue>
            {
                ["batch_id"] = Hex(batch.BatchId), ["asset"] = Hex(batch.Asset), ["previous_state_root"] = Hex(batch.PreviousStateRoot),
                ["resulting_state_root"] = Hex(batch.ResultingStateRoot), ["sequencer_public_key"] = Hex(pin),
            }),
            ["usage"] = JsonValue.Object(new Dictionary<string, JsonValue>
            {
                ["cpu_fuel"] = Number(outcome.CpuFuel), ["memory_bytes"] = Number(outcome.MemoryBytes),
                ["storage_read_bytes"] = Number(outcome.StorageReadBytes), ["storage_write_bytes"] = Number(outcome.StorageWriteBytes),
                ["output_values"] = JsonValue.Integer(outcome.OutputValues), ["output_bytes"] = Number(outcome.OutputBytes), ["fee_units"] = JsonValue.String(fee),
            }),
            ["outcome"] = NativeTerminalOutcome(terminal, outcome),
            ["verification"] = JsonValue.String("receipt-terminal-and-call-graph-verified"),
        };
    }

    private static JsonValue NativeTerminalOutcome(byte[] terminal, ProgramReceiptOutcome receipt)
    {
        var unwrapped = Assert.IsType<byte[]>(Invoke("UnwrapAppliedTerminal", terminal, receipt));
        var inner = Property<byte[]>(Invoke("UnwrapTerminal", unwrapped)!, "Inner");
        var domain = Encoding.UTF8.GetBytes("LXP/program-execution/v5\0");
        if (inner.AsSpan().StartsWith(domain))
        {
            var offset = CandidateTerminalAbiOffset(inner, domain.Length);
            Assert.Equal(receipt.AbiVersion, BinaryPrimitives.ReadUInt16BigEndian(inner.AsSpan(offset)));
            var tag = inner[offset + 2]; offset += 3;
            if (tag == 0)
            {
                Assert.Equal((byte)1, receipt.TerminalKind);
                var code = BinaryPrimitives.ReadInt32BigEndian(inner.AsSpan(offset)); offset += 4;
                Assert.Equal(receipt.ResultCode, code);
                var length = checked((int)BinaryPrimitives.ReadUInt64BigEndian(inner.AsSpan(offset))); offset += 8;
                var response = inner.AsSpan(offset, length).ToArray();
                return JsonValue.Object(new Dictionary<string, JsonValue> { ["kind"] = JsonValue.String("completed"),
                    ["code"] = JsonValue.Integer(code), ["response"] = JsonValue.String(Convert.ToHexString(response).ToLowerInvariant()) });
            }
            Assert.True(tag is 1 or 2); Assert.Equal((byte)(tag == 1 ? 2 : 3), receipt.TerminalKind);
        }
        else
        {
            Assert.True(inner.AsSpan().StartsWith(Encoding.UTF8.GetBytes("LXP/programs/callback-failure/v1\0")) ||
                inner.AsSpan().StartsWith(Encoding.UTF8.GetBytes("LXP/programs/settlement-failure/v1\0")), "unknown genuine native terminal domain");
            Assert.Equal((byte)2, receipt.TerminalKind);
        }
        var failure = new Dictionary<string, JsonValue> { ["kind"] = JsonValue.String(receipt.TerminalKind == 3 ? "resource" : "guest_refused") };
        if (receipt.TerminalKind == 2) failure["code"] = JsonValue.Integer(receipt.ResultCode);
        return JsonValue.Object(new Dictionary<string, JsonValue> { ["kind"] = JsonValue.String("refused"), ["failure"] = JsonValue.Object(failure) });
    }

    private static int CandidateTerminalAbiOffset(byte[] terminal, int offset)
    {
        offset += 10;
        var values = BinaryPrimitives.ReadUInt64BigEndian(terminal.AsSpan(offset)); offset += 8;
        Assert.True(values <= (ulong)(terminal.Length / 5));
        for (ulong index = 0; index < values; index++)
        {
            var tag = terminal[offset++]; Assert.True(tag is 1 or 2); offset += tag == 1 ? 4 : 8;
        }
        offset += 60;
        var trace = terminal[offset++]; Assert.True(trace is 0 or 1);
        if (trace == 1)
        {
            var length = checked((int)BinaryPrimitives.ReadUInt64BigEndian(terminal.AsSpan(offset))); offset += 8 + length;
        }
        offset += 32; Assert.True(offset <= terminal.Length - 2); return offset;
    }

    private static Dictionary<string, JsonValue> SignedHead(ushort abi, Ed25519PrivateKeyParameters signer)
    {
        var program = Repeat(0x11); var code = Repeat(0x22); var state = Repeat(0x33);
        var material = Encoding.UTF8.GetBytes("LayerX/program-discovery-proof/v1\0").Concat(program).Concat(new byte[] { 1 })
            .Concat(Be(3, 4)).Concat(code).Concat(Be(abi, 2)).Concat(Be(77, 8)).Concat(Be(1000, 8)).Concat(Be(10000, 8)).Concat(state).ToArray();
        var digest = SHA256.HashData(material); var signature = new Ed25519Signer(); signature.Init(true, signer);
        signature.BlockUpdate(digest, 0, digest.Length);
        string Hex(byte[] bytes) => Convert.ToHexString(bytes).ToLowerInvariant();
        return new()
        {
            ["program_id"] = JsonValue.String(Hex(program)), ["lifecycle"] = JsonValue.String("active"),
            ["version"] = JsonValue.Integer(3), ["code_hash"] = JsonValue.String(Hex(code)), ["abi_version"] = JsonValue.Integer(abi),
            ["receipt_digest"] = JsonValue.String(Hex(digest)), ["deployment_receipt_digest"] = JsonValue.String(Hex(Repeat(0x44))),
            ["state_root"] = JsonValue.String(Hex(state)), ["observed_sequence"] = JsonValue.String("77"),
            ["observed_at"] = JsonValue.String("1000"), ["valid_through"] = JsonValue.String("10000"),
            ["verification"] = JsonValue.String("registry-receipt-and-current-head-verified"),
            ["discovery_public_key"] = JsonValue.String(Hex(signer.GeneratePublicKey().GetEncoded())),
            ["discovery_signature"] = JsonValue.String(Hex(signature.GenerateSignature())),
        };
    }

    private static JsonDocument Fixture(string name, [CallerFilePath] string source = "") => JsonDocument.Parse(File.ReadAllText(
        Path.Combine(Path.GetDirectoryName(source)!, "..", "..", "..", "conformance", "fixtures", name)));
    private static byte[] FixtureBytes(JsonElement element, string name) => Convert.FromHexString(element.GetProperty(name).GetString()!);

    private static object? Invoke(string name, params object[] arguments)
    {
        var method = typeof(ProgramsClient).GetMethod(name, BindingFlags.NonPublic | BindingFlags.Static)
            ?? throw new InvalidOperationException($"missing {name}");
        return method.Invoke(null, arguments);
    }

    private static bool TransportStatus(string operation, JsonValue value, JsonValue status)
    {
        var method = typeof(AgentHttpTransport).GetMethod("ValidProgramVerification", BindingFlags.NonPublic | BindingFlags.Static)
            ?? throw new InvalidOperationException("missing ValidProgramVerification");
        return (bool)(method.Invoke(null, [operation, value, status]) ?? false);
    }

    private static JsonValue Status(string state, string? reason)
    {
        var fields = new Dictionary<string, JsonValue>
        {
            ["state"] = JsonValue.String(state),
        };
        if (reason is null) fields["level"] = JsonValue.String("SequencerSigned");
        else
        {
            fields["requested"] = JsonValue.String("SequencerSigned");
            fields["achieved"] = JsonValue.String("Unverified");
            fields["reason"] = JsonValue.String(reason);
        }
        return JsonValue.Object(fields);
    }

    private static T Property<T>(object value, string name) => (T)(value.GetType().GetProperty(name)?.GetValue(value)
        ?? throw new InvalidOperationException($"missing {name}"));

    private static byte[] TransferAuthorization(int version)
    {
        var program = Repeat(1); var principal = Repeat(2); var asset = Repeat(4); var destination = Repeat(5);
        using var stream = new MemoryStream(); Write(stream, Encoding.UTF8.GetBytes($"LayerX/programs/402LXP/transfer-set/v{version}\0"));
        Write(stream, program); Write(stream, principal); Write(stream, Repeat(3)); Write(stream, new byte[9]);
        using var events = new MemoryStream(); Write(events, Encoding.UTF8.GetBytes("LayerX/programs/events/v1\0")); Write(events, Be(0, 4));
        Write(stream, Be((ulong)events.Length, 4)); Write(stream, events.ToArray()); Write(stream, Be(0, 8)); Write(stream, Be(1, 8));
        Write(stream, new byte[9]); if (version == 2) { stream.WriteByte(1); Write(stream, principal); }
        Write(stream, asset); Write(stream, destination); Write(stream, U128(7)); Write(stream, program); return stream.ToArray();
    }

    private static byte[] EmptyOccupancy(int version)
    {
        using var stream = new MemoryStream(); Write(stream, Encoding.UTF8.GetBytes($"LXP/storage-occupancy-settlement/v{version}\0"));
        Write(stream, Be(1, 8)); if (version > 1) Write(stream, Be(1, 4));
        for (ulong value = 1; value <= 7; value++) Write(stream, Be(value, 8));
        if (version == 3) { Write(stream, new byte[16 * 4]); Write(stream, Be(0, 4)); }
        else { Write(stream, new byte[16 * 2]); Write(stream, Be(0, 8)); }
        return stream.ToArray();
    }

    private static byte[] ChargedOccupancy()
    {
        var program = Repeat(0x11); var payer = Repeat(0x77); using var stream = new MemoryStream();
        Write(stream, Encoding.UTF8.GetBytes("LXP/storage-occupancy-settlement/v3\0")); Write(stream, Be(2, 8)); Write(stream, Be(1, 4));
        foreach (ulong value in new ulong[] { 0, 0, 0, 0, 0, 0, 2 }) Write(stream, Be(value, 8));
        Write(stream, U128(3)); Write(stream, U128(6)); Write(stream, U128(6)); Write(stream, U128(0)); Write(stream, Be(1, 4));
        stream.WriteByte(65); Write(stream, program); stream.WriteByte(0); Write(stream, payer);
        Write(stream, payer); Write(stream, program); Write(stream, Repeat(0x88));
        Write(stream, Be(1, 8)); Write(stream, Be(2, 8)); Write(stream, Be(3, 8)); Write(stream, Be(3, 8));
        Write(stream, U128(3)); Write(stream, Be(2, 8)); Write(stream, U128(6)); Write(stream, U128(0));
        Write(stream, U128(6)); Write(stream, U128(0)); stream.WriteByte(1); Write(stream, U128(0));
        Write(stream, Be(3, 8)); Write(stream, Be(2, 8)); Write(stream, U128(0)); Write(stream, Repeat(0x99));
        return stream.ToArray();
    }

    private static byte[] Repeat(byte value) => Enumerable.Repeat(value, 32).ToArray();
    private static byte[] U128(ulong value) => new byte[8].Concat(Be(value, 8)).ToArray();
    private static byte[] Be(ulong value, int length)
    {
        var encoded = new byte[8]; BinaryPrimitives.WriteUInt64BigEndian(encoded, value); return encoded[(8 - length)..];
    }
    private static void Write(Stream stream, byte[] value) => stream.Write(value);
}
