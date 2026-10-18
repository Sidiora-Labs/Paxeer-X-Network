// API response interface for consistent response structure
export interface ApiResponse<T = unknown> {
  success: boolean;
  data: T;
  message?: string;
  code?: string | number;
}

// Query parameters type - flexible to accept any parameter object
export type QueryParams = Record<string, any>;

// Request interceptor function type
export type RequestInterceptor = (
  config: SimpleRequestConfig,
) => SimpleRequestConfig | Promise<SimpleRequestConfig>;

// Response interceptor function type
export type ResponseInterceptor<T = unknown> = (
  response: Response,
  data: unknown,
) => T | Promise<T>;

// Error class for API errors
export class VaultsApiError extends Error {
  constructor(
    message: string,
    public readonly code?: string | number,
    public readonly status?: number,
    public readonly response?: Response,
  ) {
    super(message);
    this.name = "VaultsApiError";
  }
}

// Utility functions for query parameter handling
function buildQueryString(params: QueryParams): string {
  const filteredParams = filterUndefinedParams(params);
  const searchParams = new URLSearchParams();

  Object.entries(filteredParams).forEach(([key, value]) => {
    if (value !== null && value !== undefined) {
      searchParams.append(key, String(value));
    }
  });

  return searchParams.toString();
}

function filterUndefinedParams(
  params: QueryParams,
): Record<string, string | number | boolean> {
  const filtered: Record<string, string | number | boolean> = {};

  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null) {
      // Handle different value types
      if (
        typeof value === "string" ||
        typeof value === "number" ||
        typeof value === "boolean"
      ) {
        filtered[key] = value;
      } else if (typeof value === "object" && value !== null) {
        // For objects, flatten their properties into the query string
        const objEntries = Object.entries(value as Record<string, unknown>);
        objEntries.forEach(([objKey, objValue]) => {
          if (
            objValue !== undefined &&
            objValue !== null &&
            (typeof objValue === "string" ||
              typeof objValue === "number" ||
              typeof objValue === "boolean")
          ) {
            filtered[objKey] = objValue;
          }
        });
      }
    }
  });

  return filtered;
}

function appendQueryParams(url: string, params?: QueryParams): string {
  if (!params || Object.keys(params).length === 0) {
    return url;
  }

  const queryString = buildQueryString(params);
  if (!queryString) {
    return url;
  }

  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}${queryString}`;
}

// Simplified config interface for all requests
export interface SimpleRequestConfig extends Omit<RequestInit, "method"> {
  baseURL?: string;
  params?: QueryParams;
  timeout?: number;
  retry?: number;
  retryDelay?: number;
  validateStatus?: (status: number) => boolean;
  data?: unknown;
  method?: string; // Allow method to be set internally
}

// Request client class for better organization and configurability
class RequestClient {
  private requestInterceptors: RequestInterceptor[] = [];
  private responseInterceptors: ResponseInterceptor[] = [];
  private defaultConfig: SimpleRequestConfig = {
    timeout: 10000,
    retry: 3,
    retryDelay: 1000,
    validateStatus: (status) => status >= 200 && status < 300,
    // mode: 'cors',
    // credentials: 'include',
    headers: {
      "Content-Type": "application/json",
    },
  };

  // Add request interceptor
  addRequestInterceptor(interceptor: RequestInterceptor): void {
    this.requestInterceptors.push(interceptor);
  }

  // Add response interceptor
  addResponseInterceptor(interceptor: ResponseInterceptor): void {
    this.responseInterceptors.push(interceptor);
  }

  // Create timeout controller
  private createTimeoutController(timeout: number): AbortController {
    const controller = new AbortController();
    setTimeout(() => controller.abort(), timeout);
    return controller;
  }

  // Merge headers
  private mergeHeaders(
    defaultHeaders: HeadersInit = {},
    customHeaders: HeadersInit = {},
  ): HeadersInit {
    return {
      ...defaultHeaders,
      ...customHeaders,
    };
  }

  // Sleep utility for retry delay
  private sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  // Main request method
  async request<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    // Merge configurations
    const finalConfig: SimpleRequestConfig = {
      ...this.defaultConfig,
      ...config,
      headers: this.mergeHeaders(this.defaultConfig.headers, config.headers),
    };

    // Apply request interceptors
    let processedConfig = finalConfig;
    for (const interceptor of this.requestInterceptors) {
      processedConfig = await interceptor(processedConfig);
    }

    // Construct full URL if baseURL is provided
    let fullUrl = url;
    if (
      typeof url === "string" &&
      !url.startsWith("http") &&
      processedConfig.baseURL
    ) {
      fullUrl = `${processedConfig.baseURL}${url.startsWith("/") ? "" : "/"}${url}`;
    }

    // Handle query parameters for GET requests or when params are provided
    if (processedConfig.params) {
      fullUrl =
        typeof fullUrl === "string"
          ? appendQueryParams(fullUrl, processedConfig.params)
          : fullUrl;
    }

    // Setup timeout if specified
    let timeoutController: AbortController | undefined;
    if (processedConfig.timeout && processedConfig.timeout > 0) {
      timeoutController = this.createTimeoutController(processedConfig.timeout);
      processedConfig.signal = timeoutController.signal;
    }

    // Extract custom config options and pass only standard RequestInit options to fetch

    const {
      retry = 0,
      retryDelay = 1000,
      validateStatus,
      params,
      baseURL,
      timeout,
      data,
      method,
      ...fetchOptions
    } = processedConfig;

    // Prepare final fetch options
    const finalFetchOptions: RequestInit = {
      ...fetchOptions,
      method,
    };

    // Handle data for POST/PUT/PATCH requests
    if (data !== undefined) {
      finalFetchOptions.body =
        typeof data === "string" ? data : JSON.stringify(data);
    }

    let lastError: Error;

    // Retry logic
    for (let attempt = 0; attempt <= retry; attempt++) {
      try {
        const response = await fetch(fullUrl, finalFetchOptions);

        // Check if status is valid
        if (!validateStatus!(response.status)) {
          let errorMessage = response.statusText;
          let errorCode: string | number | undefined;

          try {
            const errorData = await response.json();
            errorMessage = errorData.message || errorData.code || errorMessage;
            errorCode = errorData.code;
          } catch {
            // If can't parse JSON, use status text
          }

          throw new VaultsApiError(
            errorMessage,
            errorCode,
            response.status,
            response,
          );
        }

        // Parse JSON response
        let responseData: unknown;
        try {
          responseData = await response.json();
        } catch {
          throw new VaultsApiError(
            "Invalid JSON response",
            "PARSE_ERROR",
            response.status,
            response,
          );
        }

        // Apply response interceptors
        let processedData = responseData;
        for (const interceptor of this.responseInterceptors) {
          processedData = await interceptor(response, processedData);
        }

        // Handle API response format
        if (typeof processedData === "object" && processedData !== null) {
          if ("success" in processedData) {
            const apiResponse = processedData as ApiResponse<T>;
            if (apiResponse.success) {
              return apiResponse.data;
            } else {
              throw new VaultsApiError(
                apiResponse.message || "API request failed",
                apiResponse.code,
                response.status,
                response,
              );
            }
          }
        }

        // Return processed data directly if no success field
        return processedData as T;
      } catch (error) {
        lastError = error instanceof Error ? error : new Error(String(error));

        // Don't retry on certain errors
        if (
          error instanceof VaultsApiError ||
          (error as Error)?.name === "AbortError" ||
          attempt === retry
        ) {
          throw lastError;
        }

        // Wait before retry
        if (attempt < retry) {
          await this.sleep(retryDelay);
        }
      } finally {
        // Clean up timeout controller
        timeoutController?.abort();
      }
    }

    throw lastError!;
  }

  // Simplified HTTP methods - all accept a single config object
  async get<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    return this.request<T>(url, { ...config, method: "GET" });
  }

  async post<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    return this.request<T>(url, { ...config, method: "POST" });
  }

  async put<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    return this.request<T>(url, { ...config, method: "PUT" });
  }

  async delete<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    return this.request<T>(url, { ...config, method: "DELETE" });
  }

  async patch<T>(
    url: RequestInfo | URL,
    config: SimpleRequestConfig = {},
  ): Promise<T> {
    return this.request<T>(url, { ...config, method: "PATCH" });
  }
}

// Create default instance
const defaultClient = new RequestClient();

// Export the default request function
export async function request<T>(
  url: RequestInfo | URL,
  config?: SimpleRequestConfig,
): Promise<T> {
  return defaultClient.request<T>(url, config);
}

// Export the client class for advanced usage
export { RequestClient };

// Export default client instance
export default defaultClient;
