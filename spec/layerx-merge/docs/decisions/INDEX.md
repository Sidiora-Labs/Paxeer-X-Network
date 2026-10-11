# Decisions

| D | Title | Status | Affects |
|---|---|---|---|
| [D1](D01.md) | Kernel C modules and Rust program runtime inside the node | decided | every kernel-backed precompile |
| [D2](D02.md) | Kernel account and margin model | decided | 0x1001 bank, 0x1004 addr, 0x1015 layerxexchange |
| [D3](D03.md) | Home of session keys and delegated authorities | decided | 0x101C layerxauth and every kernel-backed precompile |
| [D4](D04.md) | Custody claims and guarantor bonds at the activation height | not applicable | 0x1014 layerxanchor |
| [D5](D05.md) | Kernel balances without a 0x1004 binding at activation | not applicable | 0x1004 addr |
| [D6](D06.md) | Precompile error encoding | decided | 0x1001 bank, 0x1004 addr, 0x101A-0x101D, 0x0901-0x0908 and every kernel-backed precompile |
| [D7](D07.md) | Time semantics under the sequencer cadence | decided | 0x1015 layerxexchange (oracle intake), fee-token paths |
| [D8](D08.md) | Trading semantics where kernel and exchange differ | decided | 0x1015 layerxexchange, launchpad |
| [D9](D09.md) | Program fee versus gas | decided | 0x101D layerxprograms |
| [D10](D10.md) | Paid search settlement and lxpay placement | decided | 0x101A lxpay |
| [D11](D11.md) | Program-origin web request terms | decided | 0x1019 xweb, 0x101D layerxprograms |
| [D12](D12.md) | Origin-2 observation admission | decided | 0x1019 xweb |
| [D13](D13.md) | Reference guest deployment | decided | 0x101D layerxprograms |
| [D14](D14.md) | Sponsorship path and per-identity budget home | decided | 0x1004 addr, BatchCallAndSponsor |
| [D15](D15.md) | Sequencer receipt signing key | decided | none |
| [D16](D16.md) | DID key rotation and the embedded-wallet ed25519 share | deferred to the wallet feature | 0x1004 addr |
| [D17](D17.md) | Client-facing contract | decided | none |
| [D18](D18.md) | Sequencer consensus scope | decided | none |
| [D19](D19.md) | Sequencer execution mode | decided | none |
| [D20](D20.md) | Operator tooling, registry service and agent functions | decided | 0x0901-0x0908 |
| [D21](D21.md) | layerxAnchor 0x1014 reads after the fold | decided | 0x1014 layerxAnchor |
| [D22](D22.md) | Agent functions 0x0901-0x0904 | decided | 0x0901 orob, 0x0902 clearing, 0x0903 oracle, 0x0904 pofq |
| [D23](D23.md) | Reason text for addr 0x1004 and bank 0x1001 | decided | 0x1001 bank, 0x1004 addr |
| [D24](D24.md) | Service dispute arbiter and escrow settlement | decided | 0x101B layerxpay |
| [D25](D25.md) | Exchange withdrawMargin | decided | 0x1015 layerxexchange |
| [D26](D26.md) | Signer of the agent-host web payer | decided | 0x1019 xweb, 0x101A lxpay |
| [D27](D27.md) | Typed exact payment at 0x101A | decided | 0x101A lxpay |
| [D28](D28.md) | Web precompile error naming | decided | 0x1019 xweb |
| [D29](D29.md) | One stream state for 0x0906 and 0x101B | decided | 0x0906 streams, 0x101B layerxpay |
| [D30](D30.md) | Gas rule across the new precompiles | decided | 0x101A-0x101D, 0x0901-0x0908 |
| [D31](D31.md) | ABI form of computeClearing and scoreBatch | decided | 0x0902 clearing, 0x0904 pofq |
| [D32](D32.md) | Signer rule for submitPrice at 0x0903 | decided | 0x0903 oracle, 0x1004 addr, 0x1015 layerxexchange |
| [D33](D33.md) | On-chain home of the tee attestor roots at 0x0907 | decided | 0x0907 teeAttestor, 0x1019 xweb |
| [D34](D34.md) | Tee attestor signer threshold and key type at 0x0907 | decided | 0x0907 teeAttestor |
