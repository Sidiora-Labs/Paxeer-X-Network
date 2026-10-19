export function fetchReq(path, init) {
    const { pathPrefix, ...req } = init || {};
    const url = pathPrefix ? `${pathPrefix}${path}` : path;
    return fetch(url, req).then((r) => r.json().then((body) => {
        if (!r.ok) {
            throw body;
        }
        return body;
    }));
}
function isPlainObject(value) {
    const isObject = Object.prototype.toString.call(value).slice(8, -1) === "Object";
    const isObjLike = value !== null && isObject;
    if (!isObjLike || !isObject) {
        return false;
    }
    const proto = Object.getPrototypeOf(value);
    return typeof proto === "object" && proto.constructor === Object.prototype.constructor;
}
function isPrimitive(value) {
    return typeof value === "string" || typeof value === "number" || typeof value === "boolean";
}
function isZeroValuePrimitive(value) {
    return value === false || value === 0 || value === "";
}
const flattenRequestPayload = (requestPayload, path = "") => {
    return Object.keys(requestPayload).reduce((acc, key) => {
        const value = requestPayload[key];
        const newPath = path ? [path, key].join(".") : key;
        const isNonEmptyPrimitiveArray = Array.isArray(value) && value.every((v) => isPrimitive(v)) && value.length > 0;
        const isNonZeroValuePrimitive = isPrimitive(value) && !isZeroValuePrimitive(value);
        if (isPlainObject(value)) {
            // Recursively flatten objects
            const nested = flattenRequestPayload(value, newPath);
            Object.assign(acc, nested); // Merge nested results into accumulator
        }
        else if (isNonZeroValuePrimitive || isNonEmptyPrimitiveArray) {
            // Add non-zero primitives or non-empty primitive arrays
            acc[newPath] = value;
        }
        return acc;
    }, {});
};
export function renderURLSearchParams(requestPayload, urlPathParams = []) {
    const flattenedRequestPayload = flattenRequestPayload(requestPayload);
    const urlSearchParams = Object.keys(flattenedRequestPayload).reduce((acc, key) => {
        // key should not be present in the url path as a parameter
        const value = flattenedRequestPayload[key];
        if (!urlPathParams.includes(key)) {
            if (Array.isArray(value)) {
                for (const v of value) {
                    acc.push([key, v.toString()]);
                }
            }
            else {
                acc.push([key, value.toString()]);
            }
        }
        return acc;
    }, []);
    return new URLSearchParams(urlSearchParams).toString();
}
