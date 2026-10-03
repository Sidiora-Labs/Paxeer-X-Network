package com.sidiora.layerx.sdk;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import java.util.Map;
import java.util.ArrayList;
import java.util.Objects;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.Flow;

public final class ProductionClient {
    public record Options(IdempotencyKey idempotencyKey, Map<String, String> pathParameters) {
        public Options { pathParameters = pathParameters == null ? Map.of() : Map.copyOf(pathParameters); }
        public static Options none() { return new Options(null, Map.of()); }
        public static Options idempotent(IdempotencyKey key) { return new Options(key, Map.of()); }
    }
    public record TelemetryEvent(OperationCatalog.Plane plane, String operation, String outcome,
                                 PlatformSdkException.Code code) {}
    @FunctionalInterface public interface Telemetry { void record(TelemetryEvent event); }

    private final ProductionTransport transport;
    private final ObjectMapper mapper;
    private final Telemetry telemetry;

    public ProductionClient(ProductionTransport transport, ObjectMapper mapper, Telemetry telemetry) {
        this.transport = Objects.requireNonNull(transport, "transport");
        this.mapper = Objects.requireNonNull(mapper, "mapper");
        this.telemetry = telemetry;
    }
    public ProductionClient(ProductionTransport transport) { this(transport, new ObjectMapper(), null); }

    public CompletionStage<ResumableStream<GeneratedSchema.HumanModels.StreamEvent>> openHumanStream() {
        return human(GeneratedSchema.HumanOperations.STREAM_OPEN,
            new GeneratedSchema.HumanOperations.StreamOpenRequest(), Options.none())
            .thenApply(position -> new ResumableStream<>(new ResumableStream.Cursor(position.cursor())));
    }

    public Flow.Publisher<ResumableStream.Event<GeneratedSchema.HumanModels.StreamEvent>> humanStreamPublisher(
            ResumableStream<GeneratedSchema.HumanModels.StreamEvent> stream) {
        Objects.requireNonNull(stream, "stream");
        return stream.publisher(cursor -> human(GeneratedSchema.HumanOperations.STREAM_NEXT,
            new GeneratedSchema.HumanOperations.StreamNextRequest(),
            new Options(null, Map.of("cursor", cursor.value()))).thenApply(page -> {
                var events = new ArrayList<ResumableStream.Event<GeneratedSchema.HumanModels.StreamEvent>>();
                ResumableStream.Cursor previous = cursor;
                for (var value : page.events()) {
                    var next = new ResumableStream.Cursor(value.cursor());
                    events.add(new ResumableStream.Event<>(value.cursor(), previous, next, value));
                    previous = next;
                }
                return new ResumableStream.Page<>(cursor, events, new ResumableStream.Cursor(page.next_cursor()));
            }));
    }

    public CompletionStage<SchemaTypes.AgentResponse> agent(SchemaTypes.AgentRequest request, Options options) {
        Objects.requireNonNull(request, "request");
        return execute(request.operation(), request.body(), mapper.constructType(ObjectNode.class), options)
            .thenApply(value -> new SchemaTypes.AgentResponse(request.operation(), (ObjectNode) value));
    }

    public CompletionStage<SchemaTypes.HumanResponse> human(SchemaTypes.HumanRequest request, Options options) {
        Objects.requireNonNull(request, "request");
        return execute(request.operation(), request.body(), mapper.constructType(ObjectNode.class), options)
            .thenApply(value -> new SchemaTypes.HumanResponse(request.operation(), (ObjectNode) value));
    }

    public <R extends SchemaTypes.GeneratedRequest, S extends SchemaTypes.GeneratedResponse>
            CompletionStage<S> agent(SchemaTypes.TypedOperation<R, S> operation, R request, Options options) {
        requirePlane(operation, OperationCatalog.Plane.AGENT);
        Objects.requireNonNull(request, "request");
        return execute(operation, objectBody(operation, request), mapper.constructType(operation.responseType()), options);
    }

    public <R extends SchemaTypes.GeneratedRequest, S extends SchemaTypes.GeneratedResponse>
            CompletionStage<S> human(SchemaTypes.TypedOperation<R, S> operation, R request, Options options) {
        requirePlane(operation, OperationCatalog.Plane.HUMAN);
        Objects.requireNonNull(request, "request");
        return execute(operation, objectBody(operation, request), mapper.constructType(operation.responseType()), options);
    }

    public <T> CompletionStage<T> agent(String operation, Object request, Class<T> responseType, Options options) {
        var typed = GeneratedSchema.AGENT.get(operation);
        if (typed == null) throw PlatformSdkException.invalidArgument();
        return execute(OperationCatalog.agent(operation), objectBody(typed, request), mapper.constructType(responseType), options);
    }
    public <T> CompletionStage<T> human(String operation, Object request, Class<T> responseType, Options options) {
        var typed = GeneratedSchema.HUMAN.get(operation);
        if (typed == null) throw PlatformSdkException.invalidArgument();
        return execute(OperationCatalog.human(operation), objectBody(typed, request), mapper.constructType(responseType), options);
    }
    public <T> CompletionStage<T> agent(String operation, Object request, TypeReference<T> responseType, Options options) {
        var typed = GeneratedSchema.AGENT.get(operation);
        if (typed == null) throw PlatformSdkException.invalidArgument();
        return execute(OperationCatalog.agent(operation), objectBody(typed, request), mapper.constructType(responseType), options);
    }
    public <T> CompletionStage<T> human(String operation, Object request, TypeReference<T> responseType, Options options) {
        var typed = GeneratedSchema.HUMAN.get(operation);
        if (typed == null) throw PlatformSdkException.invalidArgument();
        return execute(OperationCatalog.human(operation), objectBody(typed, request), mapper.constructType(responseType), options);
    }

    <T> CompletionStage<T> programs(String operation, ObjectNode request, Class<T> responseType, Options options) {
        Objects.requireNonNull(operation, "operation");
        Objects.requireNonNull(request, "request");
        Objects.requireNonNull(responseType, "responseType");
        Options safeOptions = options == null ? Options.none() : options;
        var catalog = OperationCatalog.agent(operation);
        if (catalog.idempotencyRequired() && safeOptions.idempotencyKey() == null) {
            throw new PlatformSdkException(PlatformSdkException.Code.IDEMPOTENCY_REQUIRED, PlatformSdkException.Retry.NEVER, null, null, null);
        }
        try {
            return transport.<T>callPrograms(new ProductionTransport.ProgramsCall(operation,
                    SchemaTypes.canonicalBody(request), SchemaTypes.PathParameters.of(safeOptions.pathParameters()),
                    safeOptions.idempotencyKey()), mapper.constructType(responseType))
                .whenComplete((value, error) -> {
                    if (telemetry != null) telemetry.record(new TelemetryEvent(OperationCatalog.Plane.AGENT,
                        operation, error == null ? "completed" : "refused",
                        error instanceof PlatformSdkException sdk ? sdk.code() :
                            error == null ? null : PlatformSdkException.Code.TRANSPORT_FAILURE));
                });
        } catch (PlatformSdkException error) {
            throw error;
        } catch (RuntimeException error) {
            throw new PlatformSdkException(PlatformSdkException.Code.TRANSPORT_FAILURE,
                PlatformSdkException.Retry.SAFE, null, null, null);
        }
    }

    private <T> CompletionStage<T> execute(SchemaTypes.Operation operation, ObjectNode request,
                                            JavaType responseType, Options options) {
        Options safeOptions = options == null ? Options.none() : options;
        if (operation.idempotencyRequired() && safeOptions.idempotencyKey() == null) {
            throw new PlatformSdkException(PlatformSdkException.Code.IDEMPOTENCY_REQUIRED,
                PlatformSdkException.Retry.NEVER, null, null, null);
        }
        try {
            return transport.<T>call(new ProductionTransport.Call(operation, request,
                    SchemaTypes.PathParameters.of(safeOptions.pathParameters()), safeOptions.idempotencyKey()), responseType)
                .whenComplete((value, error) -> {
                    if (telemetry != null) telemetry.record(new TelemetryEvent(operation.plane(), operation.wireName(),
                        error == null ? "completed" : "refused",
                        error instanceof PlatformSdkException sdk ? sdk.code() :
                            error == null ? null : PlatformSdkException.Code.TRANSPORT_FAILURE));
                });
        } catch (PlatformSdkException error) {
            throw error;
        } catch (RuntimeException error) {
            throw new PlatformSdkException(PlatformSdkException.Code.TRANSPORT_FAILURE,
                PlatformSdkException.Retry.SAFE, null, null, null);
        }
    }

    private ObjectNode objectBody(SchemaTypes.TypedOperation<?, ?> operation, Object request) {
        Object valueToEncode = request == null ? mapper.createObjectNode() : request;
        try {
            valueToEncode = mapper.convertValue(valueToEncode, operation.requestType());
        } catch (IllegalArgumentException error) {
            throw PlatformSdkException.invalidArgument();
        }
        var value = mapper.valueToTree(valueToEncode);
        if (!(value instanceof ObjectNode object)) throw PlatformSdkException.invalidArgument();
        return SchemaTypes.canonicalBody(object);
    }

    private static void requirePlane(SchemaTypes.TypedOperation<?, ?> operation,
                                     OperationCatalog.Plane expected) {
        if (operation == null || operation.plane() != expected) throw PlatformSdkException.invalidArgument();
    }
}
