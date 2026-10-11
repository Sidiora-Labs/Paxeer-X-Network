# Contracts index

| Field | Value |
|---|---|
| Version | 1.0.1 |
| Change control | changes only by amendment; each amendment raises the version of the amended contract and of this index |
| Source revision | 52b672ac4 |
| Binding decisions | ADR D1-D20 |

## Contract files
One line per contract file; the label is how the contracts cite each other.

| Label | File | What it freezes |
|---|---|---|
| address-table.md | address-table.md | precompile and agent-function address table, per-address methods and selectors, code-owned ABIs, LXP_ error registry, activation gates |
| ledger-auth.md | ledger-auth.md | layerxledger 0x101A and layerxauth 0x101C Solidity ABI, bank send overload (0-B), authority, effects, errors |
| pay.md | pay.md | layerxpay 0x101B escrow, budget, stream, service ABI |
| exchange-launchpad.md | exchange-launchpad.md | layerxexchange 0x1015 kept/retired/new methods and views, launchpad ABI |
| programs.md | programs.md | layerxprograms 0x101D ABI with feeLimit forms, views, events, framework fit |
| web.md | web.md | xweb 0x1019 methods, views, params, origin-2 rules |
| agent-functions.md | agent-functions.md | agent functions 0x0901-0x0907 ABI, authority, effects, gas |
| sequencer.md | sequencer.md | ProtocolReceipt v2, receipt key pin, RPCs, MsgSequenceBatch, WAL, nonce and fee rules |
| clients.md | clients.md | client operations, identity and keys, receipt verification, lx_* switch-off, conformance |
| services.md | services.md | per-service reads, writes, signing keys, liveness, evidence |
