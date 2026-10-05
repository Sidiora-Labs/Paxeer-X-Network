# config

Shared configuration fragments kept beside the wallet app.

- `env/schema.ts` declares an `EnvSchema` interface and a `validateEnv()` reader for a small set of variables. The app does not import it; the variables the app reads are listed in the [app README](../README.md#configuration).
- `typescript/base.json` holds base TypeScript compiler options. The app's own `tsconfig.json` does not extend it.
