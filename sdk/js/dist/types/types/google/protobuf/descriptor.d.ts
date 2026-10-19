export declare enum Edition {
    /** EDITION_UNKNOWN - A placeholder for an unknown edition value. */
    EDITION_UNKNOWN = 0,
    /**
     * EDITION_LEGACY - A placeholder edition for specifying default behaviors *before* a feature
     * was first introduced.  This is effectively an "infinite past".
     */
    EDITION_LEGACY = 900,
    /**
     * EDITION_PROTO2 - Legacy syntax "editions".  These pre-date editions, but behave much like
     * distinct editions.  These can't be used to specify the edition of proto
     * files, but feature definitions must supply proto2/proto3 defaults for
     * backwards compatibility.
     */
    EDITION_PROTO2 = 998,
    EDITION_PROTO3 = 999,
    /**
     * EDITION_2023 - Editions that have been released.  The specific values are arbitrary and
     * should not be depended on, but they will always be time-ordered for easy
     * comparison.
     */
    EDITION_2023 = 1000,
    EDITION_2024 = 1001,
    /**
     * EDITION_1_TEST_ONLY - Placeholder editions for testing feature resolution.  These should not be
     * used or relied on outside of tests.
     */
    EDITION_1_TEST_ONLY = 1,
    EDITION_2_TEST_ONLY = 2,
    EDITION_99997_TEST_ONLY = 99997,
    EDITION_99998_TEST_ONLY = 99998,
    EDITION_99999_TEST_ONLY = 99999,
    /**
     * EDITION_MAX - Placeholder for specifying unbounded edition support.  This should only
     * ever be used by plugins that can expect to never require any changes to
     * support a new edition.
     */
    EDITION_MAX = 2147483647,
    UNRECOGNIZED = -1
}
export interface FileDescriptorSet {
    file: FileDescriptorProto[];
}
export interface FileDescriptorProto {
    /** file name, relative to root of source tree */
    name?: string;
    /** e.g. "foo", "foo.bar", etc. */
    package?: string;
    /** Names of files imported by this file. */
    dependency: string[];
    /** Indexes of the public imported files in the dependency list above. */
    public_dependency: number[];
    /**
     * Indexes of the weak imported files in the dependency list.
     * For Google-internal migration only. Do not use.
     */
    weak_dependency: number[];
    /** All top-level definitions in this file. */
    message_type: DescriptorProto[];
    enum_type: EnumDescriptorProto[];
    service: ServiceDescriptorProto[];
    extension: FieldDescriptorProto[];
    options?: FileOptions;
    /**
     * This field contains optional information about the original source code.
     * You may safely remove this entire field without harming runtime
     * functionality of the descriptors -- the information is needed only by
     * development tools.
     */
    source_code_info?: SourceCodeInfo;
    /**
     * The syntax of the proto file.
     * The supported values are "proto2", "proto3", and "editions".
     *
     * If `edition` is present, this value must be "editions".
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    syntax?: string;
    /**
     * The edition of the proto file.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    edition?: Edition;
}
export interface DescriptorProto {
    name?: string;
    field: FieldDescriptorProto[];
    extension: FieldDescriptorProto[];
    nested_type: DescriptorProto[];
    enum_type: EnumDescriptorProto[];
    extension_range: DescriptorProtoExtensionRange[];
    oneof_decl: OneofDescriptorProto[];
    options?: MessageOptions;
    reserved_range: DescriptorProtoReservedRange[];
    /**
     * Reserved field names, which may not be used by fields in the same message.
     * A given name may only be reserved once.
     */
    reserved_name: string[];
}
export interface DescriptorProtoExtensionRange {
    /** Inclusive. */
    start?: number;
    /** Exclusive. */
    end?: number;
    options?: ExtensionRangeOptions;
}
export interface DescriptorProtoReservedRange {
    /** Inclusive. */
    start?: number;
    /** Exclusive. */
    end?: number;
}
export interface ExtensionRangeOptions {
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
    /**
     * For external users: DO NOT USE. We are in the process of open sourcing
     * extension declaration and executing internal cleanups before it can be
     * used externally.
     */
    declaration: ExtensionRangeOptionsDeclaration[];
    /** Any features defined in the specific edition. */
    features?: FeatureSet;
    /**
     * The verification state of the range.
     * TODO: flip the default to DECLARATION once all empty ranges
     * are marked as UNVERIFIED.
     */
    verification?: ExtensionRangeOptionsVerificationState;
}
export declare enum ExtensionRangeOptionsVerificationState {
    /** DECLARATION - All the extensions of the range must be declared. */
    DECLARATION = 0,
    UNVERIFIED = 1,
    UNRECOGNIZED = -1
}
export interface ExtensionRangeOptionsDeclaration {
    /** The extension number declared within the extension range. */
    number?: number;
    /**
     * The fully-qualified name of the extension field. There must be a leading
     * dot in front of the full name.
     */
    full_name?: string;
    /**
     * The fully-qualified type name of the extension field. Unlike
     * Metadata.type, Declaration.type must have a leading dot for messages
     * and enums.
     */
    type?: string;
    /**
     * If true, indicates that the number is reserved in the extension range,
     * and any extension field with the number will fail to compile. Set this
     * when a declared extension field is deleted.
     */
    reserved?: boolean;
    /**
     * If true, indicates that the extension must be defined as repeated.
     * Otherwise the extension must be defined as optional.
     */
    repeated?: boolean;
}
export interface FieldDescriptorProto {
    name?: string;
    number?: number;
    label?: FieldDescriptorProtoLabel;
    /**
     * If type_name is set, this need not be set.  If both this and type_name
     * are set, this must be one of TYPE_ENUM, TYPE_MESSAGE or TYPE_GROUP.
     */
    type?: FieldDescriptorProtoType;
    /**
     * For message and enum types, this is the name of the type.  If the name
     * starts with a '.', it is fully-qualified.  Otherwise, C++-like scoping
     * rules are used to find the type (i.e. first the nested types within this
     * message are searched, then within the parent, on up to the root
     * namespace).
     */
    type_name?: string;
    /**
     * For extensions, this is the name of the type being extended.  It is
     * resolved in the same manner as type_name.
     */
    extendee?: string;
    /**
     * For numeric types, contains the original text representation of the value.
     * For booleans, "true" or "false".
     * For strings, contains the default text contents (not escaped in any way).
     * For bytes, contains the C escaped value.  All bytes >= 128 are escaped.
     */
    default_value?: string;
    /**
     * If set, gives the index of a oneof in the containing type's oneof_decl
     * list.  This field is a member of that oneof.
     */
    oneof_index?: number;
    /**
     * JSON name of this field. The value is set by protocol compiler. If the
     * user has set a "json_name" option on this field, that option's value
     * will be used. Otherwise, it's deduced from the field's name by converting
     * it to camelCase.
     */
    json_name?: string;
    options?: FieldOptions;
    /**
     * If true, this is a proto3 "optional". When a proto3 field is optional, it
     * tracks presence regardless of field type.
     *
     * When proto3_optional is true, this field must belong to a oneof to signal
     * to old proto3 clients that presence is tracked for this field. This oneof
     * is known as a "synthetic" oneof, and this field must be its sole member
     * (each proto3 optional field gets its own synthetic oneof). Synthetic oneofs
     * exist in the descriptor only, and do not generate any API. Synthetic oneofs
     * must be ordered after all "real" oneofs.
     *
     * For message fields, proto3_optional doesn't create any semantic change,
     * since non-repeated message fields always track presence. However it still
     * indicates the semantic detail of whether the user wrote "optional" or not.
     * This can be useful for round-tripping the .proto file. For consistency we
     * give message fields a synthetic oneof also, even though it is not required
     * to track presence. This is especially important because the parser can't
     * tell if a field is a message or an enum, so it must always create a
     * synthetic oneof.
     *
     * Proto2 optional fields do not set this flag, because they already indicate
     * optional with `LABEL_OPTIONAL`.
     */
    proto3_optional?: boolean;
}
export declare enum FieldDescriptorProtoType {
    /**
     * TYPE_DOUBLE - 0 is reserved for errors.
     * Order is weird for historical reasons.
     */
    TYPE_DOUBLE = 1,
    TYPE_FLOAT = 2,
    /**
     * TYPE_INT64 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT64 if
     * negative values are likely.
     */
    TYPE_INT64 = 3,
    TYPE_UINT64 = 4,
    /**
     * TYPE_INT32 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT32 if
     * negative values are likely.
     */
    TYPE_INT32 = 5,
    TYPE_FIXED64 = 6,
    TYPE_FIXED32 = 7,
    TYPE_BOOL = 8,
    TYPE_STRING = 9,
    /**
     * TYPE_GROUP - Tag-delimited aggregate.
     * Group type is deprecated and not supported after google.protobuf. However, Proto3
     * implementations should still be able to parse the group wire format and
     * treat group fields as unknown fields.  In Editions, the group wire format
     * can be enabled via the `message_encoding` feature.
     */
    TYPE_GROUP = 10,
    /** TYPE_MESSAGE - Length-delimited aggregate. */
    TYPE_MESSAGE = 11,
    /** TYPE_BYTES - New in version 2. */
    TYPE_BYTES = 12,
    TYPE_UINT32 = 13,
    TYPE_ENUM = 14,
    TYPE_SFIXED32 = 15,
    TYPE_SFIXED64 = 16,
    /** TYPE_SINT32 - Uses ZigZag encoding. */
    TYPE_SINT32 = 17,
    /** TYPE_SINT64 - Uses ZigZag encoding. */
    TYPE_SINT64 = 18,
    UNRECOGNIZED = -1
}
export declare enum FieldDescriptorProtoLabel {
    /** LABEL_OPTIONAL - 0 is reserved for errors */
    LABEL_OPTIONAL = 1,
    LABEL_REPEATED = 3,
    /**
     * LABEL_REQUIRED - The required label is only allowed in google.protobuf.  In proto3 and Editions
     * it's explicitly prohibited.  In Editions, the `field_presence` feature
     * can be used to get this behavior.
     */
    LABEL_REQUIRED = 2,
    UNRECOGNIZED = -1
}
export interface OneofDescriptorProto {
    name?: string;
    options?: OneofOptions;
}
export interface EnumDescriptorProto {
    name?: string;
    value: EnumValueDescriptorProto[];
    options?: EnumOptions;
    /**
     * Range of reserved numeric values. Reserved numeric values may not be used
     * by enum values in the same enum declaration. Reserved ranges may not
     * overlap.
     */
    reserved_range: EnumDescriptorProtoEnumReservedRange[];
    /**
     * Reserved enum value names, which may not be reused. A given name may only
     * be reserved once.
     */
    reserved_name: string[];
}
export interface EnumDescriptorProtoEnumReservedRange {
    /** Inclusive. */
    start?: number;
    /** Inclusive. */
    end?: number;
}
export interface EnumValueDescriptorProto {
    name?: string;
    number?: number;
    options?: EnumValueOptions;
}
export interface ServiceDescriptorProto {
    name?: string;
    method: MethodDescriptorProto[];
    options?: ServiceOptions;
}
export interface MethodDescriptorProto {
    name?: string;
    /**
     * Input and output type names.  These are resolved in the same way as
     * FieldDescriptorProto.type_name, but must refer to a message type.
     */
    input_type?: string;
    output_type?: string;
    options?: MethodOptions;
    /** Identifies if client streams multiple client messages */
    client_streaming?: boolean;
    /** Identifies if server streams multiple server messages */
    server_streaming?: boolean;
}
export interface FileOptions {
    /**
     * Sets the Java package where classes generated from this .proto will be
     * placed.  By default, the proto package is used, but this is often
     * inappropriate because proto packages do not normally start with backwards
     * domain names.
     */
    java_package?: string;
    /**
     * Controls the name of the wrapper Java class generated for the .proto file.
     * That class will always contain the .proto file's getDescriptor() method as
     * well as any top-level extensions defined in the .proto file.
     * If java_multiple_files is disabled, then all the other classes from the
     * .proto file will be nested inside the single wrapper outer class.
     */
    java_outer_classname?: string;
    /**
     * If enabled, then the Java code generator will generate a separate .java
     * file for each top-level message, enum, and service defined in the .proto
     * file.  Thus, these types will *not* be nested inside the wrapper class
     * named by java_outer_classname.  However, the wrapper class will still be
     * generated to contain the file's getDescriptor() method as well as any
     * top-level extensions defined in the file.
     */
    java_multiple_files?: boolean;
    /**
     * This option does nothing.
     *
     * @deprecated
     */
    java_generate_equals_and_hash?: boolean;
    /**
     * A proto2 file can set this to true to opt in to UTF-8 checking for Java,
     * which will throw an exception if invalid UTF-8 is parsed from the wire or
     * assigned to a string field.
     *
     * TODO: clarify exactly what kinds of field types this option
     * applies to, and update these docs accordingly.
     *
     * Proto3 files already perform these checks. Setting the option explicitly to
     * false has no effect: it cannot be used to opt proto3 files out of UTF-8
     * checks.
     */
    java_string_check_utf8?: boolean;
    optimize_for?: FileOptionsOptimizeMode;
    /**
     * Sets the Go package where structs generated from this .proto will be
     * placed. If omitted, the Go package will be derived from the following:
     *   - The basename of the package import path, if provided.
     *   - Otherwise, the package statement in the .proto file, if present.
     *   - Otherwise, the basename of the .proto file, without extension.
     */
    go_package?: string;
    /**
     * Should generic services be generated in each language?  "Generic" services
     * are not specific to any particular RPC system.  They are generated by the
     * main code generators in each language (without additional plugins).
     * Generic services were the only kind of service generation supported by
     * early versions of google.protobuf.
     *
     * Generic services are now considered deprecated in favor of using plugins
     * that generate code specific to your particular RPC system.  Therefore,
     * these default to false.  Old code which depends on generic services should
     * explicitly set them to true.
     */
    cc_generic_services?: boolean;
    java_generic_services?: boolean;
    py_generic_services?: boolean;
    /**
     * Is this file deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for everything in the file, or it will be completely ignored; in the very
     * least, this is a formalization for deprecating files.
     */
    deprecated?: boolean;
    /**
     * Enables the use of arenas for the proto messages in this file. This applies
     * only to generated classes for C++.
     */
    cc_enable_arenas?: boolean;
    /**
     * Sets the objective c class prefix which is prepended to all objective c
     * generated classes from this .proto. There is no default.
     */
    objc_class_prefix?: string;
    /** Namespace for generated classes; defaults to the package. */
    csharp_namespace?: string;
    /**
     * By default Swift generators will take the proto package and CamelCase it
     * replacing '.' with underscore and use that to prefix the types/symbols
     * defined. When this options is provided, they will use this value instead
     * to prefix the types/symbols defined.
     */
    swift_prefix?: string;
    /**
     * Sets the php class prefix which is prepended to all php generated classes
     * from this .proto. Default is empty.
     */
    php_class_prefix?: string;
    /**
     * Use this option to change the namespace of php generated classes. Default
     * is empty. When this option is empty, the package name will be used for
     * determining the namespace.
     */
    php_namespace?: string;
    /**
     * Use this option to change the namespace of php generated metadata classes.
     * Default is empty. When this option is empty, the proto file name will be
     * used for determining the namespace.
     */
    php_metadata_namespace?: string;
    /**
     * Use this option to change the package of ruby generated classes. Default
     * is empty. When this option is not set, the package name will be used for
     * determining the ruby package.
     */
    ruby_package?: string;
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /**
     * The parser stores options it doesn't recognize here.
     * See the documentation for the "Options" section above.
     */
    uninterpreted_option: UninterpretedOption[];
}
export declare enum FileOptionsOptimizeMode {
    /** SPEED - Generate complete code for parsing, serialization, */
    SPEED = 1,
    /** CODE_SIZE - etc. */
    CODE_SIZE = 2,
    /** LITE_RUNTIME - Generate code using MessageLite and the lite runtime. */
    LITE_RUNTIME = 3,
    UNRECOGNIZED = -1
}
export interface MessageOptions {
    /**
     * Set true to use the old proto1 MessageSet wire format for extensions.
     * This is provided for backwards-compatibility with the MessageSet wire
     * format.  You should not use this for any other reason:  It's less
     * efficient, has fewer features, and is more complicated.
     *
     * The message must be defined exactly as follows:
     *   message Foo {
     *     option message_set_wire_format = true;
     *     extensions 4 to max;
     *   }
     * Note that the message cannot have any defined fields; MessageSets only
     * have extensions.
     *
     * All extensions of your type must be singular messages; e.g. they cannot
     * be int32s, enums, or repeated messages.
     *
     * Because this is an option, the above two restrictions are not enforced by
     * the protocol compiler.
     */
    message_set_wire_format?: boolean;
    /**
     * Disables the generation of the standard "descriptor()" accessor, which can
     * conflict with a field of the same name.  This is meant to make migration
     * from proto1 easier; new code should avoid fields named "descriptor".
     */
    no_standard_descriptor_accessor?: boolean;
    /**
     * Is this message deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for the message, or it will be completely ignored; in the very least,
     * this is a formalization for deprecating messages.
     */
    deprecated?: boolean;
    /**
     * Whether the message is an automatically generated map entry type for the
     * maps field.
     *
     * For maps fields:
     *     map<KeyType, ValueType> map_field = 1;
     * The parsed descriptor looks like:
     *     message MapFieldEntry {
     *         option map_entry = true;
     *         optional KeyType key = 1;
     *         optional ValueType value = 2;
     *     }
     *     repeated MapFieldEntry map_field = 1;
     *
     * Implementations may choose not to generate the map_entry=true message, but
     * use a native map in the target language to hold the keys and values.
     * The reflection APIs in such implementations still need to work as
     * if the field is a repeated message field.
     *
     * NOTE: Do not set the option in .proto files. Always use the maps syntax
     * instead. The option should only be implicitly set by the proto compiler
     * parser.
     */
    map_entry?: boolean;
    /**
     * Enable the legacy handling of JSON field name conflicts.  This lowercases
     * and strips underscored from the fields before comparison in proto3 only.
     * The new behavior takes `json_name` into account and applies to proto2 as
     * well.
     *
     * This should only be used as a temporary measure against broken builds due
     * to the change in behavior for JSON field name conflicts.
     *
     * TODO This is legacy behavior we plan to remove once downstream
     * teams have had time to migrate.
     *
     * @deprecated
     */
    deprecated_legacy_json_field_conflicts?: boolean;
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export interface FieldOptions {
    /**
     * NOTE: ctype is deprecated. Use `features.(pb.cpp).string_type` instead.
     * The ctype option instructs the C++ code generator to use a different
     * representation of the field than it normally would.  See the specific
     * options below.  This option is only implemented to support use of
     * [ctype=CORD] and [ctype=STRING] (the default) on non-repeated fields of
     * type "bytes" in the open source release.
     * TODO: make ctype actually deprecated.
     */
    ctype?: FieldOptionsCType;
    /**
     * The packed option can be enabled for repeated primitive fields to enable
     * a more efficient representation on the wire. Rather than repeatedly
     * writing the tag and type for each element, the entire array is encoded as
     * a single length-delimited blob. In proto3, only explicit setting it to
     * false will avoid using packed encoding.  This option is prohibited in
     * Editions, but the `repeated_field_encoding` feature can be used to control
     * the behavior.
     */
    packed?: boolean;
    /**
     * The jstype option determines the JavaScript type used for values of the
     * field.  The option is permitted only for 64 bit integral and fixed types
     * (int64, uint64, sint64, fixed64, sfixed64).  A field with jstype JS_STRING
     * is represented as JavaScript string, which avoids loss of precision that
     * can happen when a large value is converted to a floating point JavaScript.
     * Specifying JS_NUMBER for the jstype causes the generated JavaScript code to
     * use the JavaScript "number" type.  The behavior of the default option
     * JS_NORMAL is implementation dependent.
     *
     * This option is an enum to permit additional types to be added, e.g.
     * goog.math.Integer.
     */
    jstype?: FieldOptionsJSType;
    /**
     * Should this field be parsed lazily?  Lazy applies only to message-type
     * fields.  It means that when the outer message is initially parsed, the
     * inner message's contents will not be parsed but instead stored in encoded
     * form.  The inner message will actually be parsed when it is first accessed.
     *
     * This is only a hint.  Implementations are free to choose whether to use
     * eager or lazy parsing regardless of the value of this option.  However,
     * setting this option true suggests that the protocol author believes that
     * using lazy parsing on this field is worth the additional bookkeeping
     * overhead typically needed to implement it.
     *
     * This option does not affect the public interface of any generated code;
     * all method signatures remain the same.  Furthermore, thread-safety of the
     * interface is not affected by this option; const methods remain safe to
     * call from multiple threads concurrently, while non-const methods continue
     * to require exclusive access.
     *
     * Note that lazy message fields are still eagerly verified to check
     * ill-formed wireformat or missing required fields. Calling IsInitialized()
     * on the outer message would fail if the inner message has missing required
     * fields. Failed verification would result in parsing failure (except when
     * uninitialized messages are acceptable).
     */
    lazy?: boolean;
    /**
     * unverified_lazy does no correctness checks on the byte stream. This should
     * only be used where lazy with verification is prohibitive for performance
     * reasons.
     */
    unverified_lazy?: boolean;
    /**
     * Is this field deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for accessors, or it will be completely ignored; in the very least, this
     * is a formalization for deprecating fields.
     */
    deprecated?: boolean;
    /** For Google-internal migration only. Do not use. */
    weak?: boolean;
    /**
     * Indicate that the field value should not be printed out when using debug
     * formats, e.g. when the field contains sensitive credentials.
     */
    debug_redact?: boolean;
    retention?: FieldOptionsOptionRetention;
    targets: FieldOptionsOptionTargetType[];
    edition_defaults: FieldOptionsEditionDefault[];
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    feature_support?: FieldOptionsFeatureSupport;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export declare enum FieldOptionsCType {
    /** STRING - Default mode. */
    STRING = 0,
    /**
     * CORD - The option [ctype=CORD] may be applied to a non-repeated field of type
     * "bytes". It indicates that in C++, the data should be stored in a Cord
     * instead of a string.  For very large strings, this may reduce memory
     * fragmentation. It may also allow better performance when parsing from a
     * Cord, or when parsing with aliasing enabled, as the parsed Cord may then
     * alias the original buffer.
     */
    CORD = 1,
    STRING_PIECE = 2,
    UNRECOGNIZED = -1
}
export declare enum FieldOptionsJSType {
    /** JS_NORMAL - Use the default type. */
    JS_NORMAL = 0,
    /** JS_STRING - Use JavaScript strings. */
    JS_STRING = 1,
    /** JS_NUMBER - Use JavaScript numbers. */
    JS_NUMBER = 2,
    UNRECOGNIZED = -1
}
export declare enum FieldOptionsOptionRetention {
    RETENTION_UNKNOWN = 0,
    RETENTION_RUNTIME = 1,
    RETENTION_SOURCE = 2,
    UNRECOGNIZED = -1
}
export declare enum FieldOptionsOptionTargetType {
    TARGET_TYPE_UNKNOWN = 0,
    TARGET_TYPE_FILE = 1,
    TARGET_TYPE_EXTENSION_RANGE = 2,
    TARGET_TYPE_MESSAGE = 3,
    TARGET_TYPE_FIELD = 4,
    TARGET_TYPE_ONEOF = 5,
    TARGET_TYPE_ENUM = 6,
    TARGET_TYPE_ENUM_ENTRY = 7,
    TARGET_TYPE_SERVICE = 8,
    TARGET_TYPE_METHOD = 9,
    UNRECOGNIZED = -1
}
export interface FieldOptionsEditionDefault {
    edition?: Edition;
    /** Textproto value. */
    value?: string;
}
export interface FieldOptionsFeatureSupport {
    /**
     * The edition that this feature was first available in.  In editions
     * earlier than this one, the default assigned to EDITION_LEGACY will be
     * used, and proto files will not be able to override it.
     */
    edition_introduced?: Edition;
    /**
     * The edition this feature becomes deprecated in.  Using this after this
     * edition may trigger warnings.
     */
    edition_deprecated?: Edition;
    /**
     * The deprecation warning text if this feature is used after the edition it
     * was marked deprecated in.
     */
    deprecation_warning?: string;
    /**
     * The edition this feature is no longer available in.  In editions after
     * this one, the last default assigned will be used, and proto files will
     * not be able to override it.
     */
    edition_removed?: Edition;
}
export interface OneofOptions {
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export interface EnumOptions {
    /**
     * Set this option to true to allow mapping different tag names to the same
     * value.
     */
    allow_alias?: boolean;
    /**
     * Is this enum deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for the enum, or it will be completely ignored; in the very least, this
     * is a formalization for deprecating enums.
     */
    deprecated?: boolean;
    /**
     * Enable the legacy handling of JSON field name conflicts.  This lowercases
     * and strips underscored from the fields before comparison in proto3 only.
     * The new behavior takes `json_name` into account and applies to proto2 as
     * well.
     * TODO Remove this legacy behavior once downstream teams have
     * had time to migrate.
     *
     * @deprecated
     */
    deprecated_legacy_json_field_conflicts?: boolean;
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export interface EnumValueOptions {
    /**
     * Is this enum value deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for the enum value, or it will be completely ignored; in the very least,
     * this is a formalization for deprecating enum values.
     */
    deprecated?: boolean;
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /**
     * Indicate that fields annotated with this enum value should not be printed
     * out when using debug formats, e.g. when the field contains sensitive
     * credentials.
     */
    debug_redact?: boolean;
    /** Information about the support window of a feature value. */
    feature_support?: FieldOptionsFeatureSupport;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export interface ServiceOptions {
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /**
     * Is this service deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for the service, or it will be completely ignored; in the very least,
     * this is a formalization for deprecating services.
     */
    deprecated?: boolean;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export interface MethodOptions {
    /**
     * Is this method deprecated?
     * Depending on the target platform, this can emit Deprecated annotations
     * for the method, or it will be completely ignored; in the very least,
     * this is a formalization for deprecating methods.
     */
    deprecated?: boolean;
    idempotency_level?: MethodOptionsIdempotencyLevel;
    /**
     * Any features defined in the specific edition.
     * WARNING: This field should only be used by protobuf plugins or special
     * cases like the proto compiler. Other uses are discouraged and
     * developers should rely on the protoreflect APIs for their client language.
     */
    features?: FeatureSet;
    /** The parser stores options it doesn't recognize here. See above. */
    uninterpreted_option: UninterpretedOption[];
}
export declare enum MethodOptionsIdempotencyLevel {
    IDEMPOTENCY_UNKNOWN = 0,
    /** NO_SIDE_EFFECTS - implies idempotent */
    NO_SIDE_EFFECTS = 1,
    /** IDEMPOTENT - idempotent, but may have side effects */
    IDEMPOTENT = 2,
    UNRECOGNIZED = -1
}
export interface UninterpretedOption {
    name: UninterpretedOptionNamePart[];
    /**
     * The value of the uninterpreted option, in whatever type the tokenizer
     * identified it as during parsing. Exactly one of these should be set.
     */
    identifier_value?: string;
    positive_int_value?: number;
    negative_int_value?: number;
    double_value?: number;
    string_value?: Uint8Array;
    aggregate_value?: string;
}
export interface UninterpretedOptionNamePart {
    name_part: string;
    is_extension: boolean;
}
export interface FeatureSet {
    field_presence?: FeatureSetFieldPresence;
    enum_type?: FeatureSetEnumType;
    repeated_field_encoding?: FeatureSetRepeatedFieldEncoding;
    utf8_validation?: FeatureSetUtf8Validation;
    message_encoding?: FeatureSetMessageEncoding;
    json_format?: FeatureSetJsonFormat;
    enforce_naming_style?: FeatureSetEnforceNamingStyle;
}
export declare enum FeatureSetFieldPresence {
    FIELD_PRESENCE_UNKNOWN = 0,
    EXPLICIT = 1,
    IMPLICIT = 2,
    LEGACY_REQUIRED = 3,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetEnumType {
    ENUM_TYPE_UNKNOWN = 0,
    OPEN = 1,
    CLOSED = 2,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetRepeatedFieldEncoding {
    REPEATED_FIELD_ENCODING_UNKNOWN = 0,
    PACKED = 1,
    EXPANDED = 2,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetUtf8Validation {
    UTF8_VALIDATION_UNKNOWN = 0,
    VERIFY = 2,
    NONE = 3,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetMessageEncoding {
    MESSAGE_ENCODING_UNKNOWN = 0,
    LENGTH_PREFIXED = 1,
    DELIMITED = 2,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetJsonFormat {
    JSON_FORMAT_UNKNOWN = 0,
    ALLOW = 1,
    LEGACY_BEST_EFFORT = 2,
    UNRECOGNIZED = -1
}
export declare enum FeatureSetEnforceNamingStyle {
    ENFORCE_NAMING_STYLE_UNKNOWN = 0,
    STYLE2024 = 1,
    STYLE_LEGACY = 2,
    UNRECOGNIZED = -1
}
export interface FeatureSetDefaults {
    defaults: FeatureSetDefaultsFeatureSetEditionDefault[];
    /**
     * The minimum supported edition (inclusive) when this was constructed.
     * Editions before this will not have defaults.
     */
    minimum_edition?: Edition;
    /**
     * The maximum known edition (inclusive) when this was constructed. Editions
     * after this will not have reliable defaults.
     */
    maximum_edition?: Edition;
}
export interface FeatureSetDefaultsFeatureSetEditionDefault {
    edition?: Edition;
    /** Defaults of features that can be overridden in this edition. */
    overridable_features?: FeatureSet;
    /** Defaults of features that can't be overridden in this edition. */
    fixed_features?: FeatureSet;
}
export interface SourceCodeInfo {
    /**
     * A Location identifies a piece of source code in a .proto file which
     * corresponds to a particular definition.  This information is intended
     * to be useful to IDEs, code indexers, documentation generators, and similar
     * tools.
     *
     * For example, say we have a file like:
     *   message Foo {
     *     optional string foo = 1;
     *   }
     * Let's look at just the field definition:
     *   optional string foo = 1;
     *   ^       ^^     ^^  ^  ^^^
     *   a       bc     de  f  ghi
     * We have the following locations:
     *   span   path               represents
     *   [a,i)  [ 4, 0, 2, 0 ]     The whole field definition.
     *   [a,b)  [ 4, 0, 2, 0, 4 ]  The label (optional).
     *   [c,d)  [ 4, 0, 2, 0, 5 ]  The type (string).
     *   [e,f)  [ 4, 0, 2, 0, 1 ]  The name (foo).
     *   [g,h)  [ 4, 0, 2, 0, 3 ]  The number (1).
     *
     * Notes:
     * - A location may refer to a repeated field itself (i.e. not to any
     *   particular index within it).  This is used whenever a set of elements are
     *   logically enclosed in a single code segment.  For example, an entire
     *   extend block (possibly containing multiple extension definitions) will
     *   have an outer location whose path refers to the "extensions" repeated
     *   field without an index.
     * - Multiple locations may have the same path.  This happens when a single
     *   logical declaration is spread out across multiple places.  The most
     *   obvious example is the "extend" block again -- there may be multiple
     *   extend blocks in the same scope, each of which will have the same path.
     * - A location's span is not always a subset of its parent's span.  For
     *   example, the "extendee" of an extension declaration appears at the
     *   beginning of the "extend" block and is shared by all extensions within
     *   the block.
     * - Just because a location's span is a subset of some other location's span
     *   does not mean that it is a descendant.  For example, a "group" defines
     *   both a type and a field in a single declaration.  Thus, the locations
     *   corresponding to the type and field and their components will overlap.
     * - Code which tries to interpret locations should probably be designed to
     *   ignore those that it doesn't understand, as more types of locations could
     *   be recorded in the future.
     */
    location: SourceCodeInfoLocation[];
}
export interface SourceCodeInfoLocation {
    /**
     * Identifies which part of the FileDescriptorProto was defined at this
     * location.
     *
     * Each element is a field number or an index.  They form a path from
     * the root FileDescriptorProto to the place where the definition appears.
     * For example, this path:
     *   [ 4, 3, 2, 7, 1 ]
     * refers to:
     *   file.message_type(3)  // 4, 3
     *       .field(7)         // 2, 7
     *       .name()           // 1
     * This is because FileDescriptorProto.message_type has field number 4:
     *   repeated DescriptorProto message_type = 4;
     * and DescriptorProto.field has field number 2:
     *   repeated FieldDescriptorProto field = 2;
     * and FieldDescriptorProto.name has field number 1:
     *   optional string name = 1;
     *
     * Thus, the above path gives the location of a field name.  If we removed
     * the last element:
     *   [ 4, 3, 2, 7 ]
     * this path refers to the whole field declaration (from the beginning
     * of the label to the terminating semicolon).
     */
    path: number[];
    /**
     * Always has exactly three or four elements: start line, start column,
     * end line (optional, otherwise assumed same as start line), end column.
     * These are packed into a single field for efficiency.  Note that line
     * and column numbers are zero-based -- typically you will want to add
     * 1 to each before displaying to a user.
     */
    span: number[];
    /**
     * If this SourceCodeInfo represents a complete declaration, these are any
     * comments appearing before and after the declaration which appear to be
     * attached to the declaration.
     *
     * A series of line comments appearing on consecutive lines, with no other
     * tokens appearing on those lines, will be treated as a single comment.
     *
     * leading_detached_comments will keep paragraphs of comments that appear
     * before (but not connected to) the current element. Each paragraph,
     * separated by empty lines, will be one comment element in the repeated
     * field.
     *
     * Only the comment content is provided; comment markers (e.g. //) are
     * stripped out.  For block comments, leading whitespace and an asterisk
     * will be stripped from the beginning of each line other than the first.
     * Newlines are included in the output.
     *
     * Examples:
     *
     *   optional int32 foo = 1;  // Comment attached to foo.
     *   // Comment attached to bar.
     *   optional int32 bar = 2;
     *
     *   optional string baz = 3;
     *   // Comment attached to baz.
     *   // Another line attached to baz.
     *
     *   // Comment attached to moo.
     *   //
     *   // Another line attached to moo.
     *   optional double moo = 4;
     *
     *   // Detached comment for corge. This is not leading or trailing comments
     *   // to moo or corge because there are blank lines separating it from
     *   // both.
     *
     *   // Detached comment for corge paragraph 2.
     *
     *   optional string corge = 5;
     *   /* Block comment attached
     *    * to corge.  Leading asterisks
     *    * will be removed. * /
     *   /* Block comment attached to
     *    * grault. * /
     *   optional int32 grault = 6;
     *
     *   // ignored detached comments.
     */
    leading_comments?: string;
    trailing_comments?: string;
    leading_detached_comments: string[];
}
export interface GeneratedCodeInfo {
    /**
     * An Annotation connects some span of text in generated code to an element
     * of its generating .proto file.
     */
    annotation: GeneratedCodeInfoAnnotation[];
}
export interface GeneratedCodeInfoAnnotation {
    /**
     * Identifies the element in the original source .proto file. This field
     * is formatted the same as SourceCodeInfo.Location.path.
     */
    path: number[];
    /** Identifies the filesystem path to the original source .proto. */
    source_file?: string;
    /**
     * Identifies the starting offset in bytes in the generated code
     * that relates to the identified object.
     */
    begin?: number;
    /**
     * Identifies the ending offset in bytes in the generated code that
     * relates to the identified object. The end offset should be one past
     * the last relevant byte (so the length of the text = end - begin).
     */
    end?: number;
    semantic?: GeneratedCodeInfoAnnotationSemantic;
}
export declare enum GeneratedCodeInfoAnnotationSemantic {
    /** NONE - There is no effect or the effect is indescribable. */
    NONE = 0,
    /** SET - The element is set or otherwise mutated. */
    SET = 1,
    /** ALIAS - An alias to the element is returned. */
    ALIAS = 2,
    UNRECOGNIZED = -1
}
