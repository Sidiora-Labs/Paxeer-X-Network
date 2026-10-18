# Vaults Package

## Request API

This package provides a robust and feature-rich HTTP client for making API requests. The optimized request module includes advanced features like retry logic, timeout handling, request/response interceptors, and comprehensive error handling.

### Features

- 🔄 **Automatic Retry Logic**: Configurable retry attempts with exponential backoff
- ⏱️ **Timeout Support**: Request timeout with automatic cancellation
- 🔧 **Request/Response Interceptors**: Middleware for request and response processing
- 🎯 **Type Safety**: Full TypeScript support with generic types
- 🚨 **Enhanced Error Handling**: Structured error responses with codes and status
- 📦 **Multiple HTTP Methods**: GET, POST, PUT, DELETE, PATCH convenience methods
- 🔗 **Base URL Support**: Automatic URL construction with base URL
- 📊 **Response Validation**: Configurable status code validation
- 🔍 **Smart Query Parameters**: Automatic query string building with undefined value filtering

### Basic Usage

All methods now use a **single configuration object** for maximum simplicity and consistency:

```typescript
import { request, RequestClient } from "./api/request";
import client from "./api/request";

// Simple GET request
const user = await client.get<User>("/api/users/1");

// GET with query parameters
const users = await client.get<User[]>("/api/users", {
  params: { page: 1, limit: 10, status: "active" },
});

// POST with data
const newUser = await client.post<User>("/api/users", {
  data: { name: "John Doe", email: "john@example.com" },
});

// PUT with data and custom config
const updatedUser = await client.put<User>("/api/users/1", {
  data: { name: "Jane Doe" },
  timeout: 8000,
  retry: 1,
});

// DELETE with config
await client.delete("/api/users/1", {
  timeout: 5000,
});
```

### Complete Configuration Example

```typescript
// All options in a single, clear object
const data = await client.get<UserData[]>("/api/users", {
  baseURL: "https://api.example.com",
  params: {
    page: 1,
    limit: 10,
    status: "active",
    search: undefined, // Automatically filtered out
  },
  timeout: 10000,
  retry: 2,
  headers: {
    Authorization: "Bearer token123",
  },
});
```

### Configuration Object Structure

The simplified API uses a single configuration object with these fields:

```typescript
interface SimpleRequestConfig {
  // Request data (for POST/PUT/PATCH)
  data?: any;

  // Query parameters (flexible - accepts any object with string keys)
  params?: Record<string, unknown>;

  // API configuration
  baseURL?: string;
  timeout?: number;
  retry?: number;
  retryDelay?: number;

  // Standard fetch options
  headers?: HeadersInit;
  signal?: AbortSignal;
  cache?: RequestCache;
  credentials?: RequestCredentials;
  // ... other standard RequestInit options
}
```

### Parameter Handling

The `params` field is flexible and can accept:

- Simple key-value objects: `{ page: 1, status: 'active' }`
- Typed parameter objects: `{ vault_id: 'abc', time_range: '30d' }`
- Mixed types: `{ id: 123, enabled: true, name: 'test' }`
- Objects will be automatically flattened and undefined values filtered out

**Always use the `params` field for query parameters** - never mix them with config options!

### Advanced Configuration

```typescript
import { RequestClient } from "./api/request";

// Create a custom client instance
const apiClient = new RequestClient();

// Configure default settings
const data = await apiClient.request<ApiResponse>("/api/data", {
  timeout: 5000, // 5 second timeout
  retry: 2, // Retry 2 times on failure
  retryDelay: 2000, // Wait 2 seconds between retries
  baseURL: "https://api.example.com",
  headers: {
    Authorization: "Bearer token123",
    "X-Custom-Header": "value",
  },
});
```

### Request Interceptors

```typescript
// Add authentication header automatically
apiClient.addRequestInterceptor(async (config) => {
  const token = await getAuthToken();
  return {
    ...config,
    headers: {
      ...config.headers,
      Authorization: `Bearer ${token}`,
    },
  };
});

// Add request logging
apiClient.addRequestInterceptor((config) => {
  console.log("Making request to:", config.url);
  return config;
});
```

### Response Interceptors

```typescript
// Transform response data
apiClient.addResponseInterceptor(async (response, data) => {
  // Add timestamp to all responses
  return {
    ...data,
    timestamp: Date.now(),
  };
});

// Handle global error logging
apiClient.addResponseInterceptor(async (response, data) => {
  if (!response.ok) {
    console.error("Request failed:", response.status, data);
  }
  return data;
});
```

### Error Handling

```typescript
import { VaultsApiError } from "./api/request";

try {
  const data = await client.get("/api/data");
} catch (error) {
  if (error instanceof VaultsApiError) {
    console.error("API Error:", {
      message: error.message,
      code: error.code,
      status: error.status,
      response: error.response,
    });
  } else {
    console.error("Network or other error:", error);
  }
}
```

### Custom Validation

```typescript
// Only accept 2xx status codes
const data = await client.request("/api/data", {
  validateStatus: (status) => status >= 200 && status < 300,
});

// Accept 404 as valid (useful for optional resources)
const optionalData = await client.request("/api/optional-data", {
  validateStatus: (status) => (status >= 200 && status < 300) || status === 404,
});
```

### Working with Different Response Formats

The client automatically handles the standard `{ success: boolean, data: any, message?: string }` format used by paxeer-network APIs, but also works with any JSON response format.

```typescript
// Standard paxeer-network API response
interface StandardResponse {
  success: boolean;
  data: UserData;
  message?: string;
}

// The client will automatically extract the 'data' field
const userData = await client.get<UserData>("/api/users/1");

// For non-standard responses, the full response is returned
const customResponse = await client.get<CustomApiResponse>("/api/custom");
```

### Integration with Existing Hooks

This request client is designed to work seamlessly with the existing `useApiUrl` hook:

```typescript
import client from "../api/request";
import { useApiUrl } from "../hooks/useApiUrl";

function useVaultsData() {
  const apiUrl = useApiUrl();

  const fetchVaults = async () => {
    return client.get("/api/vaults", {
      baseURL: apiUrl,
    });
  };

  return { fetchVaults };
}
```

### Migration from Old Request Function

The new implementation is backward compatible. If you were using:

```typescript
// Old usage
const data = await request<UserData>("/api/users", { method: "GET" });
```

This will continue to work exactly as before. For new code, consider using the enhanced features:

```typescript
// New enhanced usage
const data = await client.get<UserData>("/api/users", {
  timeout: 5000,
  retry: 2,
});
```

### Best Practices

1. **Use the default client** for simple requests
2. **Create custom client instances** for different APIs or when you need different default configurations
3. **Add interceptors** at the application level for cross-cutting concerns like authentication
4. **Handle VaultsApiError specifically** for better error UX
5. **Set appropriate timeouts** based on your API's expected response times
6. **Use retry sparingly** - only for idempotent operations
7. **Leverage TypeScript** for better development experience and runtime safety
