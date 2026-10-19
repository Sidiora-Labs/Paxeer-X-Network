export interface Http {
    /**
     * A list of HTTP configuration rules that apply to individual API methods.
     *
     * **NOTE:** All service configuration rules follow "last one wins" order.
     */
    rules: HttpRule[];
    /**
     * When set to true, URL path parmeters will be fully URI-decoded except in
     * cases of single segment matches in reserved expansion, where "%2F" will be
     * left encoded.
     *
     * The default behavior is to not decode RFC 6570 reserved characters in multi
     * segment matches.
     */
    fully_decode_reserved_expansion: boolean;
}
export interface HttpRule {
    /**
     * Selects methods to which this rule applies.
     *
     * Refer to [selector][google.api.DocumentationRule.selector] for syntax details.
     */
    selector: string;
    /** Used for listing and getting information about resources. */
    get?: string;
    /** Used for updating a resource. */
    put?: string;
    /** Used for creating a resource. */
    post?: string;
    /** Used for deleting a resource. */
    delete?: string;
    /** Used for updating a resource. */
    patch?: string;
    /**
     * The custom pattern is used for specifying an HTTP method that is not
     * included in the `pattern` field, such as HEAD, or "*" to leave the
     * HTTP method unspecified for this rule. The wild-card rule is useful
     * for services that provide content to Web (HTML) clients.
     */
    custom?: CustomHttpPattern;
    /**
     * The name of the request field whose value is mapped to the HTTP body, or
     * `*` for mapping all fields not captured by the path pattern to the HTTP
     * body. NOTE: the referred field must not be a repeated field and must be
     * present at the top-level of request message type.
     */
    body: string;
    /**
     * Optional. The name of the response field whose value is mapped to the HTTP
     * body of response. Other response fields are ignored. When
     * not set, the response message will be used as HTTP body of response.
     */
    response_body: string;
    /**
     * Additional HTTP bindings for the selector. Nested bindings must
     * not contain an `additional_bindings` field themselves (that is,
     * the nesting may only be one level deep).
     */
    additional_bindings: HttpRule[];
}
export interface CustomHttpPattern {
    /** The name of this custom HTTP verb. */
    kind: string;
    /** The path matched by this custom verb. */
    path: string;
}
