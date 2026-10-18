# Requester

## Overview

HTTP client for datafeed. sendRequest<T>(datafeedUrl, urlPath, params?) builds URL with query string, sends request (credentials when same origin), returns parsed JSON. Handles UdfErrorResponse. Constructor accepts optional headers.
