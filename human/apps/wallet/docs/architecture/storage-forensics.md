# Application storage forensics

The runtime registry in `src/platform/storage/repositories.ts` is authoritative. Every application-owned browser record is a bounded version-1 envelope with a named owner, schema, sensitivity, retention rule, quota, migration, lifecycle reset list, and corruption policy.

| Record | Area | Owner | Sensitivity | Retention and reset |
| --- | --- | --- | --- | --- |
| custody-choice | local | custody-choice | security-relevant | Until custody switch, reset, or uninstall; corruption fails closed |
| contacts | local | contacts | private metadata | Until user deletion, reset, or uninstall |
| recent-recipients | local | contacts | private metadata | Five entries; cleared on logout, account removal, reset, or uninstall |
| preferences | local | preferences | preferences | Currency, language, HTTPS custom RPC, developer display controls, and notification preferences; cleared on reset or uninstall |
| dapp-tabs | local | dapp | private metadata | Eight HTTPS origins; cleared on revoke, logout, custody switch, account removal, reset, or uninstall |
| dapp-permissions | local | dapp | security-relevant | Exact HTTPS origins and bounded permission identifiers; corruption fails closed; cleared with the dApp lifecycle |
| portfolio-filters | local | portfolio | private metadata | Dust preference and bounded token-address list; cleared on reset or uninstall |
| pwa-dismissals | local | pwa | preferences | Install prompt seven days and notification prompt three days |
| notification-state | local | notifications | private metadata | Bounded counters and timestamps; cleared on logout, account removal, reset, or uninstall |
| metadata-cache | local | metadata | public | Caller TTL, 200 entries, 512 KiB total |
| pending-send | session | operations | private metadata | One validated transaction observation, one session, at most two minutes |
| announcement | local | pwa | preferences | Last acknowledged shipped version until reset |

Mnemonic material, private keys, unlock secrets, session capabilities, full approval payloads, push secrets, and authentication credentials are prohibited from every application repository. Self-custody vault state remains exclusively inside wallet-core IndexedDB. Supabase and managed-wallet authentication state remains owned by their reviewed authentication adapters. Wallet-core’s direct local-storage access is restricted to legacy cleanup and its bounded IndexedDB commit notification fallback.

Wallet-core owns one additional `paxport-wallet-session-v1` IndexedDB record
outside the application repository registry. It contains only a
structured-cloned non-extractable AES-GCM `CryptoKey` handle, vault identifier,
and bounded inactivity deadline. It contains no PIN, mnemonic, private key,
raw key bytes, or step-up state, and is deleted on explicit lock, timeout,
reset, credential replacement, failed restoration, or cross-context
revocation.

Malformed optional records are removed and surfaced through the background status channel. Corrupt custody choice or dApp permission state fails closed. `resetStorageForLifecycle` deletes only records registered for the requested lifecycle and never deletes another custody product’s authoritative wallet state.
