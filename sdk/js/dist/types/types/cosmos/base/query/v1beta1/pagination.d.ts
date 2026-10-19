export interface PageRequest {
    /**
     * key is a value returned in PageResponse.next_key to begin
     * querying the next page most efficiently. Only one of offset or key
     * should be set.
     */
    key: Uint8Array;
    /**
     * offset is a numeric offset that can be used when key is unavailable.
     * It is less efficient than using key. Only one of offset or key should
     * be set.
     */
    offset: number;
    /**
     * limit is the total number of results to be returned in the result page.
     * If left empty it will default to a value to be set by each app.
     */
    limit: number;
    /**
     * count_total is set to true  to indicate that the result set should include
     * a count of the total number of items available for pagination in UIs.
     * count_total is only respected when offset is used. It is ignored when key
     * is set.
     */
    count_total: boolean;
    /**
     * reverse is set to true if results are to be returned in the descending order.
     *
     * Since: cosmos-sdk 0.43
     */
    reverse: boolean;
}
export interface PageResponse {
    /**
     * next_key is the key to be passed to PageRequest.key to
     * query the next page most efficiently
     */
    next_key: Uint8Array;
    /**
     * total is total number of results available if PageRequest.count_total
     * was set, its value is undefined otherwise
     */
    total: number;
}
