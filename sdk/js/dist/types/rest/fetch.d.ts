export interface InitReq extends RequestInit {
    pathPrefix?: string;
}
export declare function fetchReq<_I, O>(path: string, init?: InitReq): Promise<O>;
export declare function renderURLSearchParams(requestPayload: object, urlPathParams?: string[]): string;
