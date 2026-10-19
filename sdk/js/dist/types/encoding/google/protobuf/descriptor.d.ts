import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DescriptorProtoExtensionRange as DescriptorProtoExtensionRange_type, DescriptorProtoReservedRange as DescriptorProtoReservedRange_type, DescriptorProto as DescriptorProto_type, EnumDescriptorProtoEnumReservedRange as EnumDescriptorProtoEnumReservedRange_type, EnumDescriptorProto as EnumDescriptorProto_type, EnumOptions as EnumOptions_type, EnumValueDescriptorProto as EnumValueDescriptorProto_type, EnumValueOptions as EnumValueOptions_type, ExtensionRangeOptionsDeclaration as ExtensionRangeOptionsDeclaration_type, ExtensionRangeOptions as ExtensionRangeOptions_type, FeatureSetDefaultsFeatureSetEditionDefault as FeatureSetDefaultsFeatureSetEditionDefault_type, FeatureSetDefaults as FeatureSetDefaults_type, FeatureSet as FeatureSet_type, FieldDescriptorProto as FieldDescriptorProto_type, FieldOptionsEditionDefault as FieldOptionsEditionDefault_type, FieldOptionsFeatureSupport as FieldOptionsFeatureSupport_type, FieldOptions as FieldOptions_type, FileDescriptorProto as FileDescriptorProto_type, FileDescriptorSet as FileDescriptorSet_type, FileOptions as FileOptions_type, GeneratedCodeInfoAnnotation as GeneratedCodeInfoAnnotation_type, GeneratedCodeInfo as GeneratedCodeInfo_type, MessageOptions as MessageOptions_type, MethodDescriptorProto as MethodDescriptorProto_type, MethodOptions as MethodOptions_type, OneofDescriptorProto as OneofDescriptorProto_type, OneofOptions as OneofOptions_type, ServiceDescriptorProto as ServiceDescriptorProto_type, ServiceOptions as ServiceOptions_type, SourceCodeInfoLocation as SourceCodeInfoLocation_type, SourceCodeInfo as SourceCodeInfo_type, UninterpretedOptionNamePart as UninterpretedOptionNamePart_type, UninterpretedOption as UninterpretedOption_type } from "../../../types/google/protobuf";
import { Edition, ExtensionRangeOptionsVerificationState, FeatureSetEnforceNamingStyle, FeatureSetEnumType, FeatureSetFieldPresence, FeatureSetJsonFormat, FeatureSetMessageEncoding, FeatureSetRepeatedFieldEncoding, FeatureSetUtf8Validation, FieldDescriptorProtoLabel, FieldDescriptorProtoType, FieldOptionsCType, FieldOptionsJSType, FieldOptionsOptionRetention, FieldOptionsOptionTargetType, FileOptionsOptimizeMode, GeneratedCodeInfoAnnotationSemantic, MethodOptionsIdempotencyLevel } from "../../../types/google/protobuf";
import type { MessageFns } from "../../common";
export interface FileDescriptorSet extends FileDescriptorSet_type {
}
export interface FileDescriptorProto extends FileDescriptorProto_type {
}
export interface DescriptorProto extends DescriptorProto_type {
}
export interface DescriptorProtoExtensionRange extends DescriptorProtoExtensionRange_type {
}
export interface DescriptorProtoReservedRange extends DescriptorProtoReservedRange_type {
}
export interface ExtensionRangeOptions extends ExtensionRangeOptions_type {
}
export interface ExtensionRangeOptionsDeclaration extends ExtensionRangeOptionsDeclaration_type {
}
export interface FieldDescriptorProto extends FieldDescriptorProto_type {
}
export interface OneofDescriptorProto extends OneofDescriptorProto_type {
}
export interface EnumDescriptorProto extends EnumDescriptorProto_type {
}
export interface EnumDescriptorProtoEnumReservedRange extends EnumDescriptorProtoEnumReservedRange_type {
}
export interface EnumValueDescriptorProto extends EnumValueDescriptorProto_type {
}
export interface ServiceDescriptorProto extends ServiceDescriptorProto_type {
}
export interface MethodDescriptorProto extends MethodDescriptorProto_type {
}
export interface FileOptions extends FileOptions_type {
}
export interface MessageOptions extends MessageOptions_type {
}
export interface FieldOptions extends FieldOptions_type {
}
export interface FieldOptionsEditionDefault extends FieldOptionsEditionDefault_type {
}
export interface FieldOptionsFeatureSupport extends FieldOptionsFeatureSupport_type {
}
export interface OneofOptions extends OneofOptions_type {
}
export interface EnumOptions extends EnumOptions_type {
}
export interface EnumValueOptions extends EnumValueOptions_type {
}
export interface ServiceOptions extends ServiceOptions_type {
}
export interface MethodOptions extends MethodOptions_type {
}
export interface UninterpretedOption extends UninterpretedOption_type {
}
export interface UninterpretedOptionNamePart extends UninterpretedOptionNamePart_type {
}
export interface FeatureSet extends FeatureSet_type {
}
export interface FeatureSetDefaults extends FeatureSetDefaults_type {
}
export interface FeatureSetDefaultsFeatureSetEditionDefault extends FeatureSetDefaultsFeatureSetEditionDefault_type {
}
export interface SourceCodeInfo extends SourceCodeInfo_type {
}
export interface SourceCodeInfoLocation extends SourceCodeInfoLocation_type {
}
export interface GeneratedCodeInfo extends GeneratedCodeInfo_type {
}
export interface GeneratedCodeInfoAnnotation extends GeneratedCodeInfoAnnotation_type {
}
export declare const FileDescriptorSet: MessageFns<FileDescriptorSet, "google.protobuf.FileDescriptorSet">;
export declare const FileDescriptorProto: MessageFns<FileDescriptorProto, "google.protobuf.FileDescriptorProto">;
export declare const DescriptorProto: MessageFns<DescriptorProto, "google.protobuf.DescriptorProto">;
export declare const DescriptorProtoExtensionRange: MessageFns<DescriptorProtoExtensionRange, "google.protobuf.DescriptorProto.ExtensionRange">;
export declare const DescriptorProtoReservedRange: MessageFns<DescriptorProtoReservedRange, "google.protobuf.DescriptorProto.ReservedRange">;
export declare const ExtensionRangeOptions: MessageFns<ExtensionRangeOptions, "google.protobuf.ExtensionRangeOptions">;
export declare const ExtensionRangeOptionsDeclaration: MessageFns<ExtensionRangeOptionsDeclaration, "google.protobuf.ExtensionRangeOptions.Declaration">;
export declare const FieldDescriptorProto: MessageFns<FieldDescriptorProto, "google.protobuf.FieldDescriptorProto">;
export declare const OneofDescriptorProto: MessageFns<OneofDescriptorProto, "google.protobuf.OneofDescriptorProto">;
export declare const EnumDescriptorProto: MessageFns<EnumDescriptorProto, "google.protobuf.EnumDescriptorProto">;
export declare const EnumDescriptorProtoEnumReservedRange: MessageFns<EnumDescriptorProtoEnumReservedRange, "google.protobuf.EnumDescriptorProto.EnumReservedRange">;
export declare const EnumValueDescriptorProto: MessageFns<EnumValueDescriptorProto, "google.protobuf.EnumValueDescriptorProto">;
export declare const ServiceDescriptorProto: MessageFns<ServiceDescriptorProto, "google.protobuf.ServiceDescriptorProto">;
export declare const MethodDescriptorProto: MessageFns<MethodDescriptorProto, "google.protobuf.MethodDescriptorProto">;
export declare const FileOptions: MessageFns<FileOptions, "google.protobuf.FileOptions">;
export declare const MessageOptions: MessageFns<MessageOptions, "google.protobuf.MessageOptions">;
export declare const FieldOptions: MessageFns<FieldOptions, "google.protobuf.FieldOptions">;
export declare const FieldOptionsEditionDefault: MessageFns<FieldOptionsEditionDefault, "google.protobuf.FieldOptions.EditionDefault">;
export declare const FieldOptionsFeatureSupport: MessageFns<FieldOptionsFeatureSupport, "google.protobuf.FieldOptions.FeatureSupport">;
export declare const OneofOptions: MessageFns<OneofOptions, "google.protobuf.OneofOptions">;
export declare const EnumOptions: MessageFns<EnumOptions, "google.protobuf.EnumOptions">;
export declare const EnumValueOptions: MessageFns<EnumValueOptions, "google.protobuf.EnumValueOptions">;
export declare const ServiceOptions: MessageFns<ServiceOptions, "google.protobuf.ServiceOptions">;
export declare const MethodOptions: MessageFns<MethodOptions, "google.protobuf.MethodOptions">;
export declare const UninterpretedOption: MessageFns<UninterpretedOption, "google.protobuf.UninterpretedOption">;
export declare const UninterpretedOptionNamePart: MessageFns<UninterpretedOptionNamePart, "google.protobuf.UninterpretedOption.NamePart">;
export declare const FeatureSet: MessageFns<FeatureSet, "google.protobuf.FeatureSet">;
export declare const FeatureSetDefaults: MessageFns<FeatureSetDefaults, "google.protobuf.FeatureSetDefaults">;
export declare const FeatureSetDefaultsFeatureSetEditionDefault: MessageFns<FeatureSetDefaultsFeatureSetEditionDefault, "google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault">;
export declare const SourceCodeInfo: MessageFns<SourceCodeInfo, "google.protobuf.SourceCodeInfo">;
export declare const SourceCodeInfoLocation: MessageFns<SourceCodeInfoLocation, "google.protobuf.SourceCodeInfo.Location">;
export declare const GeneratedCodeInfo: MessageFns<GeneratedCodeInfo, "google.protobuf.GeneratedCodeInfo">;
export declare const GeneratedCodeInfoAnnotation: MessageFns<GeneratedCodeInfoAnnotation, "google.protobuf.GeneratedCodeInfo.Annotation">;
export declare function editionFromJSON(object: any): Edition;
export declare function editionToJSON(object: Edition): string;
export declare function extensionRangeOptionsVerificationStateFromJSON(object: any): ExtensionRangeOptionsVerificationState;
export declare function extensionRangeOptionsVerificationStateToJSON(object: ExtensionRangeOptionsVerificationState): string;
export declare function fieldDescriptorProtoTypeFromJSON(object: any): FieldDescriptorProtoType;
export declare function fieldDescriptorProtoTypeToJSON(object: FieldDescriptorProtoType): string;
export declare function fieldDescriptorProtoLabelFromJSON(object: any): FieldDescriptorProtoLabel;
export declare function fieldDescriptorProtoLabelToJSON(object: FieldDescriptorProtoLabel): string;
export declare function fileOptionsOptimizeModeFromJSON(object: any): FileOptionsOptimizeMode;
export declare function fileOptionsOptimizeModeToJSON(object: FileOptionsOptimizeMode): string;
export declare function fieldOptionsCTypeFromJSON(object: any): FieldOptionsCType;
export declare function fieldOptionsCTypeToJSON(object: FieldOptionsCType): string;
export declare function fieldOptionsJSTypeFromJSON(object: any): FieldOptionsJSType;
export declare function fieldOptionsJSTypeToJSON(object: FieldOptionsJSType): string;
export declare function fieldOptionsOptionRetentionFromJSON(object: any): FieldOptionsOptionRetention;
export declare function fieldOptionsOptionRetentionToJSON(object: FieldOptionsOptionRetention): string;
export declare function fieldOptionsOptionTargetTypeFromJSON(object: any): FieldOptionsOptionTargetType;
export declare function fieldOptionsOptionTargetTypeToJSON(object: FieldOptionsOptionTargetType): string;
export declare function methodOptionsIdempotencyLevelFromJSON(object: any): MethodOptionsIdempotencyLevel;
export declare function methodOptionsIdempotencyLevelToJSON(object: MethodOptionsIdempotencyLevel): string;
export declare function featureSetFieldPresenceFromJSON(object: any): FeatureSetFieldPresence;
export declare function featureSetFieldPresenceToJSON(object: FeatureSetFieldPresence): string;
export declare function featureSetEnumTypeFromJSON(object: any): FeatureSetEnumType;
export declare function featureSetEnumTypeToJSON(object: FeatureSetEnumType): string;
export declare function featureSetRepeatedFieldEncodingFromJSON(object: any): FeatureSetRepeatedFieldEncoding;
export declare function featureSetRepeatedFieldEncodingToJSON(object: FeatureSetRepeatedFieldEncoding): string;
export declare function featureSetUtf8ValidationFromJSON(object: any): FeatureSetUtf8Validation;
export declare function featureSetUtf8ValidationToJSON(object: FeatureSetUtf8Validation): string;
export declare function featureSetMessageEncodingFromJSON(object: any): FeatureSetMessageEncoding;
export declare function featureSetMessageEncodingToJSON(object: FeatureSetMessageEncoding): string;
export declare function featureSetJsonFormatFromJSON(object: any): FeatureSetJsonFormat;
export declare function featureSetJsonFormatToJSON(object: FeatureSetJsonFormat): string;
export declare function featureSetEnforceNamingStyleFromJSON(object: any): FeatureSetEnforceNamingStyle;
export declare function featureSetEnforceNamingStyleToJSON(object: FeatureSetEnforceNamingStyle): string;
export declare function generatedCodeInfoAnnotationSemanticFromJSON(object: any): GeneratedCodeInfoAnnotationSemantic;
export declare function generatedCodeInfoAnnotationSemanticToJSON(object: GeneratedCodeInfoAnnotationSemantic): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/google.protobuf.FileDescriptorSet": {
        aminoType: string;
        toAmino: (message: FileDescriptorSet) => {
            file: FileDescriptorProto_type[];
        };
        fromAmino: (object: FileDescriptorSet) => {
            file: FileDescriptorProto_type[];
        };
    };
    "/google.protobuf.FileDescriptorProto": {
        aminoType: string;
        toAmino: (message: FileDescriptorProto) => {
            name?: string;
            package?: string;
            dependency: string[];
            public_dependency: number[];
            weak_dependency: number[];
            message_type: DescriptorProto_type[];
            enum_type: EnumDescriptorProto_type[];
            service: ServiceDescriptorProto_type[];
            extension: FieldDescriptorProto_type[];
            options?: FileOptions_type;
            source_code_info?: SourceCodeInfo_type;
            syntax?: string;
            edition?: Edition;
        };
        fromAmino: (object: FileDescriptorProto) => {
            name?: string;
            package?: string;
            dependency: string[];
            public_dependency: number[];
            weak_dependency: number[];
            message_type: DescriptorProto_type[];
            enum_type: EnumDescriptorProto_type[];
            service: ServiceDescriptorProto_type[];
            extension: FieldDescriptorProto_type[];
            options?: FileOptions_type;
            source_code_info?: SourceCodeInfo_type;
            syntax?: string;
            edition?: Edition;
        };
    };
    "/google.protobuf.DescriptorProto": {
        aminoType: string;
        toAmino: (message: DescriptorProto) => {
            name?: string;
            field: FieldDescriptorProto_type[];
            extension: FieldDescriptorProto_type[];
            nested_type: DescriptorProto_type[];
            enum_type: EnumDescriptorProto_type[];
            extension_range: DescriptorProtoExtensionRange_type[];
            oneof_decl: OneofDescriptorProto_type[];
            options?: MessageOptions_type;
            reserved_range: DescriptorProtoReservedRange_type[];
            reserved_name: string[];
        };
        fromAmino: (object: DescriptorProto) => {
            name?: string;
            field: FieldDescriptorProto_type[];
            extension: FieldDescriptorProto_type[];
            nested_type: DescriptorProto_type[];
            enum_type: EnumDescriptorProto_type[];
            extension_range: DescriptorProtoExtensionRange_type[];
            oneof_decl: OneofDescriptorProto_type[];
            options?: MessageOptions_type;
            reserved_range: DescriptorProtoReservedRange_type[];
            reserved_name: string[];
        };
    };
    "/google.protobuf.DescriptorProto.ExtensionRange": {
        aminoType: string;
        toAmino: (message: DescriptorProtoExtensionRange) => {
            start?: number;
            end?: number;
            options?: ExtensionRangeOptions_type;
        };
        fromAmino: (object: DescriptorProtoExtensionRange) => {
            start?: number;
            end?: number;
            options?: ExtensionRangeOptions_type;
        };
    };
    "/google.protobuf.DescriptorProto.ReservedRange": {
        aminoType: string;
        toAmino: (message: DescriptorProtoReservedRange) => {
            start?: number;
            end?: number;
        };
        fromAmino: (object: DescriptorProtoReservedRange) => {
            start?: number;
            end?: number;
        };
    };
    "/google.protobuf.ExtensionRangeOptions": {
        aminoType: string;
        toAmino: (message: ExtensionRangeOptions) => {
            uninterpreted_option: UninterpretedOption_type[];
            declaration: ExtensionRangeOptionsDeclaration_type[];
            features?: FeatureSet_type;
            verification?: ExtensionRangeOptionsVerificationState;
        };
        fromAmino: (object: ExtensionRangeOptions) => {
            uninterpreted_option: UninterpretedOption_type[];
            declaration: ExtensionRangeOptionsDeclaration_type[];
            features?: FeatureSet_type;
            verification?: ExtensionRangeOptionsVerificationState;
        };
    };
    "/google.protobuf.ExtensionRangeOptions.Declaration": {
        aminoType: string;
        toAmino: (message: ExtensionRangeOptionsDeclaration) => {
            number?: number;
            full_name?: string;
            type?: string;
            reserved?: boolean;
            repeated?: boolean;
        };
        fromAmino: (object: ExtensionRangeOptionsDeclaration) => {
            number?: number;
            full_name?: string;
            type?: string;
            reserved?: boolean;
            repeated?: boolean;
        };
    };
    "/google.protobuf.FieldDescriptorProto": {
        aminoType: string;
        toAmino: (message: FieldDescriptorProto) => {
            name?: string;
            number?: number;
            label?: FieldDescriptorProtoLabel;
            type?: FieldDescriptorProtoType;
            type_name?: string;
            extendee?: string;
            default_value?: string;
            oneof_index?: number;
            json_name?: string;
            options?: FieldOptions_type;
            proto3_optional?: boolean;
        };
        fromAmino: (object: FieldDescriptorProto) => {
            name?: string;
            number?: number;
            label?: FieldDescriptorProtoLabel;
            type?: FieldDescriptorProtoType;
            type_name?: string;
            extendee?: string;
            default_value?: string;
            oneof_index?: number;
            json_name?: string;
            options?: FieldOptions_type;
            proto3_optional?: boolean;
        };
    };
    "/google.protobuf.OneofDescriptorProto": {
        aminoType: string;
        toAmino: (message: OneofDescriptorProto) => {
            name?: string;
            options?: OneofOptions_type;
        };
        fromAmino: (object: OneofDescriptorProto) => {
            name?: string;
            options?: OneofOptions_type;
        };
    };
    "/google.protobuf.EnumDescriptorProto": {
        aminoType: string;
        toAmino: (message: EnumDescriptorProto) => {
            name?: string;
            value: EnumValueDescriptorProto_type[];
            options?: EnumOptions_type;
            reserved_range: EnumDescriptorProtoEnumReservedRange_type[];
            reserved_name: string[];
        };
        fromAmino: (object: EnumDescriptorProto) => {
            name?: string;
            value: EnumValueDescriptorProto_type[];
            options?: EnumOptions_type;
            reserved_range: EnumDescriptorProtoEnumReservedRange_type[];
            reserved_name: string[];
        };
    };
    "/google.protobuf.EnumDescriptorProto.EnumReservedRange": {
        aminoType: string;
        toAmino: (message: EnumDescriptorProtoEnumReservedRange) => {
            start?: number;
            end?: number;
        };
        fromAmino: (object: EnumDescriptorProtoEnumReservedRange) => {
            start?: number;
            end?: number;
        };
    };
    "/google.protobuf.EnumValueDescriptorProto": {
        aminoType: string;
        toAmino: (message: EnumValueDescriptorProto) => {
            name?: string;
            number?: number;
            options?: EnumValueOptions_type;
        };
        fromAmino: (object: EnumValueDescriptorProto) => {
            name?: string;
            number?: number;
            options?: EnumValueOptions_type;
        };
    };
    "/google.protobuf.ServiceDescriptorProto": {
        aminoType: string;
        toAmino: (message: ServiceDescriptorProto) => {
            name?: string;
            method: MethodDescriptorProto_type[];
            options?: ServiceOptions_type;
        };
        fromAmino: (object: ServiceDescriptorProto) => {
            name?: string;
            method: MethodDescriptorProto_type[];
            options?: ServiceOptions_type;
        };
    };
    "/google.protobuf.MethodDescriptorProto": {
        aminoType: string;
        toAmino: (message: MethodDescriptorProto) => {
            name?: string;
            input_type?: string;
            output_type?: string;
            options?: MethodOptions_type;
            client_streaming?: boolean;
            server_streaming?: boolean;
        };
        fromAmino: (object: MethodDescriptorProto) => {
            name?: string;
            input_type?: string;
            output_type?: string;
            options?: MethodOptions_type;
            client_streaming?: boolean;
            server_streaming?: boolean;
        };
    };
    "/google.protobuf.FileOptions": {
        aminoType: string;
        toAmino: (message: FileOptions) => {
            java_package?: string;
            java_outer_classname?: string;
            java_multiple_files?: boolean;
            java_generate_equals_and_hash?: boolean;
            java_string_check_utf8?: boolean;
            optimize_for?: FileOptionsOptimizeMode;
            go_package?: string;
            cc_generic_services?: boolean;
            java_generic_services?: boolean;
            py_generic_services?: boolean;
            deprecated?: boolean;
            cc_enable_arenas?: boolean;
            objc_class_prefix?: string;
            csharp_namespace?: string;
            swift_prefix?: string;
            php_class_prefix?: string;
            php_namespace?: string;
            php_metadata_namespace?: string;
            ruby_package?: string;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: FileOptions) => {
            java_package?: string;
            java_outer_classname?: string;
            java_multiple_files?: boolean;
            java_generate_equals_and_hash?: boolean;
            java_string_check_utf8?: boolean;
            optimize_for?: FileOptionsOptimizeMode;
            go_package?: string;
            cc_generic_services?: boolean;
            java_generic_services?: boolean;
            py_generic_services?: boolean;
            deprecated?: boolean;
            cc_enable_arenas?: boolean;
            objc_class_prefix?: string;
            csharp_namespace?: string;
            swift_prefix?: string;
            php_class_prefix?: string;
            php_namespace?: string;
            php_metadata_namespace?: string;
            ruby_package?: string;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.MessageOptions": {
        aminoType: string;
        toAmino: (message: MessageOptions) => {
            message_set_wire_format?: boolean;
            no_standard_descriptor_accessor?: boolean;
            deprecated?: boolean;
            map_entry?: boolean;
            deprecated_legacy_json_field_conflicts?: boolean;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: MessageOptions) => {
            message_set_wire_format?: boolean;
            no_standard_descriptor_accessor?: boolean;
            deprecated?: boolean;
            map_entry?: boolean;
            deprecated_legacy_json_field_conflicts?: boolean;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.FieldOptions": {
        aminoType: string;
        toAmino: (message: FieldOptions) => {
            ctype?: FieldOptionsCType;
            packed?: boolean;
            jstype?: FieldOptionsJSType;
            lazy?: boolean;
            unverified_lazy?: boolean;
            deprecated?: boolean;
            weak?: boolean;
            debug_redact?: boolean;
            retention?: FieldOptionsOptionRetention;
            targets: FieldOptionsOptionTargetType[];
            edition_defaults: FieldOptionsEditionDefault_type[];
            features?: FeatureSet_type;
            feature_support?: FieldOptionsFeatureSupport_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: FieldOptions) => {
            ctype?: FieldOptionsCType;
            packed?: boolean;
            jstype?: FieldOptionsJSType;
            lazy?: boolean;
            unverified_lazy?: boolean;
            deprecated?: boolean;
            weak?: boolean;
            debug_redact?: boolean;
            retention?: FieldOptionsOptionRetention;
            targets: FieldOptionsOptionTargetType[];
            edition_defaults: FieldOptionsEditionDefault_type[];
            features?: FeatureSet_type;
            feature_support?: FieldOptionsFeatureSupport_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.FieldOptions.EditionDefault": {
        aminoType: string;
        toAmino: (message: FieldOptionsEditionDefault) => {
            edition?: Edition;
            value?: string;
        };
        fromAmino: (object: FieldOptionsEditionDefault) => {
            edition?: Edition;
            value?: string;
        };
    };
    "/google.protobuf.FieldOptions.FeatureSupport": {
        aminoType: string;
        toAmino: (message: FieldOptionsFeatureSupport) => {
            edition_introduced?: Edition;
            edition_deprecated?: Edition;
            deprecation_warning?: string;
            edition_removed?: Edition;
        };
        fromAmino: (object: FieldOptionsFeatureSupport) => {
            edition_introduced?: Edition;
            edition_deprecated?: Edition;
            deprecation_warning?: string;
            edition_removed?: Edition;
        };
    };
    "/google.protobuf.OneofOptions": {
        aminoType: string;
        toAmino: (message: OneofOptions) => {
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: OneofOptions) => {
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.EnumOptions": {
        aminoType: string;
        toAmino: (message: EnumOptions) => {
            allow_alias?: boolean;
            deprecated?: boolean;
            deprecated_legacy_json_field_conflicts?: boolean;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: EnumOptions) => {
            allow_alias?: boolean;
            deprecated?: boolean;
            deprecated_legacy_json_field_conflicts?: boolean;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.EnumValueOptions": {
        aminoType: string;
        toAmino: (message: EnumValueOptions) => {
            deprecated?: boolean;
            features?: FeatureSet_type;
            debug_redact?: boolean;
            feature_support?: FieldOptionsFeatureSupport_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: EnumValueOptions) => {
            deprecated?: boolean;
            features?: FeatureSet_type;
            debug_redact?: boolean;
            feature_support?: FieldOptionsFeatureSupport_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.ServiceOptions": {
        aminoType: string;
        toAmino: (message: ServiceOptions) => {
            features?: FeatureSet_type;
            deprecated?: boolean;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: ServiceOptions) => {
            features?: FeatureSet_type;
            deprecated?: boolean;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.MethodOptions": {
        aminoType: string;
        toAmino: (message: MethodOptions) => {
            deprecated?: boolean;
            idempotency_level?: MethodOptionsIdempotencyLevel;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
        fromAmino: (object: MethodOptions) => {
            deprecated?: boolean;
            idempotency_level?: MethodOptionsIdempotencyLevel;
            features?: FeatureSet_type;
            uninterpreted_option: UninterpretedOption_type[];
        };
    };
    "/google.protobuf.UninterpretedOption": {
        aminoType: string;
        toAmino: (message: UninterpretedOption) => {
            name: UninterpretedOptionNamePart_type[];
            identifier_value?: string;
            positive_int_value?: number;
            negative_int_value?: number;
            double_value?: number;
            string_value?: Uint8Array;
            aggregate_value?: string;
        };
        fromAmino: (object: UninterpretedOption) => {
            name: UninterpretedOptionNamePart_type[];
            identifier_value?: string;
            positive_int_value?: number;
            negative_int_value?: number;
            double_value?: number;
            string_value?: Uint8Array;
            aggregate_value?: string;
        };
    };
    "/google.protobuf.UninterpretedOption.NamePart": {
        aminoType: string;
        toAmino: (message: UninterpretedOptionNamePart) => {
            name_part: string;
            is_extension: boolean;
        };
        fromAmino: (object: UninterpretedOptionNamePart) => {
            name_part: string;
            is_extension: boolean;
        };
    };
    "/google.protobuf.FeatureSet": {
        aminoType: string;
        toAmino: (message: FeatureSet) => {
            field_presence?: FeatureSetFieldPresence;
            enum_type?: FeatureSetEnumType;
            repeated_field_encoding?: FeatureSetRepeatedFieldEncoding;
            utf8_validation?: FeatureSetUtf8Validation;
            message_encoding?: FeatureSetMessageEncoding;
            json_format?: FeatureSetJsonFormat;
            enforce_naming_style?: FeatureSetEnforceNamingStyle;
        };
        fromAmino: (object: FeatureSet) => {
            field_presence?: FeatureSetFieldPresence;
            enum_type?: FeatureSetEnumType;
            repeated_field_encoding?: FeatureSetRepeatedFieldEncoding;
            utf8_validation?: FeatureSetUtf8Validation;
            message_encoding?: FeatureSetMessageEncoding;
            json_format?: FeatureSetJsonFormat;
            enforce_naming_style?: FeatureSetEnforceNamingStyle;
        };
    };
    "/google.protobuf.FeatureSetDefaults": {
        aminoType: string;
        toAmino: (message: FeatureSetDefaults) => {
            defaults: FeatureSetDefaultsFeatureSetEditionDefault_type[];
            minimum_edition?: Edition;
            maximum_edition?: Edition;
        };
        fromAmino: (object: FeatureSetDefaults) => {
            defaults: FeatureSetDefaultsFeatureSetEditionDefault_type[];
            minimum_edition?: Edition;
            maximum_edition?: Edition;
        };
    };
    "/google.protobuf.FeatureSetDefaults.FeatureSetEditionDefault": {
        aminoType: string;
        toAmino: (message: FeatureSetDefaultsFeatureSetEditionDefault) => {
            edition?: Edition;
            overridable_features?: FeatureSet_type;
            fixed_features?: FeatureSet_type;
        };
        fromAmino: (object: FeatureSetDefaultsFeatureSetEditionDefault) => {
            edition?: Edition;
            overridable_features?: FeatureSet_type;
            fixed_features?: FeatureSet_type;
        };
    };
    "/google.protobuf.SourceCodeInfo": {
        aminoType: string;
        toAmino: (message: SourceCodeInfo) => {
            location: SourceCodeInfoLocation_type[];
        };
        fromAmino: (object: SourceCodeInfo) => {
            location: SourceCodeInfoLocation_type[];
        };
    };
    "/google.protobuf.SourceCodeInfo.Location": {
        aminoType: string;
        toAmino: (message: SourceCodeInfoLocation) => {
            path: number[];
            span: number[];
            leading_comments?: string;
            trailing_comments?: string;
            leading_detached_comments: string[];
        };
        fromAmino: (object: SourceCodeInfoLocation) => {
            path: number[];
            span: number[];
            leading_comments?: string;
            trailing_comments?: string;
            leading_detached_comments: string[];
        };
    };
    "/google.protobuf.GeneratedCodeInfo": {
        aminoType: string;
        toAmino: (message: GeneratedCodeInfo) => {
            annotation: GeneratedCodeInfoAnnotation_type[];
        };
        fromAmino: (object: GeneratedCodeInfo) => {
            annotation: GeneratedCodeInfoAnnotation_type[];
        };
    };
    "/google.protobuf.GeneratedCodeInfo.Annotation": {
        aminoType: string;
        toAmino: (message: GeneratedCodeInfoAnnotation) => {
            path: number[];
            source_file?: string;
            begin?: number;
            end?: number;
            semantic?: GeneratedCodeInfoAnnotationSemantic;
        };
        fromAmino: (object: GeneratedCodeInfoAnnotation) => {
            path: number[];
            source_file?: string;
            begin?: number;
            end?: number;
            semantic?: GeneratedCodeInfoAnnotationSemantic;
        };
    };
};
