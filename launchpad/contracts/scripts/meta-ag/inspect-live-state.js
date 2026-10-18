#!/usr/bin/env node
"use strict";
// Provider-only replacement. RPC_URL=... node scripts/meta-ag/inspect-live-state.js manifest.json [policy.json]
require("../production-health").main().catch(() => {
  console.error(JSON.stringify({ status: "critical", error: "Health snapshot failed; validate manifest, policy and RPC availability" }));
  process.exitCode = 1;
});
