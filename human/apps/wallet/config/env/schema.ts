/**
 * Environment variable schema and validation
 */

export interface EnvSchema {
  // API Keys
  NEXT_PUBLIC_SENTRY_DSN?: string;
  SENTRY_DSN?: string;
  
  // RPC URLs
  NEXT_PUBLIC_RPC_URL?: string;
  
  // Feature Flags
  NEXT_PUBLIC_ENABLE_TESTNETS?: string;
  NEXT_PUBLIC_ENABLE_ANALYTICS?: string;
  
  // External APIs
  NEXT_PUBLIC_SIDIORA_API_URL?: string;
  NEXT_PUBLIC_CROSSVERSE_API_URL?: string;
  NEXT_PUBLIC_BLOCKSCOUT_API_URL?: string;
  
  // Build
  NODE_ENV?: 'development' | 'production' | 'test';
}

export function validateEnv(): EnvSchema {
  const env: EnvSchema = {
    NEXT_PUBLIC_SENTRY_DSN: process.env.NEXT_PUBLIC_SENTRY_DSN,
    SENTRY_DSN: process.env.SENTRY_DSN,
    NEXT_PUBLIC_RPC_URL: process.env.NEXT_PUBLIC_RPC_URL,
    NEXT_PUBLIC_ENABLE_TESTNETS: process.env.NEXT_PUBLIC_ENABLE_TESTNETS,
    NEXT_PUBLIC_ENABLE_ANALYTICS: process.env.NEXT_PUBLIC_ENABLE_ANALYTICS,
    NEXT_PUBLIC_SIDIORA_API_URL: process.env.NEXT_PUBLIC_SIDIORA_API_URL,
    NEXT_PUBLIC_CROSSVERSE_API_URL: process.env.NEXT_PUBLIC_CROSSVERSE_API_URL,
    NEXT_PUBLIC_BLOCKSCOUT_API_URL: process.env.NEXT_PUBLIC_BLOCKSCOUT_API_URL,
    NODE_ENV: process.env.NODE_ENV as EnvSchema['NODE_ENV'],
  };

  // Add validation logic as needed
  
  return env;
}

export const env = validateEnv();
