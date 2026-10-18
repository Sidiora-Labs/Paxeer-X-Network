// Layer 1 — Types. Pure TS contracts. No runtime, no React, no imports.
// Imports allowed from: (none — leaf)
//
// Use subpaths directly: `import type { Foo } from "@paxeer-network/paxeer-backend/types/perps"` or
// `import type { Bar } from "@paxeer-network/paxeer-backend/types/paxscan"`. We deliberately do NOT
// re-export across subdomains here — perps and paxscan have name collisions
// (e.g. both define `Fee`).
export {};
