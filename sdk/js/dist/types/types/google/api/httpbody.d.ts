import type { Any } from "../protobuf/any";
export interface HttpBody {
    /** The HTTP Content-Type header value specifying the content type of the body. */
    content_type: string;
    /** The HTTP request/response body as raw binary. */
    data: Uint8Array;
    /**
     * Application specific response metadata. Must be set in the first response
     * for streaming APIs.
     */
    extensions: Any[];
}
