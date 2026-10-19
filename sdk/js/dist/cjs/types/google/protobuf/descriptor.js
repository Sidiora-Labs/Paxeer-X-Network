"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.GeneratedCodeInfoAnnotationSemantic = exports.FeatureSetEnforceNamingStyle = exports.FeatureSetJsonFormat = exports.FeatureSetMessageEncoding = exports.FeatureSetUtf8Validation = exports.FeatureSetRepeatedFieldEncoding = exports.FeatureSetEnumType = exports.FeatureSetFieldPresence = exports.MethodOptionsIdempotencyLevel = exports.FieldOptionsOptionTargetType = exports.FieldOptionsOptionRetention = exports.FieldOptionsJSType = exports.FieldOptionsCType = exports.FileOptionsOptimizeMode = exports.FieldDescriptorProtoLabel = exports.FieldDescriptorProtoType = exports.ExtensionRangeOptionsVerificationState = exports.Edition = void 0;
var Edition;
(function (Edition) {
    /** EDITION_UNKNOWN - A placeholder for an unknown edition value. */
    Edition[Edition["EDITION_UNKNOWN"] = 0] = "EDITION_UNKNOWN";
    /**
     * EDITION_LEGACY - A placeholder edition for specifying default behaviors *before* a feature
     * was first introduced.  This is effectively an "infinite past".
     */
    Edition[Edition["EDITION_LEGACY"] = 900] = "EDITION_LEGACY";
    /**
     * EDITION_PROTO2 - Legacy syntax "editions".  These pre-date editions, but behave much like
     * distinct editions.  These can't be used to specify the edition of proto
     * files, but feature definitions must supply proto2/proto3 defaults for
     * backwards compatibility.
     */
    Edition[Edition["EDITION_PROTO2"] = 998] = "EDITION_PROTO2";
    Edition[Edition["EDITION_PROTO3"] = 999] = "EDITION_PROTO3";
    /**
     * EDITION_2023 - Editions that have been released.  The specific values are arbitrary and
     * should not be depended on, but they will always be time-ordered for easy
     * comparison.
     */
    Edition[Edition["EDITION_2023"] = 1000] = "EDITION_2023";
    Edition[Edition["EDITION_2024"] = 1001] = "EDITION_2024";
    /**
     * EDITION_1_TEST_ONLY - Placeholder editions for testing feature resolution.  These should not be
     * used or relied on outside of tests.
     */
    Edition[Edition["EDITION_1_TEST_ONLY"] = 1] = "EDITION_1_TEST_ONLY";
    Edition[Edition["EDITION_2_TEST_ONLY"] = 2] = "EDITION_2_TEST_ONLY";
    Edition[Edition["EDITION_99997_TEST_ONLY"] = 99997] = "EDITION_99997_TEST_ONLY";
    Edition[Edition["EDITION_99998_TEST_ONLY"] = 99998] = "EDITION_99998_TEST_ONLY";
    Edition[Edition["EDITION_99999_TEST_ONLY"] = 99999] = "EDITION_99999_TEST_ONLY";
    /**
     * EDITION_MAX - Placeholder for specifying unbounded edition support.  This should only
     * ever be used by plugins that can expect to never require any changes to
     * support a new edition.
     */
    Edition[Edition["EDITION_MAX"] = 2147483647] = "EDITION_MAX";
    Edition[Edition["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(Edition || (exports.Edition = Edition = {}));
var ExtensionRangeOptionsVerificationState;
(function (ExtensionRangeOptionsVerificationState) {
    /** DECLARATION - All the extensions of the range must be declared. */
    ExtensionRangeOptionsVerificationState[ExtensionRangeOptionsVerificationState["DECLARATION"] = 0] = "DECLARATION";
    ExtensionRangeOptionsVerificationState[ExtensionRangeOptionsVerificationState["UNVERIFIED"] = 1] = "UNVERIFIED";
    ExtensionRangeOptionsVerificationState[ExtensionRangeOptionsVerificationState["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ExtensionRangeOptionsVerificationState || (exports.ExtensionRangeOptionsVerificationState = ExtensionRangeOptionsVerificationState = {}));
var FieldDescriptorProtoType;
(function (FieldDescriptorProtoType) {
    /**
     * TYPE_DOUBLE - 0 is reserved for errors.
     * Order is weird for historical reasons.
     */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_DOUBLE"] = 1] = "TYPE_DOUBLE";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_FLOAT"] = 2] = "TYPE_FLOAT";
    /**
     * TYPE_INT64 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT64 if
     * negative values are likely.
     */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_INT64"] = 3] = "TYPE_INT64";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_UINT64"] = 4] = "TYPE_UINT64";
    /**
     * TYPE_INT32 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT32 if
     * negative values are likely.
     */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_INT32"] = 5] = "TYPE_INT32";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_FIXED64"] = 6] = "TYPE_FIXED64";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_FIXED32"] = 7] = "TYPE_FIXED32";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_BOOL"] = 8] = "TYPE_BOOL";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_STRING"] = 9] = "TYPE_STRING";
    /**
     * TYPE_GROUP - Tag-delimited aggregate.
     * Group type is deprecated and not supported after google.protobuf. However, Proto3
     * implementations should still be able to parse the group wire format and
     * treat group fields as unknown fields.  In Editions, the group wire format
     * can be enabled via the `message_encoding` feature.
     */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_GROUP"] = 10] = "TYPE_GROUP";
    /** TYPE_MESSAGE - Length-delimited aggregate. */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_MESSAGE"] = 11] = "TYPE_MESSAGE";
    /** TYPE_BYTES - New in version 2. */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_BYTES"] = 12] = "TYPE_BYTES";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_UINT32"] = 13] = "TYPE_UINT32";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_ENUM"] = 14] = "TYPE_ENUM";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_SFIXED32"] = 15] = "TYPE_SFIXED32";
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_SFIXED64"] = 16] = "TYPE_SFIXED64";
    /** TYPE_SINT32 - Uses ZigZag encoding. */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_SINT32"] = 17] = "TYPE_SINT32";
    /** TYPE_SINT64 - Uses ZigZag encoding. */
    FieldDescriptorProtoType[FieldDescriptorProtoType["TYPE_SINT64"] = 18] = "TYPE_SINT64";
    FieldDescriptorProtoType[FieldDescriptorProtoType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldDescriptorProtoType || (exports.FieldDescriptorProtoType = FieldDescriptorProtoType = {}));
var FieldDescriptorProtoLabel;
(function (FieldDescriptorProtoLabel) {
    /** LABEL_OPTIONAL - 0 is reserved for errors */
    FieldDescriptorProtoLabel[FieldDescriptorProtoLabel["LABEL_OPTIONAL"] = 1] = "LABEL_OPTIONAL";
    FieldDescriptorProtoLabel[FieldDescriptorProtoLabel["LABEL_REPEATED"] = 3] = "LABEL_REPEATED";
    /**
     * LABEL_REQUIRED - The required label is only allowed in google.protobuf.  In proto3 and Editions
     * it's explicitly prohibited.  In Editions, the `field_presence` feature
     * can be used to get this behavior.
     */
    FieldDescriptorProtoLabel[FieldDescriptorProtoLabel["LABEL_REQUIRED"] = 2] = "LABEL_REQUIRED";
    FieldDescriptorProtoLabel[FieldDescriptorProtoLabel["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldDescriptorProtoLabel || (exports.FieldDescriptorProtoLabel = FieldDescriptorProtoLabel = {}));
var FileOptionsOptimizeMode;
(function (FileOptionsOptimizeMode) {
    /** SPEED - Generate complete code for parsing, serialization, */
    FileOptionsOptimizeMode[FileOptionsOptimizeMode["SPEED"] = 1] = "SPEED";
    /** CODE_SIZE - etc. */
    FileOptionsOptimizeMode[FileOptionsOptimizeMode["CODE_SIZE"] = 2] = "CODE_SIZE";
    /** LITE_RUNTIME - Generate code using MessageLite and the lite runtime. */
    FileOptionsOptimizeMode[FileOptionsOptimizeMode["LITE_RUNTIME"] = 3] = "LITE_RUNTIME";
    FileOptionsOptimizeMode[FileOptionsOptimizeMode["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FileOptionsOptimizeMode || (exports.FileOptionsOptimizeMode = FileOptionsOptimizeMode = {}));
var FieldOptionsCType;
(function (FieldOptionsCType) {
    /** STRING - Default mode. */
    FieldOptionsCType[FieldOptionsCType["STRING"] = 0] = "STRING";
    /**
     * CORD - The option [ctype=CORD] may be applied to a non-repeated field of type
     * "bytes". It indicates that in C++, the data should be stored in a Cord
     * instead of a string.  For very large strings, this may reduce memory
     * fragmentation. It may also allow better performance when parsing from a
     * Cord, or when parsing with aliasing enabled, as the parsed Cord may then
     * alias the original buffer.
     */
    FieldOptionsCType[FieldOptionsCType["CORD"] = 1] = "CORD";
    FieldOptionsCType[FieldOptionsCType["STRING_PIECE"] = 2] = "STRING_PIECE";
    FieldOptionsCType[FieldOptionsCType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptionsCType || (exports.FieldOptionsCType = FieldOptionsCType = {}));
var FieldOptionsJSType;
(function (FieldOptionsJSType) {
    /** JS_NORMAL - Use the default type. */
    FieldOptionsJSType[FieldOptionsJSType["JS_NORMAL"] = 0] = "JS_NORMAL";
    /** JS_STRING - Use JavaScript strings. */
    FieldOptionsJSType[FieldOptionsJSType["JS_STRING"] = 1] = "JS_STRING";
    /** JS_NUMBER - Use JavaScript numbers. */
    FieldOptionsJSType[FieldOptionsJSType["JS_NUMBER"] = 2] = "JS_NUMBER";
    FieldOptionsJSType[FieldOptionsJSType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptionsJSType || (exports.FieldOptionsJSType = FieldOptionsJSType = {}));
var FieldOptionsOptionRetention;
(function (FieldOptionsOptionRetention) {
    FieldOptionsOptionRetention[FieldOptionsOptionRetention["RETENTION_UNKNOWN"] = 0] = "RETENTION_UNKNOWN";
    FieldOptionsOptionRetention[FieldOptionsOptionRetention["RETENTION_RUNTIME"] = 1] = "RETENTION_RUNTIME";
    FieldOptionsOptionRetention[FieldOptionsOptionRetention["RETENTION_SOURCE"] = 2] = "RETENTION_SOURCE";
    FieldOptionsOptionRetention[FieldOptionsOptionRetention["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptionsOptionRetention || (exports.FieldOptionsOptionRetention = FieldOptionsOptionRetention = {}));
var FieldOptionsOptionTargetType;
(function (FieldOptionsOptionTargetType) {
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_UNKNOWN"] = 0] = "TARGET_TYPE_UNKNOWN";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_FILE"] = 1] = "TARGET_TYPE_FILE";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_EXTENSION_RANGE"] = 2] = "TARGET_TYPE_EXTENSION_RANGE";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_MESSAGE"] = 3] = "TARGET_TYPE_MESSAGE";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_FIELD"] = 4] = "TARGET_TYPE_FIELD";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_ONEOF"] = 5] = "TARGET_TYPE_ONEOF";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_ENUM"] = 6] = "TARGET_TYPE_ENUM";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_ENUM_ENTRY"] = 7] = "TARGET_TYPE_ENUM_ENTRY";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_SERVICE"] = 8] = "TARGET_TYPE_SERVICE";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["TARGET_TYPE_METHOD"] = 9] = "TARGET_TYPE_METHOD";
    FieldOptionsOptionTargetType[FieldOptionsOptionTargetType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptionsOptionTargetType || (exports.FieldOptionsOptionTargetType = FieldOptionsOptionTargetType = {}));
var MethodOptionsIdempotencyLevel;
(function (MethodOptionsIdempotencyLevel) {
    MethodOptionsIdempotencyLevel[MethodOptionsIdempotencyLevel["IDEMPOTENCY_UNKNOWN"] = 0] = "IDEMPOTENCY_UNKNOWN";
    /** NO_SIDE_EFFECTS - implies idempotent */
    MethodOptionsIdempotencyLevel[MethodOptionsIdempotencyLevel["NO_SIDE_EFFECTS"] = 1] = "NO_SIDE_EFFECTS";
    /** IDEMPOTENT - idempotent, but may have side effects */
    MethodOptionsIdempotencyLevel[MethodOptionsIdempotencyLevel["IDEMPOTENT"] = 2] = "IDEMPOTENT";
    MethodOptionsIdempotencyLevel[MethodOptionsIdempotencyLevel["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(MethodOptionsIdempotencyLevel || (exports.MethodOptionsIdempotencyLevel = MethodOptionsIdempotencyLevel = {}));
var FeatureSetFieldPresence;
(function (FeatureSetFieldPresence) {
    FeatureSetFieldPresence[FeatureSetFieldPresence["FIELD_PRESENCE_UNKNOWN"] = 0] = "FIELD_PRESENCE_UNKNOWN";
    FeatureSetFieldPresence[FeatureSetFieldPresence["EXPLICIT"] = 1] = "EXPLICIT";
    FeatureSetFieldPresence[FeatureSetFieldPresence["IMPLICIT"] = 2] = "IMPLICIT";
    FeatureSetFieldPresence[FeatureSetFieldPresence["LEGACY_REQUIRED"] = 3] = "LEGACY_REQUIRED";
    FeatureSetFieldPresence[FeatureSetFieldPresence["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetFieldPresence || (exports.FeatureSetFieldPresence = FeatureSetFieldPresence = {}));
var FeatureSetEnumType;
(function (FeatureSetEnumType) {
    FeatureSetEnumType[FeatureSetEnumType["ENUM_TYPE_UNKNOWN"] = 0] = "ENUM_TYPE_UNKNOWN";
    FeatureSetEnumType[FeatureSetEnumType["OPEN"] = 1] = "OPEN";
    FeatureSetEnumType[FeatureSetEnumType["CLOSED"] = 2] = "CLOSED";
    FeatureSetEnumType[FeatureSetEnumType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetEnumType || (exports.FeatureSetEnumType = FeatureSetEnumType = {}));
var FeatureSetRepeatedFieldEncoding;
(function (FeatureSetRepeatedFieldEncoding) {
    FeatureSetRepeatedFieldEncoding[FeatureSetRepeatedFieldEncoding["REPEATED_FIELD_ENCODING_UNKNOWN"] = 0] = "REPEATED_FIELD_ENCODING_UNKNOWN";
    FeatureSetRepeatedFieldEncoding[FeatureSetRepeatedFieldEncoding["PACKED"] = 1] = "PACKED";
    FeatureSetRepeatedFieldEncoding[FeatureSetRepeatedFieldEncoding["EXPANDED"] = 2] = "EXPANDED";
    FeatureSetRepeatedFieldEncoding[FeatureSetRepeatedFieldEncoding["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetRepeatedFieldEncoding || (exports.FeatureSetRepeatedFieldEncoding = FeatureSetRepeatedFieldEncoding = {}));
var FeatureSetUtf8Validation;
(function (FeatureSetUtf8Validation) {
    FeatureSetUtf8Validation[FeatureSetUtf8Validation["UTF8_VALIDATION_UNKNOWN"] = 0] = "UTF8_VALIDATION_UNKNOWN";
    FeatureSetUtf8Validation[FeatureSetUtf8Validation["VERIFY"] = 2] = "VERIFY";
    FeatureSetUtf8Validation[FeatureSetUtf8Validation["NONE"] = 3] = "NONE";
    FeatureSetUtf8Validation[FeatureSetUtf8Validation["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetUtf8Validation || (exports.FeatureSetUtf8Validation = FeatureSetUtf8Validation = {}));
var FeatureSetMessageEncoding;
(function (FeatureSetMessageEncoding) {
    FeatureSetMessageEncoding[FeatureSetMessageEncoding["MESSAGE_ENCODING_UNKNOWN"] = 0] = "MESSAGE_ENCODING_UNKNOWN";
    FeatureSetMessageEncoding[FeatureSetMessageEncoding["LENGTH_PREFIXED"] = 1] = "LENGTH_PREFIXED";
    FeatureSetMessageEncoding[FeatureSetMessageEncoding["DELIMITED"] = 2] = "DELIMITED";
    FeatureSetMessageEncoding[FeatureSetMessageEncoding["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetMessageEncoding || (exports.FeatureSetMessageEncoding = FeatureSetMessageEncoding = {}));
var FeatureSetJsonFormat;
(function (FeatureSetJsonFormat) {
    FeatureSetJsonFormat[FeatureSetJsonFormat["JSON_FORMAT_UNKNOWN"] = 0] = "JSON_FORMAT_UNKNOWN";
    FeatureSetJsonFormat[FeatureSetJsonFormat["ALLOW"] = 1] = "ALLOW";
    FeatureSetJsonFormat[FeatureSetJsonFormat["LEGACY_BEST_EFFORT"] = 2] = "LEGACY_BEST_EFFORT";
    FeatureSetJsonFormat[FeatureSetJsonFormat["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetJsonFormat || (exports.FeatureSetJsonFormat = FeatureSetJsonFormat = {}));
var FeatureSetEnforceNamingStyle;
(function (FeatureSetEnforceNamingStyle) {
    FeatureSetEnforceNamingStyle[FeatureSetEnforceNamingStyle["ENFORCE_NAMING_STYLE_UNKNOWN"] = 0] = "ENFORCE_NAMING_STYLE_UNKNOWN";
    FeatureSetEnforceNamingStyle[FeatureSetEnforceNamingStyle["STYLE2024"] = 1] = "STYLE2024";
    FeatureSetEnforceNamingStyle[FeatureSetEnforceNamingStyle["STYLE_LEGACY"] = 2] = "STYLE_LEGACY";
    FeatureSetEnforceNamingStyle[FeatureSetEnforceNamingStyle["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FeatureSetEnforceNamingStyle || (exports.FeatureSetEnforceNamingStyle = FeatureSetEnforceNamingStyle = {}));
var GeneratedCodeInfoAnnotationSemantic;
(function (GeneratedCodeInfoAnnotationSemantic) {
    /** NONE - There is no effect or the effect is indescribable. */
    GeneratedCodeInfoAnnotationSemantic[GeneratedCodeInfoAnnotationSemantic["NONE"] = 0] = "NONE";
    /** SET - The element is set or otherwise mutated. */
    GeneratedCodeInfoAnnotationSemantic[GeneratedCodeInfoAnnotationSemantic["SET"] = 1] = "SET";
    /** ALIAS - An alias to the element is returned. */
    GeneratedCodeInfoAnnotationSemantic[GeneratedCodeInfoAnnotationSemantic["ALIAS"] = 2] = "ALIAS";
    GeneratedCodeInfoAnnotationSemantic[GeneratedCodeInfoAnnotationSemantic["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(GeneratedCodeInfoAnnotationSemantic || (exports.GeneratedCodeInfoAnnotationSemantic = GeneratedCodeInfoAnnotationSemantic = {}));
