"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GeneratedCodeInfoAnnotation = exports.GeneratedCodeInfo = exports.SourceCodeInfoLocation = exports.SourceCodeInfo = exports.FeatureSetDefaultsFeatureSetEditionDefault = exports.FeatureSetDefaults = exports.FeatureSet = exports.UninterpretedOptionNamePart = exports.UninterpretedOption = exports.MethodOptions = exports.ServiceOptions = exports.EnumValueOptions = exports.EnumOptions = exports.OneofOptions = exports.FieldOptionsFeatureSupport = exports.FieldOptionsEditionDefault = exports.FieldOptions = exports.MessageOptions = exports.FileOptions = exports.MethodDescriptorProto = exports.ServiceDescriptorProto = exports.EnumValueDescriptorProto = exports.EnumDescriptorProtoEnumReservedRange = exports.EnumDescriptorProto = exports.OneofDescriptorProto = exports.FieldDescriptorProto = exports.ExtensionRangeOptionsDeclaration = exports.ExtensionRangeOptions = exports.DescriptorProtoReservedRange = exports.DescriptorProtoExtensionRange = exports.DescriptorProto = exports.FileDescriptorProto = exports.FileDescriptorSet = void 0;
exports.editionFromJSON = editionFromJSON;
exports.editionToJSON = editionToJSON;
exports.extensionRangeOptionsVerificationStateFromJSON = extensionRangeOptionsVerificationStateFromJSON;
exports.extensionRangeOptionsVerificationStateToJSON = extensionRangeOptionsVerificationStateToJSON;
exports.fieldDescriptorProtoTypeFromJSON = fieldDescriptorProtoTypeFromJSON;
exports.fieldDescriptorProtoTypeToJSON = fieldDescriptorProtoTypeToJSON;
exports.fieldDescriptorProtoLabelFromJSON = fieldDescriptorProtoLabelFromJSON;
exports.fieldDescriptorProtoLabelToJSON = fieldDescriptorProtoLabelToJSON;
exports.fileOptionsOptimizeModeFromJSON = fileOptionsOptimizeModeFromJSON;
exports.fileOptionsOptimizeModeToJSON = fileOptionsOptimizeModeToJSON;
exports.fieldOptionsCTypeFromJSON = fieldOptionsCTypeFromJSON;
exports.fieldOptionsCTypeToJSON = fieldOptionsCTypeToJSON;
exports.fieldOptionsJSTypeFromJSON = fieldOptionsJSTypeFromJSON;
exports.fieldOptionsJSTypeToJSON = fieldOptionsJSTypeToJSON;
exports.fieldOptionsOptionRetentionFromJSON = fieldOptionsOptionRetentionFromJSON;
exports.fieldOptionsOptionRetentionToJSON = fieldOptionsOptionRetentionToJSON;
exports.fieldOptionsOptionTargetTypeFromJSON = fieldOptionsOptionTargetTypeFromJSON;
exports.fieldOptionsOptionTargetTypeToJSON = fieldOptionsOptionTargetTypeToJSON;
exports.methodOptionsIdempotencyLevelFromJSON = methodOptionsIdempotencyLevelFromJSON;
exports.methodOptionsIdempotencyLevelToJSON = methodOptionsIdempotencyLevelToJSON;
exports.featureSetFieldPresenceFromJSON = featureSetFieldPresenceFromJSON;
exports.featureSetFieldPresenceToJSON = featureSetFieldPresenceToJSON;
exports.featureSetEnumTypeFromJSON = featureSetEnumTypeFromJSON;
exports.featureSetEnumTypeToJSON = featureSetEnumTypeToJSON;
exports.featureSetRepeatedFieldEncodingFromJSON = featureSetRepeatedFieldEncodingFromJSON;
exports.featureSetRepeatedFieldEncodingToJSON = featureSetRepeatedFieldEncodingToJSON;
exports.featureSetUtf8ValidationFromJSON = featureSetUtf8ValidationFromJSON;
exports.featureSetUtf8ValidationToJSON = featureSetUtf8ValidationToJSON;
exports.featureSetMessageEncodingFromJSON = featureSetMessageEncodingFromJSON;
exports.featureSetMessageEncodingToJSON = featureSetMessageEncodingToJSON;
exports.featureSetJsonFormatFromJSON = featureSetJsonFormatFromJSON;
exports.featureSetJsonFormatToJSON = featureSetJsonFormatToJSON;
exports.featureSetEnforceNamingStyleFromJSON = featureSetEnforceNamingStyleFromJSON;
exports.featureSetEnforceNamingStyleToJSON = featureSetEnforceNamingStyleToJSON;
exports.generatedCodeInfoAnnotationSemanticFromJSON = generatedCodeInfoAnnotationSemanticFromJSON;
exports.generatedCodeInfoAnnotationSemanticToJSON = generatedCodeInfoAnnotationSemanticToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const protobuf_1 = require("../../../types/google/protobuf");
exports.FileDescriptorSet = {
    $type: "google.protobuf.FileDescriptorSet",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.file) {
            exports.FileDescriptorProto.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFileDescriptorSet();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.file.push(exports.FileDescriptorProto.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            file: globalThis.Array.isArray(object?.file) ? object.file.map((e) => exports.FileDescriptorProto.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.file?.length) {
            obj.file = message.file.map((e) => exports.FileDescriptorProto.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.FileDescriptorSet.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFileDescriptorSet();
        message.file = object.file?.map((e) => exports.FileDescriptorProto.fromPartial(e)) || [];
        return message;
    }
};
exports.FileDescriptorProto = {
    $type: "google.protobuf.FileDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.package !== undefined && message.package !== "") {
            writer.uint32(18).string(message.package);
        }
        for (const v of message.dependency) {
            writer.uint32(26).string(v);
        }
        writer.uint32(82).fork();
        for (const v of message.public_dependency) {
            writer.int32(v);
        }
        writer.join();
        writer.uint32(90).fork();
        for (const v of message.weak_dependency) {
            writer.int32(v);
        }
        writer.join();
        for (const v of message.message_type) {
            exports.DescriptorProto.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.enum_type) {
            exports.EnumDescriptorProto.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.service) {
            exports.ServiceDescriptorProto.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.extension) {
            exports.FieldDescriptorProto.encode(v, writer.uint32(58).fork()).join();
        }
        if (message.options !== undefined) {
            exports.FileOptions.encode(message.options, writer.uint32(66).fork()).join();
        }
        if (message.source_code_info !== undefined) {
            exports.SourceCodeInfo.encode(message.source_code_info, writer.uint32(74).fork()).join();
        }
        if (message.syntax !== undefined && message.syntax !== "") {
            writer.uint32(98).string(message.syntax);
        }
        if (message.edition !== undefined && message.edition !== 0) {
            writer.uint32(112).int32(message.edition);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFileDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.package = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.dependency.push(reader.string());
                    continue;
                case 10:
                    if (tag === 80) {
                        message.public_dependency.push(reader.int32());
                        continue;
                    }
                    if (tag === 82) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.public_dependency.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 11:
                    if (tag === 88) {
                        message.weak_dependency.push(reader.int32());
                        continue;
                    }
                    if (tag === 90) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.weak_dependency.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.message_type.push(exports.DescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.enum_type.push(exports.EnumDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.service.push(exports.ServiceDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.extension.push(exports.FieldDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.options = exports.FileOptions.decode(reader, reader.uint32());
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.source_code_info = exports.SourceCodeInfo.decode(reader, reader.uint32());
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.syntax = reader.string();
                    continue;
                case 14:
                    if (tag !== 112) {
                        break;
                    }
                    message.edition = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            package: isSet(object.package) ? globalThis.String(object.package) : "",
            dependency: globalThis.Array.isArray(object?.dependency) ? object.dependency.map((e) => globalThis.String(e)) : [],
            public_dependency: globalThis.Array.isArray(object?.public_dependency) ? object.public_dependency.map((e) => globalThis.Number(e)) : [],
            weak_dependency: globalThis.Array.isArray(object?.weak_dependency) ? object.weak_dependency.map((e) => globalThis.Number(e)) : [],
            message_type: globalThis.Array.isArray(object?.message_type) ? object.message_type.map((e) => exports.DescriptorProto.fromJSON(e)) : [],
            enum_type: globalThis.Array.isArray(object?.enum_type) ? object.enum_type.map((e) => exports.EnumDescriptorProto.fromJSON(e)) : [],
            service: globalThis.Array.isArray(object?.service) ? object.service.map((e) => exports.ServiceDescriptorProto.fromJSON(e)) : [],
            extension: globalThis.Array.isArray(object?.extension) ? object.extension.map((e) => exports.FieldDescriptorProto.fromJSON(e)) : [],
            options: isSet(object.options) ? exports.FileOptions.fromJSON(object.options) : undefined,
            source_code_info: isSet(object.source_code_info) ? exports.SourceCodeInfo.fromJSON(object.source_code_info) : undefined,
            syntax: isSet(object.syntax) ? globalThis.String(object.syntax) : "",
            edition: isSet(object.edition) ? editionFromJSON(object.edition) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.package !== undefined && message.package !== "") {
            obj.package = message.package;
        }
        if (message.dependency?.length) {
            obj.dependency = message.dependency;
        }
        if (message.public_dependency?.length) {
            obj.public_dependency = message.public_dependency.map((e) => Math.round(e));
        }
        if (message.weak_dependency?.length) {
            obj.weak_dependency = message.weak_dependency.map((e) => Math.round(e));
        }
        if (message.message_type?.length) {
            obj.message_type = message.message_type.map((e) => exports.DescriptorProto.toJSON(e));
        }
        if (message.enum_type?.length) {
            obj.enum_type = message.enum_type.map((e) => exports.EnumDescriptorProto.toJSON(e));
        }
        if (message.service?.length) {
            obj.service = message.service.map((e) => exports.ServiceDescriptorProto.toJSON(e));
        }
        if (message.extension?.length) {
            obj.extension = message.extension.map((e) => exports.FieldDescriptorProto.toJSON(e));
        }
        if (message.options !== undefined) {
            obj.options = exports.FileOptions.toJSON(message.options);
        }
        if (message.source_code_info !== undefined) {
            obj.source_code_info = exports.SourceCodeInfo.toJSON(message.source_code_info);
        }
        if (message.syntax !== undefined && message.syntax !== "") {
            obj.syntax = message.syntax;
        }
        if (message.edition !== undefined && message.edition !== 0) {
            obj.edition = editionToJSON(message.edition);
        }
        return obj;
    },
    create(base) {
        return exports.FileDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFileDescriptorProto();
        message.name = object.name ?? "";
        message.package = object.package ?? "";
        message.dependency = object.dependency?.map((e) => e) || [];
        message.public_dependency = object.public_dependency?.map((e) => e) || [];
        message.weak_dependency = object.weak_dependency?.map((e) => e) || [];
        message.message_type = object.message_type?.map((e) => exports.DescriptorProto.fromPartial(e)) || [];
        message.enum_type = object.enum_type?.map((e) => exports.EnumDescriptorProto.fromPartial(e)) || [];
        message.service = object.service?.map((e) => exports.ServiceDescriptorProto.fromPartial(e)) || [];
        message.extension = object.extension?.map((e) => exports.FieldDescriptorProto.fromPartial(e)) || [];
        message.options = object.options !== undefined && object.options !== null ? exports.FileOptions.fromPartial(object.options) : undefined;
        message.source_code_info =
            object.source_code_info !== undefined && object.source_code_info !== null ? exports.SourceCodeInfo.fromPartial(object.source_code_info) : undefined;
        message.syntax = object.syntax ?? "";
        message.edition = object.edition ?? 0;
        return message;
    }
};
exports.DescriptorProto = {
    $type: "google.protobuf.DescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.field) {
            exports.FieldDescriptorProto.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.extension) {
            exports.FieldDescriptorProto.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.nested_type) {
            exports.DescriptorProto.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.enum_type) {
            exports.EnumDescriptorProto.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.extension_range) {
            exports.DescriptorProtoExtensionRange.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.oneof_decl) {
            exports.OneofDescriptorProto.encode(v, writer.uint32(66).fork()).join();
        }
        if (message.options !== undefined) {
            exports.MessageOptions.encode(message.options, writer.uint32(58).fork()).join();
        }
        for (const v of message.reserved_range) {
            exports.DescriptorProtoReservedRange.encode(v, writer.uint32(74).fork()).join();
        }
        for (const v of message.reserved_name) {
            writer.uint32(82).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.field.push(exports.FieldDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.extension.push(exports.FieldDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.nested_type.push(exports.DescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.enum_type.push(exports.EnumDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.extension_range.push(exports.DescriptorProtoExtensionRange.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.oneof_decl.push(exports.OneofDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.options = exports.MessageOptions.decode(reader, reader.uint32());
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.reserved_range.push(exports.DescriptorProtoReservedRange.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.reserved_name.push(reader.string());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            field: globalThis.Array.isArray(object?.field) ? object.field.map((e) => exports.FieldDescriptorProto.fromJSON(e)) : [],
            extension: globalThis.Array.isArray(object?.extension) ? object.extension.map((e) => exports.FieldDescriptorProto.fromJSON(e)) : [],
            nested_type: globalThis.Array.isArray(object?.nested_type) ? object.nested_type.map((e) => exports.DescriptorProto.fromJSON(e)) : [],
            enum_type: globalThis.Array.isArray(object?.enum_type) ? object.enum_type.map((e) => exports.EnumDescriptorProto.fromJSON(e)) : [],
            extension_range: globalThis.Array.isArray(object?.extension_range)
                ? object.extension_range.map((e) => exports.DescriptorProtoExtensionRange.fromJSON(e))
                : [],
            oneof_decl: globalThis.Array.isArray(object?.oneof_decl) ? object.oneof_decl.map((e) => exports.OneofDescriptorProto.fromJSON(e)) : [],
            options: isSet(object.options) ? exports.MessageOptions.fromJSON(object.options) : undefined,
            reserved_range: globalThis.Array.isArray(object?.reserved_range) ? object.reserved_range.map((e) => exports.DescriptorProtoReservedRange.fromJSON(e)) : [],
            reserved_name: globalThis.Array.isArray(object?.reserved_name) ? object.reserved_name.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.field?.length) {
            obj.field = message.field.map((e) => exports.FieldDescriptorProto.toJSON(e));
        }
        if (message.extension?.length) {
            obj.extension = message.extension.map((e) => exports.FieldDescriptorProto.toJSON(e));
        }
        if (message.nested_type?.length) {
            obj.nested_type = message.nested_type.map((e) => exports.DescriptorProto.toJSON(e));
        }
        if (message.enum_type?.length) {
            obj.enum_type = message.enum_type.map((e) => exports.EnumDescriptorProto.toJSON(e));
        }
        if (message.extension_range?.length) {
            obj.extension_range = message.extension_range.map((e) => exports.DescriptorProtoExtensionRange.toJSON(e));
        }
        if (message.oneof_decl?.length) {
            obj.oneof_decl = message.oneof_decl.map((e) => exports.OneofDescriptorProto.toJSON(e));
        }
        if (message.options !== undefined) {
            obj.options = exports.MessageOptions.toJSON(message.options);
        }
        if (message.reserved_range?.length) {
            obj.reserved_range = message.reserved_range.map((e) => exports.DescriptorProtoReservedRange.toJSON(e));
        }
        if (message.reserved_name?.length) {
            obj.reserved_name = message.reserved_name;
        }
        return obj;
    },
    create(base) {
        return exports.DescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDescriptorProto();
        message.name = object.name ?? "";
        message.field = object.field?.map((e) => exports.FieldDescriptorProto.fromPartial(e)) || [];
        message.extension = object.extension?.map((e) => exports.FieldDescriptorProto.fromPartial(e)) || [];
        message.nested_type = object.nested_type?.map((e) => exports.DescriptorProto.fromPartial(e)) || [];
        message.enum_type = object.enum_type?.map((e) => exports.EnumDescriptorProto.fromPartial(e)) || [];
        message.extension_range = object.extension_range?.map((e) => exports.DescriptorProtoExtensionRange.fromPartial(e)) || [];
        message.oneof_decl = object.oneof_decl?.map((e) => exports.OneofDescriptorProto.fromPartial(e)) || [];
        message.options = object.options !== undefined && object.options !== null ? exports.MessageOptions.fromPartial(object.options) : undefined;
        message.reserved_range = object.reserved_range?.map((e) => exports.DescriptorProtoReservedRange.fromPartial(e)) || [];
        message.reserved_name = object.reserved_name?.map((e) => e) || [];
        return message;
    }
};
exports.DescriptorProtoExtensionRange = {
    $type: "google.protobuf.DescriptorProto.ExtensionRange",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.start !== undefined && message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        if (message.options !== undefined) {
            exports.ExtensionRangeOptions.encode(message.options, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDescriptorProtoExtensionRange();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.start = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.end = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.options = exports.ExtensionRangeOptions.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            start: isSet(object.start) ? globalThis.Number(object.start) : 0,
            end: isSet(object.end) ? globalThis.Number(object.end) : 0,
            options: isSet(object.options) ? exports.ExtensionRangeOptions.fromJSON(object.options) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.start !== undefined && message.start !== 0) {
            obj.start = Math.round(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            obj.end = Math.round(message.end);
        }
        if (message.options !== undefined) {
            obj.options = exports.ExtensionRangeOptions.toJSON(message.options);
        }
        return obj;
    },
    create(base) {
        return exports.DescriptorProtoExtensionRange.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDescriptorProtoExtensionRange();
        message.start = object.start ?? 0;
        message.end = object.end ?? 0;
        message.options = object.options !== undefined && object.options !== null ? exports.ExtensionRangeOptions.fromPartial(object.options) : undefined;
        return message;
    }
};
exports.DescriptorProtoReservedRange = {
    $type: "google.protobuf.DescriptorProto.ReservedRange",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.start !== undefined && message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDescriptorProtoReservedRange();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.start = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.end = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            start: isSet(object.start) ? globalThis.Number(object.start) : 0,
            end: isSet(object.end) ? globalThis.Number(object.end) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.start !== undefined && message.start !== 0) {
            obj.start = Math.round(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            obj.end = Math.round(message.end);
        }
        return obj;
    },
    create(base) {
        return exports.DescriptorProtoReservedRange.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDescriptorProtoReservedRange();
        message.start = object.start ?? 0;
        message.end = object.end ?? 0;
        return message;
    }
};
exports.ExtensionRangeOptions = {
    $type: "google.protobuf.ExtensionRangeOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        for (const v of message.declaration) {
            exports.ExtensionRangeOptionsDeclaration.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(402).fork()).join();
        }
        if (message.verification !== undefined && message.verification !== 1) {
            writer.uint32(24).int32(message.verification);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExtensionRangeOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.declaration.push(exports.ExtensionRangeOptionsDeclaration.decode(reader, reader.uint32()));
                    continue;
                case 50:
                    if (tag !== 402) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.verification = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : [],
            declaration: globalThis.Array.isArray(object?.declaration) ? object.declaration.map((e) => exports.ExtensionRangeOptionsDeclaration.fromJSON(e)) : [],
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            verification: isSet(object.verification) ? extensionRangeOptionsVerificationStateFromJSON(object.verification) : 1
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        if (message.declaration?.length) {
            obj.declaration = message.declaration.map((e) => exports.ExtensionRangeOptionsDeclaration.toJSON(e));
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.verification !== undefined && message.verification !== 1) {
            obj.verification = extensionRangeOptionsVerificationStateToJSON(message.verification);
        }
        return obj;
    },
    create(base) {
        return exports.ExtensionRangeOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExtensionRangeOptions();
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        message.declaration = object.declaration?.map((e) => exports.ExtensionRangeOptionsDeclaration.fromPartial(e)) || [];
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.verification = object.verification ?? 1;
        return message;
    }
};
exports.ExtensionRangeOptionsDeclaration = {
    $type: "google.protobuf.ExtensionRangeOptions.Declaration",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.number !== undefined && message.number !== 0) {
            writer.uint32(8).int32(message.number);
        }
        if (message.full_name !== undefined && message.full_name !== "") {
            writer.uint32(18).string(message.full_name);
        }
        if (message.type !== undefined && message.type !== "") {
            writer.uint32(26).string(message.type);
        }
        if (message.reserved !== undefined && message.reserved !== false) {
            writer.uint32(40).bool(message.reserved);
        }
        if (message.repeated !== undefined && message.repeated !== false) {
            writer.uint32(48).bool(message.repeated);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExtensionRangeOptionsDeclaration();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.number = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.full_name = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.type = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.reserved = reader.bool();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.repeated = reader.bool();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            number: isSet(object.number) ? globalThis.Number(object.number) : 0,
            full_name: isSet(object.full_name) ? globalThis.String(object.full_name) : "",
            type: isSet(object.type) ? globalThis.String(object.type) : "",
            reserved: isSet(object.reserved) ? globalThis.Boolean(object.reserved) : false,
            repeated: isSet(object.repeated) ? globalThis.Boolean(object.repeated) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.number !== undefined && message.number !== 0) {
            obj.number = Math.round(message.number);
        }
        if (message.full_name !== undefined && message.full_name !== "") {
            obj.full_name = message.full_name;
        }
        if (message.type !== undefined && message.type !== "") {
            obj.type = message.type;
        }
        if (message.reserved !== undefined && message.reserved !== false) {
            obj.reserved = message.reserved;
        }
        if (message.repeated !== undefined && message.repeated !== false) {
            obj.repeated = message.repeated;
        }
        return obj;
    },
    create(base) {
        return exports.ExtensionRangeOptionsDeclaration.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExtensionRangeOptionsDeclaration();
        message.number = object.number ?? 0;
        message.full_name = object.full_name ?? "";
        message.type = object.type ?? "";
        message.reserved = object.reserved ?? false;
        message.repeated = object.repeated ?? false;
        return message;
    }
};
exports.FieldDescriptorProto = {
    $type: "google.protobuf.FieldDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.number !== undefined && message.number !== 0) {
            writer.uint32(24).int32(message.number);
        }
        if (message.label !== undefined && message.label !== 1) {
            writer.uint32(32).int32(message.label);
        }
        if (message.type !== undefined && message.type !== 1) {
            writer.uint32(40).int32(message.type);
        }
        if (message.type_name !== undefined && message.type_name !== "") {
            writer.uint32(50).string(message.type_name);
        }
        if (message.extendee !== undefined && message.extendee !== "") {
            writer.uint32(18).string(message.extendee);
        }
        if (message.default_value !== undefined && message.default_value !== "") {
            writer.uint32(58).string(message.default_value);
        }
        if (message.oneof_index !== undefined && message.oneof_index !== 0) {
            writer.uint32(72).int32(message.oneof_index);
        }
        if (message.json_name !== undefined && message.json_name !== "") {
            writer.uint32(82).string(message.json_name);
        }
        if (message.options !== undefined) {
            exports.FieldOptions.encode(message.options, writer.uint32(66).fork()).join();
        }
        if (message.proto3_optional !== undefined && message.proto3_optional !== false) {
            writer.uint32(136).bool(message.proto3_optional);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFieldDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.number = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.label = reader.int32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.type = reader.int32();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.type_name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.extendee = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.default_value = reader.string();
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.oneof_index = reader.int32();
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.json_name = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.options = exports.FieldOptions.decode(reader, reader.uint32());
                    continue;
                case 17:
                    if (tag !== 136) {
                        break;
                    }
                    message.proto3_optional = reader.bool();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            number: isSet(object.number) ? globalThis.Number(object.number) : 0,
            label: isSet(object.label) ? fieldDescriptorProtoLabelFromJSON(object.label) : 1,
            type: isSet(object.type) ? fieldDescriptorProtoTypeFromJSON(object.type) : 1,
            type_name: isSet(object.type_name) ? globalThis.String(object.type_name) : "",
            extendee: isSet(object.extendee) ? globalThis.String(object.extendee) : "",
            default_value: isSet(object.default_value) ? globalThis.String(object.default_value) : "",
            oneof_index: isSet(object.oneof_index) ? globalThis.Number(object.oneof_index) : 0,
            json_name: isSet(object.json_name) ? globalThis.String(object.json_name) : "",
            options: isSet(object.options) ? exports.FieldOptions.fromJSON(object.options) : undefined,
            proto3_optional: isSet(object.proto3_optional) ? globalThis.Boolean(object.proto3_optional) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.number !== undefined && message.number !== 0) {
            obj.number = Math.round(message.number);
        }
        if (message.label !== undefined && message.label !== 1) {
            obj.label = fieldDescriptorProtoLabelToJSON(message.label);
        }
        if (message.type !== undefined && message.type !== 1) {
            obj.type = fieldDescriptorProtoTypeToJSON(message.type);
        }
        if (message.type_name !== undefined && message.type_name !== "") {
            obj.type_name = message.type_name;
        }
        if (message.extendee !== undefined && message.extendee !== "") {
            obj.extendee = message.extendee;
        }
        if (message.default_value !== undefined && message.default_value !== "") {
            obj.default_value = message.default_value;
        }
        if (message.oneof_index !== undefined && message.oneof_index !== 0) {
            obj.oneof_index = Math.round(message.oneof_index);
        }
        if (message.json_name !== undefined && message.json_name !== "") {
            obj.json_name = message.json_name;
        }
        if (message.options !== undefined) {
            obj.options = exports.FieldOptions.toJSON(message.options);
        }
        if (message.proto3_optional !== undefined && message.proto3_optional !== false) {
            obj.proto3_optional = message.proto3_optional;
        }
        return obj;
    },
    create(base) {
        return exports.FieldDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFieldDescriptorProto();
        message.name = object.name ?? "";
        message.number = object.number ?? 0;
        message.label = object.label ?? 1;
        message.type = object.type ?? 1;
        message.type_name = object.type_name ?? "";
        message.extendee = object.extendee ?? "";
        message.default_value = object.default_value ?? "";
        message.oneof_index = object.oneof_index ?? 0;
        message.json_name = object.json_name ?? "";
        message.options = object.options !== undefined && object.options !== null ? exports.FieldOptions.fromPartial(object.options) : undefined;
        message.proto3_optional = object.proto3_optional ?? false;
        return message;
    }
};
exports.OneofDescriptorProto = {
    $type: "google.protobuf.OneofDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.options !== undefined) {
            exports.OneofOptions.encode(message.options, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseOneofDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.options = exports.OneofOptions.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            options: isSet(object.options) ? exports.OneofOptions.fromJSON(object.options) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.options !== undefined) {
            obj.options = exports.OneofOptions.toJSON(message.options);
        }
        return obj;
    },
    create(base) {
        return exports.OneofDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOneofDescriptorProto();
        message.name = object.name ?? "";
        message.options = object.options !== undefined && object.options !== null ? exports.OneofOptions.fromPartial(object.options) : undefined;
        return message;
    }
};
exports.EnumDescriptorProto = {
    $type: "google.protobuf.EnumDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.value) {
            exports.EnumValueDescriptorProto.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.options !== undefined) {
            exports.EnumOptions.encode(message.options, writer.uint32(26).fork()).join();
        }
        for (const v of message.reserved_range) {
            exports.EnumDescriptorProtoEnumReservedRange.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.reserved_name) {
            writer.uint32(42).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEnumDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.value.push(exports.EnumValueDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.options = exports.EnumOptions.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.reserved_range.push(exports.EnumDescriptorProtoEnumReservedRange.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.reserved_name.push(reader.string());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            value: globalThis.Array.isArray(object?.value) ? object.value.map((e) => exports.EnumValueDescriptorProto.fromJSON(e)) : [],
            options: isSet(object.options) ? exports.EnumOptions.fromJSON(object.options) : undefined,
            reserved_range: globalThis.Array.isArray(object?.reserved_range)
                ? object.reserved_range.map((e) => exports.EnumDescriptorProtoEnumReservedRange.fromJSON(e))
                : [],
            reserved_name: globalThis.Array.isArray(object?.reserved_name) ? object.reserved_name.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.value?.length) {
            obj.value = message.value.map((e) => exports.EnumValueDescriptorProto.toJSON(e));
        }
        if (message.options !== undefined) {
            obj.options = exports.EnumOptions.toJSON(message.options);
        }
        if (message.reserved_range?.length) {
            obj.reserved_range = message.reserved_range.map((e) => exports.EnumDescriptorProtoEnumReservedRange.toJSON(e));
        }
        if (message.reserved_name?.length) {
            obj.reserved_name = message.reserved_name;
        }
        return obj;
    },
    create(base) {
        return exports.EnumDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEnumDescriptorProto();
        message.name = object.name ?? "";
        message.value = object.value?.map((e) => exports.EnumValueDescriptorProto.fromPartial(e)) || [];
        message.options = object.options !== undefined && object.options !== null ? exports.EnumOptions.fromPartial(object.options) : undefined;
        message.reserved_range = object.reserved_range?.map((e) => exports.EnumDescriptorProtoEnumReservedRange.fromPartial(e)) || [];
        message.reserved_name = object.reserved_name?.map((e) => e) || [];
        return message;
    }
};
exports.EnumDescriptorProtoEnumReservedRange = {
    $type: "google.protobuf.EnumDescriptorProto.EnumReservedRange",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.start !== undefined && message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEnumDescriptorProtoEnumReservedRange();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.start = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.end = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            start: isSet(object.start) ? globalThis.Number(object.start) : 0,
            end: isSet(object.end) ? globalThis.Number(object.end) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.start !== undefined && message.start !== 0) {
            obj.start = Math.round(message.start);
        }
        if (message.end !== undefined && message.end !== 0) {
            obj.end = Math.round(message.end);
        }
        return obj;
    },
    create(base) {
        return exports.EnumDescriptorProtoEnumReservedRange.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEnumDescriptorProtoEnumReservedRange();
        message.start = object.start ?? 0;
        message.end = object.end ?? 0;
        return message;
    }
};
exports.EnumValueDescriptorProto = {
    $type: "google.protobuf.EnumValueDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.number !== undefined && message.number !== 0) {
            writer.uint32(16).int32(message.number);
        }
        if (message.options !== undefined) {
            exports.EnumValueOptions.encode(message.options, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEnumValueDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.number = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.options = exports.EnumValueOptions.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            number: isSet(object.number) ? globalThis.Number(object.number) : 0,
            options: isSet(object.options) ? exports.EnumValueOptions.fromJSON(object.options) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.number !== undefined && message.number !== 0) {
            obj.number = Math.round(message.number);
        }
        if (message.options !== undefined) {
            obj.options = exports.EnumValueOptions.toJSON(message.options);
        }
        return obj;
    },
    create(base) {
        return exports.EnumValueDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEnumValueDescriptorProto();
        message.name = object.name ?? "";
        message.number = object.number ?? 0;
        message.options = object.options !== undefined && object.options !== null ? exports.EnumValueOptions.fromPartial(object.options) : undefined;
        return message;
    }
};
exports.ServiceDescriptorProto = {
    $type: "google.protobuf.ServiceDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.method) {
            exports.MethodDescriptorProto.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.options !== undefined) {
            exports.ServiceOptions.encode(message.options, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseServiceDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.method.push(exports.MethodDescriptorProto.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.options = exports.ServiceOptions.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            method: globalThis.Array.isArray(object?.method) ? object.method.map((e) => exports.MethodDescriptorProto.fromJSON(e)) : [],
            options: isSet(object.options) ? exports.ServiceOptions.fromJSON(object.options) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.method?.length) {
            obj.method = message.method.map((e) => exports.MethodDescriptorProto.toJSON(e));
        }
        if (message.options !== undefined) {
            obj.options = exports.ServiceOptions.toJSON(message.options);
        }
        return obj;
    },
    create(base) {
        return exports.ServiceDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseServiceDescriptorProto();
        message.name = object.name ?? "";
        message.method = object.method?.map((e) => exports.MethodDescriptorProto.fromPartial(e)) || [];
        message.options = object.options !== undefined && object.options !== null ? exports.ServiceOptions.fromPartial(object.options) : undefined;
        return message;
    }
};
exports.MethodDescriptorProto = {
    $type: "google.protobuf.MethodDescriptorProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== undefined && message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.input_type !== undefined && message.input_type !== "") {
            writer.uint32(18).string(message.input_type);
        }
        if (message.output_type !== undefined && message.output_type !== "") {
            writer.uint32(26).string(message.output_type);
        }
        if (message.options !== undefined) {
            exports.MethodOptions.encode(message.options, writer.uint32(34).fork()).join();
        }
        if (message.client_streaming !== undefined && message.client_streaming !== false) {
            writer.uint32(40).bool(message.client_streaming);
        }
        if (message.server_streaming !== undefined && message.server_streaming !== false) {
            writer.uint32(48).bool(message.server_streaming);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMethodDescriptorProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.input_type = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.output_type = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.options = exports.MethodOptions.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.client_streaming = reader.bool();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.server_streaming = reader.bool();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            input_type: isSet(object.input_type) ? globalThis.String(object.input_type) : "",
            output_type: isSet(object.output_type) ? globalThis.String(object.output_type) : "",
            options: isSet(object.options) ? exports.MethodOptions.fromJSON(object.options) : undefined,
            client_streaming: isSet(object.client_streaming) ? globalThis.Boolean(object.client_streaming) : false,
            server_streaming: isSet(object.server_streaming) ? globalThis.Boolean(object.server_streaming) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== undefined && message.name !== "") {
            obj.name = message.name;
        }
        if (message.input_type !== undefined && message.input_type !== "") {
            obj.input_type = message.input_type;
        }
        if (message.output_type !== undefined && message.output_type !== "") {
            obj.output_type = message.output_type;
        }
        if (message.options !== undefined) {
            obj.options = exports.MethodOptions.toJSON(message.options);
        }
        if (message.client_streaming !== undefined && message.client_streaming !== false) {
            obj.client_streaming = message.client_streaming;
        }
        if (message.server_streaming !== undefined && message.server_streaming !== false) {
            obj.server_streaming = message.server_streaming;
        }
        return obj;
    },
    create(base) {
        return exports.MethodDescriptorProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMethodDescriptorProto();
        message.name = object.name ?? "";
        message.input_type = object.input_type ?? "";
        message.output_type = object.output_type ?? "";
        message.options = object.options !== undefined && object.options !== null ? exports.MethodOptions.fromPartial(object.options) : undefined;
        message.client_streaming = object.client_streaming ?? false;
        message.server_streaming = object.server_streaming ?? false;
        return message;
    }
};
exports.FileOptions = {
    $type: "google.protobuf.FileOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.java_package !== undefined && message.java_package !== "") {
            writer.uint32(10).string(message.java_package);
        }
        if (message.java_outer_classname !== undefined && message.java_outer_classname !== "") {
            writer.uint32(66).string(message.java_outer_classname);
        }
        if (message.java_multiple_files !== undefined && message.java_multiple_files !== false) {
            writer.uint32(80).bool(message.java_multiple_files);
        }
        if (message.java_generate_equals_and_hash !== undefined && message.java_generate_equals_and_hash !== false) {
            writer.uint32(160).bool(message.java_generate_equals_and_hash);
        }
        if (message.java_string_check_utf8 !== undefined && message.java_string_check_utf8 !== false) {
            writer.uint32(216).bool(message.java_string_check_utf8);
        }
        if (message.optimize_for !== undefined && message.optimize_for !== 1) {
            writer.uint32(72).int32(message.optimize_for);
        }
        if (message.go_package !== undefined && message.go_package !== "") {
            writer.uint32(90).string(message.go_package);
        }
        if (message.cc_generic_services !== undefined && message.cc_generic_services !== false) {
            writer.uint32(128).bool(message.cc_generic_services);
        }
        if (message.java_generic_services !== undefined && message.java_generic_services !== false) {
            writer.uint32(136).bool(message.java_generic_services);
        }
        if (message.py_generic_services !== undefined && message.py_generic_services !== false) {
            writer.uint32(144).bool(message.py_generic_services);
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(184).bool(message.deprecated);
        }
        if (message.cc_enable_arenas !== undefined && message.cc_enable_arenas !== true) {
            writer.uint32(248).bool(message.cc_enable_arenas);
        }
        if (message.objc_class_prefix !== undefined && message.objc_class_prefix !== "") {
            writer.uint32(290).string(message.objc_class_prefix);
        }
        if (message.csharp_namespace !== undefined && message.csharp_namespace !== "") {
            writer.uint32(298).string(message.csharp_namespace);
        }
        if (message.swift_prefix !== undefined && message.swift_prefix !== "") {
            writer.uint32(314).string(message.swift_prefix);
        }
        if (message.php_class_prefix !== undefined && message.php_class_prefix !== "") {
            writer.uint32(322).string(message.php_class_prefix);
        }
        if (message.php_namespace !== undefined && message.php_namespace !== "") {
            writer.uint32(330).string(message.php_namespace);
        }
        if (message.php_metadata_namespace !== undefined && message.php_metadata_namespace !== "") {
            writer.uint32(354).string(message.php_metadata_namespace);
        }
        if (message.ruby_package !== undefined && message.ruby_package !== "") {
            writer.uint32(362).string(message.ruby_package);
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(402).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFileOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.java_package = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.java_outer_classname = reader.string();
                    continue;
                case 10:
                    if (tag !== 80) {
                        break;
                    }
                    message.java_multiple_files = reader.bool();
                    continue;
                case 20:
                    if (tag !== 160) {
                        break;
                    }
                    message.java_generate_equals_and_hash = reader.bool();
                    continue;
                case 27:
                    if (tag !== 216) {
                        break;
                    }
                    message.java_string_check_utf8 = reader.bool();
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.optimize_for = reader.int32();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.go_package = reader.string();
                    continue;
                case 16:
                    if (tag !== 128) {
                        break;
                    }
                    message.cc_generic_services = reader.bool();
                    continue;
                case 17:
                    if (tag !== 136) {
                        break;
                    }
                    message.java_generic_services = reader.bool();
                    continue;
                case 18:
                    if (tag !== 144) {
                        break;
                    }
                    message.py_generic_services = reader.bool();
                    continue;
                case 23:
                    if (tag !== 184) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 31:
                    if (tag !== 248) {
                        break;
                    }
                    message.cc_enable_arenas = reader.bool();
                    continue;
                case 36:
                    if (tag !== 290) {
                        break;
                    }
                    message.objc_class_prefix = reader.string();
                    continue;
                case 37:
                    if (tag !== 298) {
                        break;
                    }
                    message.csharp_namespace = reader.string();
                    continue;
                case 39:
                    if (tag !== 314) {
                        break;
                    }
                    message.swift_prefix = reader.string();
                    continue;
                case 40:
                    if (tag !== 322) {
                        break;
                    }
                    message.php_class_prefix = reader.string();
                    continue;
                case 41:
                    if (tag !== 330) {
                        break;
                    }
                    message.php_namespace = reader.string();
                    continue;
                case 44:
                    if (tag !== 354) {
                        break;
                    }
                    message.php_metadata_namespace = reader.string();
                    continue;
                case 45:
                    if (tag !== 362) {
                        break;
                    }
                    message.ruby_package = reader.string();
                    continue;
                case 50:
                    if (tag !== 402) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            java_package: isSet(object.java_package) ? globalThis.String(object.java_package) : "",
            java_outer_classname: isSet(object.java_outer_classname) ? globalThis.String(object.java_outer_classname) : "",
            java_multiple_files: isSet(object.java_multiple_files) ? globalThis.Boolean(object.java_multiple_files) : false,
            java_generate_equals_and_hash: isSet(object.java_generate_equals_and_hash) ? globalThis.Boolean(object.java_generate_equals_and_hash) : false,
            java_string_check_utf8: isSet(object.java_string_check_utf8) ? globalThis.Boolean(object.java_string_check_utf8) : false,
            optimize_for: isSet(object.optimize_for) ? fileOptionsOptimizeModeFromJSON(object.optimize_for) : 1,
            go_package: isSet(object.go_package) ? globalThis.String(object.go_package) : "",
            cc_generic_services: isSet(object.cc_generic_services) ? globalThis.Boolean(object.cc_generic_services) : false,
            java_generic_services: isSet(object.java_generic_services) ? globalThis.Boolean(object.java_generic_services) : false,
            py_generic_services: isSet(object.py_generic_services) ? globalThis.Boolean(object.py_generic_services) : false,
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            cc_enable_arenas: isSet(object.cc_enable_arenas) ? globalThis.Boolean(object.cc_enable_arenas) : true,
            objc_class_prefix: isSet(object.objc_class_prefix) ? globalThis.String(object.objc_class_prefix) : "",
            csharp_namespace: isSet(object.csharp_namespace) ? globalThis.String(object.csharp_namespace) : "",
            swift_prefix: isSet(object.swift_prefix) ? globalThis.String(object.swift_prefix) : "",
            php_class_prefix: isSet(object.php_class_prefix) ? globalThis.String(object.php_class_prefix) : "",
            php_namespace: isSet(object.php_namespace) ? globalThis.String(object.php_namespace) : "",
            php_metadata_namespace: isSet(object.php_metadata_namespace) ? globalThis.String(object.php_metadata_namespace) : "",
            ruby_package: isSet(object.ruby_package) ? globalThis.String(object.ruby_package) : "",
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.java_package !== undefined && message.java_package !== "") {
            obj.java_package = message.java_package;
        }
        if (message.java_outer_classname !== undefined && message.java_outer_classname !== "") {
            obj.java_outer_classname = message.java_outer_classname;
        }
        if (message.java_multiple_files !== undefined && message.java_multiple_files !== false) {
            obj.java_multiple_files = message.java_multiple_files;
        }
        if (message.java_generate_equals_and_hash !== undefined && message.java_generate_equals_and_hash !== false) {
            obj.java_generate_equals_and_hash = message.java_generate_equals_and_hash;
        }
        if (message.java_string_check_utf8 !== undefined && message.java_string_check_utf8 !== false) {
            obj.java_string_check_utf8 = message.java_string_check_utf8;
        }
        if (message.optimize_for !== undefined && message.optimize_for !== 1) {
            obj.optimize_for = fileOptionsOptimizeModeToJSON(message.optimize_for);
        }
        if (message.go_package !== undefined && message.go_package !== "") {
            obj.go_package = message.go_package;
        }
        if (message.cc_generic_services !== undefined && message.cc_generic_services !== false) {
            obj.cc_generic_services = message.cc_generic_services;
        }
        if (message.java_generic_services !== undefined && message.java_generic_services !== false) {
            obj.java_generic_services = message.java_generic_services;
        }
        if (message.py_generic_services !== undefined && message.py_generic_services !== false) {
            obj.py_generic_services = message.py_generic_services;
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.cc_enable_arenas !== undefined && message.cc_enable_arenas !== true) {
            obj.cc_enable_arenas = message.cc_enable_arenas;
        }
        if (message.objc_class_prefix !== undefined && message.objc_class_prefix !== "") {
            obj.objc_class_prefix = message.objc_class_prefix;
        }
        if (message.csharp_namespace !== undefined && message.csharp_namespace !== "") {
            obj.csharp_namespace = message.csharp_namespace;
        }
        if (message.swift_prefix !== undefined && message.swift_prefix !== "") {
            obj.swift_prefix = message.swift_prefix;
        }
        if (message.php_class_prefix !== undefined && message.php_class_prefix !== "") {
            obj.php_class_prefix = message.php_class_prefix;
        }
        if (message.php_namespace !== undefined && message.php_namespace !== "") {
            obj.php_namespace = message.php_namespace;
        }
        if (message.php_metadata_namespace !== undefined && message.php_metadata_namespace !== "") {
            obj.php_metadata_namespace = message.php_metadata_namespace;
        }
        if (message.ruby_package !== undefined && message.ruby_package !== "") {
            obj.ruby_package = message.ruby_package;
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.FileOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFileOptions();
        message.java_package = object.java_package ?? "";
        message.java_outer_classname = object.java_outer_classname ?? "";
        message.java_multiple_files = object.java_multiple_files ?? false;
        message.java_generate_equals_and_hash = object.java_generate_equals_and_hash ?? false;
        message.java_string_check_utf8 = object.java_string_check_utf8 ?? false;
        message.optimize_for = object.optimize_for ?? 1;
        message.go_package = object.go_package ?? "";
        message.cc_generic_services = object.cc_generic_services ?? false;
        message.java_generic_services = object.java_generic_services ?? false;
        message.py_generic_services = object.py_generic_services ?? false;
        message.deprecated = object.deprecated ?? false;
        message.cc_enable_arenas = object.cc_enable_arenas ?? true;
        message.objc_class_prefix = object.objc_class_prefix ?? "";
        message.csharp_namespace = object.csharp_namespace ?? "";
        message.swift_prefix = object.swift_prefix ?? "";
        message.php_class_prefix = object.php_class_prefix ?? "";
        message.php_namespace = object.php_namespace ?? "";
        message.php_metadata_namespace = object.php_metadata_namespace ?? "";
        message.ruby_package = object.ruby_package ?? "";
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.MessageOptions = {
    $type: "google.protobuf.MessageOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_set_wire_format !== undefined && message.message_set_wire_format !== false) {
            writer.uint32(8).bool(message.message_set_wire_format);
        }
        if (message.no_standard_descriptor_accessor !== undefined && message.no_standard_descriptor_accessor !== false) {
            writer.uint32(16).bool(message.no_standard_descriptor_accessor);
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(24).bool(message.deprecated);
        }
        if (message.map_entry !== undefined && message.map_entry !== false) {
            writer.uint32(56).bool(message.map_entry);
        }
        if (message.deprecated_legacy_json_field_conflicts !== undefined && message.deprecated_legacy_json_field_conflicts !== false) {
            writer.uint32(88).bool(message.deprecated_legacy_json_field_conflicts);
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(98).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMessageOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.message_set_wire_format = reader.bool();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.no_standard_descriptor_accessor = reader.bool();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 7:
                    if (tag !== 56) {
                        break;
                    }
                    message.map_entry = reader.bool();
                    continue;
                case 11:
                    if (tag !== 88) {
                        break;
                    }
                    message.deprecated_legacy_json_field_conflicts = reader.bool();
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            message_set_wire_format: isSet(object.message_set_wire_format) ? globalThis.Boolean(object.message_set_wire_format) : false,
            no_standard_descriptor_accessor: isSet(object.no_standard_descriptor_accessor) ? globalThis.Boolean(object.no_standard_descriptor_accessor) : false,
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            map_entry: isSet(object.map_entry) ? globalThis.Boolean(object.map_entry) : false,
            deprecated_legacy_json_field_conflicts: isSet(object.deprecated_legacy_json_field_conflicts)
                ? globalThis.Boolean(object.deprecated_legacy_json_field_conflicts)
                : false,
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_set_wire_format !== undefined && message.message_set_wire_format !== false) {
            obj.message_set_wire_format = message.message_set_wire_format;
        }
        if (message.no_standard_descriptor_accessor !== undefined && message.no_standard_descriptor_accessor !== false) {
            obj.no_standard_descriptor_accessor = message.no_standard_descriptor_accessor;
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.map_entry !== undefined && message.map_entry !== false) {
            obj.map_entry = message.map_entry;
        }
        if (message.deprecated_legacy_json_field_conflicts !== undefined && message.deprecated_legacy_json_field_conflicts !== false) {
            obj.deprecated_legacy_json_field_conflicts = message.deprecated_legacy_json_field_conflicts;
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MessageOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMessageOptions();
        message.message_set_wire_format = object.message_set_wire_format ?? false;
        message.no_standard_descriptor_accessor = object.no_standard_descriptor_accessor ?? false;
        message.deprecated = object.deprecated ?? false;
        message.map_entry = object.map_entry ?? false;
        message.deprecated_legacy_json_field_conflicts = object.deprecated_legacy_json_field_conflicts ?? false;
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.FieldOptions = {
    $type: "google.protobuf.FieldOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.ctype !== undefined && message.ctype !== 0) {
            writer.uint32(8).int32(message.ctype);
        }
        if (message.packed !== undefined && message.packed !== false) {
            writer.uint32(16).bool(message.packed);
        }
        if (message.jstype !== undefined && message.jstype !== 0) {
            writer.uint32(48).int32(message.jstype);
        }
        if (message.lazy !== undefined && message.lazy !== false) {
            writer.uint32(40).bool(message.lazy);
        }
        if (message.unverified_lazy !== undefined && message.unverified_lazy !== false) {
            writer.uint32(120).bool(message.unverified_lazy);
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(24).bool(message.deprecated);
        }
        if (message.weak !== undefined && message.weak !== false) {
            writer.uint32(80).bool(message.weak);
        }
        if (message.debug_redact !== undefined && message.debug_redact !== false) {
            writer.uint32(128).bool(message.debug_redact);
        }
        if (message.retention !== undefined && message.retention !== 0) {
            writer.uint32(136).int32(message.retention);
        }
        writer.uint32(154).fork();
        for (const v of message.targets) {
            writer.int32(v);
        }
        writer.join();
        for (const v of message.edition_defaults) {
            exports.FieldOptionsEditionDefault.encode(v, writer.uint32(162).fork()).join();
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(170).fork()).join();
        }
        if (message.feature_support !== undefined) {
            exports.FieldOptionsFeatureSupport.encode(message.feature_support, writer.uint32(178).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFieldOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.ctype = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.packed = reader.bool();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.jstype = reader.int32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.lazy = reader.bool();
                    continue;
                case 15:
                    if (tag !== 120) {
                        break;
                    }
                    message.unverified_lazy = reader.bool();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 10:
                    if (tag !== 80) {
                        break;
                    }
                    message.weak = reader.bool();
                    continue;
                case 16:
                    if (tag !== 128) {
                        break;
                    }
                    message.debug_redact = reader.bool();
                    continue;
                case 17:
                    if (tag !== 136) {
                        break;
                    }
                    message.retention = reader.int32();
                    continue;
                case 19:
                    if (tag === 152) {
                        message.targets.push(reader.int32());
                        continue;
                    }
                    if (tag === 154) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.targets.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 20:
                    if (tag !== 162) {
                        break;
                    }
                    message.edition_defaults.push(exports.FieldOptionsEditionDefault.decode(reader, reader.uint32()));
                    continue;
                case 21:
                    if (tag !== 170) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 22:
                    if (tag !== 178) {
                        break;
                    }
                    message.feature_support = exports.FieldOptionsFeatureSupport.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            ctype: isSet(object.ctype) ? fieldOptionsCTypeFromJSON(object.ctype) : 0,
            packed: isSet(object.packed) ? globalThis.Boolean(object.packed) : false,
            jstype: isSet(object.jstype) ? fieldOptionsJSTypeFromJSON(object.jstype) : 0,
            lazy: isSet(object.lazy) ? globalThis.Boolean(object.lazy) : false,
            unverified_lazy: isSet(object.unverified_lazy) ? globalThis.Boolean(object.unverified_lazy) : false,
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            weak: isSet(object.weak) ? globalThis.Boolean(object.weak) : false,
            debug_redact: isSet(object.debug_redact) ? globalThis.Boolean(object.debug_redact) : false,
            retention: isSet(object.retention) ? fieldOptionsOptionRetentionFromJSON(object.retention) : 0,
            targets: globalThis.Array.isArray(object?.targets) ? object.targets.map((e) => fieldOptionsOptionTargetTypeFromJSON(e)) : [],
            edition_defaults: globalThis.Array.isArray(object?.edition_defaults)
                ? object.edition_defaults.map((e) => exports.FieldOptionsEditionDefault.fromJSON(e))
                : [],
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            feature_support: isSet(object.feature_support) ? exports.FieldOptionsFeatureSupport.fromJSON(object.feature_support) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.ctype !== undefined && message.ctype !== 0) {
            obj.ctype = fieldOptionsCTypeToJSON(message.ctype);
        }
        if (message.packed !== undefined && message.packed !== false) {
            obj.packed = message.packed;
        }
        if (message.jstype !== undefined && message.jstype !== 0) {
            obj.jstype = fieldOptionsJSTypeToJSON(message.jstype);
        }
        if (message.lazy !== undefined && message.lazy !== false) {
            obj.lazy = message.lazy;
        }
        if (message.unverified_lazy !== undefined && message.unverified_lazy !== false) {
            obj.unverified_lazy = message.unverified_lazy;
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.weak !== undefined && message.weak !== false) {
            obj.weak = message.weak;
        }
        if (message.debug_redact !== undefined && message.debug_redact !== false) {
            obj.debug_redact = message.debug_redact;
        }
        if (message.retention !== undefined && message.retention !== 0) {
            obj.retention = fieldOptionsOptionRetentionToJSON(message.retention);
        }
        if (message.targets?.length) {
            obj.targets = message.targets.map((e) => fieldOptionsOptionTargetTypeToJSON(e));
        }
        if (message.edition_defaults?.length) {
            obj.edition_defaults = message.edition_defaults.map((e) => exports.FieldOptionsEditionDefault.toJSON(e));
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.feature_support !== undefined) {
            obj.feature_support = exports.FieldOptionsFeatureSupport.toJSON(message.feature_support);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.FieldOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFieldOptions();
        message.ctype = object.ctype ?? 0;
        message.packed = object.packed ?? false;
        message.jstype = object.jstype ?? 0;
        message.lazy = object.lazy ?? false;
        message.unverified_lazy = object.unverified_lazy ?? false;
        message.deprecated = object.deprecated ?? false;
        message.weak = object.weak ?? false;
        message.debug_redact = object.debug_redact ?? false;
        message.retention = object.retention ?? 0;
        message.targets = object.targets?.map((e) => e) || [];
        message.edition_defaults = object.edition_defaults?.map((e) => exports.FieldOptionsEditionDefault.fromPartial(e)) || [];
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.feature_support =
            object.feature_support !== undefined && object.feature_support !== null ? exports.FieldOptionsFeatureSupport.fromPartial(object.feature_support) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.FieldOptionsEditionDefault = {
    $type: "google.protobuf.FieldOptions.EditionDefault",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.edition !== undefined && message.edition !== 0) {
            writer.uint32(24).int32(message.edition);
        }
        if (message.value !== undefined && message.value !== "") {
            writer.uint32(18).string(message.value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFieldOptionsEditionDefault();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.edition = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            edition: isSet(object.edition) ? editionFromJSON(object.edition) : 0,
            value: isSet(object.value) ? globalThis.String(object.value) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.edition !== undefined && message.edition !== 0) {
            obj.edition = editionToJSON(message.edition);
        }
        if (message.value !== undefined && message.value !== "") {
            obj.value = message.value;
        }
        return obj;
    },
    create(base) {
        return exports.FieldOptionsEditionDefault.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFieldOptionsEditionDefault();
        message.edition = object.edition ?? 0;
        message.value = object.value ?? "";
        return message;
    }
};
exports.FieldOptionsFeatureSupport = {
    $type: "google.protobuf.FieldOptions.FeatureSupport",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.edition_introduced !== undefined && message.edition_introduced !== 0) {
            writer.uint32(8).int32(message.edition_introduced);
        }
        if (message.edition_deprecated !== undefined && message.edition_deprecated !== 0) {
            writer.uint32(16).int32(message.edition_deprecated);
        }
        if (message.deprecation_warning !== undefined && message.deprecation_warning !== "") {
            writer.uint32(26).string(message.deprecation_warning);
        }
        if (message.edition_removed !== undefined && message.edition_removed !== 0) {
            writer.uint32(32).int32(message.edition_removed);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFieldOptionsFeatureSupport();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.edition_introduced = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.edition_deprecated = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.deprecation_warning = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.edition_removed = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            edition_introduced: isSet(object.edition_introduced) ? editionFromJSON(object.edition_introduced) : 0,
            edition_deprecated: isSet(object.edition_deprecated) ? editionFromJSON(object.edition_deprecated) : 0,
            deprecation_warning: isSet(object.deprecation_warning) ? globalThis.String(object.deprecation_warning) : "",
            edition_removed: isSet(object.edition_removed) ? editionFromJSON(object.edition_removed) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.edition_introduced !== undefined && message.edition_introduced !== 0) {
            obj.edition_introduced = editionToJSON(message.edition_introduced);
        }
        if (message.edition_deprecated !== undefined && message.edition_deprecated !== 0) {
            obj.edition_deprecated = editionToJSON(message.edition_deprecated);
        }
        if (message.deprecation_warning !== undefined && message.deprecation_warning !== "") {
            obj.deprecation_warning = message.deprecation_warning;
        }
        if (message.edition_removed !== undefined && message.edition_removed !== 0) {
            obj.edition_removed = editionToJSON(message.edition_removed);
        }
        return obj;
    },
    create(base) {
        return exports.FieldOptionsFeatureSupport.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFieldOptionsFeatureSupport();
        message.edition_introduced = object.edition_introduced ?? 0;
        message.edition_deprecated = object.edition_deprecated ?? 0;
        message.deprecation_warning = object.deprecation_warning ?? "";
        message.edition_removed = object.edition_removed ?? 0;
        return message;
    }
};
exports.OneofOptions = {
    $type: "google.protobuf.OneofOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(10).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseOneofOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.OneofOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOneofOptions();
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.EnumOptions = {
    $type: "google.protobuf.EnumOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.allow_alias !== undefined && message.allow_alias !== false) {
            writer.uint32(16).bool(message.allow_alias);
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(24).bool(message.deprecated);
        }
        if (message.deprecated_legacy_json_field_conflicts !== undefined && message.deprecated_legacy_json_field_conflicts !== false) {
            writer.uint32(48).bool(message.deprecated_legacy_json_field_conflicts);
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(58).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEnumOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.allow_alias = reader.bool();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.deprecated_legacy_json_field_conflicts = reader.bool();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            allow_alias: isSet(object.allow_alias) ? globalThis.Boolean(object.allow_alias) : false,
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            deprecated_legacy_json_field_conflicts: isSet(object.deprecated_legacy_json_field_conflicts)
                ? globalThis.Boolean(object.deprecated_legacy_json_field_conflicts)
                : false,
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allow_alias !== undefined && message.allow_alias !== false) {
            obj.allow_alias = message.allow_alias;
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.deprecated_legacy_json_field_conflicts !== undefined && message.deprecated_legacy_json_field_conflicts !== false) {
            obj.deprecated_legacy_json_field_conflicts = message.deprecated_legacy_json_field_conflicts;
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.EnumOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEnumOptions();
        message.allow_alias = object.allow_alias ?? false;
        message.deprecated = object.deprecated ?? false;
        message.deprecated_legacy_json_field_conflicts = object.deprecated_legacy_json_field_conflicts ?? false;
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.EnumValueOptions = {
    $type: "google.protobuf.EnumValueOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(8).bool(message.deprecated);
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(18).fork()).join();
        }
        if (message.debug_redact !== undefined && message.debug_redact !== false) {
            writer.uint32(24).bool(message.debug_redact);
        }
        if (message.feature_support !== undefined) {
            exports.FieldOptionsFeatureSupport.encode(message.feature_support, writer.uint32(34).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEnumValueOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.debug_redact = reader.bool();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.feature_support = exports.FieldOptionsFeatureSupport.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            debug_redact: isSet(object.debug_redact) ? globalThis.Boolean(object.debug_redact) : false,
            feature_support: isSet(object.feature_support) ? exports.FieldOptionsFeatureSupport.fromJSON(object.feature_support) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.debug_redact !== undefined && message.debug_redact !== false) {
            obj.debug_redact = message.debug_redact;
        }
        if (message.feature_support !== undefined) {
            obj.feature_support = exports.FieldOptionsFeatureSupport.toJSON(message.feature_support);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.EnumValueOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEnumValueOptions();
        message.deprecated = object.deprecated ?? false;
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.debug_redact = object.debug_redact ?? false;
        message.feature_support =
            object.feature_support !== undefined && object.feature_support !== null ? exports.FieldOptionsFeatureSupport.fromPartial(object.feature_support) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.ServiceOptions = {
    $type: "google.protobuf.ServiceOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(274).fork()).join();
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(264).bool(message.deprecated);
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseServiceOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 34:
                    if (tag !== 274) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 33:
                    if (tag !== 264) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ServiceOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseServiceOptions();
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.deprecated = object.deprecated ?? false;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.MethodOptions = {
    $type: "google.protobuf.MethodOptions",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.deprecated !== undefined && message.deprecated !== false) {
            writer.uint32(264).bool(message.deprecated);
        }
        if (message.idempotency_level !== undefined && message.idempotency_level !== 0) {
            writer.uint32(272).int32(message.idempotency_level);
        }
        if (message.features !== undefined) {
            exports.FeatureSet.encode(message.features, writer.uint32(282).fork()).join();
        }
        for (const v of message.uninterpreted_option) {
            exports.UninterpretedOption.encode(v, writer.uint32(7994).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMethodOptions();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 33:
                    if (tag !== 264) {
                        break;
                    }
                    message.deprecated = reader.bool();
                    continue;
                case 34:
                    if (tag !== 272) {
                        break;
                    }
                    message.idempotency_level = reader.int32();
                    continue;
                case 35:
                    if (tag !== 282) {
                        break;
                    }
                    message.features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 999:
                    if (tag !== 7994) {
                        break;
                    }
                    message.uninterpreted_option.push(exports.UninterpretedOption.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            deprecated: isSet(object.deprecated) ? globalThis.Boolean(object.deprecated) : false,
            idempotency_level: isSet(object.idempotency_level) ? methodOptionsIdempotencyLevelFromJSON(object.idempotency_level) : 0,
            features: isSet(object.features) ? exports.FeatureSet.fromJSON(object.features) : undefined,
            uninterpreted_option: globalThis.Array.isArray(object?.uninterpreted_option)
                ? object.uninterpreted_option.map((e) => exports.UninterpretedOption.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.deprecated !== undefined && message.deprecated !== false) {
            obj.deprecated = message.deprecated;
        }
        if (message.idempotency_level !== undefined && message.idempotency_level !== 0) {
            obj.idempotency_level = methodOptionsIdempotencyLevelToJSON(message.idempotency_level);
        }
        if (message.features !== undefined) {
            obj.features = exports.FeatureSet.toJSON(message.features);
        }
        if (message.uninterpreted_option?.length) {
            obj.uninterpreted_option = message.uninterpreted_option.map((e) => exports.UninterpretedOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MethodOptions.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMethodOptions();
        message.deprecated = object.deprecated ?? false;
        message.idempotency_level = object.idempotency_level ?? 0;
        message.features = object.features !== undefined && object.features !== null ? exports.FeatureSet.fromPartial(object.features) : undefined;
        message.uninterpreted_option = object.uninterpreted_option?.map((e) => exports.UninterpretedOption.fromPartial(e)) || [];
        return message;
    }
};
exports.UninterpretedOption = {
    $type: "google.protobuf.UninterpretedOption",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.name) {
            exports.UninterpretedOptionNamePart.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.identifier_value !== undefined && message.identifier_value !== "") {
            writer.uint32(26).string(message.identifier_value);
        }
        if (message.positive_int_value !== undefined && message.positive_int_value !== 0) {
            writer.uint32(32).uint64(message.positive_int_value);
        }
        if (message.negative_int_value !== undefined && message.negative_int_value !== 0) {
            writer.uint32(40).int64(message.negative_int_value);
        }
        if (message.double_value !== undefined && message.double_value !== 0) {
            writer.uint32(49).double(message.double_value);
        }
        if (message.string_value !== undefined && message.string_value.length !== 0) {
            writer.uint32(58).bytes(message.string_value);
        }
        if (message.aggregate_value !== undefined && message.aggregate_value !== "") {
            writer.uint32(66).string(message.aggregate_value);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseUninterpretedOption();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.name.push(exports.UninterpretedOptionNamePart.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.identifier_value = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.positive_int_value = longToNumber(reader.uint64());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.negative_int_value = longToNumber(reader.int64());
                    continue;
                case 6:
                    if (tag !== 49) {
                        break;
                    }
                    message.double_value = reader.double();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.string_value = reader.bytes();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.aggregate_value = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name: globalThis.Array.isArray(object?.name) ? object.name.map((e) => exports.UninterpretedOptionNamePart.fromJSON(e)) : [],
            identifier_value: isSet(object.identifier_value) ? globalThis.String(object.identifier_value) : "",
            positive_int_value: isSet(object.positive_int_value) ? globalThis.Number(object.positive_int_value) : 0,
            negative_int_value: isSet(object.negative_int_value) ? globalThis.Number(object.negative_int_value) : 0,
            double_value: isSet(object.double_value) ? globalThis.Number(object.double_value) : 0,
            string_value: isSet(object.string_value) ? bytesFromBase64(object.string_value) : new Uint8Array(0),
            aggregate_value: isSet(object.aggregate_value) ? globalThis.String(object.aggregate_value) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name?.length) {
            obj.name = message.name.map((e) => exports.UninterpretedOptionNamePart.toJSON(e));
        }
        if (message.identifier_value !== undefined && message.identifier_value !== "") {
            obj.identifier_value = message.identifier_value;
        }
        if (message.positive_int_value !== undefined && message.positive_int_value !== 0) {
            obj.positive_int_value = Math.round(message.positive_int_value);
        }
        if (message.negative_int_value !== undefined && message.negative_int_value !== 0) {
            obj.negative_int_value = Math.round(message.negative_int_value);
        }
        if (message.double_value !== undefined && message.double_value !== 0) {
            obj.double_value = message.double_value;
        }
        if (message.string_value !== undefined && message.string_value.length !== 0) {
            obj.string_value = base64FromBytes(message.string_value);
        }
        if (message.aggregate_value !== undefined && message.aggregate_value !== "") {
            obj.aggregate_value = message.aggregate_value;
        }
        return obj;
    },
    create(base) {
        return exports.UninterpretedOption.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseUninterpretedOption();
        message.name = object.name?.map((e) => exports.UninterpretedOptionNamePart.fromPartial(e)) || [];
        message.identifier_value = object.identifier_value ?? "";
        message.positive_int_value = object.positive_int_value ?? 0;
        message.negative_int_value = object.negative_int_value ?? 0;
        message.double_value = object.double_value ?? 0;
        message.string_value = object.string_value ?? new Uint8Array(0);
        message.aggregate_value = object.aggregate_value ?? "";
        return message;
    }
};
exports.UninterpretedOptionNamePart = {
    $type: "google.protobuf.UninterpretedOption.NamePart",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name_part !== "") {
            writer.uint32(10).string(message.name_part);
        }
        if (message.is_extension !== false) {
            writer.uint32(16).bool(message.is_extension);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseUninterpretedOptionNamePart();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name_part = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.is_extension = reader.bool();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            name_part: isSet(object.name_part) ? globalThis.String(object.name_part) : "",
            is_extension: isSet(object.is_extension) ? globalThis.Boolean(object.is_extension) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name_part !== "") {
            obj.name_part = message.name_part;
        }
        if (message.is_extension !== false) {
            obj.is_extension = message.is_extension;
        }
        return obj;
    },
    create(base) {
        return exports.UninterpretedOptionNamePart.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseUninterpretedOptionNamePart();
        message.name_part = object.name_part ?? "";
        message.is_extension = object.is_extension ?? false;
        return message;
    }
};
exports.FeatureSet = {
    $type: "google.protobuf.FeatureSet",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.field_presence !== undefined && message.field_presence !== 0) {
            writer.uint32(8).int32(message.field_presence);
        }
        if (message.enum_type !== undefined && message.enum_type !== 0) {
            writer.uint32(16).int32(message.enum_type);
        }
        if (message.repeated_field_encoding !== undefined && message.repeated_field_encoding !== 0) {
            writer.uint32(24).int32(message.repeated_field_encoding);
        }
        if (message.utf8_validation !== undefined && message.utf8_validation !== 0) {
            writer.uint32(32).int32(message.utf8_validation);
        }
        if (message.message_encoding !== undefined && message.message_encoding !== 0) {
            writer.uint32(40).int32(message.message_encoding);
        }
        if (message.json_format !== undefined && message.json_format !== 0) {
            writer.uint32(48).int32(message.json_format);
        }
        if (message.enforce_naming_style !== undefined && message.enforce_naming_style !== 0) {
            writer.uint32(56).int32(message.enforce_naming_style);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeatureSet();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.field_presence = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.enum_type = reader.int32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.repeated_field_encoding = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.utf8_validation = reader.int32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.message_encoding = reader.int32();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.json_format = reader.int32();
                    continue;
                case 7:
                    if (tag !== 56) {
                        break;
                    }
                    message.enforce_naming_style = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            field_presence: isSet(object.field_presence) ? featureSetFieldPresenceFromJSON(object.field_presence) : 0,
            enum_type: isSet(object.enum_type) ? featureSetEnumTypeFromJSON(object.enum_type) : 0,
            repeated_field_encoding: isSet(object.repeated_field_encoding) ? featureSetRepeatedFieldEncodingFromJSON(object.repeated_field_encoding) : 0,
            utf8_validation: isSet(object.utf8_validation) ? featureSetUtf8ValidationFromJSON(object.utf8_validation) : 0,
            message_encoding: isSet(object.message_encoding) ? featureSetMessageEncodingFromJSON(object.message_encoding) : 0,
            json_format: isSet(object.json_format) ? featureSetJsonFormatFromJSON(object.json_format) : 0,
            enforce_naming_style: isSet(object.enforce_naming_style) ? featureSetEnforceNamingStyleFromJSON(object.enforce_naming_style) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.field_presence !== undefined && message.field_presence !== 0) {
            obj.field_presence = featureSetFieldPresenceToJSON(message.field_presence);
        }
        if (message.enum_type !== undefined && message.enum_type !== 0) {
            obj.enum_type = featureSetEnumTypeToJSON(message.enum_type);
        }
        if (message.repeated_field_encoding !== undefined && message.repeated_field_encoding !== 0) {
            obj.repeated_field_encoding = featureSetRepeatedFieldEncodingToJSON(message.repeated_field_encoding);
        }
        if (message.utf8_validation !== undefined && message.utf8_validation !== 0) {
            obj.utf8_validation = featureSetUtf8ValidationToJSON(message.utf8_validation);
        }
        if (message.message_encoding !== undefined && message.message_encoding !== 0) {
            obj.message_encoding = featureSetMessageEncodingToJSON(message.message_encoding);
        }
        if (message.json_format !== undefined && message.json_format !== 0) {
            obj.json_format = featureSetJsonFormatToJSON(message.json_format);
        }
        if (message.enforce_naming_style !== undefined && message.enforce_naming_style !== 0) {
            obj.enforce_naming_style = featureSetEnforceNamingStyleToJSON(message.enforce_naming_style);
        }
        return obj;
    },
    create(base) {
        return exports.FeatureSet.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeatureSet();
        message.field_presence = object.field_presence ?? 0;
        message.enum_type = object.enum_type ?? 0;
        message.repeated_field_encoding = object.repeated_field_encoding ?? 0;
        message.utf8_validation = object.utf8_validation ?? 0;
        message.message_encoding = object.message_encoding ?? 0;
        message.json_format = object.json_format ?? 0;
        message.enforce_naming_style = object.enforce_naming_style ?? 0;
        return message;
    }
};
exports.FeatureSetDefaults = {
    $type: "google.protobuf.FeatureSetDefaults",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.defaults) {
            exports.FeatureSetDefaultsFeatureSetEditionDefault.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.minimum_edition !== undefined && message.minimum_edition !== 0) {
            writer.uint32(32).int32(message.minimum_edition);
        }
        if (message.maximum_edition !== undefined && message.maximum_edition !== 0) {
            writer.uint32(40).int32(message.maximum_edition);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeatureSetDefaults();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.defaults.push(exports.FeatureSetDefaultsFeatureSetEditionDefault.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.minimum_edition = reader.int32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.maximum_edition = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            defaults: globalThis.Array.isArray(object?.defaults) ? object.defaults.map((e) => exports.FeatureSetDefaultsFeatureSetEditionDefault.fromJSON(e)) : [],
            minimum_edition: isSet(object.minimum_edition) ? editionFromJSON(object.minimum_edition) : 0,
            maximum_edition: isSet(object.maximum_edition) ? editionFromJSON(object.maximum_edition) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.defaults?.length) {
            obj.defaults = message.defaults.map((e) => exports.FeatureSetDefaultsFeatureSetEditionDefault.toJSON(e));
        }
        if (message.minimum_edition !== undefined && message.minimum_edition !== 0) {
            obj.minimum_edition = editionToJSON(message.minimum_edition);
        }
        if (message.maximum_edition !== undefined && message.maximum_edition !== 0) {
            obj.maximum_edition = editionToJSON(message.maximum_edition);
        }
        return obj;
    },
    create(base) {
        return exports.FeatureSetDefaults.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeatureSetDefaults();
        message.defaults = object.defaults?.map((e) => exports.FeatureSetDefaultsFeatureSetEditionDefault.fromPartial(e)) || [];
        message.minimum_edition = object.minimum_edition ?? 0;
        message.maximum_edition = object.maximum_edition ?? 0;
        return message;
    }
};
exports.FeatureSetDefaultsFeatureSetEditionDefault = {
    $type: "google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.edition !== undefined && message.edition !== 0) {
            writer.uint32(24).int32(message.edition);
        }
        if (message.overridable_features !== undefined) {
            exports.FeatureSet.encode(message.overridable_features, writer.uint32(34).fork()).join();
        }
        if (message.fixed_features !== undefined) {
            exports.FeatureSet.encode(message.fixed_features, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeatureSetDefaultsFeatureSetEditionDefault();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.edition = reader.int32();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.overridable_features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.fixed_features = exports.FeatureSet.decode(reader, reader.uint32());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            edition: isSet(object.edition) ? editionFromJSON(object.edition) : 0,
            overridable_features: isSet(object.overridable_features) ? exports.FeatureSet.fromJSON(object.overridable_features) : undefined,
            fixed_features: isSet(object.fixed_features) ? exports.FeatureSet.fromJSON(object.fixed_features) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.edition !== undefined && message.edition !== 0) {
            obj.edition = editionToJSON(message.edition);
        }
        if (message.overridable_features !== undefined) {
            obj.overridable_features = exports.FeatureSet.toJSON(message.overridable_features);
        }
        if (message.fixed_features !== undefined) {
            obj.fixed_features = exports.FeatureSet.toJSON(message.fixed_features);
        }
        return obj;
    },
    create(base) {
        return exports.FeatureSetDefaultsFeatureSetEditionDefault.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeatureSetDefaultsFeatureSetEditionDefault();
        message.edition = object.edition ?? 0;
        message.overridable_features =
            object.overridable_features !== undefined && object.overridable_features !== null ? exports.FeatureSet.fromPartial(object.overridable_features) : undefined;
        message.fixed_features = object.fixed_features !== undefined && object.fixed_features !== null ? exports.FeatureSet.fromPartial(object.fixed_features) : undefined;
        return message;
    }
};
exports.SourceCodeInfo = {
    $type: "google.protobuf.SourceCodeInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.location) {
            exports.SourceCodeInfoLocation.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSourceCodeInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.location.push(exports.SourceCodeInfoLocation.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            location: globalThis.Array.isArray(object?.location) ? object.location.map((e) => exports.SourceCodeInfoLocation.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.location?.length) {
            obj.location = message.location.map((e) => exports.SourceCodeInfoLocation.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.SourceCodeInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSourceCodeInfo();
        message.location = object.location?.map((e) => exports.SourceCodeInfoLocation.fromPartial(e)) || [];
        return message;
    }
};
exports.SourceCodeInfoLocation = {
    $type: "google.protobuf.SourceCodeInfo.Location",
    encode(message, writer = new wire_1.BinaryWriter()) {
        writer.uint32(10).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.join();
        writer.uint32(18).fork();
        for (const v of message.span) {
            writer.int32(v);
        }
        writer.join();
        if (message.leading_comments !== undefined && message.leading_comments !== "") {
            writer.uint32(26).string(message.leading_comments);
        }
        if (message.trailing_comments !== undefined && message.trailing_comments !== "") {
            writer.uint32(34).string(message.trailing_comments);
        }
        for (const v of message.leading_detached_comments) {
            writer.uint32(50).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSourceCodeInfoLocation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag === 8) {
                        message.path.push(reader.int32());
                        continue;
                    }
                    if (tag === 10) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.path.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 2:
                    if (tag === 16) {
                        message.span.push(reader.int32());
                        continue;
                    }
                    if (tag === 18) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.span.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.leading_comments = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.trailing_comments = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.leading_detached_comments.push(reader.string());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            path: globalThis.Array.isArray(object?.path) ? object.path.map((e) => globalThis.Number(e)) : [],
            span: globalThis.Array.isArray(object?.span) ? object.span.map((e) => globalThis.Number(e)) : [],
            leading_comments: isSet(object.leading_comments) ? globalThis.String(object.leading_comments) : "",
            trailing_comments: isSet(object.trailing_comments) ? globalThis.String(object.trailing_comments) : "",
            leading_detached_comments: globalThis.Array.isArray(object?.leading_detached_comments)
                ? object.leading_detached_comments.map((e) => globalThis.String(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.path?.length) {
            obj.path = message.path.map((e) => Math.round(e));
        }
        if (message.span?.length) {
            obj.span = message.span.map((e) => Math.round(e));
        }
        if (message.leading_comments !== undefined && message.leading_comments !== "") {
            obj.leading_comments = message.leading_comments;
        }
        if (message.trailing_comments !== undefined && message.trailing_comments !== "") {
            obj.trailing_comments = message.trailing_comments;
        }
        if (message.leading_detached_comments?.length) {
            obj.leading_detached_comments = message.leading_detached_comments;
        }
        return obj;
    },
    create(base) {
        return exports.SourceCodeInfoLocation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSourceCodeInfoLocation();
        message.path = object.path?.map((e) => e) || [];
        message.span = object.span?.map((e) => e) || [];
        message.leading_comments = object.leading_comments ?? "";
        message.trailing_comments = object.trailing_comments ?? "";
        message.leading_detached_comments = object.leading_detached_comments?.map((e) => e) || [];
        return message;
    }
};
exports.GeneratedCodeInfo = {
    $type: "google.protobuf.GeneratedCodeInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.annotation) {
            exports.GeneratedCodeInfoAnnotation.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGeneratedCodeInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.annotation.push(exports.GeneratedCodeInfoAnnotation.decode(reader, reader.uint32()));
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            annotation: globalThis.Array.isArray(object?.annotation) ? object.annotation.map((e) => exports.GeneratedCodeInfoAnnotation.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.annotation?.length) {
            obj.annotation = message.annotation.map((e) => exports.GeneratedCodeInfoAnnotation.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GeneratedCodeInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGeneratedCodeInfo();
        message.annotation = object.annotation?.map((e) => exports.GeneratedCodeInfoAnnotation.fromPartial(e)) || [];
        return message;
    }
};
exports.GeneratedCodeInfoAnnotation = {
    $type: "google.protobuf.GeneratedCodeInfo.Annotation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        writer.uint32(10).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.join();
        if (message.source_file !== undefined && message.source_file !== "") {
            writer.uint32(18).string(message.source_file);
        }
        if (message.begin !== undefined && message.begin !== 0) {
            writer.uint32(24).int32(message.begin);
        }
        if (message.end !== undefined && message.end !== 0) {
            writer.uint32(32).int32(message.end);
        }
        if (message.semantic !== undefined && message.semantic !== 0) {
            writer.uint32(40).int32(message.semantic);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGeneratedCodeInfoAnnotation();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag === 8) {
                        message.path.push(reader.int32());
                        continue;
                    }
                    if (tag === 10) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.path.push(reader.int32());
                        }
                        continue;
                    }
                    break;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.source_file = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.begin = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.end = reader.int32();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.semantic = reader.int32();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            path: globalThis.Array.isArray(object?.path) ? object.path.map((e) => globalThis.Number(e)) : [],
            source_file: isSet(object.source_file) ? globalThis.String(object.source_file) : "",
            begin: isSet(object.begin) ? globalThis.Number(object.begin) : 0,
            end: isSet(object.end) ? globalThis.Number(object.end) : 0,
            semantic: isSet(object.semantic) ? generatedCodeInfoAnnotationSemanticFromJSON(object.semantic) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.path?.length) {
            obj.path = message.path.map((e) => Math.round(e));
        }
        if (message.source_file !== undefined && message.source_file !== "") {
            obj.source_file = message.source_file;
        }
        if (message.begin !== undefined && message.begin !== 0) {
            obj.begin = Math.round(message.begin);
        }
        if (message.end !== undefined && message.end !== 0) {
            obj.end = Math.round(message.end);
        }
        if (message.semantic !== undefined && message.semantic !== 0) {
            obj.semantic = generatedCodeInfoAnnotationSemanticToJSON(message.semantic);
        }
        return obj;
    },
    create(base) {
        return exports.GeneratedCodeInfoAnnotation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGeneratedCodeInfoAnnotation();
        message.path = object.path?.map((e) => e) || [];
        message.source_file = object.source_file ?? "";
        message.begin = object.begin ?? 0;
        message.end = object.end ?? 0;
        message.semantic = object.semantic ?? 0;
        return message;
    }
};
function editionFromJSON(object) {
    switch (object) {
        case 0:
        case "EDITION_UNKNOWN":
            return protobuf_1.Edition.EDITION_UNKNOWN;
        case 900:
        case "EDITION_LEGACY":
            return protobuf_1.Edition.EDITION_LEGACY;
        case 998:
        case "EDITION_PROTO2":
            return protobuf_1.Edition.EDITION_PROTO2;
        case 999:
        case "EDITION_PROTO3":
            return protobuf_1.Edition.EDITION_PROTO3;
        case 1000:
        case "EDITION_2023":
            return protobuf_1.Edition.EDITION_2023;
        case 1001:
        case "EDITION_2024":
            return protobuf_1.Edition.EDITION_2024;
        case 1:
        case "EDITION_1_TEST_ONLY":
            return protobuf_1.Edition.EDITION_1_TEST_ONLY;
        case 2:
        case "EDITION_2_TEST_ONLY":
            return protobuf_1.Edition.EDITION_2_TEST_ONLY;
        case 99997:
        case "EDITION_99997_TEST_ONLY":
            return protobuf_1.Edition.EDITION_99997_TEST_ONLY;
        case 99998:
        case "EDITION_99998_TEST_ONLY":
            return protobuf_1.Edition.EDITION_99998_TEST_ONLY;
        case 99999:
        case "EDITION_99999_TEST_ONLY":
            return protobuf_1.Edition.EDITION_99999_TEST_ONLY;
        case 2147483647:
        case "EDITION_MAX":
            return protobuf_1.Edition.EDITION_MAX;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.Edition.UNRECOGNIZED;
    }
}
function editionToJSON(object) {
    switch (object) {
        case protobuf_1.Edition.EDITION_UNKNOWN:
            return "EDITION_UNKNOWN";
        case protobuf_1.Edition.EDITION_LEGACY:
            return "EDITION_LEGACY";
        case protobuf_1.Edition.EDITION_PROTO2:
            return "EDITION_PROTO2";
        case protobuf_1.Edition.EDITION_PROTO3:
            return "EDITION_PROTO3";
        case protobuf_1.Edition.EDITION_2023:
            return "EDITION_2023";
        case protobuf_1.Edition.EDITION_2024:
            return "EDITION_2024";
        case protobuf_1.Edition.EDITION_1_TEST_ONLY:
            return "EDITION_1_TEST_ONLY";
        case protobuf_1.Edition.EDITION_2_TEST_ONLY:
            return "EDITION_2_TEST_ONLY";
        case protobuf_1.Edition.EDITION_99997_TEST_ONLY:
            return "EDITION_99997_TEST_ONLY";
        case protobuf_1.Edition.EDITION_99998_TEST_ONLY:
            return "EDITION_99998_TEST_ONLY";
        case protobuf_1.Edition.EDITION_99999_TEST_ONLY:
            return "EDITION_99999_TEST_ONLY";
        case protobuf_1.Edition.EDITION_MAX:
            return "EDITION_MAX";
        case protobuf_1.Edition.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function extensionRangeOptionsVerificationStateFromJSON(object) {
    switch (object) {
        case 0:
        case "DECLARATION":
            return protobuf_1.ExtensionRangeOptionsVerificationState.DECLARATION;
        case 1:
        case "UNVERIFIED":
            return protobuf_1.ExtensionRangeOptionsVerificationState.UNVERIFIED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.ExtensionRangeOptionsVerificationState.UNRECOGNIZED;
    }
}
function extensionRangeOptionsVerificationStateToJSON(object) {
    switch (object) {
        case protobuf_1.ExtensionRangeOptionsVerificationState.DECLARATION:
            return "DECLARATION";
        case protobuf_1.ExtensionRangeOptionsVerificationState.UNVERIFIED:
            return "UNVERIFIED";
        case protobuf_1.ExtensionRangeOptionsVerificationState.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldDescriptorProtoTypeFromJSON(object) {
    switch (object) {
        case 1:
        case "TYPE_DOUBLE":
            return protobuf_1.FieldDescriptorProtoType.TYPE_DOUBLE;
        case 2:
        case "TYPE_FLOAT":
            return protobuf_1.FieldDescriptorProtoType.TYPE_FLOAT;
        case 3:
        case "TYPE_INT64":
            return protobuf_1.FieldDescriptorProtoType.TYPE_INT64;
        case 4:
        case "TYPE_UINT64":
            return protobuf_1.FieldDescriptorProtoType.TYPE_UINT64;
        case 5:
        case "TYPE_INT32":
            return protobuf_1.FieldDescriptorProtoType.TYPE_INT32;
        case 6:
        case "TYPE_FIXED64":
            return protobuf_1.FieldDescriptorProtoType.TYPE_FIXED64;
        case 7:
        case "TYPE_FIXED32":
            return protobuf_1.FieldDescriptorProtoType.TYPE_FIXED32;
        case 8:
        case "TYPE_BOOL":
            return protobuf_1.FieldDescriptorProtoType.TYPE_BOOL;
        case 9:
        case "TYPE_STRING":
            return protobuf_1.FieldDescriptorProtoType.TYPE_STRING;
        case 10:
        case "TYPE_GROUP":
            return protobuf_1.FieldDescriptorProtoType.TYPE_GROUP;
        case 11:
        case "TYPE_MESSAGE":
            return protobuf_1.FieldDescriptorProtoType.TYPE_MESSAGE;
        case 12:
        case "TYPE_BYTES":
            return protobuf_1.FieldDescriptorProtoType.TYPE_BYTES;
        case 13:
        case "TYPE_UINT32":
            return protobuf_1.FieldDescriptorProtoType.TYPE_UINT32;
        case 14:
        case "TYPE_ENUM":
            return protobuf_1.FieldDescriptorProtoType.TYPE_ENUM;
        case 15:
        case "TYPE_SFIXED32":
            return protobuf_1.FieldDescriptorProtoType.TYPE_SFIXED32;
        case 16:
        case "TYPE_SFIXED64":
            return protobuf_1.FieldDescriptorProtoType.TYPE_SFIXED64;
        case 17:
        case "TYPE_SINT32":
            return protobuf_1.FieldDescriptorProtoType.TYPE_SINT32;
        case 18:
        case "TYPE_SINT64":
            return protobuf_1.FieldDescriptorProtoType.TYPE_SINT64;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldDescriptorProtoType.UNRECOGNIZED;
    }
}
function fieldDescriptorProtoTypeToJSON(object) {
    switch (object) {
        case protobuf_1.FieldDescriptorProtoType.TYPE_DOUBLE:
            return "TYPE_DOUBLE";
        case protobuf_1.FieldDescriptorProtoType.TYPE_FLOAT:
            return "TYPE_FLOAT";
        case protobuf_1.FieldDescriptorProtoType.TYPE_INT64:
            return "TYPE_INT64";
        case protobuf_1.FieldDescriptorProtoType.TYPE_UINT64:
            return "TYPE_UINT64";
        case protobuf_1.FieldDescriptorProtoType.TYPE_INT32:
            return "TYPE_INT32";
        case protobuf_1.FieldDescriptorProtoType.TYPE_FIXED64:
            return "TYPE_FIXED64";
        case protobuf_1.FieldDescriptorProtoType.TYPE_FIXED32:
            return "TYPE_FIXED32";
        case protobuf_1.FieldDescriptorProtoType.TYPE_BOOL:
            return "TYPE_BOOL";
        case protobuf_1.FieldDescriptorProtoType.TYPE_STRING:
            return "TYPE_STRING";
        case protobuf_1.FieldDescriptorProtoType.TYPE_GROUP:
            return "TYPE_GROUP";
        case protobuf_1.FieldDescriptorProtoType.TYPE_MESSAGE:
            return "TYPE_MESSAGE";
        case protobuf_1.FieldDescriptorProtoType.TYPE_BYTES:
            return "TYPE_BYTES";
        case protobuf_1.FieldDescriptorProtoType.TYPE_UINT32:
            return "TYPE_UINT32";
        case protobuf_1.FieldDescriptorProtoType.TYPE_ENUM:
            return "TYPE_ENUM";
        case protobuf_1.FieldDescriptorProtoType.TYPE_SFIXED32:
            return "TYPE_SFIXED32";
        case protobuf_1.FieldDescriptorProtoType.TYPE_SFIXED64:
            return "TYPE_SFIXED64";
        case protobuf_1.FieldDescriptorProtoType.TYPE_SINT32:
            return "TYPE_SINT32";
        case protobuf_1.FieldDescriptorProtoType.TYPE_SINT64:
            return "TYPE_SINT64";
        case protobuf_1.FieldDescriptorProtoType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldDescriptorProtoLabelFromJSON(object) {
    switch (object) {
        case 1:
        case "LABEL_OPTIONAL":
            return protobuf_1.FieldDescriptorProtoLabel.LABEL_OPTIONAL;
        case 3:
        case "LABEL_REPEATED":
            return protobuf_1.FieldDescriptorProtoLabel.LABEL_REPEATED;
        case 2:
        case "LABEL_REQUIRED":
            return protobuf_1.FieldDescriptorProtoLabel.LABEL_REQUIRED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldDescriptorProtoLabel.UNRECOGNIZED;
    }
}
function fieldDescriptorProtoLabelToJSON(object) {
    switch (object) {
        case protobuf_1.FieldDescriptorProtoLabel.LABEL_OPTIONAL:
            return "LABEL_OPTIONAL";
        case protobuf_1.FieldDescriptorProtoLabel.LABEL_REPEATED:
            return "LABEL_REPEATED";
        case protobuf_1.FieldDescriptorProtoLabel.LABEL_REQUIRED:
            return "LABEL_REQUIRED";
        case protobuf_1.FieldDescriptorProtoLabel.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fileOptionsOptimizeModeFromJSON(object) {
    switch (object) {
        case 1:
        case "SPEED":
            return protobuf_1.FileOptionsOptimizeMode.SPEED;
        case 2:
        case "CODE_SIZE":
            return protobuf_1.FileOptionsOptimizeMode.CODE_SIZE;
        case 3:
        case "LITE_RUNTIME":
            return protobuf_1.FileOptionsOptimizeMode.LITE_RUNTIME;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FileOptionsOptimizeMode.UNRECOGNIZED;
    }
}
function fileOptionsOptimizeModeToJSON(object) {
    switch (object) {
        case protobuf_1.FileOptionsOptimizeMode.SPEED:
            return "SPEED";
        case protobuf_1.FileOptionsOptimizeMode.CODE_SIZE:
            return "CODE_SIZE";
        case protobuf_1.FileOptionsOptimizeMode.LITE_RUNTIME:
            return "LITE_RUNTIME";
        case protobuf_1.FileOptionsOptimizeMode.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldOptionsCTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "STRING":
            return protobuf_1.FieldOptionsCType.STRING;
        case 1:
        case "CORD":
            return protobuf_1.FieldOptionsCType.CORD;
        case 2:
        case "STRING_PIECE":
            return protobuf_1.FieldOptionsCType.STRING_PIECE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldOptionsCType.UNRECOGNIZED;
    }
}
function fieldOptionsCTypeToJSON(object) {
    switch (object) {
        case protobuf_1.FieldOptionsCType.STRING:
            return "STRING";
        case protobuf_1.FieldOptionsCType.CORD:
            return "CORD";
        case protobuf_1.FieldOptionsCType.STRING_PIECE:
            return "STRING_PIECE";
        case protobuf_1.FieldOptionsCType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldOptionsJSTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "JS_NORMAL":
            return protobuf_1.FieldOptionsJSType.JS_NORMAL;
        case 1:
        case "JS_STRING":
            return protobuf_1.FieldOptionsJSType.JS_STRING;
        case 2:
        case "JS_NUMBER":
            return protobuf_1.FieldOptionsJSType.JS_NUMBER;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldOptionsJSType.UNRECOGNIZED;
    }
}
function fieldOptionsJSTypeToJSON(object) {
    switch (object) {
        case protobuf_1.FieldOptionsJSType.JS_NORMAL:
            return "JS_NORMAL";
        case protobuf_1.FieldOptionsJSType.JS_STRING:
            return "JS_STRING";
        case protobuf_1.FieldOptionsJSType.JS_NUMBER:
            return "JS_NUMBER";
        case protobuf_1.FieldOptionsJSType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldOptionsOptionRetentionFromJSON(object) {
    switch (object) {
        case 0:
        case "RETENTION_UNKNOWN":
            return protobuf_1.FieldOptionsOptionRetention.RETENTION_UNKNOWN;
        case 1:
        case "RETENTION_RUNTIME":
            return protobuf_1.FieldOptionsOptionRetention.RETENTION_RUNTIME;
        case 2:
        case "RETENTION_SOURCE":
            return protobuf_1.FieldOptionsOptionRetention.RETENTION_SOURCE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldOptionsOptionRetention.UNRECOGNIZED;
    }
}
function fieldOptionsOptionRetentionToJSON(object) {
    switch (object) {
        case protobuf_1.FieldOptionsOptionRetention.RETENTION_UNKNOWN:
            return "RETENTION_UNKNOWN";
        case protobuf_1.FieldOptionsOptionRetention.RETENTION_RUNTIME:
            return "RETENTION_RUNTIME";
        case protobuf_1.FieldOptionsOptionRetention.RETENTION_SOURCE:
            return "RETENTION_SOURCE";
        case protobuf_1.FieldOptionsOptionRetention.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function fieldOptionsOptionTargetTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "TARGET_TYPE_UNKNOWN":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_UNKNOWN;
        case 1:
        case "TARGET_TYPE_FILE":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_FILE;
        case 2:
        case "TARGET_TYPE_EXTENSION_RANGE":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_EXTENSION_RANGE;
        case 3:
        case "TARGET_TYPE_MESSAGE":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_MESSAGE;
        case 4:
        case "TARGET_TYPE_FIELD":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_FIELD;
        case 5:
        case "TARGET_TYPE_ONEOF":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ONEOF;
        case 6:
        case "TARGET_TYPE_ENUM":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ENUM;
        case 7:
        case "TARGET_TYPE_ENUM_ENTRY":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ENUM_ENTRY;
        case 8:
        case "TARGET_TYPE_SERVICE":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_SERVICE;
        case 9:
        case "TARGET_TYPE_METHOD":
            return protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_METHOD;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FieldOptionsOptionTargetType.UNRECOGNIZED;
    }
}
function fieldOptionsOptionTargetTypeToJSON(object) {
    switch (object) {
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_UNKNOWN:
            return "TARGET_TYPE_UNKNOWN";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_FILE:
            return "TARGET_TYPE_FILE";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_EXTENSION_RANGE:
            return "TARGET_TYPE_EXTENSION_RANGE";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_MESSAGE:
            return "TARGET_TYPE_MESSAGE";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_FIELD:
            return "TARGET_TYPE_FIELD";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ONEOF:
            return "TARGET_TYPE_ONEOF";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ENUM:
            return "TARGET_TYPE_ENUM";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_ENUM_ENTRY:
            return "TARGET_TYPE_ENUM_ENTRY";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_SERVICE:
            return "TARGET_TYPE_SERVICE";
        case protobuf_1.FieldOptionsOptionTargetType.TARGET_TYPE_METHOD:
            return "TARGET_TYPE_METHOD";
        case protobuf_1.FieldOptionsOptionTargetType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function methodOptionsIdempotencyLevelFromJSON(object) {
    switch (object) {
        case 0:
        case "IDEMPOTENCY_UNKNOWN":
            return protobuf_1.MethodOptionsIdempotencyLevel.IDEMPOTENCY_UNKNOWN;
        case 1:
        case "NO_SIDE_EFFECTS":
            return protobuf_1.MethodOptionsIdempotencyLevel.NO_SIDE_EFFECTS;
        case 2:
        case "IDEMPOTENT":
            return protobuf_1.MethodOptionsIdempotencyLevel.IDEMPOTENT;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.MethodOptionsIdempotencyLevel.UNRECOGNIZED;
    }
}
function methodOptionsIdempotencyLevelToJSON(object) {
    switch (object) {
        case protobuf_1.MethodOptionsIdempotencyLevel.IDEMPOTENCY_UNKNOWN:
            return "IDEMPOTENCY_UNKNOWN";
        case protobuf_1.MethodOptionsIdempotencyLevel.NO_SIDE_EFFECTS:
            return "NO_SIDE_EFFECTS";
        case protobuf_1.MethodOptionsIdempotencyLevel.IDEMPOTENT:
            return "IDEMPOTENT";
        case protobuf_1.MethodOptionsIdempotencyLevel.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetFieldPresenceFromJSON(object) {
    switch (object) {
        case 0:
        case "FIELD_PRESENCE_UNKNOWN":
            return protobuf_1.FeatureSetFieldPresence.FIELD_PRESENCE_UNKNOWN;
        case 1:
        case "EXPLICIT":
            return protobuf_1.FeatureSetFieldPresence.EXPLICIT;
        case 2:
        case "IMPLICIT":
            return protobuf_1.FeatureSetFieldPresence.IMPLICIT;
        case 3:
        case "LEGACY_REQUIRED":
            return protobuf_1.FeatureSetFieldPresence.LEGACY_REQUIRED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetFieldPresence.UNRECOGNIZED;
    }
}
function featureSetFieldPresenceToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetFieldPresence.FIELD_PRESENCE_UNKNOWN:
            return "FIELD_PRESENCE_UNKNOWN";
        case protobuf_1.FeatureSetFieldPresence.EXPLICIT:
            return "EXPLICIT";
        case protobuf_1.FeatureSetFieldPresence.IMPLICIT:
            return "IMPLICIT";
        case protobuf_1.FeatureSetFieldPresence.LEGACY_REQUIRED:
            return "LEGACY_REQUIRED";
        case protobuf_1.FeatureSetFieldPresence.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetEnumTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "ENUM_TYPE_UNKNOWN":
            return protobuf_1.FeatureSetEnumType.ENUM_TYPE_UNKNOWN;
        case 1:
        case "OPEN":
            return protobuf_1.FeatureSetEnumType.OPEN;
        case 2:
        case "CLOSED":
            return protobuf_1.FeatureSetEnumType.CLOSED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetEnumType.UNRECOGNIZED;
    }
}
function featureSetEnumTypeToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetEnumType.ENUM_TYPE_UNKNOWN:
            return "ENUM_TYPE_UNKNOWN";
        case protobuf_1.FeatureSetEnumType.OPEN:
            return "OPEN";
        case protobuf_1.FeatureSetEnumType.CLOSED:
            return "CLOSED";
        case protobuf_1.FeatureSetEnumType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetRepeatedFieldEncodingFromJSON(object) {
    switch (object) {
        case 0:
        case "REPEATED_FIELD_ENCODING_UNKNOWN":
            return protobuf_1.FeatureSetRepeatedFieldEncoding.REPEATED_FIELD_ENCODING_UNKNOWN;
        case 1:
        case "PACKED":
            return protobuf_1.FeatureSetRepeatedFieldEncoding.PACKED;
        case 2:
        case "EXPANDED":
            return protobuf_1.FeatureSetRepeatedFieldEncoding.EXPANDED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetRepeatedFieldEncoding.UNRECOGNIZED;
    }
}
function featureSetRepeatedFieldEncodingToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetRepeatedFieldEncoding.REPEATED_FIELD_ENCODING_UNKNOWN:
            return "REPEATED_FIELD_ENCODING_UNKNOWN";
        case protobuf_1.FeatureSetRepeatedFieldEncoding.PACKED:
            return "PACKED";
        case protobuf_1.FeatureSetRepeatedFieldEncoding.EXPANDED:
            return "EXPANDED";
        case protobuf_1.FeatureSetRepeatedFieldEncoding.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetUtf8ValidationFromJSON(object) {
    switch (object) {
        case 0:
        case "UTF8_VALIDATION_UNKNOWN":
            return protobuf_1.FeatureSetUtf8Validation.UTF8_VALIDATION_UNKNOWN;
        case 2:
        case "VERIFY":
            return protobuf_1.FeatureSetUtf8Validation.VERIFY;
        case 3:
        case "NONE":
            return protobuf_1.FeatureSetUtf8Validation.NONE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetUtf8Validation.UNRECOGNIZED;
    }
}
function featureSetUtf8ValidationToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetUtf8Validation.UTF8_VALIDATION_UNKNOWN:
            return "UTF8_VALIDATION_UNKNOWN";
        case protobuf_1.FeatureSetUtf8Validation.VERIFY:
            return "VERIFY";
        case protobuf_1.FeatureSetUtf8Validation.NONE:
            return "NONE";
        case protobuf_1.FeatureSetUtf8Validation.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetMessageEncodingFromJSON(object) {
    switch (object) {
        case 0:
        case "MESSAGE_ENCODING_UNKNOWN":
            return protobuf_1.FeatureSetMessageEncoding.MESSAGE_ENCODING_UNKNOWN;
        case 1:
        case "LENGTH_PREFIXED":
            return protobuf_1.FeatureSetMessageEncoding.LENGTH_PREFIXED;
        case 2:
        case "DELIMITED":
            return protobuf_1.FeatureSetMessageEncoding.DELIMITED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetMessageEncoding.UNRECOGNIZED;
    }
}
function featureSetMessageEncodingToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetMessageEncoding.MESSAGE_ENCODING_UNKNOWN:
            return "MESSAGE_ENCODING_UNKNOWN";
        case protobuf_1.FeatureSetMessageEncoding.LENGTH_PREFIXED:
            return "LENGTH_PREFIXED";
        case protobuf_1.FeatureSetMessageEncoding.DELIMITED:
            return "DELIMITED";
        case protobuf_1.FeatureSetMessageEncoding.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetJsonFormatFromJSON(object) {
    switch (object) {
        case 0:
        case "JSON_FORMAT_UNKNOWN":
            return protobuf_1.FeatureSetJsonFormat.JSON_FORMAT_UNKNOWN;
        case 1:
        case "ALLOW":
            return protobuf_1.FeatureSetJsonFormat.ALLOW;
        case 2:
        case "LEGACY_BEST_EFFORT":
            return protobuf_1.FeatureSetJsonFormat.LEGACY_BEST_EFFORT;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetJsonFormat.UNRECOGNIZED;
    }
}
function featureSetJsonFormatToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetJsonFormat.JSON_FORMAT_UNKNOWN:
            return "JSON_FORMAT_UNKNOWN";
        case protobuf_1.FeatureSetJsonFormat.ALLOW:
            return "ALLOW";
        case protobuf_1.FeatureSetJsonFormat.LEGACY_BEST_EFFORT:
            return "LEGACY_BEST_EFFORT";
        case protobuf_1.FeatureSetJsonFormat.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function featureSetEnforceNamingStyleFromJSON(object) {
    switch (object) {
        case 0:
        case "ENFORCE_NAMING_STYLE_UNKNOWN":
            return protobuf_1.FeatureSetEnforceNamingStyle.ENFORCE_NAMING_STYLE_UNKNOWN;
        case 1:
        case "STYLE2024":
            return protobuf_1.FeatureSetEnforceNamingStyle.STYLE2024;
        case 2:
        case "STYLE_LEGACY":
            return protobuf_1.FeatureSetEnforceNamingStyle.STYLE_LEGACY;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.FeatureSetEnforceNamingStyle.UNRECOGNIZED;
    }
}
function featureSetEnforceNamingStyleToJSON(object) {
    switch (object) {
        case protobuf_1.FeatureSetEnforceNamingStyle.ENFORCE_NAMING_STYLE_UNKNOWN:
            return "ENFORCE_NAMING_STYLE_UNKNOWN";
        case protobuf_1.FeatureSetEnforceNamingStyle.STYLE2024:
            return "STYLE2024";
        case protobuf_1.FeatureSetEnforceNamingStyle.STYLE_LEGACY:
            return "STYLE_LEGACY";
        case protobuf_1.FeatureSetEnforceNamingStyle.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function generatedCodeInfoAnnotationSemanticFromJSON(object) {
    switch (object) {
        case 0:
        case "NONE":
            return protobuf_1.GeneratedCodeInfoAnnotationSemantic.NONE;
        case 1:
        case "SET":
            return protobuf_1.GeneratedCodeInfoAnnotationSemantic.SET;
        case 2:
        case "ALIAS":
            return protobuf_1.GeneratedCodeInfoAnnotationSemantic.ALIAS;
        case -1:
        case "UNRECOGNIZED":
        default:
            return protobuf_1.GeneratedCodeInfoAnnotationSemantic.UNRECOGNIZED;
    }
}
function generatedCodeInfoAnnotationSemanticToJSON(object) {
    switch (object) {
        case protobuf_1.GeneratedCodeInfoAnnotationSemantic.NONE:
            return "NONE";
        case protobuf_1.GeneratedCodeInfoAnnotationSemantic.SET:
            return "SET";
        case protobuf_1.GeneratedCodeInfoAnnotationSemantic.ALIAS:
            return "ALIAS";
        case protobuf_1.GeneratedCodeInfoAnnotationSemantic.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseFileDescriptorSet() {
    return { file: [] };
}
function createBaseFileDescriptorProto() {
    return {
        name: "",
        package: "",
        dependency: [],
        public_dependency: [],
        weak_dependency: [],
        message_type: [],
        enum_type: [],
        service: [],
        extension: [],
        options: undefined,
        source_code_info: undefined,
        syntax: "",
        edition: 0
    };
}
function createBaseDescriptorProto() {
    return {
        name: "",
        field: [],
        extension: [],
        nested_type: [],
        enum_type: [],
        extension_range: [],
        oneof_decl: [],
        options: undefined,
        reserved_range: [],
        reserved_name: []
    };
}
function createBaseDescriptorProtoExtensionRange() {
    return { start: 0, end: 0, options: undefined };
}
function createBaseDescriptorProtoReservedRange() {
    return { start: 0, end: 0 };
}
function createBaseExtensionRangeOptions() {
    return { uninterpreted_option: [], declaration: [], features: undefined, verification: 1 };
}
function createBaseExtensionRangeOptionsDeclaration() {
    return { number: 0, full_name: "", type: "", reserved: false, repeated: false };
}
function createBaseFieldDescriptorProto() {
    return {
        name: "",
        number: 0,
        label: 1,
        type: 1,
        type_name: "",
        extendee: "",
        default_value: "",
        oneof_index: 0,
        json_name: "",
        options: undefined,
        proto3_optional: false
    };
}
function createBaseOneofDescriptorProto() {
    return { name: "", options: undefined };
}
function createBaseEnumDescriptorProto() {
    return { name: "", value: [], options: undefined, reserved_range: [], reserved_name: [] };
}
function createBaseEnumDescriptorProtoEnumReservedRange() {
    return { start: 0, end: 0 };
}
function createBaseEnumValueDescriptorProto() {
    return { name: "", number: 0, options: undefined };
}
function createBaseServiceDescriptorProto() {
    return { name: "", method: [], options: undefined };
}
function createBaseMethodDescriptorProto() {
    return {
        name: "",
        input_type: "",
        output_type: "",
        options: undefined,
        client_streaming: false,
        server_streaming: false
    };
}
function createBaseFileOptions() {
    return {
        java_package: "",
        java_outer_classname: "",
        java_multiple_files: false,
        java_generate_equals_and_hash: false,
        java_string_check_utf8: false,
        optimize_for: 1,
        go_package: "",
        cc_generic_services: false,
        java_generic_services: false,
        py_generic_services: false,
        deprecated: false,
        cc_enable_arenas: true,
        objc_class_prefix: "",
        csharp_namespace: "",
        swift_prefix: "",
        php_class_prefix: "",
        php_namespace: "",
        php_metadata_namespace: "",
        ruby_package: "",
        features: undefined,
        uninterpreted_option: []
    };
}
function createBaseMessageOptions() {
    return {
        message_set_wire_format: false,
        no_standard_descriptor_accessor: false,
        deprecated: false,
        map_entry: false,
        deprecated_legacy_json_field_conflicts: false,
        features: undefined,
        uninterpreted_option: []
    };
}
function createBaseFieldOptions() {
    return {
        ctype: 0,
        packed: false,
        jstype: 0,
        lazy: false,
        unverified_lazy: false,
        deprecated: false,
        weak: false,
        debug_redact: false,
        retention: 0,
        targets: [],
        edition_defaults: [],
        features: undefined,
        feature_support: undefined,
        uninterpreted_option: []
    };
}
function createBaseFieldOptionsEditionDefault() {
    return { edition: 0, value: "" };
}
function createBaseFieldOptionsFeatureSupport() {
    return { edition_introduced: 0, edition_deprecated: 0, deprecation_warning: "", edition_removed: 0 };
}
function createBaseOneofOptions() {
    return { features: undefined, uninterpreted_option: [] };
}
function createBaseEnumOptions() {
    return {
        allow_alias: false,
        deprecated: false,
        deprecated_legacy_json_field_conflicts: false,
        features: undefined,
        uninterpreted_option: []
    };
}
function createBaseEnumValueOptions() {
    return {
        deprecated: false,
        features: undefined,
        debug_redact: false,
        feature_support: undefined,
        uninterpreted_option: []
    };
}
function createBaseServiceOptions() {
    return { features: undefined, deprecated: false, uninterpreted_option: [] };
}
function createBaseMethodOptions() {
    return { deprecated: false, idempotency_level: 0, features: undefined, uninterpreted_option: [] };
}
function createBaseUninterpretedOption() {
    return {
        name: [],
        identifier_value: "",
        positive_int_value: 0,
        negative_int_value: 0,
        double_value: 0,
        string_value: new Uint8Array(0),
        aggregate_value: ""
    };
}
function createBaseUninterpretedOptionNamePart() {
    return { name_part: "", is_extension: false };
}
function createBaseFeatureSet() {
    return {
        field_presence: 0,
        enum_type: 0,
        repeated_field_encoding: 0,
        utf8_validation: 0,
        message_encoding: 0,
        json_format: 0,
        enforce_naming_style: 0
    };
}
function createBaseFeatureSetDefaults() {
    return { defaults: [], minimum_edition: 0, maximum_edition: 0 };
}
function createBaseFeatureSetDefaultsFeatureSetEditionDefault() {
    return { edition: 0, overridable_features: undefined, fixed_features: undefined };
}
function createBaseSourceCodeInfo() {
    return { location: [] };
}
function createBaseSourceCodeInfoLocation() {
    return { path: [], span: [], leading_comments: "", trailing_comments: "", leading_detached_comments: [] };
}
function createBaseGeneratedCodeInfo() {
    return { annotation: [] };
}
function createBaseGeneratedCodeInfoAnnotation() {
    return { path: [], source_file: "", begin: 0, end: 0, semantic: 0 };
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/google.protobuf.FileDescriptorSet", exports.FileDescriptorSet],
    ["/google.protobuf.FileDescriptorProto", exports.FileDescriptorProto],
    ["/google.protobuf.DescriptorProto", exports.DescriptorProto],
    ["/google.protobuf.DescriptorProto.ExtensionRange", exports.DescriptorProtoExtensionRange],
    ["/google.protobuf.DescriptorProto.ReservedRange", exports.DescriptorProtoReservedRange],
    ["/google.protobuf.ExtensionRangeOptions", exports.ExtensionRangeOptions],
    ["/google.protobuf.ExtensionRangeOptions.Declaration", exports.ExtensionRangeOptionsDeclaration],
    ["/google.protobuf.FieldDescriptorProto", exports.FieldDescriptorProto],
    ["/google.protobuf.OneofDescriptorProto", exports.OneofDescriptorProto],
    ["/google.protobuf.EnumDescriptorProto", exports.EnumDescriptorProto],
    ["/google.protobuf.EnumDescriptorProto.EnumReservedRange", exports.EnumDescriptorProtoEnumReservedRange],
    ["/google.protobuf.EnumValueDescriptorProto", exports.EnumValueDescriptorProto],
    ["/google.protobuf.ServiceDescriptorProto", exports.ServiceDescriptorProto],
    ["/google.protobuf.MethodDescriptorProto", exports.MethodDescriptorProto],
    ["/google.protobuf.FileOptions", exports.FileOptions],
    ["/google.protobuf.MessageOptions", exports.MessageOptions],
    ["/google.protobuf.FieldOptions", exports.FieldOptions],
    ["/google.protobuf.FieldOptions.EditionDefault", exports.FieldOptionsEditionDefault],
    ["/google.protobuf.FieldOptions.FeatureSupport", exports.FieldOptionsFeatureSupport],
    ["/google.protobuf.OneofOptions", exports.OneofOptions],
    ["/google.protobuf.EnumOptions", exports.EnumOptions],
    ["/google.protobuf.EnumValueOptions", exports.EnumValueOptions],
    ["/google.protobuf.ServiceOptions", exports.ServiceOptions],
    ["/google.protobuf.MethodOptions", exports.MethodOptions],
    ["/google.protobuf.UninterpretedOption", exports.UninterpretedOption],
    ["/google.protobuf.UninterpretedOption.NamePart", exports.UninterpretedOptionNamePart],
    ["/google.protobuf.FeatureSet", exports.FeatureSet],
    ["/google.protobuf.FeatureSetDefaults", exports.FeatureSetDefaults],
    ["/google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault", exports.FeatureSetDefaultsFeatureSetEditionDefault],
    ["/google.protobuf.SourceCodeInfo", exports.SourceCodeInfo],
    ["/google.protobuf.SourceCodeInfo.Location", exports.SourceCodeInfoLocation],
    ["/google.protobuf.GeneratedCodeInfo", exports.GeneratedCodeInfo],
    ["/google.protobuf.GeneratedCodeInfo.Annotation", exports.GeneratedCodeInfoAnnotation]
];
exports.aminoConverters = {
    "/google.protobuf.FileDescriptorSet": {
        aminoType: "google.protobuf.FileDescriptorSet",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FileDescriptorProto": {
        aminoType: "google.protobuf.FileDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.DescriptorProto": {
        aminoType: "google.protobuf.DescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.DescriptorProto.ExtensionRange": {
        aminoType: "google.protobuf.DescriptorProto.ExtensionRange",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.DescriptorProto.ReservedRange": {
        aminoType: "google.protobuf.DescriptorProto.ReservedRange",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.ExtensionRangeOptions": {
        aminoType: "google.protobuf.ExtensionRangeOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.ExtensionRangeOptions.Declaration": {
        aminoType: "google.protobuf.ExtensionRangeOptions.Declaration",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FieldDescriptorProto": {
        aminoType: "google.protobuf.FieldDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.OneofDescriptorProto": {
        aminoType: "google.protobuf.OneofDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.EnumDescriptorProto": {
        aminoType: "google.protobuf.EnumDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.EnumDescriptorProto.EnumReservedRange": {
        aminoType: "google.protobuf.EnumDescriptorProto.EnumReservedRange",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.EnumValueDescriptorProto": {
        aminoType: "google.protobuf.EnumValueDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.ServiceDescriptorProto": {
        aminoType: "google.protobuf.ServiceDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.MethodDescriptorProto": {
        aminoType: "google.protobuf.MethodDescriptorProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FileOptions": {
        aminoType: "google.protobuf.FileOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.MessageOptions": {
        aminoType: "google.protobuf.MessageOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FieldOptions": {
        aminoType: "google.protobuf.FieldOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FieldOptions.EditionDefault": {
        aminoType: "google.protobuf.FieldOptions.EditionDefault",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FieldOptions.FeatureSupport": {
        aminoType: "google.protobuf.FieldOptions.FeatureSupport",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.OneofOptions": {
        aminoType: "google.protobuf.OneofOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.EnumOptions": {
        aminoType: "google.protobuf.EnumOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.EnumValueOptions": {
        aminoType: "google.protobuf.EnumValueOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.ServiceOptions": {
        aminoType: "google.protobuf.ServiceOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.MethodOptions": {
        aminoType: "google.protobuf.MethodOptions",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.UninterpretedOption": {
        aminoType: "google.protobuf.UninterpretedOption",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.UninterpretedOption.NamePart": {
        aminoType: "google.protobuf.UninterpretedOption.NamePart",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FeatureSet": {
        aminoType: "google.protobuf.FeatureSet",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FeatureSetDefaults": {
        aminoType: "google.protobuf.FeatureSetDefaults",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault": {
        aminoType: "google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.SourceCodeInfo": {
        aminoType: "google.protobuf.SourceCodeInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.SourceCodeInfo.Location": {
        aminoType: "google.protobuf.SourceCodeInfo.Location",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.GeneratedCodeInfo": {
        aminoType: "google.protobuf.GeneratedCodeInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.protobuf.GeneratedCodeInfo.Annotation": {
        aminoType: "google.protobuf.GeneratedCodeInfo.Annotation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
