# Changelog

All notable changes to Paxeer X Network are documented in this file.

## 2026-09-28

### Other

- px26-leader px26-fm00 2026-09-28 main 9052283ab6ad740fa50d8d5fc84f27b3e73fe109 Regenerate the web feature's rendered task document from the spec so it lists the sidecar's chunked JSON-RPC transport task in the second wave
- px26-leader px26-fm00 2026-09-28 main c73e052ceade7ca8377873d5d0c2cfcc6138cad8 Merge the web-search sidecar's chunked JSON-RPC transport into main: the sidecar's HTTP client now joins chunked answers from the node's HTTP server, so the submitter reads the attestor set and the larger precompile views; the task's own verify command passed in its lane
- px26-w176 px26-fm00 2026-09-28 fix/xweb-chunked-transport c73e052ceade7ca8377873d5d0c2cfcc6138cad8 The web-search sidecar's HTTP JSON-RPC client now reads answers sent with a chunked transfer encoding, joining the chunks in order and tolerating chunk extensions and trailers while refusing an answer that carries both a length and a chunked encoding, an unknown or repeated transfer encoding, a chunk cut short or bytes after the terminating chunk, so the submitter reads the attestor set and every larger view the node's HTTP server chunks; the crate's chunked tests pass over a loopback endpoint
- px26-leader px26-fm00 2026-09-28 main e59ddc81f25c143285661b32b6af6999a98437a7 Merge the activation feature's fourth-wave tasks: the web-search module's authority messages become transaction types with a Msg service and a transaction command, and the v6.10 plan writes the chain's operating values in the block its upgrade info names, the anchor and custody identifiers from the EVM chain id, the launchpad quote denomination, the one-hour and twenty-minute governance voting periods and the initial four-attestor web-search set at a threshold of three with the pause lifted; the plan adds no store, and each task's own verify command passed in its lane
- px26-leader px26-fm05 2026-09-28 feature/paxeer-x-activation e59ddc81f25c143285661b32b6af6999a98437a7 Merge the fourth wave's first two tasks into the activation feature: the web-search module's authority messages are now protobuf transaction types with a Msg service, codec registration and a transaction command, and the v6.10 plan writes the chain's operating values in the block its upgrade info names, the anchor and custody identifiers from the EVM chain id, the launchpad quote denomination, the one-hour and twenty-minute governance voting periods and the initial four-attestor web-search set at a threshold of three with the pause lifted; each task's own verify command passed in its lane
- px26-w175 px26-fm05 2026-09-28 wave/paxeer-x-activation/4.4 e18fd7b7d2ff2422ef791d7f13fc010444374283 The v6.10 plan writes the chain's operating values in the block its upgrade info names: the anchor module's chain and network identifiers and the custody module's network identifier from the EVM chain identifier the application derives for its own chain id, the launchpad quote denomination from the base coin unit, a one-hour governance voting period with a twenty-minute expedited period beside untouched deposit and tally parameters, and the four supplied web-search attestors with a threshold of three and the module's pause lifted, and the node package's v6.10 and activation suites pass over it
- px26-w174 px26-fm05 2026-09-28 wave/paxeer-x-activation/4.1 b77b2ab253a3037c23a153fdc397ece5f4cb70b0 The web-search module's attestor registration, attestor removal, threshold, settings, pause and unpause are now protobuf transaction messages with a Msg service over the module keeper, registered on the interface registry and on the legacy amino codec and each sendable as its own subcommand of the module's transaction command, and the module's own test suite passes
- px26-leader px26-fm05 2026-09-28 tmp/act-sync e29b6461f838897a05107c5feb38fc0549769c81 Bring the operator-driven activation and the explorer merges from main into the activation feature: the wave-3 section now carries both the pending-store task and the upgrade-info task, the second and third requirements sit in order, the observations and gates of both lines are kept, and the rendered task and requirement documents are regenerated from the merged spec
- px26-leader px26-fm00 2026-09-28 main 0d0927c457855834c142096b1cbfdb702f368169 Merge the explorer's eighth-wave gate records: the first run of the wave gate on the merged revision stopped in the type check because the install's Chakra typegen had left the generated conditions typing empty, and the rerun after regenerating those typings passed the type check and all 1102 vitest tests across 192 spec files at the same revision; the lint leg passed on the first run and was not repeated
- px26-w173 px26-fm04 2026-09-28 feature/paxeer-x-explorer 0d0927c457855834c142096b1cbfdb702f368169 The wave gate for the explorer's eighth wave re-ran after the Chakra style typings were regenerated, with the type check and the vitest suite passing
- px26-w170 px26-fm04 2026-09-28 feature/paxeer-x-explorer f2a805a7dcfde9717bf998e77f90e6eedb4c3078 Run the eighth wave's gate once on the merged revision: the frontend lint leg passes over the project with seven warnings and no error, the type check leg then exits 2 with thirty-one thousand diagnostics over fourteen hundred files the wave never touched, traced to the zero-length Chakra conditions typing this worktree's own install left behind rather than to anything the wave wrote, so the vitest leg was never reached and the wave carries no test-leg record; the run is written to the ledger as it happened and nothing is repaired or rerun
- px26-w172 px26-fm05 2026-09-28 feature/paxeer-x-activation 420455d3c383360f573a3235b9c3102062e3aad9 The spec gains a fourth wave that makes every fork module's authority messages submittable protobuf transaction types carried by one governance route and executed whole, and a v6.10 plan that writes the chain's known operating values - the anchor and custody identifiers, the launchpad quote denom, the shortened governance voting periods and the initial web-search attestor set with its threshold - on the same upgrade-info path as the activation, with a replay proof over a post-activation state.

## 2026-09-27

### Other

- px26-leader px26-fm00 2026-09-27 main 410a72c5bb3d301ef140d3d25147b108a12f5134 Merge the explorer's eighth wave: the home page's live feeds flush on one two-second cadence so the page stays clickable at the chain's block pace, pill, badge, tag and pill-tab sizes derive from the line box so labels never overflow, the scan pages and primitives lay out for a 375px screen with the latest block rows wrapping inside their card, every API resource carries the stale time of its class with paged lists holding their rows and the address counters requested alongside the page, the frontend vitest setup fills in the font-face set jsdom lacks with a spec covering it, and the responsive specs assert the wrapping contract; each task's own verify command passed in its lane, the wave gate on this merged revision is still to run
- px26-w166 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.2 5ad2ef966a5189a39bf484838a47b8b9b8470d4c Pill, badge, tag and pill-tab sizes now derive from the line box plus padding and border so labels never overflow their background, with truncation on narrow labels
- px26-w162 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.7 dd7c40a27479629b9bf9546eee1cf31766763b23 Keep the home page's latest block rows inside their card on a phone screen: the row wraps under the medium breakpoint so the block number with its age and the value chip hold the first line and the hash takes a line of its own, the height column sizes itself from its text rather than a fixed width, and the hash is now cut by the shortener that measures the slot it sits in, with the type check and all forty-nine cases of the home suite passing and a headless measurement at 375 and at 1280 finding no part of a row past the body of its card
- px26-w163 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.8 6ac8790df7950d746d0e748ab16115b9574e3ab2 Let the specs beside the address page, the two kernel pages, the transaction heading and the transaction stat row assert the wrapping contract the eighth wave gave them, reading each responsive declaration off the breakpoint rule the provider inserts so a fixed inset at phone width would fail the spec
- px26-w161 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.6 5f612c34d5350be4a43774c43b89a692196ee1f8 Cover the font face set the frontend vitest setup fills in with a spec that reads the real jsdom document, proves load hands back a non-empty list, check answers true and ready resolves with the set, and drives the real font observer hook the dynamic hash shortener uses under fake timers to show nothing is left pending once it has settled
- px26-w153 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.1 c7b06ef5c0d1ec031a36e87a016ce34078e6bff1 Put the home page's live feeds on one two-second flush cadence so the page stays clickable while the chain seals a block every 150 to 200 milliseconds: a new socket buffer collects the block and transaction messages in a ref and hands them over once per cadence, holding while the pointer rests on a list and while the tab is hidden, the block list merges, dedupes and caps one flush into the cache instead of writing it per message, the rows lost their entry fade and now compare the fields they render so a flush renders only the row it adds, and the stats counters follow the same cadence behind memo boundaries, with the type check and all fifty-three cases of ui/home and lib/socket passing at exit 0
- px26-w156 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.4 20358e9a3b0bb6a3a917daaa1729f3b4aad4bd00 Cut what the scan pages pay to load by giving every API resource the stale time of its class, letting a paged list hold the rows it is leaving, starting the address counters and the token page's address request in the same round as the requests they belong with, and letting the verified-fetch client, the QR generator and the coin balance chart arrive only when they are used, which brings the address counters from 12.1 s to 2.5 s, leaves one call to /api/v2/stats per load instead of three and takes 484 KB and 540 KB of JavaScript off the transaction and address pages, with the frontend type check passing and the test gate stopping on three page specs this task does not touch that reach vitest's default five-second budget under host load
- px26-w155 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.3 4bd45cb0efbe9bf7365b32d4df60182ce9c31fb0 Lay the scan pages and the scan primitives out for a 375px screen by wrapping the page title rows so the identifier keeps a line of its own beside the identicon, letting the detail values stack under their labels and wrap instead of clipping, giving the mobile list rows the card gutter and the full width they lacked, holding the header pagination in a slot the narrow footer leaves alone and letting the pagination and show-rows rows wrap, with the type check clean and the 395 tests of the task suites passing at exit 0
- px26-w161 px26-fm04 2026-09-27 wave/paxeer-x-explorer/8.5 25463e9a43d5630651a4433b9b4524b401179bb5 Fill the font-face set jsdom leaves out in the frontend vitest setup so the font observer behind the dynamic hash shortener settles on its first attempt instead of polling with timers that outlive a spec file's environment, ending the reference errors the transaction, transactions and token tracker suites raised after teardown while every test in them passed
- px26-leader px26-fm00 2026-09-27 main d9086a85dc0040d7557b2143326ff25b763efc4f Merge the operator-driven activation: the v6.9 plan now applies in the block named by the node's on-disk upgrade info, the same file that drives its store loader, and the compiled fork height with its self-scheduling and halt is removed; the node upgrade suites and the replay fixture pass
- px26-w169 px26-fm05 2026-09-27 wave/paxeer-x-activation/3.3 d9086a85dc0040d7557b2143326ff25b763efc4f Apply the activation plan in the block the on-disk upgrade info names, the same file the store loader reads, and drop the compiled fork height together with the scheduling and the halt it needed, stopping the block instead when the plan's stores were never mounted
- px26-leader px26-fm00 2026-09-27 main 00b2cc30b299f67579910747eb745286445a7114 Merge the activation wave-gate record: the release build and the replay gate pass at the merged tip, the package-wide go test gate records two pre-existing failures outside the feature (an upgrade-list override leaking across tests in the node package and three embedded testnet genesis digests no longer matching), and the version.json mismatch is noted
- px26-leader px26-fm00 2026-09-27 main d29602b0175ab106103f58b590cc79a03bd1018d Merge the activation feature: one self-scheduled v6.9 plan at the fork height that mounts the six fork module stores and brings the eight precompiles online together, the precompile reachability keyed on the applied plan, and the replay harness that proved the plan on a copied chain state at exit 0
- px26-w171 px26-fm05 2026-09-27 feature/paxeer-x-activation e377b0d56bde203bfa055fb12e12a1475b71dfab Record the live-chain activation of the v6.9 plan as a passed gate: the plan applied in the first block after the freeze on every validator and every full node, with all eight fork precompiles answering there.
- px26-fm05 px26-fm05 2026-09-27 feature/paxeer-x-activation d3a1579bb341af793cb29818c2434a2ebd5af72c Record that the operator-driven activation replays cleanly over a copy of a live full node's pre-fork state: the plan applies in the block after the committed height, adds the six module stores, brings the eight precompiles online there while withholding them one block earlier, and survives the commit and reopen
- px26-w167 px26-fm05 2026-09-27 wave/paxeer-x-activation/3.1 be77d7f2ce59feb020eee99ff0fd6a3661be27a6 Serve a mounted module store the state-commitment database carries no tree for yet as an empty, write-discarding store so a binary whose mount list has grown starts on older state and commits the same app hash until a store upgrade adds the tree
- px26-w164 px26-fm05 2026-09-27 wave/paxeer-x-activation/2.1 00b2cc30b299f67579910747eb745286445a7114 Record the activation wave gate at the merged revision: the release build and the upgrade replay over a generated pre-fork state both passed, while the full node and EVM package run fails on two clusters this feature never touched, an upgrade-list override a test leaves behind and three embedded genesis digests that no longer match their files
- px26-w160 px26-fm05 2026-09-27 wave/paxeer-x-activation/1.2 d29602b0175ab106103f58b590cc79a03bd1018d Add the upgrade replay harness that carries a copy of a chain's state through the activation plan: it opens the data directory with the real application so the plan's store loader mounts the added stores, applies the plan on a cache-wrapped block at one above the committed height, and asserts the six fork stores, the version map, each module's own genesis with the web-search module paused on an empty attestor set, the eight precompiles served at that height and withheld one below it, then one commit and a reopen, with the generated pre-fork fixture and a copy of a pruned full node's state both passing every assertion
- px26-w159 px26-fm05 2026-09-27 wave/paxeer-x-activation/1.1 c8fa552b7ac454c86a5bd1a26e7840511af639df Bring the whole fork online at one height: a new activation plan whose store upgrades mount the six fork module stores, a handler that initialises every module the version map lacks from its own default genesis with the web-search module paused and its attestor set empty, begin-block scheduling one height below the fork plus the stop that writes upgrade-info.json so the store loader mounts the added stores on restart, and a precompile reachability predicate that serves the eight fork addresses only from the applied plan or a genesis that already carries the modules, with the node and EVM keeper upgrade suites passing
- px26-fm05 px26-fm05 2026-09-27 feature/paxeer-x-activation b80567d4094b1cc7e06a48345ac0ecbe63544541 Add the activation feature spec: one fork-height plan that mounts the fork module stores and brings the eight precompiles online together
- px26-leader px26-fm00 2026-09-27 main 516226f184e36b70f9990bab94cd9901d82ddcc6 Merge the explorer feature's fourth wave-gate record: the frontend image built cleanly at the merged tip and the gate evidence now names all four runs
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 69e229aebf2148ca16b009868407d3942f3cada0 Record the frontend image build of the scan-wave aggregate run: the Dockerfile builds every stage from the merged tip 50ad0f4af at exit 0 and tags the image locally without pushing it, completing the four gate records for that revision
- px26-leader px26-fm00 2026-09-27 main 88104e6b800dd7181727cae343dc4ab5adf63594 Merge the explorer feature's scan redesign waves: the home, transactions and blocks lists, transaction, block, address, token, token tracker, top accounts, charts and kernel pages rebuilt on the scan shell and primitives, the list card nesting and pagination fix-ups, and the wave gate recorded at the merged tip with lint and test-ratio passing and the test gate stopping on one load-bound spec timeout
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 529144d8955b287b40dc2695e253f67411f40792 Record the one aggregate run over the scan waves at the merged tip 50ad0f4af: the lint gate passes over its backend, frontend and services legs, the test-ratio gate passes with 144 changed source files matched by 146 changed tests, and the test gate passes its four backend legs and the frontend dependency and type-check legs before exiting 1 on a single five-second per-test timeout in the block page spec, with the vitest teardown errors and the release-time Playwright baseline refresh recorded beside it
- px26-w152 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.12 6bca9fecde5f58289e6452c03fb50599d354bd04 Let the address transactions tab embed the shared transactions list inside the one table card it opens by giving the list content an explicit flag that says the caller already carries the count line, the page-data download and the pagination, so the tab shows a single Download Page Data link with the CSV export beneath it while the standalone transactions and blocks pages keep opening their own card, with the frontend type check and the transaction, block, shared address and address suites passing
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 3de46630279cf69023c9f5b2868a614946f35bae Add the task that lets the embedded list content render inside the page's own table card
- px26-w151 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.11 d72dae3b6c94f594c5bf742a0664a68f1b2cb546 Leave the transaction and block lists one pagination by taking it out of the tab strips so the shared scan table card carries it alone on desktop while the mobile action bars keep theirs, and move the in and out badge onto the counterparty column of address-scoped transaction, internal transaction and token transfer rows in place of the course arrow, dropping the interim filter badge the address cards kept in their headers, with the frontend type check and the new tab strip, counterparty and row specs passing and one pre-existing export-row assertion on the address transactions tab recorded instead of touched
- px26-w150 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.10 72f615e27f260582bb84eaa3f941715a7229d3fd Cover the transaction page rebuild with the vitest specs its sources were missing, rendering the degraded banner, the logs, internal transactions, raw trace and state cards and both action cards through the real query paths so the transaction suite carries one spec per source it changed
- px26-w148 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.8 03282e41715cf16f2b0c51ffb4a417035443b8f3 Rebuild the statistics page on the scan layout - a sticky section list on the left marks the section in view and scrolls to the one chosen, the counters become a four-across grid of stat cards carrying the label, its information icon, the value and the percentage change in the rising or falling tone, and every chart sits in its own card with its title and interval, an information icon, a view link, the current value and the dated plot - and take the chart line, area, bar and axis colours together with the interval selector and the filter bar from the theme tokens, with the frontend type check and ten new vitest specs passing
- px26-w149 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.9 a76fc437f62c3a71ad8c272b35eb77ecd034aa24 Rebuild the kernel pages on the scan layout - the unified account leads with the identicon, the account label, its full identifier and copy control above three detail cards carrying the asset, activity and identity counts, the bound chain address with the latest entry and block and the three node capabilities, and its identities, assets and activity sections move onto the pill strip inside the shared scan table card with the count line, the record note, the pagination and the row selector; the anchor and receipt lists gain a settlement column on the status ladder in the same card and keep their mobile list items, and the receipt page reads from shared key-value rows with hint labels and divider groups beside the section pill and the API entry - covered by sixteen vitest specs that pass together with the frontend type check
- px26-w147 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.7 90869740a92905923741ac71025b4eef38464cda Rebuild the token tracker and the top accounts list on the scan layout - the token table ranks the contracts with their price and its coin-denominated line beneath, the change and volume columns the list endpoint cannot answer, both capitalisations and the holders, while the accounts table carries the rank, the address, the name tag, the balance, the share of the supply and the transaction count - and head both table cards with their count line, the search, filter and download actions and the pagination, closing them with the row selector, with the frontend type check and ten new vitest specs passing
- px26-w146 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.6 307408828282954f95a8877f3674d7ef4a0a02cf Rebuild the token and token instance pages on the scan layout - the token logo, name and symbol with the verified mark in the title row above a chip row carrying the standard, the source-code and implementation chips beside the wallet, QR and menu actions, three detail cards holding the supply, the holders and the transfers, the price with the onchain and circulating market capitalisation and the contract with its decimals, and the transfers, holders and inventory tabs moved into the shared scan table card with the honest count line, the download and advanced-filter actions, the pagination and the show-records footer - with the instance page reading the same way from its media, overview, other-info and metadata cards, covered by vitest specs beside every changed component that pass together with the type check
- px26-w141 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.1 9dbbe0359d6fe8bf304472056134c4c6b432d56c Rebuild the home page on the scan layout: the hero band carries the page title and the wide search field with its submit control, the statistics become one card divided into the coin, the chain and the fourteen-day history columns, and the latest blocks and latest transactions sit side by side as icon-row cards closed by view-all footers, with the frontend type check and the home vitest suite passing.
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 33e683db68c94aebad911b84be71ec3675dc1630 Add the task that leaves the list pages one pagination and puts the direction badge on the counterparty column
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer f7361f6c0aa731de47f67aa7492478aac9dc6584 Add the task that gives the transaction page rebuild the specs its test ratio needs
- px26-w142 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.2 9e4e0850905391f7747957a9b812de823524dd14 Rebuild the transactions and blocks lists on the scan layout - a row of stat cards above the transactions table carrying the day's transactions, the pending queue and the total and average transaction fee each with an honest derived delta, the transactions table reordered into preview, hash with its failed marker, action chip, block, age, sender and recipient, amount and fee, and both lists moved inside the shared scan table card with the count line, the record note, the page-data download and the pagination in its header and footer - covered by vitest specs beside every changed component that pass together with the type check
- px26-w143 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.3 481d8d6e8269bf2d61c66e80f34e59659d117ae3 Rebuild the transaction page on the scan layout - a page title carrying the previous and next controls with the API entry beside the pill section tabs, the decoded action as its own card above a detail card running from the hash and status through the block with its confirmation chip, the timestamp, sender and verified recipient to the value, fee and gas price, and a more-details expander holding the gas usage, fee per gas, burnt fees, nonce and raw input - put the logs, internal transactions, state and raw trace tabs in the shared table card, and cover the new structure with vitest specs
- px26-w145 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.5 dd0144c6671dc7bfd5a6438c183f1cc6b3cba5f7 Rebuild the address page on the scan layout - the identicon, the label, the full address with its copy and QR controls in the title row and the watchlist, API and menu entries in the icon row beneath it, three detail cards carrying the balance, net worth and token holdings, the name tags, counters, last balance update and funding source, and either the linked Paxeer X account or the contract information - and put the transactions, internal transactions, token transfers, tokens, contract and coin balance tabs on the pill strip inside the shared table card with the latest-of-total count line, the page-data download, the direction badge for the filtered direction, a centred view-all row and the CSV export beneath, covered by vitest specs beside every changed component that pass together with the type check
- px26-w144 px26-fm04 2026-09-27 wave/paxeer-x-explorer/6.4 22b73f7acd90aa3e207b1e6ed642f248974b11c0 Rebuild the block page on the scan layout - pill section tabs beside the API entry, one detail card carrying the height with its previous and next controls, timestamp, transactions, hash, reward, difficulty, size, gas used with its percentage, burnt fees and the extra data as a read-only field, closed by a more-details expander - and put the transactions, internal transactions, deposits and withdrawals tabs in the shared table card, each covered by vitest specs
- px26-w137 px26-fm04 2026-09-27 wave/paxeer-x-explorer/5.1 7f12e13dfd9bd38cb2796173b1e671f898e3f305 Rebuild the explorer shell as the scan layout - a utility bar carrying the coin and gas prices with the desktop search box on every route but home, a brand row with the grouped horizontal menu, a centred content container, a home hero band and a four-column footer - and cover each piece with vitest specs
- px26-w138 px26-fm04 2026-09-27 wave/paxeer-x-explorer/5.2 f82a8945adee51c77b02b845e895eec3716d5a8c Add the scan primitives the redesigned explorer pages compose - stat and table cards, pagination with a show-rows selector, pill section tabs, an expander, a preview control, a method chip, a direction badge and a key-value row - rebuild the shared pagination on top of them, and carry the detail grid, entity links, previous-next, action bar, copy control and status tag onto the same tokens with the six recipe variants they consume
- px26-leader px26-fm00 2026-09-27 main c1e29aef05213a955e38cfc6957f2ddadd9f0b4c Merge the web feature's sidecar fix: the submitter reads the eleven-field getRequest view the xweb precompile publishes, the recorded node answers carry the same view, and the x-websearch dry run now completes its fulfil, callback and credential legs against anvil and records the exchange
- px26-w140 px26-fm01 2026-09-27 wave/paxeer-x-web/2.46 11ca3039d63b737b40f2ce4cb6de6101f05f616a Read the eleven-field getRequest view the xweb precompile publishes when the submitter checks a request's status, widening the decoder and the recorded node answers past the nine-word tuple, and commit the dry run fixture the loop now records, with the request_status tests and the x-websearch dry run passing
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 988db280d36b9c808ef2df73147a75fee34e6a12 Add the task that reads the eleven-field getRequest view in the submitter and records the dry run
- px26-leader px26-fm00 2026-09-27 main a8928c209e1886de61164880a62797e8c68a8384 Merge the explorer feature's scan layout plan: the requirements for the scan shell, primitives, list, detail, entity, statistics and kernel pages, and the foundation, page and gate waves that build them on the product tokens
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 2b15301f4028b84c92f094f61694b14d9c204d5d Add the scan layout requirements and the foundation, page and gate waves to the explorer feature
- px26-leader px26-fm00 2026-09-27 main 4a716dbdbfe87280fe8f499ac55d4bed1335ba18 Merge the web feature's second wave so far: the kernel program call path is bound to its published ABI versions through the generated programs module constants, the agent test harnesses run under one blocked-path root with the local gateway and daemon custody funded from the harness, the finality evidence vectors and native encoders follow the current program registry, the agent workspace clippy findings are cleared, and the x-websearch dry run stands up anvil with three real sidecars and records the getRequest view drift that still blocks its fulfil leg
- px26-w139 px26-fm01 2026-09-27 wave/paxeer-x-web/2.6 db0624362314ffe7ce3e6b7d0a7cf4ec8a72154b Add the x-websearch dry run that stands up anvil, a loopback site and API over TLS and three sidecars with run-local attestor keys, deploys the consumer, and asserts one content digest, ascending valid signatures over the origin-1 preimage, fulfil calldata against the precompile ABI, the impersonated callback and the credential-free API legs, recording the request view drift that stops the fulfil
- px26-w136 px26-fm01 2026-09-27 wave/paxeer-x-web/2.45 da7eda4c6b0fa8a1575bfa31fd4dd69e1bcd35a2 Verify a program call receipt at the protocol, module and guest ABI versions the kernel publishes it with, generating the Programs module ABI table from the kernel header so the core accepts the web reader's call receipt
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web fd8ebc0717ea3d6f79b04001a3c885c7215a913a Add the task that verifies program call receipts at the protocol version the kernel publishes
- px26-w135 px26-fm01 2026-09-27 wave/paxeer-x-web/2.11 e02887c7ec38a538ce2a6c833f92bf89b5c30c38 Clear the agent workspace clippy findings at their source: bind the program execution receipt arguments into one struct, split the long runtime and vector-generation functions into named helpers, box the large bind plan variant, take the receipt evidence inventory by reference, and add tests for the execution usage decode and the budget divergence branches
- px26-w134 px26-fm01 2026-09-27 wave/paxeer-x-web/2.44 ba6bf5560164cae974a1a497060b7e1c77b270c3 Bind a program call's capability encoding to its ABI version in one place, so the scheduling projection and the execution of the same call stop disagreeing about how its grants are written: abi_policy answers the encoding an admitted ABI version carries, admitted_schedule_accesses and layerx_programs_call_begin both read that one binding, and two unit tests pin that every admitted ABI has exactly one encoding and none is admitted without one; the reference web reader's guest ABI 4 call is now planned and settles a committed receipt well inside the core deadline, and the gate still stops one step later at 503 receipt_unavailable because the receipt proof crate verifies a call receipt only at the historical protocol version, recorded as observation 2.44.1
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web a0b373dcbe665da0a81be86dcd81c47cd5355037 Add the task that makes the web reader's program call settle a receipt inside the core deadline
- px26-w133 px26-fm01 2026-09-27 wave/paxeer-x-web/2.43 ed03eccf778c1358c3c34b8988f6331e2b1e3611 Derive the scheduling identity actor of a program call from its actor DID so a batch plans the same activity the serial path settles, and pin that precondition against the real kernel path
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 516ff362ee529b0d0f9d5e9e94d84ec1d674ff20 Add the task that resolves the fatal invariant in the metered allowance admission
- px26-w132 px26-fm01 2026-09-27 wave/paxeer-x-web/2.42 a83ed97365209e131dca76dadd67afea8858aca7 Take the local gateway's program receipt expectation from the Programs activity table the kernel header generates, so a lifecycle receipt is checked against the module and operation the kernel actually binds
- px26-w131 px26-fm01 2026-09-27 wave/paxeer-x-web/2.41 456ac78119830c748eb255b66fc29e04cc6a6301 Fund the daemon custody harness through the native custody module and publish the anchor precompile as its settlement contract
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 85a81a8627cf012a23528a8881a9e43636375b50 Add the task that moves the daemon custody harness onto the native custody module
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 0c29f774cdd4f4aee87769b685464e289cd418c1 Add the task that aligns the program deploy receipt ordinals with the kernel
- px26-w130 px26-fm01 2026-09-27 wave/paxeer-x-web/2.40 a91a83fc413a851244271a7db4a78f484d972c79 Take the native program encoders' guest ABI range from the kernel's own maximum
- px26-w129 px26-fm01 2026-09-27 wave/paxeer-x-web/2.39 15daba94b8d0b6999cfcf838c7d7e85f6f43a886 Point the agent test harness blocker at the custody profile vault the confined session fee run now stops on
- px26-w129 px26-fm01 2026-09-27 wave/paxeer-x-web/2.39 76ae2e19209ee16150de63f1cd57e1fe5c01bb8d Give every agent test harness under the agent-test recipe one build output directory
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web f5647d8ee5a1802413160f48521f679ae8f1232a Add the task that aligns the program lifecycle validator with the kernel's guest ABI range
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 44bd9cb71756fdc4237ce48e9b315c1c528f1873 Add the task that gives every agent test harness its build output directory from one place
- px26-w126 px26-fm01 2026-09-27 wave/paxeer-x-web/2.37 b20358f96d73c84a939d02aa41bc1a3303ab5ef5 Fund the local gateway qualification through the native custody module on a real local chain
- px26-w127 px26-fm01 2026-09-27 wave/paxeer-x-web/2.38 47566097466466d76545023f79b8fa66520bf1ca Regenerate the finality evidence vector and its pinned attestations against the anchor precompile settlement contract
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 9d99096c64df75f326e33ce869140b11092e0cf9 Add the task that brings the finality evidence vector in line with the extended module set
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 6f70576f6f3a5f8e7eb03b39d7097ddd9cd42e13 Add the task that funds the local gateway harness through the native custody module
- px26-w113 px26-fm01 2026-09-27 wave/paxeer-x-web/2.34 d7377cf1bd3719dabb51167a96554169f4027a0f Teach the agent client the kernel's spot and web modules so a node's preparation snapshot decodes, and pin the availability genesis request against the six configured genesis modules
- px26-w123 px26-fm01 2026-09-27 wave/paxeer-x-web/2.36 b3669c7b325b1ad14e58d966c4095a9b481e7946 Reword the web answer comment so the consensus no-float scan reads it as prose and list the gas station and web search surfaces in the beta contract
- px26-fm01 px26-fm01 2026-09-27 feature/paxeer-x-web 73d30d77b55ff5a3f6a9a24114070da35e7c41a5 Add the task that clears the two continuous integration legs the web feature turned red
- px26-w114 px26-fm01 2026-09-27 wave/paxeer-x-web/2.35 9a4890988fb69f623efaadb0b12128331b3f47a5 Name the anchor precompile for the settlement contract and the checkpoint registry in the core boundary and local gateway clusters, and list lx_getProgramEvents in the gateway's published method set
- px26-leader px26-fm00 2026-09-27 main 0a2ff7a48b8bd3ced1bb1128835d094e2f691696 Merge the explorer feature's completed wave: the human workspace strict lint passes and its web image builds from the tracked sources, the recorded stub and screenshot names fit a Windows checkout with a tracked-path length check, the committed program artifacts carry remapped source paths with the fixture build script that sets them, and the merged wave gate is recorded across its three runs
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer f0c93aa104cbb2d88798387faa4c6a5e291f9ee3 Record the explorer wave gate reruns
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 667a6bde29145e8cc8adc7321b9611822718054d Record the merged wave gate for the explorer feature
- px26-w128 px26-fm04 2026-09-27 wave/paxeer-x-explorer/2.14 89e230ce6df2245cf3f9187c5aa86796fe75ae70 Rebuild the three committed program artifacts with their source paths remapped onto the virtual source root, regenerate the interfaces and registry state values bound to them by hash, and add the fixture build script that is the only place the remapping is set, so the publication audit no longer finds a checkout path in the published set
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer 0f11b54e97e3027b37b29b351deaf03755219149 Add the task that rebuilds the program fixtures with source paths remapped
- px26-w124 px26-fm04 2026-09-27 wave/paxeer-x-explorer/2.13 c58f2a28015b99b09daafae8887aafe896579c0c Shorten the recorded stub and screenshot names that break a Windows checkout and add the tracked-path length check
- px26-w117 px26-fm04 2026-09-27 wave/paxeer-x-explorer/1.7 c8e90837768d9ef1d82e2f1460160f2275918f3c Make the human workspace strict lint pass and build its web image from the tracked sources
- px26-w115 px26-fm04 2026-09-27 wave/paxeer-x-explorer/1.3 3f9bf43a67bececc336373573ffbc6df4c2cedbf Replace the credential-shaped explorer test fixtures with synthetic ones and assemble the bridge documentation check's private-key marker so the publication audit's secret scan is clean
- px26-fm04 px26-fm04 2026-09-27 feature/paxeer-x-explorer dc461987ab5a571aae516b523c61131c4f55ae0b Add the task that keeps recorded fixture paths under the Windows checkout limit
- px26-w118 px26-fm04 2026-09-27 wave/paxeer-x-explorer/2.8 e0d12ab76d467abd6b53088ec0195277f2c2885f Name the wrapped coin Wrapped Paxeer and prove the rename moved nothing else
- px26-w119 px26-fm04 2026-09-27 wave/paxeer-x-explorer/2.11 78da0a316aab7be287373e478081747ca82086f9 Schedule the Paxeer X capability probe's first call on the process message loop and cover the start with a boot regression test
- px26-w116 px26-fm04 2026-09-27 wave/paxeer-x-explorer/1.6 53dd54e2ccea7b943aafe447d1678c43f49ea3fe Record the agent daemon terminal-transition and confirmed-creation budget cases passing on the current agent workspace
- px26-leader px26-fm00 2026-09-27 main 6871402b905b99904f105880db1ee9f31b28a812 Merge the Sidiora fee token feature's fifth wave: the solidity job bootstraps its pinned libraries from the one script the forge workflow also uses, the wallet screen's fee choice and maximum field are composed from the component kit with an end-to-end assertion, and the wave gate is recorded
- px26-fm03 px26-fm03 2026-09-27 feature/sidiora-fee-token c4a165c3c0b7a1f0ccacfca25df603f907c01105 Record the wave 5 gate for the Sidiora fee token feature
- px26-w125 px26-fm03 2026-09-27 wave/sidiora-fee-token/5.2 ec5b9a7f3b26446f8b6bd6ebe8c2ce76b682d374 Compose the wallet screen's Sidiora fee choice through the component kit so the Human component gate passes
- px26-w125 px26-fm03 2026-09-27 wave/sidiora-fee-token/5.1 f56ce6f8a5ccc23e2baa60eb8dcfd9f77a7eae7e Resolve the pinned Solidity libraries for the kernel Foundry build from the one bootstrap script
- px26-fm03 px26-fm03 2026-09-27 feature/sidiora-fee-token 9771846931dcda80add8afe3155244ef26b77f95 Add the wave that clears the solidity and wallet screen continuous integration legs
- Apply the cleanups from the first full Jev sweep: README translation drift and rustls RUSTSEC-2026-0285 (#334)
- px26-w121 px26-fm00 2026-09-27 chore/readme-banner bf810b5afc657bc0b926c31e345db7d45b2c08df Point the translated READMEs at the current banner
- px26-leader px26-fm00 2026-09-27 main 4eaa20b776a0aad3163d73a5d87242dfd4e5c3f5 Merge the continuous integration feature's first wave: the Linux runner image with its self-check, the controller that turns each queued job into a single-use machine and reconciles finished ones, the workflow routing onto the Linux runner label with the routing check, the notifier and diff steps fed through step environment variables, and the recorded wave gate
- px26-fm05 px26-fm05 2026-09-27 feature/paxeer-x-ci 6243099fb67919fea247d40cba3848e4ca77b715 Record the wave 1 gate for the continuous integration feature
- px26-w120 px26-fm05 2026-09-27 wave/paxeer-x-ci/1.4 e7b21266237594f2345f14ffb0a922cdd0a03045 Pass the pull request event fields to the Slack notifier and the diff steps through step environment variables, keep the four publishing jobs on GitHub-hosted runners with an explicit exclusion reason, and quote the empty CDPATH assignment in the routing check
- px26-leader px26-fm00 2026-09-27 main cb840793dabe62671f2d40c869833c36cd11eef2 Rename the package and notice to Paxeer X Network, regenerate the changelog, ignore the bridge build outputs and drop the old network banner

## 2026-09-26

### Added

- Add the Paxeer X bridge contracts and deployment spec (#545)
- Add durable Sidiora sponsored transaction submission (#540)

### Documentation

- Document Sidiora and its two gas paths on the protocol site (#544)

### Other

- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 7351f96843bb9eefa869b61aedca8db82bfc1197 Add the task that qualifies the program events relay, naming the anchor address in the boundary and local clusters, making the events test selectable and listing the method in the gateway's published set
- px26-w106 px26-fm01 2026-09-26 wave/paxeer-x-web/2.28 e8312d5a303faab9f5ba45408cf7926de17f7204 Relay program events through core with topic, sequence and limit validation, list lx_getProgramEvents in the gateway schema and add the local web request events run, leaving the task implemented while the boundary cluster fails to bootstrap
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web fdd986191afb0f338614c7eafc52c9aa2b2b3d79 Add the task that teaches the agent client the spot and web modules, since a node's preparation snapshot listing them is refused as malformed
- px26-w105 px26-fm01 2026-09-26 wave/paxeer-x-web/2.33 d7034452231c206c97e123c3a4ee55bed706415d Report the native read route's connection and preparation failures by cause, tracing the unavailable finalized receipt read to the client refusing the spot module in the preparation snapshot
- px26-w110 px26-fm05 2026-09-26 wave/paxeer-x-ci/1.2 04d1e783821c6183bd37de674cbcd95f9e42392e Add the CI controller that turns each queued Linux job into a single-use Fly Machine and reconciles finished, orphaned and stopped machines
- px26-w109 px26-fm05 2026-09-26 wave/paxeer-x-ci/1.1 6daa8dc4e31d5e6cc5cb19383485dcf4431e315b Build the ephemeral CI runner image with a just-in-time entrypoint, an idle watchdog and a self-check
- px26-fm05 px26-fm05 2026-09-26 feature/paxeer-x-ci bdca9e709e02ac12a8bbc338bb4e740870a46330 Add the task that clears the routing gate by removing the pull request title injection and keeping the publishing jobs on GitHub-hosted runners
- px26-w111 px26-fm05 2026-09-26 wave/paxeer-x-ci/1.3 9d482325c6b7930095a0d791535df89546d8e884 Route every Linux CI job through the CI_LINUX_RUNNER variable with a fork guard, a routing check and a runner canary; the task gate stops on an existing actionlint expression finding
- px26-fm05 px26-fm05 2026-09-26 feature/paxeer-x-ci 418c4690dfce378b9c3c8625c6bcad10597922e5 Specify continuous integration on Fly.io Machines: an ephemeral runner image, a controller that creates one machine per queued job, reversible routing through one repository variable, and a deployment task
- px26-leader px26-fm00 2026-09-26 main fa1dc8496ae9c97a4f011f4f6bdbce2310f108cd Merge the web feature's program event persistence: the node keeps each call's event list in its receipt authority log and serves program events by topic and sequence, the availability harness serves its contracts at the anchor address, and the task that diagnoses the finalized receipt read is laid out
- px26-w104 px26-fm01 2026-09-26 wave/paxeer-x-web/2.27 55b17991f04442ea1c443d1b0331138d1e3d4390 Persist the event list in the node's receipt authority log and serve program events by topic and sequence
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 778492d4aeb5e532cb5257964f421a9099bb1061 Add the task that finds why the finalized receipt read is unavailable after finality registration, so the agent workspace tests can be qualified
- px26-w102 px26-fm01 2026-09-26 wave/paxeer-x-web/2.32 9f09f71d95ac5d49993288b104680297a6658ac7 Serve the availability harness's bond and registry at the anchor address so finality registration verifies, leaving the agent workspace tests implemented pending the native receipt read
- px26-leader px26-fm00 2026-09-26 main ffa437090b1c183f927f6a98939ef8b57f8c8200 Record the fee token feature's wave 4 gate: the Go packages, the gas station crate and the Paxeer Foundry project each pass once on the merged wave revision
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 03f4fdc2728524ac631a92ae60ae27cf191a1008 Record the wave 4 gate for the fee token feature
- px26-leader px26-fm00 2026-09-26 main cbeb9da797402d8186397fe8fa9d935cc2506b80 Merge the fee token feature's station service: the binary serves the quote and sponsored submission endpoints over a real socket with the status split both adapters read
- px26-w101 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.3 d035ce2b73f3f8c7732ccf08fa2909b93775b674 Serve the quote and sponsored submission endpoints from the station binary
- px26-leader px26-fm00 2026-09-26 main a7c8cf9bee2e2994e590d9cfaafdfe9e2ad512a0 Merge the web feature's event list ownership fix: prepared outcomes and batches now hold their own copy of a call's event list, so the list bound to a receipt hashes to the outcome's envelope digest and the web program path, prepare pass staging and module context tests qualify
- px26-w103 px26-fm01 2026-09-26 wave/paxeer-x-web/2.31 3540d255a0a07ac84023a34cb7734a390980d0f9 Reconcile the bound event list with the call outcome's event envelope digest and qualify the web program path
- px26-leader px26-fm00 2026-09-26 main b9ca99b7852085a0e1304510581401c1512fae48 Merge the web feature's module context test repair, whose transfer fixture now applies its set through the real ledger path, and the tasks that reconcile the bound event list digest and serve the availability harness's contracts at the anchor address
- px26-w100 px26-fm01 2026-09-26 wave/paxeer-x-web/2.29 73e594e223d28c5a5838cb28227539d89e49b21d Restore the module context test's transfer fixture after the ledger balance check, with the prepare pass staging recorded as implemented pending the event list digest
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web f20010eeeb63f8701d8c211234a650879cf59bfe Add the tasks that reconcile the bound event list with the outcome digest and serve the availability harness's contracts at the anchor address, so the web program path and the agent workspace tests can be qualified
- px26-leader px26-fm00 2026-09-26 main 50702c678724b3a1cbc9451f22a9e1d6027bb6a9 Merge the fee token feature's parameter regeneration reconciliation: the rate is read as a field since the pipeline emits no getter, and the generated file is committed exactly as produced
- px26-w98 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.14 14bdf630cc19c6a6c2cfc3ec5669ff1bbe5a02e5 Reconcile the fee-token rate accessor with the pipeline-generated parameters
- px26-leader px26-fm00 2026-09-26 main 79bb1b1ffb92691d0266e2a455a34b59f3bb3fff Merge the web feature's kernel relay wiring into the sidecar binary, with the agent workspace test repairs recorded as implemented pending the availability harness contradiction
- px26-w99 px26-fm01 2026-09-26 wave/paxeer-x-web/2.30 0a446bd096d8a675af01148e10e3747cc4d12129 Pin the api preimage vector in the attestation test and qualify the api request path
- px26-w64 px26-fm01 2026-09-26 wave/paxeer-x-web/2.16 5d8c544bb1ce712ab584b0799825197900678647 Run the agent workspace tests with the boundary and native binaries they need and repair the native terminal evidence they decode
- px26-w94 px26-fm01 2026-09-26 wave/paxeer-x-web/2.20 deaaa6447a99402716df38bcb9dedcd94f0e0fdc Restore the module context and api vector tasks to the web spec after the kernel relay commit dropped them
- px26-leader px26-fm00 2026-09-26 main d844606d7159a893832b6b7e16e063c1aadba523 Merge the fee token feature's sponsor nonce lifecycle, spread-bounded rate updates and proxy upgrade initialize call, restoring the pointer binding and nonce lifecycle files that two stale-base landings had dropped
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token f6d37ae3f0b5502c57ef78fa056eb602d29316c9 Restore the gas station nonce lifecycle work and its evidence, which the spread bound landing had reverted
- px26-w88 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.7 a1f2ee820d3e5365d68c43655f4cc6421acfb0f2 Encode the implementation's initialize call in the scheduled proxy upgrade
- px26-w93 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.11 53a0aa7336d154d4db45ccc00284383b311cd795 Bound every governed rate update by the maximum spread
- px26-w97 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.2 6c487799e315ac16b391e6c27b2fd68f5c46e246 Release a refused sponsor nonce and journal replacement and cancellation
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 0482a1e32ec32b6d742a0f5cfe7617d8d8ae5ad3 Restore the pointer binding governance message, its proposal, message server, CLI, generated files and tests, which a commit based on a stale tip had removed
- px26-leader px26-fm00 2026-09-26 main b837b1ba39e19d8e1cb476b53d7a686845774717 Merge the web feature's select vector correction: the shared number vector expects ECMAScript's shortest digits and the attestation test's stale vector count is recorded, with the tasks that restore the module context test and pin the api preimage vector laid out
- px26-w94 px26-fm01 2026-09-26 wave/paxeer-x-web/2.20 aa0c556af304b4fc13cd1444e109e1d730469646 Wire the kernel relay into the sidecar binary and scope the signature exchange by program
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web e220d40c450b1081bcbc5f023c556b9861ee2ac0 Add the tasks that restore the module context test after the web module registration and pin the api preimage vector in the attestation test, so the prepare pass staging and the api request path can be qualified
- px26-w91 px26-fm01 2026-09-26 wave/paxeer-x-web/2.26 4958ac94d920e75b2b1c877f2c58b6bc009490e5 Carry a call's full program event list as receipt side data bound by the outcome's event envelope digest; the web program path check of that binding still fails
- px26-w95 px26-fm01 2026-09-26 wave/paxeer-x-web/2.25 55f5f2a3ee4863d0d232aa7c67213b72a86c6ff5 Correct the select vector's number digits to ECMAScript's shortest closest form and pin the long literal's double and digits in the api tests; the api tests pass, the attest preimage test still expects two vectors
- px26-leader px26-fm00 2026-09-26 main 8192ef0a7fb6318a7abb62eb7a1e4794a17d671e Merge the fee token feature's chain id signature test and protobuf regeneration record: a sponsored batch signed for one chain id is refused after the chain id changes, and the pipeline's missing rate getter is recorded with the task that reconciles it
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 4d4b94b6b2d09b58920a7ec4db8348e0ff76d6ac Add the task that reconciles the fee-token rate accessor with the pipeline-generated parameters, since the pinned pipeline emits no getter the parameter tests call
- px26-w92 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.8 3ceee44069f4d7986182bac3b435bfef204ec0c6 Assert the account signature fails after a chain id change
- px26-w87 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.6 3abe4881d52f8a9beaabe3e8d28ab75ca780a296 Bind the existing Sidiora address as the native pointer through a governance message
- px26-w96 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.12 ac52863bf9c90b269cf56e949f810bf72e073f6f Record that the pinned protobuf pipeline drops the fee-token rate getter the parameter tests call
- px26-leader px26-fm00 2026-09-26 main 26dbacfddbd85a206138159a46b7da1a4ce41790 Merge the web feature's prepare pass staging: a program's first paying call creates the web fee account at its own sequence over a private journal, with the module context test's pre-existing opening failure recorded
- px26-w83 px26-fm01 2026-09-26 wave/paxeer-x-web/2.23 4824a9fe7079a4e1d77500a793a8a99b57a789d9 Let the prepare pass stage module values under a journal on its snapshot so a program's first paying call creates the web fee account, and record that the module-ctx target stops in its unchanged transfer check first
- px26-leader px26-fm00 2026-09-26 main 45bf38a54326a2e75bbd371ea6e3b0e44eeae13e Merge the fee token feature's gas station pricing: quotes are priced only from the paymaster's governed rate, a missing, stale or too-old rate is refused before anything is signed, and the oracle path is removed
- px26-w84 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.1 c2daa1df7562466fa9f8881c80e41b0e4e22a9c0 Price the gas station from the paymaster's governed rate
- px26-leader px26-fm00 2026-09-26 main 1a9f30a7a0699306a72986e6f36a1e3966b49be0 Merge the fee token feature's withdrawn preference rule: a preference naming a denom that is no longer allowed is charged as no preference in the network coin, the call that clears it is accepted, and the missing-rate refusal is kept for an allowed denom without a rate
- px26-w90 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.10 5325129f7b8a5dcc2b6fef338c994d54fc7d0728 Charge a withdrawn preference as no preference
- px26-leader px26-fm00 2026-09-26 main ce207896e54a90635cc88fe2bedef5be3388ed4b Merge the fee token feature's coinbase sweep: end-of-block moves every allowed fee token from each transaction's coinbase to the fee collector, leaves locked coins in place, and fails the block when a sweep is refused
- px26-w89 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.9 6f979b5a43f4488321d5df04181f805d247a0939 Sweep fee-token fees from the coinbase to the collector
- px26-leader px26-fm00 2026-09-26 main 24bd784e544fad6273b3726532b8644d69ae6940 Merge the web feature's program request path qualification: the observation activity fixture is signed by keys the tests derive, the intake and adapter vectors follow, the sidecar's kernel test compares against a fixture it can reproduce, and the program request path passes end to end
- px26-w82 px26-fm01 2026-09-26 wave/paxeer-x-web/2.19 8aaaeed90f46adf8965af97008d3335dc748cdd9 Re-sign the observation activity fixture with the attestor keys the tests derive, pin the new signer vectors in the intake and adapter tests, and qualify the program request path
- px26-leader px26-fm00 2026-09-26 main d54c6ebc70675dacfb3a477c59d572e6e332a8d9 Merge the fee token feature's Foundry bootstrap: the Paxeer contract libraries resolve at pinned versions without a manual clone and the build no longer needs an environment override
- px26-w86 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.5 0b02aaab984019263bd5fbb5c09a3875100978aa Accept the registered Sidiora denom in bridge genesis
- px26-w85 px26-fm03 2026-09-26 wave/sidiora-fee-token/4.4 f95281331a87cc1542ff7ecd7fe4b7a1b55e9588 Resolve the Paxeer Foundry libraries without a manual clone
- px26-leader px26-fm00 2026-09-26 main 53b126199f6e4a8358d6d298e600e0d93262c36c Merge the web feature's gateway program events: the gateway serves lx_getProgramEvents for the sidecar's kernel watcher, the platform lock file resolves again, and the tasks that keep, persist and relay a call's full event list are laid out
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web ffec79b90cf65fe24eb6a2c9bfbb46054549216c Lay out the program event tasks: the kernel keeps a call's full event list beside the receipt artifacts bound to the outcome digest, the node persists and serves it by topic and sequence, and core and the gateway relay it to the sidecar's watcher
- px26-w78 px26-fm01 2026-09-26 wave/paxeer-x-web/2.21 11026230f36e0593c786101fdf315c2dcf5e11d0 Serve program events through the gateway for the sidecar's kernel watcher and bring the platform lock file up to date
- px26-leader px26-fm00 2026-09-26 main 2a3a533764c50d7901bd2ed64a9ae1bd551da04a Merge the fee token feature's fourth wave plan: the gas station prices from the governed paymaster rate, sponsor nonces have a lifecycle, the station serves its endpoints, the Foundry libraries bootstrap without a manual clone, bridge genesis accepts the registered denom, and the governance binding, upgrade initialisation, coinbase sweep, withdrawn preference and rate bound rules are laid out
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token ed2550fb0b9ed2400af8df55a7f0ae410347683f Write the retired oracle and the governed rate into the Sidiora decisions, revise the requirements that still priced through it, and add the fourth wave that closes the ledger: the gas station's rate source, lifecycle and service, the library bootstrap, the bridge genesis check, the pointer binding message, the proxy initialize call, the fee sweep, the withdrawn preference rule, the rate bound, the generated parameters and the gate rerun
- px26-leader px26-fm00 2026-09-26 main 7fe91d2aded571baf5a6cf9dd1a257acd28f4b71 Merge the web feature's number canonicalisation: JSON numbers parse correctly rounded and print ECMAScript's shortest digits, the sidecar's kernel relay carries its attestation level, the select vector's stale digits are recorded, and the tasks correcting that vector, re-signing the kernel fixture and updating the platform lock file are laid out
- px26-w76 px26-fm01 2026-09-26 wave/paxeer-x-web/2.18 2676ac97f63e1bc25bca5d8547279aa94b5a5a34 Register the web module with the kernel so its observation and attestor-set activities route through dispatch, raising the module bound to eleven
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web edc81d80d00fbea0f0720d5a6eae4c134267af95 Add the task that corrects the select vector's number digits and qualifies the api request path, extend the kernel test key task to clear the clippy line-count warning, and have the gateway events task bring the platform lock file up to date
- px26-w81 px26-fm01 2026-09-26 wave/paxeer-x-web/2.24 7eb7a44b6f18bceb4ce14b13749ffb8f527f01ec Record that the pinned number vector's digits disagree with ECMAScript's for the correctly rounded double while the canonicaliser already matches ECMAScript, and set the majority level on the kernel attestor's answers so the sidecar library compiles
- px26-leader px26-fm00 2026-09-26 main a6edb955f0caad83ce70cc59f73b8199aa001f1d Merge the web feature's program lint update: the lint accepts the v3 and v4 ABI host function sets and the web-reader reference build is linted, with the observation activity fixture's unreproducible signatures recorded and its re-signing task rewritten
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 79c585e2b717635d10d0db26dd7b5c94752e11a1 Rewrite the kernel test key task: the observation activity fixture is re-signed with keys the tests hold and the intake and adapter vectors follow, since its current signatures come from keys no test can reproduce
- px26-w79 px26-fm01 2026-09-26 wave/paxeer-x-web/2.22 c9adbef1ab112bab325e6484cecf02bf8e8a7d4d Teach the program lint the v3 and v4 ABI host function sets and lint the web-reader reference build
- px26-w77 px26-fm01 2026-09-26 wave/paxeer-x-web/2.19 39a6d8ac7a0a19c49e27f6db6a8b507a4d1303c6 Record that the adapter fixture's signatures recover to the intake test's signer vectors, not to any key the program path test holds, and that the kernel attestor's answer lacks its level field
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 68a9b95edf6b07715865fbd1612c88176299147a Add the task that canonicalises JSON numbers as ECMAScript does so the api vectors agree across the four languages
- px26-leader px26-fm00 2026-09-26 main ecb8816f00c9dad314224d4ac0830b39524fe6c3 Merge the Sidiora qualification ledger triage: twenty observations that later revisions resolved are closed naming the revision or decision that closed each
- px26-fm03 px26-fm03 2026-09-26 wave/sidiora-fee-token/triage ac33b1c00d88ac50c615fbd52e856e2b358126d7 Close the Sidiora qualification observations that later revisions resolved, naming the revision or decision that closed each
- px26-leader px26-fm00 2026-09-26 main e0d9c2a08dfea34a12406c66598565febb8faf88 Merge the web feature's api answer serving: api answers carry their own content kind and are served by the content endpoint, the loopback API refuses a call without its credential, the api test crate passes lint, and the one canonical number vector still disagreeing is recorded, alongside the tasks that close the program request path
- px26-w73 px26-fm01 2026-09-26 wave/paxeer-x-web/2.17 e48c089f7d776b168a45b0b4e8e73854a75fff7f Serve api answers from the sidecar's content store under their own kind, refuse the loopback call without its credential, and clear the api test crate's lint findings
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web c29370541d9cc0fa9478937f409377b14596f9e2 Add the tasks that close the program request path: the web module registers with the kernel and routes through dispatch, the kernel test's attestor keys match the intake fixture, the relay is wired into the sidecar with a program-scoped exchange, the gateway serves program events, the lint learns ABI v4, and the prepare pass stages module values
- px26-leader px26-fm00 2026-09-26 main 1f29396b9395c64613cb735c71866131371ef30c Merge the Sidiora feature's aggregate gate rerun: the fee-path Go packages, the gas-station crate and the Paxeer Foundry project all ran once on the merged revision and passed
- px26-w75 px26-fm03 2026-09-26 wave/sidiora-fee-token/3.1b 87e138c17b0aa09f84a32723763974c3c93dbf47 Record the Sidiora gate rerun on the merged revision: the fee-path Go packages, the gas-station crate and the Paxeer Foundry project
- px26-leader px26-fm00 2026-09-26 main 6ab42617f1084d3e655a9e5f51eecf2567b64131 Merge the bridge feature's aggregate gate record: the Foundry suite, the Solana program tests and build, the relayer crate and the deploy and vector packages with their check scripts all ran once on the merged revision and passed
- px26-w70 px26-fm02 2026-09-26 wave/paxeer-x-bridge/3.1 7409775b3daa12202de4f3058243d238c383ec0b Record the bridge gate run on the merged revision: the Foundry suite, the Solana program tests and build, the relayer crate and the deploy and vector packages with their check scripts
- px26-leader px26-fm00 2026-09-26 main 355f471828028caed779502b198334b1e785bd05 Merge the evm module test alignment: the exported genesis expectation carries the fee-token parameters at their defaults and the consensus version expectation matches the raised module version
- px26-w74 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.17 8a026f0e94516558b197a01f5dbcca3890c10052 Expect the fee-token parameters in the evm module's exported genesis and its raised consensus version
- px26-leader px26-fm00 2026-09-26 main 2ad8bfb4b3427e315b55eef96a45e7385bd57510 Merge the web feature's program request path: a program's web request is recorded only when its 402 transfer lands, intake splits the fee to the signing attestors, the web-reader reference program pays and reads through web_read, the sidecar watches program requests and posts the observation activity, and the kernel test's signer mismatch, the dispatch bound and the remaining wiring are recorded for follow-up tasks
- px26-w62 px26-fm01 2026-09-26 wave/paxeer-x-web/2.2 34545dc9540a3ac77d24c9e78f4dd3b144cd8b35 Close the program web request path from the request record through payment, attestation intake and web_read, with the sidecar's kernel watcher and a reference web-reader program
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 07de0ef6b08a76fbf0551bcdbb246a481b03b33f Add the task that qualifies the sidecar api request path: the test crate's lint findings are cleared, the loopback server refuses a call without its credential, and api answers gain a content kind so the content endpoint serves them
- px26-leader px26-fm00 2026-09-26 main ea8fa395301aca1a8d7e35ff8b7e6d0a22865e10 Merge the Sidiora feature's first aggregate gate record: the fee-path Go leg ran on the merged revision and stopped on the evm module's genesis export and consensus version tests, and the task that brings those tests to the fee-token state is added ahead of the rerun
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 0f683f5b43e98a50d4139f2dc19227d6722b8d91 Add the task that brings the evm module's genesis export and consensus version tests to the fee-token state before the aggregate gate reruns
- px26-w71 px26-fm03 2026-09-26 wave/sidiora-fee-token/3.1 4a37904787fe235d703c67ce5e8d021c79309059 Record the Sidiora gate run on the merged revision: the fee-path Go leg failed in the evm module tests and stopped the run before the gas-station crate and the Paxeer Foundry project
- px26-leader px26-fm00 2026-09-26 main b843dcddd1fbc544185fe7b3fa09b54fd5e15cf7 Merge the web feature's sidecar api requests: the sidecar decrypts its attestor's credential envelope in memory, calls the API with it, selects and canonicalises the answer and signs under the majority or the single level, with the test crate's lint and loopback findings recorded for the follow-up
- px26-w65 px26-fm01 2026-09-26 wave/paxeer-x-web/2.14 1ff7a795e00e12b9ad28689f120469ab74709282 Perform api requests in the sidecar: decrypt the attestor's credential envelope in memory, call with it, select and canonicalise the answer, and sign under the majority or the single level
- px26-leader px26-fm00 2026-09-26 main 4e0e1ad1831d0079d7e664dcc4e7c0d0137431e5 Merge the bridge feature's Solana Sidiora pair registration: the pair is registered through a governance message emitted ahead of the cap in the same proposal, so the bridge no longer depends on an upgrade handler for it
- px26-w67 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.18 c328d0156b03482a82160af84a7bce3ae969ca74 Register the Solana Sidiora pair through a governance message emitted ahead of the cap in the same proposal, so the bridge no longer depends on an upgrade handler for it
- px26-leader px26-fm00 2026-09-26 main ff5914fec64cb338a0efd48f5f5be68dad5ff372 Merge the fee-token precompile gating: the fee-token entry registers only from its upgrade with the precompile set chosen by block height, and the registration test asserts it at that upgrade and absent below
- px26-w69 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.16 95054f1740672cf520e51d8c3810a02f1eef6f17 Assert the fee-token precompile is registered at its upgrade and absent below it
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 25464ead167109d37b9980b0e5bbed04bcf0e293 Add the task that asserts the fee-token precompile registration at its upgrade rather than below it
- px26-w68 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.14 bc4ffb738ff05303ecf0cfe32572d3a9b91d4af1 Serve the fee-token precompile only from its upgrade on and select the custom precompile set by the block's upgrade height for ordinary execution
- px26-leader px26-fm00 2026-09-26 main 28b2473b702db983881e20ed3fe9ba9ae67068d8 Merge the web feature's api call helpers: TypeScript and Python builders for api payloads and credential envelopes, and the page section showing a contract calling an API through XWebApi
- px26-w66 px26-fm01 2026-09-26 wave/paxeer-x-web/2.15 0f88925412546b129a67d6df6463ea577ac68307 Give developers the api call helpers: TypeScript and Python builders for api payloads and credential envelopes, and the page section that shows a contract calling an API through XWebApi
- px26-leader px26-fm00 2026-09-26 main 78e341e255a6cb9278f6132526b2e6a97dac2cee Merge the relayer journal fix: a completed transaction is marked landed so a finished release no longer reads as pending, and the relayer lint task is recorded passing
- px26-w60 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.19 feff7d3004d0d4bde3471710a51641c89ef007eb Mark a completed transaction as landed in the relayer journal so a finished release no longer reads as pending, and record the relayer lint task passing
- px26-leader px26-fm00 2026-09-26 main 21ea9d10a464b27863464a0fa8a9ece181ad97bf Merge the web feature's api request kind: contracts call an API through xweb with encrypted credential envelopes, field selectors, the single attestation level and the XWebApi library, and the sidecar's crawl interval is configurable with a clean stop on SIGTERM and SIGINT
- px26-w57 px26-fm01 2026-09-26 wave/paxeer-x-web/2.13 bf26a24b4f542a69683bab95b1bfddaef0427b71 Let a contract call an API through xweb: the api request kind, encrypted credential envelopes, field selectors, the single attestation level and the XWebApi library
- px26-w63 px26-fm01 2026-09-26 wave/paxeer-x-web/2.8 a76155a2e02f4444db32f49824bf68cba71a4507 Let the sidecar's crawl interval be configured and stop it cleanly on SIGTERM and SIGINT with the index committed
- px26-leader px26-fm00 2026-09-26 main 691de554feb7e1f572b8b2e9e6d444b518e67fac Merge the bridge feature's second wave tail: the EVM dry run reads governance bodies through the proposal decoder, the Solana deploy waits for the executable and keeps the program keypair beside its record, the EVM deploy resolves its script from its own location, the deploy scripts check, the recorded Solana dry run, and the relayer restructured to pass clippy
- px26-w51 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.16 4bdc199a567c9832c2dc30c2df62adc42a333fcb Wait for the deployed Solana program to become executable before initialising it, keep its keypair from the first deployment, deploy the vaults from any directory and record the Solana dry run
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge edb75a3813d90ff5e2d27e66c3876a5ea34cd2ed Add the task that replays a completion out of the relayer journal's pending set and re-qualifies the lint task
- px26-w56 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.17b 888dbf33d0445d819db09201c88bb07c153a6c98 Restructure the relayer's tests so the whole crate passes clippy, and close the observations the earlier bridge tasks resolved
- px26-w50 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.15 057c7ea3f7439be8c3684f80826628c9902c645f Read the generated governance bodies through the proposal decoder in the EVM dry run and re-record it
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 534ac6cc7ef12a4175040c8bdf5231b5ac1d86b4 Let the relayer lint task restructure its tests as well as its sources
- px26-w52 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.17 4d5c8542a08785f440d8ec9a7019ffbde13a58c4 Clear the relayer's source clippy findings by restructuring its code, point the attestation references at the contract's home under bridge/evm, and record the integration-test lints still outside the task
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 74d0c752f87946b6662d4edccf6460da9c9922c3 Add the tasks that decode bodies in the EVM dry run, harden the deploy scripts, clear the relayer lints and register the Solana Sidiora pair through governance
- px26-leader px26-fm00 2026-09-26 main c164087a1994220b4a8e823e5897bf0e1abc382d Merge the Sidiora fee token's second wave tail: gas is charged in the account's fee denom on the block execution path and on the transaction delivery path alike, and the fee path is proven over real blocks
- px26-w61 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.15 2acf4819541a3474418f30522e5dc28cec4dd9e3 Charge gas in the account's fee denom on the transaction delivery path as the block execution path does, and prove the Sidiora fee path over real blocks
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 37d58bd9beb942937d54d37a9ef1fd4a2912618c Add the task that charges the fee token on the transaction delivery path and re-qualifies the block proof
- px26-w53 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.13 42d74448607e915c56b213bac662669c04780cea Charge gas in the account's fee denom on the block execution path and prove the Sidiora fee path over real blocks
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 052bd4549728ad2ee0e3d5c91e2fc382bffe6b18 Add the tasks that charge the fee-token path on the block execution path and serve the fee-token precompile only after the upgrade
- px26-fm03 px26-fm03 2026-09-26 feature/sidiora-fee-token 45ab8e0ad62d24b94a1ab3ab5f638bba8f48c1a8 Bring the Sidiora feature up to date with the bridge and web work on main
- px26-w13 px26-fm03 2026-09-26 wave/sidiora-fee-token/1.5r f1be3c0f09449db9d9d36fd923869b40c143d30f Record the passing Sidiora fee selection test run and close its loader finding
- px26-w07 px26-fm03 2026-09-26 wave/sidiora-fee-token/2.10 3edf19a4bb240e444b9a3394da3de66a444bb294 Prove the Sidiora fee path in real blocks and record the harness ante gap
- px26-w07 px26-fm03 2026-09-26 wave/sidiora-fee-token/1.5 c91ef8a0cb206ec34dd0dc0129784be7e25d462d Add Sidiora fee selection and sponsored wallet consent
- px26-leader px26-fm00 2026-09-26 main dccefbe06e5ceb40d850200dafe22a6cc9316ffb Merge the web feature's second wave so far: the sidecar signs, exchanges and submits attestations, the v6.8 upgrade plan wires xweb into the application and serves its precompile only from the upgrade on, daemon-bound sessions carry the web route, the metered draw signer and the sidecar's payment configuration are settled, the sidecar ships as an image and a service unit, the documentation page, the Python receipt verification by protocol version, the widened workflow filters, and the api request kind with credential envelopes now specified
- px26-w58 px26-fm01 2026-09-26 wave/paxeer-x-web/2.4b dccefbe06e5ceb40d850200dafe22a6cc9316ffb Name the xweb store, make v6.8 the latest upgrade in the upgrade tests and archive the precompile versions the bump produces
- px26-w59 px26-fm01 2026-09-26 wave/paxeer-x-web/2.9r 6adc36bae860bc9527ec562e9c6cfac685944e58 Record the daemon-bound web session task passing with its agent daemon tests scoped to the tenant module
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web b24558ee384fb8c325df32de1b856bff8bf773a1 Scope the daemon-bound session task to its own tests, run the agent workspace tests with the binaries the workflow builds, finish the upgrade plan's cross-cutting edits and re-gate the lint task on the real workflow steps
- px26-w46 px26-fm01 2026-09-26 wave/paxeer-x-web/2.4 296a079ec127ecdcaf5c35855d0d5f9a7d0f502e Add the upgrade plan that wires the xweb module into the application and serves its precompile only from the upgrade on
- px26-w44 px26-fm01 2026-09-26 wave/paxeer-x-web/2.1 a5c7ad395bd6d8a5895a1c99164cfb451e7e88d0 Sign web requests as an attestor, exchange signatures with peers and submit fulfil from the sidecar
- px26-w49 px26-fm01 2026-09-26 wave/paxeer-x-web/2.10 db6137d042337b3b2f14a216179640db75cd5543 Settle the metered draw signer in the payment protocol and let the sidecar configure its payer, per-draw fee limit and conformance suite
- px26-w48 px26-fm01 2026-09-26 wave/paxeer-x-web/2.9 4134863df98bcb56bfb74fd665e19c2e5685af63 Serve the web tools to daemon-bound sessions opened with the web scopes and report a held spend in its own words
- px26-w54 px26-fm01 2026-09-26 wave/paxeer-x-web/2.5 15f746b7ae21f7f7e152a90dad92585ef001e3f1 Document the web search sidecar, the xweb precompile and the program web_read path
- px26-w45 px26-fm01 2026-09-26 wave/paxeer-x-web/2.3 0cd9edac51d2a1f23ac3aacbaf555a5c70a09047 Package the web search sidecar as an image and a service unit and build it into the node image
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 19cbe4be71589339551a6114695a845290ae894c Let contracts call APIs beyond the network: add the api request kind with credential envelopes, selectors and a single attestation level, its sidecar path and its developer helpers
- px26-w55 px26-fm01 2026-09-26 wave/paxeer-x-web/2.12 0b928b48a4d610d8f1ce4151a1aca2bd250bc44a Verify a payment receipt by the protocol version it carries in the Python client
- px26-w47 px26-fm01 2026-09-26 wave/paxeer-x-web/2.7 9d259fe4135c1e959ece4a550863a03ee69ea6fa Run the xweb workflow for every tree its legs exercise
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web a49df20e72da4ff5e0a18688b35aea9cac4ac76f Add the task that verifies a receipt by the protocol version it carries in the Python client
- px26-w43 px26-fm01 2026-09-26 wave/paxeer-x-web/1.18r 33b034c3110f3df60646951ed67b3c5d91f470c5 Record the buyer and payload tasks passing with their lint scoped to their own crates, and close the observations earlier tasks resolved
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web f6e0faf4f27ac330af9dc78a9000fa9600911ff2 Scope the buyer and payload tasks' lint to their crates, and add the tasks that settle the metered draw signer and clear the agent workspace clippy findings
- px26-leader px26-fm00 2026-09-26 main d56d05eb07dee670efe346632008b423a09b8d44 Merge the bridge feature wave two: Solana secp256k1 releases, the relayer's Solana observer and submitter, the post-deploy checklist, the runbook, the Solana admin client, the module's governance messages, proposal content, application route and submit command, the pinned platform tools, and the recorded EVM dry run
- px26-w38 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.12 dbcfebf687cd0fe04616b4274ef5c76c5b643909 Fund the Solana dry run's wrapped SOL account exactly by opening it first and syncing the transferred SOL, read the generated governance bodies through the generator's own decoder on the Paxeer side, and record that the dry run now builds and deploys with the pinned platform tools but an initialise can be refused right after the deploy
- px26-w40 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.14 339c988939419390d3cacd9ce7ec317de54e2b26 Mount the bridge proposal on paxd tx gov submit-proposal as layerxbridge-proposal: the subcommand decodes a generated proposal file through the application codec into the bridge proposal, refuses unknown or missing fields and a missing deposit, and builds MsgSubmitProposal from the standard flags; the node mounts it beside the other proposal handlers, the runbook shows the command with each generated file and docs-check asserts it, and the module, node and docs tests pass
- px26-w38 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.12 10984d802bb38c80dae6ec32b2ff184e65bdf70c Restore the governance submit command task and its place in the final gate's requirements, which the platform tools commit dropped from the bridge spec
- px26-w38 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.12 a6796aa0c49fa413cd73ad897c2479669ebe5306 Build the Solana custody program with platform tools v1.56 in the deploy script and the bridge workflow, whose cargo accepts the edition 2024 dependency manifests, and make the deploy-scripts check refuse a workflow step that names another release or none, a declared release older than v1.52, and a build not handed the declared release
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge ed52e889f4dbfb33f8539b5f2905b45d2b8fc345 Add the task that mounts the bridge proposal on the node's governance submit command
- px26-w39 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.13 783af00ad09475c752afd20c6e7ab90599cf9ea4 Make the runbook submit the generated proposals through governance: sections 5 and 6 name the -proposals output, the open-chain and Sidiora cap proposals and the proposal route that executes them, state that the node's submit-proposal command carries only Text content, and docs-check.sh asserts both proposal files, the submit command and the absence of the stale no-message-service claims
- px26-w33 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.3 83386d951dc94c93b5f4f484e5bb5833bdcef82e Release burns to Solana from the relayer: build the custody program's release behind a native secp256k1 instruction carrying the attestor signatures, sign it with an ed25519 fee payer held by the remote signer under its own policy domain, journal the signed bytes before broadcast so a restart rebroadcasts them, complete on an existing nullifier, resubmit the journalled attestation after blockhash expiry, and hold burns whose recipient PDA does not exist yet; the solana_release test replays recorded Solana and Paxeer exchanges and passes
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 71ad8dca6e3ca96c6bb582714e6162bd75a35574 Add the task that makes the runbook submit the generated proposals through governance
- px26-w37 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.11 3fdecaee3400dabdb181b1b1af2fc45d2c5efea0 Route bridge governance proposals through the application: a BridgeProposal content generated from api/layerxbridge/proposal.proto, a proposal handler that executes every carried message through the Msg service for the governance module account and refuses any other authority or a malformed proposal, the layerxbridge route in the governance router, and a -proposals output in the generator that sets Sidiora's cap in a proposal of its own, with handler, application-route and field-for-field decode tests
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge b750599e59a147862b23018e3bb0e230421b5854 Add the task that pins platform tools whose cargo accepts edition 2024 for every cargo build-sbf
- px26-w36 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.7 2d4ee46bf3520f48d53539ad30e2eaf3afa40c1c Add the Solana deployment dry run against a loopback solana-test-validator: deploy, initialise and register through the real deploy script and admin client, deposit and release wrapped SOL and Sidiora against native secp256k1 attestations, read back with the checklist against the real keeper, and record a replayable fixture; it stops naming cargo-build-sbf when the platform tools cannot build the program
- px26-w34 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.6 b75e20ed06a105ef9c807da46088a678bded93d5 Dry-run the EVM vault deployment against a loopback anvil node through the real deploy script and checklist, with deposits, attested releases and a replayable recorded fixture
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 2e1e6eae044d8f90e8d3bda788357cde1d4e283a Add the task that routes bridge governance proposals through the application
- px26-w31 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.9 e5a0abccb9b5f914eac243020d6c0f71e9893b53 Generate the bridge module's governance messages from api/layerxbridge/tx.proto, serve them through a Msg service that refuses any authority but the module's, and write every proposal body under its type URL, with router-to-keeper and field-for-field decode tests
- px26-w32 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.10 c97dbd50c6e084452825c021497c928c63ffef0b Derive the Solana vault authority from the program's vault-authority seed, record a first deployment and stop it before initialise while solana.program_id is a placeholder, and check both offline against the pinned vault vector with a replayed Solana toolchain
- px26-w26 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.4 56de736da168a56e14bff8b862fee874d09d1bea Add the post-deploy checklist that reads each chain and the Paxeer precompile back against the configuration and generated bodies, with a replayed-fixture check, and derive chainconfig handles from bridge/vectors
- px26-w24 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.1 83b5eb898829f8617e12331be525c6796aab96c6 Release Solana custody against attestations the native secp256k1 program verified, with a nullifier per Paxeer burn and program tests of every refusal
- px26-w25 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.2 2184fdb52dfae342e5a8654c54c14f49188ca8e0 Observe Solana custody deposits in the relayer and submit them as bridgeIn, with a Solana RPC seam, an optional solana configuration entry, record-and-receipt refusal and recorded-fixture tests of a full cycle against the pinned vector
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge f75e461e724edb9f3804739d8bae10822d7012c5 Add the governance message service and Solana vault seed tasks to the bridge wave two gate
- px26-w28 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.8 cd5e4fc2572e6e32b04534eca2c661c8d9861f57 Add the Solana admin client that initialises the custody program, registers its assets in configuration order, sets caps, pauses and registers recipients from the chain configuration, with program-test runs of the real binary
- px26-w27 px26-fm02 2026-09-26 wave/paxeer-x-bridge/2.5 38006a2c564848de05be81471d43c8edb766042e Add the bridge operator runbook, a page for each of the nine chains, and a documentation check that proves its link, path and public-text rules bite
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 3939a7cc4ba7ced6eff3e9e9c1d2c967daa0a180 Let the Solana release tests sign with libsecp256k1 and keep recipient registration in identity.rs
- px26-leader px26-fm00 2026-09-26 main 7864043397dc826d0117b8c4b071d34ab194a97c Merge the web search feature wave one: the x-websearch sidecar with its crawler, index, payment gateway and content service, the xweb precompile and the program web_read call, the TypeScript and Python clients, the MCP web tools and daemon route, the workflow, and the follow-up tasks that align the buyer, the sidecar and the web tool with the payment protocol
- px26-w42 px26-fm01 2026-09-26 wave/paxeer-x-web/1.18 bcb8993721231dab64ac500e4655d794de4f7707 Box the perps market payload so layerx-types clears large_enum_variant with its vectors unchanged, and record the layerx-proof argument-count lint that still stops the agent clippy step
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web fedb1955ec6f4fa5cc73dce8274e071f9d47003f Add the task that boxes the perps market payload variant so the agent workspace passes clippy
- px26-w41 px26-fm01 2026-09-26 wave/paxeer-x-web/1.17 3bbca968251aec0f0dc40895a7d2f508a1737ec1 Record the paid-success recording task as qualified: its seller, sidecar, TypeScript, Python and MCP web tests pass on the revision that settles main-account offers and grant evidence, and close its three observations
- px26-w41 px26-fm01 2026-09-26 wave/paxeer-x-web/1.17 a234eff7dd73547afbd94bc1d3133d64c5d2bdc9 Have the buyer check the repeated purposeHash of a grant settlement and refuse one on an exact settlement, pay PAX from and into the main accounts in the web tool and read content by digest unpaid; the buyer, seller and MCP web tests pass, and the MCP clippy step stops on the recorded layerx-types lint
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 66c44c322ec1d820119c4c5cdaee218b7df3f7fe Add the task that settles main-account offers and grant evidence in the buyer and the web tool
- px26-w35 px26-fm01 2026-09-26 wave/paxeer-x-web/1.15 5181049b6d8de682da1ca83aa5076fece0ef920e Pay PAX into the main accounts, have the Seller repeat purposeHash for grant settlements, and re-record the gateway fixtures and client exchange with a paid success in every asset; the seller, sidecar, TypeScript and Python tests pass, and the MCP metered search is refused because the buyer evidence type does not accept purposeHash, recorded as an observation
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 8284206d29f7422c8f63afd74d7d071b55d9b947 Add the task that opens daemon-bound sessions with the web route and the web scopes
- px26-w30 px26-fm01 2026-09-26 wave/paxeer-x-web/1.16 64e62abda06cb30b2d72a0bdcd9820ef3a117165 Route the paid web tools through the bound MCP server: list them for web-scoped sessions and pay tools/call over the web tool behind the approval registry, marking every result untrusted output
- px26-w29 px26-fm01 2026-09-26 wave/paxeer-x-web/1.14 51343fcd74439dbfcbb8526bf616631672f1317c Probe the guest ABI 4 transitions in the programs registration test and write the SDK naming prefix check as a byte string
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web cb09f4109e8b5e6805bd5c4a6eebbb64eccdcb3e Add the guest ABI 4 registration probe, PAX main-account payment, MCP routing, workflow filter and sidecar shutdown tasks, and gate the xweb precompile on the upgrade
- px26-w22 px26-fm01 2026-09-26 wave/paxeer-x-web/1.10 c5859d6d6765ba9f41f085e467e4b8617940bda1 Add the paid web search, fetch and content tool to the MCP server, settling over 402LXP through the approval boundary, verifying the sequencer-signed receipt and refusing content whose digest does not match
- px26-w21 px26-fm01 2026-09-26 wave/paxeer-x-web/1.9 b6a4d7aff6739269a6abcb474b9e97f6511926c0 Add the TypeScript and Python web search clients that pay the sidecar over 402LXP, verify the settlement receipt and refuse content whose digest does not match
- px26-w18 px26-fm01 2026-09-26 wave/paxeer-x-web/1.8 f3c42bc32ab74007e80de07b93eda65bca3ad507 Allocate guest ABI 4 with a web_read host import that returns a program's own committed web answer, backed by committed module storage and the call bridge views, with the SDK reader and its tests
- px26-w23 px26-fm01 2026-09-26 wave/paxeer-x-web/1.13 f7c2f03545bf941b9a27e82eef81daa539f0bd31 Wire the x-websearch binary: search and fetch behind the payment gate, content unpaid, a crawl cycle thread and pages fetched once
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web b8db3a50b8e84a048a3c15cc43542872d2281d63 Add the sidecar binary wiring task that puts the payment gate on every paid route
- px26-w19 px26-fm01 2026-09-26 wave/paxeer-x-web/1.3 3ad721e187571c334f864eb6125e549a187fa846 Crawl the seed list into a local tantivy index and serve ranked search with its canonical bytes
- px26-w14 px26-fm01 2026-09-26 wave/paxeer-x-web/1.4 1e1f0ef25b02a80bac0370290a8c1ce40a18b2fc Sell web search over 402LXP in SID, PAX, USDC and USDL with receiver-signed draws, exact receipts and per-asset payee accounts
- px26-w20 px26-fm01 2026-09-26 wave/paxeer-x-web/1.12 151902d4ee1ee0ffc0e4799e67d601aa868147b1 Bring the pointer tests' addr precompile doubles up to the full binding interface and qualify the xweb precompile
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 3d01a199700244ddc5db93ac12341add064b09bc Add the pointer test double repair that unblocks the xweb precompile gate
- px26-w16 px26-fm01 2026-09-26 wave/paxeer-x-web/1.6 e7a84e4d495868e04aaa8d6e4b71d23b0df8230b Add the xweb precompile with its metered callback, Solidity interface and reference consumer
- px26-w15 px26-fm01 2026-09-26 wave/paxeer-x-web/1.2 982cd108cbb56a77cb88723241ea2a4fffe010af Fetch pages under robots.txt and destination limits, canonicalise their text, digest it and serve the content store
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web 33cb119920899481da9568b9791a70c497eeaaac Allocate guest ABI 4 for web_read and split the kernel binding between the runtime and the request path tasks
- px26-w12 px26-fm01 2026-09-26 wave/paxeer-x-web/1.7 c25c6d81ee81184024a4d7b8e9059420d6dc51f6 Add the kernel web observation activity with its intake, attestor set, root and adapter
- px26-fm01 px26-fm01 2026-09-26 feature/paxeer-x-web c946298ecfe0c9477dad4cefc4c39e1478baa7be Let 402LXP offers name a per-asset payee account and widen the web payment task's touch list
- px26-w10 px26-fm01 2026-09-26 wave/paxeer-x-web/1.5 a510957b7510734b4af1ea9395946d62ee708806 Add the xweb module with its request store, attested fulfilment, fee split, timeout refunds and governance messages
- px26-w09 px26-fm01 2026-09-26 wave/paxeer-x-web/1.1 ab18fd0d42759670339b5b141c7e941303b6e304 Add the x-websearch sidecar crate with its configuration loader, key files and std-library HTTP server
- px26-w11 px26-fm01 2026-09-26 wave/paxeer-x-web/1.11 51c84614781c72dd7d776c7e6b2380ae4bc53d3c Add the xweb workflow and a check that proves its filters and legs are true
- px26-w01 px26-fm01 2026-09-26 wave/paxeer-x-web/0 19c3d0a4d394a8da71b6e81112709f1aaaad199e Add the Paxeer X Network web search sidecar and xweb precompile specification
- px26-leader px26-fm00 2026-09-26 main 788ebde1da03aa2865b4d8a0d037b4a5c8e9523b Merge the bridge feature wave one: the EVM vault under bridge/evm, the Solana custody program, the attestation vectors, the chain configurations and deploy scripts, the governance proposal bundles and the deploy workflow
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge c3faf5e6d6b75c958d9e47f7b5aa71bd091090b1 Reword the bridge spec's process notes in product terms
- px26-fm02 px26-fm02 2026-09-26 feature/paxeer-x-bridge 172275350eebcb693c79e52f7bbb78bbee79c089 Record the bridge wave one gate on the merged feature revision
- px26-w05 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.6 8af2658ce3f331c7907d4aa0e6ced811c15d80a8 Add the one bridge CI workflow and a check that proves its filters and jobs are true
- px26-w05 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.3 fabcee4266a0febc3f14f54227ba1d383bb42517 Document the Solana identity mapping and pin it as digest vectors
- px26-w02 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.2 8761e3abc040c586bae33be20e727276ddef4d11 Add the Solana custody program core for the Paxeer X bridge
- px26-w06 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.5 0e379da1ca6ebb030e61c9b2c0263b925734ce9d Generate the bridge governance proposals from one chain configuration
- px26-w05 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.1 f23384883e4b61647dace074887ed363d84f32c9 Move the EVM bridge vault to bridge/evm and harden it for production
- px26-w03 px26-fm02 2026-09-26 wave/paxeer-x-bridge/1.4 eae099db01929c6379fe125206c2445d24261f0f Add the nine bridge chain configurations with their validator and deployment scripts
- px26-w04 px26-fm02 2026-09-26 wave/paxeer-x-bridge/0 c899a532119a501e531460e58de1314a34de6e7e Amend the bridge specification for the review findings and the Solana admin client
- px26-leader px26-fm00 2026-09-26 main 9d076cdeb9db22c0fa0b118e3f63b13d3b6959da Merge the continuous integration feature: cancel superseded workflow runs per ref across every workflow
- px26-w08 px26-fm05 2026-09-26 wave/paxeer-x-ci/1 9d076cdeb9db22c0fa0b118e3f63b13d3b6959da Cancel superseded workflow runs per ref across every workflow
- Gate the Sidiora fee token behind one upgrade handler (#546)
- Accept governed fee tokens for Cosmos transaction fees (#543)
- Charge and refund EVM gas in the selected fee token (#541)

## 2026-09-25

### Added

- Add the Sidiora bank-backed proxy implementation (#532)
- Add Sidiora sponsored transaction builders to the TypeScript SDK (#529)
- Add account fee-token preferences and precompile (#527)
- Add governed EVM fee-token parameters and keeper readers (#524)
- Add Sidiora gas station quoting and policy core (#526)
- Add signed Sidiora sponsorship to account batches (#523)
- Add the two bounded explorer fleet gates (#520)
- Add the backend lint gate and make the explorer umbrella pass it (#516)
- Add strict Rust linting for explorer services (#502)
- Add the kernel receipt detail page to the explorer (#509)
- Add the Sidiora fee-token spec
- Add the paxeer-x-explorer spec and enable the Codify fleet hierarchy

### Fixed

- Resolve kernel identities through the explorer search path (#508)
- Repair the documentation site build (#499)

### Changed

- Replace fee-token oracle pairs with governed rates (#537)
- Enforce the explorer test ratio and run both gates in continuous integration (#521)
- Make the Blockscout database copy resumable and verifiable (#512)
- Make the explorer frontend lint gate green (#511)
- Make the beta contract and the evidence ledger agree with their sources (#500)

### Removed

- Remove process-environment access from kernel preparation (#504)

### Documentation

- Document the public RPC and WebSocket endpoints for Paxeer X Network

### Housekeeping

- Record the wave 3 explorer gate passing on the merged revision (#542)
- Update README.md

### Other

- Price sponsored gas with an owner-set Sidiora rate (#536)
- Track the vitest environment preset and complete the Paxeer X resource enumeration (#538)
- Run the explorer test gate one umbrella application at a time (#535)
- Render token-denominated fees in the explorer (#531)
- Route allowed fee tokens to holding or distribution (#530)
- Govern Sidiora proxy upgrades through a timelock (#522)
- Register Sidiora under the bridge tokenfactory account (#528)
- Run the explorer deployment definitions on the published fork images (#514)
- Leave one explorer surface and link the control plane to it (#515)
- Decode the remaining kernel precompile events into their own tables (#518)
- Stop the test factory from minting the genesis block (#519)
- Persist the guarantor and deposit-root events of the kernel precompiles (#513)
- Dress the explorer in the Paxeer X product design tokens (#507)
- Declare the six Paxeer X API paths in the OpenAPI specification (#517)
- Render the generated pointers for the Sidiora fee-token spec
- Analyse Go in CodeQL with a build mode Go supports (#498)
- Give every mix command against the explorer backend one containerised recipe (#505)
- Follow the Paxeer X API shapes in the explorer frontend types and mocks (#496)

## 2026-09-24

### Added

- Add CodeQL scanning and CodeRabbit review configuration
- Add the feature-flagged Paxeer X unified account page to the explorer frontend
- Add the Paxeer X Network branding preset to the explorer frontend
- Add the Paxeer X one-account view to the explorer API
- Add the Paxeer X settlement ladder as one pure status function
- Add the Paxeer X kernel tables, schemas and import runners to the explorer
- Add reproducible deployment definitions for the explorer

### Fixed

- Guard the indexing node against concurrent archive-style JSON-RPC bursts

### Changed

- Make the Paxeer X kernel-event write path reach the lx_* tables
- Reconcile docs/wiki against code: modules count, precompiles, duplicate page (#473)
- Reconcile stale documentation across the monorepo (#472)
- Replace the README system-flow image with a Mermaid architecture diagram
- Backfill Paxeer history from the paxscan Blockscout database up to a cutover

### Documentation

- Document the explorer environment variables wave 1 added or changed
- Docs: state that the beta and gateway are not yet open and that there is no public faucet (#474)

### Housekeeping

- Close the remaining wave-9 tasks after their gates passed on the merged tree

### Other

- Read the Paxeer X tables through Ecto and serve the six API paths the frontend calls
- Generate the changelog from git history with git-cliff
- Generate the changelog from git history with git-cliff
- Set the explorer native coin name to Paxeer in both Paxeer X presets
- Explorer frontend: settlement status badge, anchors and receipts pages, Paxeer X search
- Discover which Paxeer X surfaces the node answers, and publish them
- Import realtime blocks as ranges instead of one block per newHeads
- Teach the indexer the Paxeer X chain quirks behind a paxeer_x JSON-RPC variant
- Decode Paxeer X precompile logs into the lx_* rows inside the logs pipeline
- Publish the explorer container images to the container registry
- Tune the explorer's hot tables for 500k blocks per day
- Explorer CI: skip husky in monorepo install, install protoc openapiv2 plugin for service builds
- Size catchup indexing for a sub-second chain and stop retrying untraceable blocks
- Import Blockscout backend v10.2.6, frontend v2.7.2 and MIT services into explorer/

## 2026-09-23

### Added

- Add the governance scripting task for the v6.6 upgrade and consensus parameters
- Add launchpad, exchange and bridge surfaces to the human web app
- Add the Paxeer X fork rehearsal script and validator runbook
- Expose unified account history on the gateway and SDKs
- Add the spot matching module to the kernel
- Wire the exchange, bridge and launchpad modules into the app and the v6.5 upgrade
- Add the layerx-bridge-relayer service
- Add the native launchpad module and 0x1017 precompile
- Add the layerx-indexer service with a decoded SQLite history store
- Add the layerxexchange module and the 0x1015 exchange precompile
- Add the dormant layerxbridge module and 0x1016 precompile
- Add the PaxeerXVault Ethereum bridge contract

### Fixed

- Fix the bridge precompile constructor and the missing-keeper tests after the app wiring

### Changed

- Let the light-client custody credit through the disclosure send-size cap
- Script the v6.6 upgrade and consensus-timeout governance for the Paxeer X fork
- Name the fork upgrade v6.6 in the spec since the fleet binary already carries v6.5
- Name the Paxeer X fork upgrade v6.6 and restore v6.5 to the custody-only plan
- Keep the launchpad accounts receivable and test the missing keeper directly
- Make perps fills open and close positions at oracle-bounded prices

### Housekeeping

- Record the fork surface tasks, wave-9 close-out and the capability-gating and paxscan backfill tasks

### Other

- Pass optional explorer account props only when they are defined
- Gate the fork surfaces on live precompile capabilities
- Route precompile events through the intent router and add typed perps, spot and precompile SDK helpers
- Bring the CLI contract fixture back in line with the gateway
- Skip unfillable makers, derive position growth only from fills

## 2026-09-21

### Added

- Build the explorer read funding profile from the real light-client vector
- Add layerx wallet derive
- Add one-secret account derivation to the TypeScript and Python SDKs
- Serve the public network from beta.layerx.network instead of the apex

### Fixed

- Fix four defects a real bring-up hit on the node and Paxeer scripts

### Changed

- Keep the guarantor waiting when a publication signer is unreachable and ship the signing tool
- Move the beta domain and the remaining service hosts to paxeer.network
- Move the public service hostnames to paxeer.network
- Name app.paxeer.network as the origin in the key derivation message
- Keep publishing when a withdrawal names an unrecorded anchor
- Let the finality RPC client read chunked HTTP responses

### Documentation

- Document one account across Paxeer and LayerX

### Tests

- Qualify the owner custody credit material against the real light-client vectors

### Other

- Derive the EVM account and the LayerX identity from one secret
- Describe the light-client custody formats and stop leaking paxd
- Give the withdrawal journey the custody key the credit's account owns
- Judge a deposit credit at admission against the batch time it will get
- Call the public network beta and point the repository links at Paxeer-X-Network
- Give the custody genesis its deposit-root authority and let the guarantor wait for authorizations
- Teach the guarantor the 223-byte LXBC3 custody profile

## 2026-09-20

### Added

- Add intent and px bindings to the TypeScript and Python SDKs
- Add a trusting period and the reference bounds to the light-client verifier
- Serve the whole network from one JSON-RPC endpoint
- Add a unified network architecture page
- Add the native LayerX anchor module and the layerxAnchor precompile
- Add the native LayerX custody module and the layerxCustody precompile
- Add the Go LayerX verifier and the layerxVerify precompile

### Fixed

- Resolve names in the explorer index through a signed, verified naming read

### Changed

- Use the name Paxeer X Network across root documents and translations
- Move the Rust credit consumers onto the light credit and drop the attestor history mode
- Keep the anchor authority valid after its key associates
- Move checkpoint and bond publishers to the layerxAnchor precompile
- Make the anchor module custody's source of finalized LayerX roots
- Bind a LayerX DID to an EVM address in the addr precompile
- Reconcile the root with the relocated Paxeer chain and retarget every path
- Move every paxeer-network entry to the repository root
- Move the remaining program receipt verifiers to the payer-aware occupancy proof

### Removed

- Drop the testnet framing from public documentation
- Drop attestor key handling from genesis, bring-up and custody tools
- Remove the stale paxeer-docs site

### Tests

- Verify Paxeer deposits by light-client proof inside the bridge module
- Verify checkpoint publication against the anchor's record
- Verify the protocol 3 occupancy transfer root against the payer's proven payment account

### Housekeeping

- Update README.md
- Update README.md

### Other

- Say in the docs that the intent endpoints exist
- Give the explorer and the service one gateway decoder
- Declare the intent operations and serve them from the router
- Give the unified intent router a truthful observed state
- Present the project publicly as Paxeer-LX-Network
- Join both halves of an account onto one explorer page
- Plan one cross-domain movement from a single stated intent
- Authorize the node's own sequencer id and cancel claims as a signable authority
- Fund the anchor escrow and surface module revert reasons
- Drive the human withdrawal test against a real paxd node
- Read the checkpoint proposer from the anchor submission event
- Register deposit roots on the custody module
- Check guarantor publication against the anchor and stop publishing witnesses
- Fund the anchor escrow when the anchor genesis records bonds
- Return a revert reason from the anchor and custody precompiles
- Stop deploying the Solidity settlement suite in the bring-up and sign Paxeer transactions without cast
- Switch the human custody clients and journeys to the layerxCustody precompile
- Switch the bring-up and the custody credit path to the native custody module
- Pin the chain Foundry remappings and gofmt the specgen sources now linted from the root
- Pay the protocol 3 occupancy charge from the payer DID account in the TypeScript, Python and Go SDKs
- Pass the expected call to the agent boundary maintenance test helper
- Deploy the naming program after the owner admission and before the journal export
- Fund the explorer read principal so its name reads pass the kernel's payment gates

## 2026-09-19

### Changed

- Move the native LNI clients up to interface minor 7

### Tests

- Verify protocol-3 terminal evidence in the CLI against the served batch authority

### Other

- Give the pending-artifacts route a receipt scratch the encoder can actually use
- Give program simulation a receipt scratch the encoder can use, and refuse undelivered submissions
- Relay the program head attestation through the node boundary for the hosted registry
- Grant the marketplace calls shared-storage authority and bind the emulator's verified receipts
- Sign the marketplace program calls with a fee limit that covers their declared budget
- Have the sequencer sign the program discovery proof and publish it from the hosted registry

## 2026-09-18

### Added

- Build every npm workspace in dependency order from a fresh tree
- Serve the sequencer-signed program discovery proof the CLI verifies
- Build the hermetic program builder environment during bring-up when the owner supplies none
- Wire agentd startup recovery to real receipt evidence and node budget state
- Wire the reference fiat ramp into the beta cluster bring-up
- Serve the human web application over the HTTPS origin the human service enforces
- Ship the human web application as a bring-up image and workload
- Add the reference mirror signer daemon the publisher keys live in
- Serve a proof-backed GET /v1/state from the hosted gateway
- Implement POST /v1/settle in the gateway and the emulator
- Wire the Ethereum and Solana mirror publisher into the beta bring-up
- Wire the Jev checks into make targets and a non-blocking PR advisory workflow
- Add the Jev docs check for README translation drift
- Add the Jev deps check for cargo-deny advisory triage
- Add the Jev failures check for test, fuzz and fault-injection triage
- Add the Jev pr check for pull request and commit message coherence
- Add the Jev ledger check for qualification severity, closure and duplicates
- Add the Jev advisory tooling foundation: OpenRouter client, report format and CLI

### Fixed

- Fix three bring-up blockers in the beta cluster scripts

### Changed

- Bind the marketplace example to the identity sequence a program call must carry
- Bind emulator receipts to their own batch and run the reference scenario at protocol 3
- Make the x402 buyer quote against the human API's real account and currency contract
- Make the emulator reference-app scenario bring up its own emulator
- Make the last six interop conformance suites first party
- Move the explorer index port-forward off the interop gateway port
- Reconcile the capability ceiling from verified budget recovery at startup
- Make the Solana mirror target optional and derive its deployment record

### Housekeeping

- Regenerate stale docs reference pages and fix the seller-key count
- Complete the program registry source-publication and listing routes

### Other

- Publish receipt-proven program value accounts through the registry reads
- Align the TypeScript SDK and asset docs with the paginated lx_listAssets contract
- Select the merchant receipt protocol version from configuration and read the wrapped program registry envelope
- Select the receipt protocol version from configuration and read wrapped program registry documents
- Align the CLI with the published paginated lx_listAssets contract
- Carry and enforce the LayerX quote terms on every x402 offer the interop adapter touches
- Confirm created protocol budgets from proven core state and add the budget.create operator command
- Deploy the reference naming program during bring-up and wire its id into the explorer index and Human web
- Assert the registry pod's root delegation per container instead of by count
- Deploy the explorer program-read index the human web calls
- Derive the interop conformance suites from this repository's own adapter vectors
- Leave the reference ramp out of bring-up when its owner coordinates are absent
- Read the gateway receipt envelope in the ramps toolkit
- Anchor the marketplace sequence from the DID account listing on hosted
- Derive the interop gateway pins from the vendored specifications and take the rest as variables
- Derive the human passkey relying party from the deployed web origin
- Generate the mirror publisher keys during the beta bring-up
- Answer movement provider readiness from the conditions its executing paths need
- Deploy the interop gateway and accept the receipt authority's real response
- Co-locate the mirror publisher and its signer with the node pod
- Bring the relay/archive node up with the beta testnet cluster
- Decode the core budget record in agentd reconciliation and route operator commands through the audited admin surface
- Anchor the marketplace lifecycle from live state and sign the program call
- Point the reference apps at the emulator and sign the marketplace deployment
- Give the three human providers a real probe subcommand for their readiness gates
- Deliver the owner custody deposit proof and credit material to the in-cluster movement provider
- Publish the receipt authority block on the gateway and emulator receipt reads
- Describe the real beta asset in the node genesis metadata
- Produce the node genesis metadata ConfigMap during beta cluster bring-up
- Authorize the settlement consumers during Paxeer beta genesis
- Settle challenge bonds by pull payment so a reverting recipient cannot freeze resolution
- Run the custody identity check and gate the status publisher CronJob
- Publish the owner custody credit under the per-transaction name the movement provider reads
- Align the on-chain and Paxeer-client Merkle node domain with the native tag
- Point the SDK candidate ABI aliases back at the frozen v2 namespace
- Pass the guarantor checkpoint authority as the deposit-root authority of the beta deployment

## 2026-09-16

### Added

- Expose the committed perps oracle observation to programs
- Add a MkDocs Material documentation site under docs/site
- Add the LXT721 non-fungible standard and reference program
- Add the constant-product swap reference program
- Add multisig and timelock authority kinds
- Add the naming registry reference program
- Add snapshot reads and durable completion paths

### Changed

- Make self-custody the default with a step-up key export ceremony

### Housekeeping

- Record wave 8 verify gates after freeing the stale in-progress slots
- Close out wave 8: record verify gates for tasks 8.1-8.7 on merged main
- Regenerate the human web API client for the key export ceremony
- Record the genesis asset import bound from task 8.6

### Other

- Finish wave 8: task 8.4 gate on the re-indexed graph
- Finish wave 8 gates for tasks 8.2, 8.4 and 8.6
- Report the executing guest ABI version in call terminal evidence
- Mirror the multisig authority result codes into layerx-types
- Allocate guest ABI v3 for oracle_read instead of re-freezing v2
- Mirror the multisig authority result codes into layerx-types
- Refresh platform, human, and interop wiki pages and add SDK quickstarts
- Refresh protocol, programs, and agent wiki from the current sources
- Raise the asset registry capacity to 1024 and paginate listing
- Plan the beta surface expansion wave
- Bound task qualification to one build, one gate and thirty minutes

## 2026-09-15

### Added

- Add public relay/archive nodes with end-to-end sync and failover

### Changed

- Keep beta working records out of the published tree

## 2026-09-14

### Added

- Integrate qualified identity helper for complete handover qualification
- Integrate qualified custody and SessionFee prerequisites for handover
- Integrate release qualification with expired Budget maintenance repair
- Integrate native authority, Human custody and hosted recovery repairs (#305)
- Build the complete Human native fixture before qualification
- Build real Agent custody prerequisites and confine the session probe clock
- Integrate clean native aggregate qualification
- Integrate genesis registration qualification evidence
- Integrate native withdrawal journey qualification
- Integrate authenticated genesis module registration
- Integrate qualification harness target paths and local dependency locks
- Integrate regenerated genesis roots and verified snapshot fixtures
- Integrate real Human CI prerequisites and qualified Agent checks
- Integrate the qualified boundary and consumer changes
- Build real withdrawal prerequisites in Human CI
- Integrate complete Agent sanitizer qualification
- Integrate durable evidence and admission fixture qualification
- Integrate the actual handover verifier in Core recovery tests
- Integrate exact checkpoint association and publication polling
- Integrate the wallet verifier dependency lock
- Integrate funded post-upgrade program execution and exit coverage
- Integrate qualified invoice refusal and finality test linkage repairs
- Integrate the shared dashboard dependency pin
- Integrate exact native genesis parameter qualification
- Integrate the complete hosted clock qualification surface
- Integrate selected Paxeer home and authenticated consensus fixtures
- Integrate searchable documentation with current wallet and receipt guidance
- Add searchable documentation and clarify the available wallet paths
- Integrate current release validation inputs
- Integrate verified builder construction and native SDK test prerequisites
- Create the user namespace explicitly when replaying builder commands
- Integrate current production and publication qualification repairs
- Integrate production HTTPS browser qualification and builder inputs
- Build the Programs toolchain from pinned source inputs for browser CI
- Integrate qualified publication checks into the Human candidate
- Integrate durable onboarding phase separation
- Integrate compiled Human owner composition corrections
- Integrate formatted Human composition and bounded bootstrap decoding
- Integrate explicit clock authority and canonical Human payload APIs
- Integrate verified handover reads with supervised deadlines
- Integrate authenticated checkpoint publication and treasury binding
- Integrate explicit clocks with native owner and authority state
- Integrate subject-bound authority and durable dispatch recovery
- Integrate protected onboarding and publication recipient transport
- Integrate qualified native and principal authority dependencies
- Add KMS sponsor provisioning and authenticated principal store scopes
- Integrate authenticated receipt authority with handover consumers
- Integrate authenticated sequencer history consumers
- Integrate authenticated withdrawal and maintained Programs receipt paths
- Integrate authenticated handover publication and guarantor recovery
- Integrate authenticated rotation continuation and fee sequence validation
- Integrate native owner rotation and explicit module fees with Human receipt verification
- Implement canonical owner rotation and durable managed continuation
- Integrate explicit withdrawal pricing and replay authority
- Integrate authenticated handover runtime qualification
- Integrate qualified native handover and complete maintenance replay
- Integrate qualified managed creation API and recovery corrections
- Integrate canonical native Budget account validation
- Expose canonical header verification to module evidence clients
- Add native module state proof reads
- Integrate hosted proof chains and Human browser journeys
- Integrate native managed creation with verified session authority
- Integrate bound module runtimes with explicit allowance accounting
- Implement sponsored native identities and managed account funding

### Fixed

- Resolve the actual interpreter for confined native artifact reads
- Resolve qualification harnesses in the configured Cargo target
- Resolve the wallet tool verifier dependency in its lockfile
- Fix Paxeer blocktest home and canonical consensus test prerequisites
- Repair publication checks and remove absolute checkout prefixes (#306)
- Resolve Human operations from each principal current native owner
- Resolve authority verification dependencies and strict lint
- Resolve Human operations from each principal current native owner
- Resolve Programs account facts from authenticated batch maintenance
- Restore replica history before attaching the protocol owner
- Resolve the shared timeout dependency for node qualification
- Resolve module proof authority from the authenticated handover chain
- Restore guarantor membership at the saved observation block
- Resolve published native evidence on the configured chain
- Resolve registered checkpoints within actual chain history bounds
- Resolve live guarantor signers from authenticated replay history

### Changed

- Reconcile Paxeer image permissions with main
- Reconcile handover qualification with main
- Reconcile finality feedback with the integrated release source
- Reconcile current integration for complete handover qualification
- Keep expired budgets unchanged during batch maintenance
- Make packaged Paxeer shared libraries readable at runtime
- Keep public initialization directories traversable
- Use the configured production HTTPS origin and CA for the existing browser suites. Shell syntax and Make recipe checks pass; deployed browser qualification remains pending.
- Preserve typed movement codec errors in evidence storage tests
- Keep expired budgets unchanged during batch maintenance
- Keep withdrawal submission within the existing strict lint bound
- Use the authenticated native protocol throughout withdrawal qualification
- Bind native fixture startup to validated settlement and committed account state
- Keep the asset registration selector while parameterizing fixtures
- Use authenticated native withdrawal fixtures in Human journeys
- Bind Governance registration to the genesis handover authority
- Bind handover consumer finality to its verified activity inclusion
- Bind Programs finality waits to verified receipt batches
- Preserve grant refusal reasons before rejecting a second invoice settlement
- Keep guarantors active through post-handover Programs publication
- Keep guarantors online while Programs consumers finalize new batches
- Bind builder construction to an immutable revision and verified Rust mirror
- Reconcile publication validation and beta report evidence with main
- Bind native identity registration to its disclosed owner key
- Preserve owner fixture errors under strict Human lint
- Preserve recipient provisioning retries in the Human integration
- Preserve bound recipient input across provisioning retries
- Separate durable native onboarding phases without holding publication locks
- Separate production service initialization and session construction
- Bind subject balance authority to its verified checkpoint session
- Use explicit owner bootstrap dependencies and format recipient policy
- Preserve native owner evidence ordering and exercise KMS bootstrap recovery
- Bind recipient transport to one supervised deadline
- Keep scoped authority checks explicit and bounded
- Bind scoped receipt authority to its authenticated batch network
- Bind treasury recipient signatures to explicit native policy
- Reconcile shared authority workspace dependencies
- Keep scoped authority checks explicit and bounded
- Bind scoped receipt authority to its authenticated batch network
- Bind Human subjects to durable provider identities and isolated Agent state
- Bind Human subjects to durable provider identities and isolated Agent state
- Make recipient custody dependencies explicit
- Bind native Human onboarding stages to explicit custody sponsorship
- Separate signed Deploy success verification from journey setup
- Use the public account verifier for Programs funding evidence
- Bind handover Programs fee authorization to proven funding
- Separate Programs authority setup and execution checks
- Bind Programs reads to authenticated signer history
- Use supervised clock deadlines for native agent reads
- Bind hosted authority reads to verified sequencer history
- Make recipient custody dependencies explicit
- Preserve the default build target with consensus compiler settings
- Keep consensus objects in general-purpose registers
- Keep maintenance mutation statements explicit
- Keep verified activity response persistence in a dedicated encoder
- Separate canonical batch identity decoding from replica framing
- Bind withdrawal receipts to native ledger settlement and signed requests
- Keep native setup disclosure matching exhaustive without duplicate arms
- Bind rotation continuation to the authenticated identity revision and primary key
- Separate durable owner rotation announcement and revocation stages
- Bind rotation scheduling to the maintained head and separate projection proof checks
- Keep native receipt refusal checks in a focused helper
- Separate authenticated receipt persistence from Human lookup
- Bind native owner receipt ingress to retained signed activities
- Preserve one canonical copy of merged qualification evidence
- Bind native onboarding refusals and budget owner revisions
- Bind explicit native module prices and owner budget revisions
- Bind replay fee debits to the committed occupancy asset
- Bind serial fee authorization to captured schedule versions
- Bind withdrawal grant generation to the canonical sequence origin
- Bind guarantor replay roots to every maintenance event
- Keep native receipt refusal checks in a focused helper
- Separate authenticated receipt persistence from Human lookup
- Bind native owner receipt ingress to retained signed activities
- Use the retained native receipt fixture in authority qualification
- Bind Budget account headers through the public authority fields
- Bind module reads to authenticated historical sequencer authority
- Separate paired handover trust configuration from service startup
- Bind historical handover reads to independently verified publication
- Separate authenticated history assertions in native read qualification
- Use client module configuration in native read qualification
- Separate history selector validation from streaming
- Bind public reads to authenticated sequencer history
- Bind verified history chunks and genesis export to native state
- Keep handover formatting imports at module scope
- Separate bounded publication discovery from receipt verification
- Preserve the checkpoint proposer across handover peer recovery
- Preserve original signatures in availability finality fixtures
- Bind guarantor transport to the current native interface
- Bind replay fee debits to the committed occupancy asset
- Separate durable funding authorization from intent submission
- Use canonical account and native receipt binding APIs
- Use canonical hexadecimal fields in module proof vectors
- Keep native disclosure decoding within strict lint limits
- Bind native onboarding and budget policies to KMS disclosure
- Keep native disclosure decoding within strict lint limits
- Bind native onboarding and budget policies to KMS disclosure

### Removed

- Remove checkout prefixes and repair publication validation
- Remove redundant maintenance receipt borrow
- Remove unused imports from Human signing dependencies

### Tests

- Exercise movement proof export through native custody and live checkpoint publication
- Exercise the real Rust session fee client during native grant lifecycle qualification
- Verify exact Programs checkpoint headers and pending receipt responses
- Verify every CGO library directive and packaged archive part
- Verify canonical native module outcomes in receipt authority
- Verify the original sponsored registration envelope
- Verify the original sponsored registration envelope
- Verify Programs funding against the authenticated current term
- Verify Programs calls through authenticated receipt inclusion
- Exercise hosted receipt authority across a live sequencer handover
- Verify checkpoint publication against the pinned settlement chain
- Verify Programs evidence with canonical batch maintenance
- Verify native withdrawal batch evidence in gateway
- Exercise paid public withdrawals through the native gateway path
- Verify unified module maintenance at the program registry boundary
- Verify rotation fee accounting in each canonical sequence domain
- Verify owner receipt ingestion against an executed handover batch
- Exercise native owner rotation through the KMS provider
- Verify the committed module parameter generation
- Verify owner receipt ingestion against an executed handover batch
- Verify checkpoint publication against the pinned settlement chain
- Verify module state under authenticated historical terms
- Exercise module proofs through the real checkpoint boundary

### Housekeeping

- Record unprivileged Paxeer image qualification
- Record complete handover consumer qualification against main
- Record verified Programs fee settlement conflict
- Record Programs replay repair and remaining live qualification
- Record current handover consumer qualification blockers
- Record passing native aggregate on the authority-bound registry
- Record authority-bound genesis registration qualification
- Record regenerated genesis commitment checks
- Regenerate genesis commitments for the declared governance operations
- Format integrated owner provisioning and recipient transport
- Close signer process pipes after qualification cleanup
- Format principal owner resolution and native evidence cases
- Complete shared settlement verifier dependency integration
- Format principal owner resolution and native evidence cases
- Format withdrawal receipt verification boundaries
- Format maintained activity receipt authority bindings
- Record the additive rotation API compatibility pairing
- Format native owner receipt ingestion
- Regenerate signed genesis with explicit module fee prices
- Record the additive rotation API compatibility pairing
- Format native owner receipt ingestion
- Complete handover publication dependency and formatting integration
- Complete shared settlement verifier dependency integration
- Format native managed creation and account proof paths
- Format native module proof interfaces and regression coverage

### Other

- Replay independently reproduced native terminal refusals
- Release unused receipt encoding capacity during guarantor comparison
- Replay independently reproduced native terminal refusals
- Release signed comparison scratch before receipt indexing
- Recover finality feedback transport within its original deadline
- Release unused receipt encoding capacity during guarantor comparison
- Charge prepared Programs fees before bound module settlement
- Reconnect before Programs receipt proof reads
- Derive insufficient-funds input from authenticated account balance
- Bound receipt replay allocations and fund Programs handover execution
- Initialize batch timestamp before fallible clock read
- Report native batch preparation failure stages
- Set public Paxeer initialization file modes explicitly
- Run browser gates through the production TLS setup
- Export movement deposit proofs from authenticated publication quorum
- Collect early identity CLI refusal status after stdin closes
- Declare direct UI formatter and source map dependencies
- Generate clean UI CommonJS bundles with portable source maps
- Bound mirror image compilation in its isolated Cargo target
- Lock the mirror workspace to its declared Paxeer verifier dependency
- Reconnect the real withdrawal fixture before its first durable submission
- Pass encoded custody calldata through the supported transaction argument
- Report native queued call position and fixture process exit
- Require every native session lifecycle client probe
- Declare and negotiate committed session fee state messages
- Install the Authority process supervisor dependency
- Collect actual native receipts for the maintained withdrawal proof
- Register generated withdrawal genesis through a real local contract
- Run ThreadSanitizer with an instrumented standard library
- Initialize bounded sequencer authority for durable admission coverage
- Provide durable batch availability to multi-activity recovery coverage
- Provide the real handover verifier to supervisor boundary tests
- Fund the upgraded program before exercising its exit
- Retain funded calls across program upgrades
- Validate receive grant binding before invoice replay and link finality evidence checks
- Align the dashboard dependency with the shared locked Next version
- Align the legacy fee record fixture with the Paxeer base denomination
- Supply validated settlement configuration to real seed supervisor tests
- Initialize governance and upgrade fixtures with consensus block time
- Read symlinked bootstrap seed files through their regular targets
- Pin the seven native genesis parameters in availability coverage
- Include the hosted runtime clock in the beta qualification contract
- Supply the native clock fixture and keep receipt verification within strict lint limits
- Run Human production browser checks through the configured HTTPS origin
- Provide declared CI toolchains and verified dependency metadata
- Distinguish the owner envelope from its signer in bootstrap checks
- Pass the configured clock through identity binding reads
- Check native no-follow flags before opening provider state
- Group canonical native event length matches
- Pin activation receipt evidence to its certificate sequencer
- Enable authenticated subject discovery in the hosted Authority
- Connect deployed Human authority to authenticated principal discovery
- Route owner signing through canonical Human protocol interfaces
- Decouple recipient signing from agent tracking
- Pin the authenticated native asset before recipient publication
- Bound owner bootstrap decoding to borrowed fields
- Bound recipient operations to the supervised clock and verify process authority
- Compose owner onboarding with native publication policy
- Compose native owner onboarding with bounded custody publication
- Select receipt proofs from retained activity metadata
- Require supervised deadlines for principal binding reads
- Share the supervised clock capability with identity readers
- Read retained activities through the bound subject outbox
- Resume retained subject submissions after restart
- Release the principal store while native outcomes are resolved
- Support recipient binding through the packaged signer client
- Connect checkpoint publication to typed owner and deposit signing
- Select receipt proofs from retained activity metadata
- Require supervised deadlines for principal binding reads
- Share the supervised clock capability with identity readers
- Read retained activities through the bound subject outbox
- Resume retained subject submissions after restart
- Connect Human recipient signing and public genesis trust inputs
- Provision native Human sponsors with retained KMS custody
- Connect principal custody to private settlement signing
- Clarify recipient binding mutation cases
- Sign native settlement recipient bindings through principal custody
- Carry validated principal context through Human operations and continuations
- Persist native onboarding outcomes and bind sponsor funding
- Match the current native term envelope before verifying its proven prefix
- Configure the Programs journey cursor authentication key
- Pass the maintenance header signature into history verification
- Share the supervised clock capability with identity readers
- Clarify recipient binding mutation cases
- Sign native settlement recipient bindings through principal custody
- Carry validated principal context through Human operations and continuations
- Honor temporary storage selection for disposable settlement chains
- Check the retained state-diff prefix with native log scanning
- Rebuild the replica prefix through the native durable log writer
- Recover the receipt replica prefix before pending batch publication
- Supply original receipt chain to the variant mismatch regression
- Encode withdrawal mutation signatures with the canonical writer
- Propagate fixture decoding failures in withdrawal qualification
- Check exact withdrawal effect ordering
- Validate the complete canonical module set at the gateway
- Encode retained legacy proof fields through the native wire codec
- Link Core to the common hosted timeout contract
- Expire pooled gateway connections before the shared server idle timeout
- Retain original checkpoint attestations across registration and emission
- Declare canonical withdrawals at the public gateway boundary
- Schedule rotation announcements after the complete durable head
- Generate the typed rotation disclosure client
- Confirm explicit rotation timing with operation-bound passkey evidence
- Render native rotation challenges through the shared response type
- Size rotation test arenas for the canonical activity codec
- Initialize rotation fixture with canonical fee parameters
- Clarify receipt authentication callback naming
- Declare owner-approved rotation timing and step-up disclosure
- Derive the expected activity identity before onboarding replay
- Check the complete canonical onboarding account leaf
- Read the native recovery outcome from its protocol receipt
- Establish committed authority before withdrawal delegation checks
- Borrow canonical genesis input in withdrawal qualification
- Commit canonical asset metadata in the daemon allowance fixture
- Commit canonical asset metadata in the daemon allowance fixture
- Generate the typed rotation disclosure client
- Confirm explicit rotation timing with operation-bound passkey evidence
- Declare owner-approved rotation timing and step-up disclosure
- Clarify receipt authentication callback naming
- Open finality feedback transport after durable evidence writes
- Generate native Budget record compatibility vectors
- Commit canonical asset metadata in the daemon allowance fixture
- Wake receipt readers after durable publication becomes visible
- Return explicit finality policy qualification failures
- Canonicalize public genesis module declaration order
- Initialize manifest authority before exporting genesis trust
- Export committed genesis authority for public handover verification
- Match native sequencer identity framing
- Derive the genesis receipt anchor from authenticated state
- Express the bounded handover history interval directly
- Derive native sequencer history from committed governance authority
- Open finality feedback transport after durable evidence writes
- Refuse conflicting committed timing during chain reinitialization
- Configure committed block timing for disposable settlement chains
- Bound idle block production in disposable settlement fixtures
- Honor temporary storage selection for disposable settlement chains
- Apply bounded chain history lookup to publication evidence
- Back publication membership with a real USDL vault deposit
- Retain the exact registered handover certificate signatures
- Fund live handover membership through actual USDL custody
- Read current Budget authority from authenticated checkpoint state
- Classify native creation stages as receipted operations
- Release temporary canonical onboarding encoding buffers
- Share retained native lifecycle evidence with its adapter
- Register the asset module for native budget state setup
- Commit native asset state in budget coverage
- Supply native encoder capacity and propagate test errors
- Clarify native budget validation diagnostics
- Generate signed module evidence through the native producer
- Reuse canonical session grant setup in KMS restart tests
- Extract session validation helpers for strict Human lint

## 2026-09-13

### Added

- Build real settlement tools before Human integration tests
- Build real delivery services before Human runtime qualification
- Build both native availability fixture prerequisites
- Integrate hosted receipt chains with module maintenance envelopes
- Wire committed withdrawal configuration and paid recovery coverage
- Integrate native fee authority with withdrawal schedule support
- Integrate withdrawal fee schedules with bound module replay
- Add explicit managed-agent fee limits to the Human contract and web flow
- Build and consume individual UI component entrypoints
- Expose complete JVM dependency preparation for clean builds
- Expose registry refusal diagnostics with transfer timing enabled
- Integrate authenticated multi-activity receipt regressions
- Integrate qualified receipt-chain regression checks
- Integrate complete maintained receipt-chain verification
- Integrate qualified native and product boundaries for hosted verification
- Integrate authenticated product boundaries for hosted qualification
- Integrate authenticated product verification for hosted qualification
- Add canonical sponsored identity consent codec
- Create the canonical insurance account for enabled Perps genesis
- Integrate durable replay and verified custody maintenance paths
- Add canonical sponsored identity consent codec
- Integrate current native recovery fixes with allowance charging

### Fixed

- Resolve actual JVM consumer graphs before offline builds
- Resolve authenticated maintenance receipts for current Programs state
- Resolve platform policy inputs from their workspace and propagate verifier errors
- Restore Budget state when an epoch hook refuses
- Resolve delegated fee assets from the recorded native schedule
- Resolve maintained outcomes through exact authenticated history evidence
- Resolve maintenance ownership only for records with payers
- Resolve native fee principals and publish committed refusal artifacts
- Restore replay feed position after a partial publication write

### Changed

- Bind Human genesis fixture builds to the checkout revision
- Separate real producer client construction from recovery assertions
- Use the canonical clock reading conversion at service boundaries
- Bind real fixture clock before trait object coercion
- Preserve native fixture decoder refusal in supervisor qualification
- Bind public withdrawals to committed custody and fee configuration
- Preserve authenticated historical evidence during recovery
- Bind epoch escrow transfers to maintained batch evidence
- Preserve canonical module ordering across epoch maintenance
- Preserve legacy operation identities and type fee input validation
- Keep browser server logs outside the durable telemetry record directory
- Split interface patterns into independent component modules
- Use the shared shell context for embedded agent details
- Use the authenticated shell context and explicit checked web values
- Bind telemetry origin checks to the configured public web endpoint
- Keep real authority fixture listener ports distinct
- Bind Programs response codes and retained call metadata to signed evidence
- Bind guest response codes to signed canonical terminal bytes
- Bind maintained authority shape to its native network header
- Preserve native proof fixture bytes across checkout platforms
- Use the verified terminal reference in CLI commitment checks
- Bind registry runtime readiness to prequalified immutable image evidence
- Separate canonical call binding from response decoding
- Use the admitted CPU ceiling for the hosted Programs journey
- Separate hosted runtime qualification stages for strict linting
- Use the component-backed event fixture initialization
- Use the deployed component route for local webhook verification
- Bind TypeScript batch envelopes to their protocol version
- Keep native wallet conformance independent of Cargo target layout
- Bind Programs state reads to the node protocol version
- Use the public digest API for native maintenance records
- Bind maintenance initialization to signed sequence and observation coordinates
- Use explicit even-length checks for maintained receipt encodings
- Keep independent receipt mutation evidence alive during verification
- Preserve authenticated reorg observations through journal restart
- Preserve signed terminal refusal semantics through lifecycle recovery
- Separate portable export verification process handling
- Preserve uploaded archive hash state across initialization retries
- Use a fixed round schedule for bounded archive hashing gas
- Bind displacement observations to the previously included Paxeer block
- Use typed SHA256 rotations and explicit append refusal tests
- Bind registry publication to the canonical unsigned receipt digest
- Bind image publication to complete registry root manifests
- Use the native deployment boundary for registry qualification
- Bind gateway authority to the protocol network and qualify committed receive fees
- Bind authority transport to its configured protocol network and activity roots
- Keep receipt-chain refusal coverage within strict lint boundaries
- Keep shared imports stable across workspace formatting editions
- Bind fee estimates to the actual signed Receive network
- Keep publication waits active across concurrent activity admission
- Use the canonical receipt wait flag in funded qualification
- Bind guarantor transport to the current native interface
- Keep publication waits on the published receipt journal
- Use the current native receipt transport version
- Bind native owner outcomes to signed activities
- Bind Budget debits to admitted account sequences
- Separate native session fixtures from provider restart assertions
- Keep custody key validation explicit in lifecycle submission
- Separate retained lifecycle preparation and evidence checks
- Use the fixed-width custody signature in lifecycle binding
- Bind managed lifecycle outcomes to retained owner activities
- Bind native owner outcomes to signed activities
- Keep requested session identity independent of decoded state
- Bind session issuance and renewal to explicit native fee authority
- Bind session owners to native identities and keep lease units distinct
- Bind session registration and replacement authority to signing disclosures
- Bind prepared metered calls to their committed fee schedules
- Preserve static WASM provenance in bounded linkable archives (#304)
- Preserve authenticated agent and Human operation outcomes (#302)
- Preserve explicit refusal for unsupported daemon reads
- Keep signed receive fixture construction explicit under strict lint
- Name authenticated maintenance payer evidence
- Keep unsupported receive route tags on the canonical refusal branch
- Keep history boundary tests after production definitions
- Preserve Paxeer epoch progress and canonical receipt evidence (#300)
- Bind RPC receipt exclusion to committed account nonces
- Preserve canonical peer receipt hashes and expose trace refusal details
- Bind fee history fixtures to their actual transaction heights and base fee
- Bind missing execution receipts to historical nonce consumption
- Use the validated height throughout oracle migration
- Preserve native authority and durable publication (#301)

### Tests

- Verify withdrawal network through the authenticated activity binding
- Exercise passkey interoperability with a real software authenticator
- Verify signed refusal artifacts in real boundary journeys
- Verify native Programs admission refusals in TypeScript
- Verify native Programs refusals against retained signed calls
- Verify Programs transfer authority against canonical source accounts
- Verify account-bound Programs transfer evidence in Python
- Exercise gateway mutations through the deployed agent boundary
- Verify native supply bindings in TypeScript receipts
- Verify native asset supply fields in Python receipts
- Exercise pinned wallet balance verification in the hosted journey
- Verify wallet account discovery and bind native payment contexts
- Verify smoke effects and refuse credential redirects
- Exercise authenticated native payments in hosted smoke
- Verify maintained Programs state heads through complete receipt chains
- Verify Solana publisher and reader against a runtime-produced manifest
- Verify portable exports with the independent Python receipt implementation
- Verify checkpointed receipts with the authorized sequencer key
- Exercise registry deployment through the real authenticated Agent boundary
- Qualify Human authority clients with a proper private certificate chain
- Verify individual outcomes from a real maintained multi-call batch
- Verify committed budget replay against native signed receipt evidence
- Verify lifecycle outcomes through the shared receipt contract
- Exercise archive publication with authenticated native batches
- Verify funded wallet receipts through the actual consumer and TLS transport
- Exercise funded escrow execution and preserve refused recovery states
- Exercise funded operation recovery against durable native execution
- Verify archived receipts through authenticated maintenance transitions
- Verify live session revocation and replacement membership from native receipts
- Verify session membership against original native Governance receipts
- Verify session intents against original native execution bytes
- Exercise fee insertion at the exact mixed-call storage boundary
- Exercise fee rollback after the delegated call is prepared
- Exercise durable terminal recovery with an actual signed daemon activity
- Verify daemon credit receipts against signed maintenance and head commitments
- Prove same-block nonce conflicts through prior canonical receipts

### Housekeeping

- Complete explicit clocks in real Human service fixtures
- Regenerate beta genesis with explicit withdrawal pricing
- Format withdrawal admission and qualification helpers
- Record configured genesis qualification and legacy request conflict
- Close production fixture logs after child streams finish
- Regenerate distributable interface component entrypoints
- Format shared read verification imports for workspace checks
- Format native read imports for the workspace edition
- Regenerate mixed-log contract for the Paxeer address precompile

### Other

- Always invoke the Paxeer compiler for build targets
- Honor the configured temporary storage for movement fixtures
- Declare disposable custody transaction dependencies
- Install declared cryptography dependencies for custody qualification
- Pass native receipt verification keys by value
- Construct typed refusal for unsupported send vectors
- Centralize Human canonical protocol construction
- Borrow KMS clock ownership and bound supervisor name formatting
- Carry clock authority into scoped Human mutations
- Inject supervised clock authority into timed client and Human operations
- Install settlement prerequisites for native test runners
- Provision confined native processes for CI tests
- Pass the retained maintenance reference directly to chain verification
- Generate the enabled testnet genesis snapshot with the native builder
- Enable the public testnet module configuration across genesis producers
- Refuse guarantor startup after durable replay divergence
- Retain transaction admission facts on custody timeout
- Seed committed asset evidence for epoch transfer regression
- Emit durable default outcomes from Service epoch hooks
- Include the maintenance codec in guarantor replay checks
- Recompute guarantor roots over all maintenance event leaves
- Authorize sequencer handover from committed governance keys
- Authenticate browser performance journeys through public passkey and session APIs
- Read registered passkey identifiers through the public accessor
- Give each performance test sole ownership of its production server
- Isolate production browser telemetry and require the measured route
- Show active plane navigation progress and bound Explorer loading to requested routes
- Generate runtime copy and load only required interface components
- Import server component dependencies without unrelated client entrypoints
- Apply the Explorer script budget to program detail pages
- Identify the off-chain deployment orchestrator as a Foundry script
- Install local JVM dependencies before offline resolution
- Authenticate guest response codes independently of execution status
- Decode historical native Programs refusal call bindings
- Reuse retained call binding across Programs responses
- Reject legacy authority inside current account-bound receipts
- Reject redundant wrappers around native Programs refusals
- Borrow authorized Programs execution expectations
- Authenticate native Programs admission refusals against retained calls
- Unwind image inspection when registry startup is interrupted
- Read the isolation executable from the immutable runtime image
- Lock receipt example dependencies to the current SDK transport
- Align local gateway and core receipt-feed credentials
- Persist registry record directory entries after atomic replacement
- Decode supply-preserving asset pause and unpause receipts
- Persist independently verified registry heads during synchronization
- Check publication scripts with their declared shell interpreter
- Generate payment batch fixtures with the canonical native codec
- Measure registry preparation during runtime qualification
- Decode matching native batch header versions in the Python verifier
- Share the canonical native SEND context commitment
- Require committed state proofs for smoke account discovery
- Wait for pending payment commitment with retained signed bytes
- Authenticate native maintenance heads during Human initialization
- Retain protocol refusal codes in registry authority errors
- Decode native receipt inclusion proofs in the Agent Programs reader
- Decode native maintenance inclusion proofs at the registry boundary
- Share terminal response handling across submission and replay
- Recreate finality tracking from durable displacement during recovery
- Authorize receipt reads in the funded Programs qualification
- Store immutable archive chunks within normal EVM gas limits
- Pin Solana system interface dependencies
- Declare Solana runtime targets and use current system interfaces
- Reuse the archive hash round schedule in bounded groups
- Load archive hash blocks within their allocated memory
- Retain bounded registry worker failure diagnostics
- Pin Solana bank runtime qualification dependencies
- Authenticate uploaded Solana archive bytes before finalization
- Give compression scratch pointers explicit assembly scope
- Persist fulfillment claims before release and reconcile uncertain outcomes
- Bound streaming archive compression cost per chunk
- Refuse incompatible terminal fiat translation state before execution
- Identify registry head refusal under enabled transfer diagnostics
- Authenticate complete archive content during Ethereum finalization
- Configure the registry runtime image and quota filesystem explicitly
- Pass registry credentials through the canonical bearer encoder
- Protect the registry trust history with its required private ownership
- Run RPC deployment checks with the actual registry dependency
- Select qualified registry binaries explicitly for the runtime journey
- Assert the committed grant issuance fee in the public RPC journey
- Return the verified activity transition to Human receipt lookup
- Declare constant-time comparison in receipt-chain serialization
- Construct an independent signed chain for signature refusal
- Authenticate complete archive receipt chains and bind registry replica identity
- Bound maintained history to native batch capacity
- Authenticate complete maintained receipt transition chains
- Carry the complete authenticated receipt chain through hosted authority
- Publish authenticated deployments before completing Programs lifecycle operations
- Report malformed native archive fixture evidence without unchecked expects
- Bound funded sends to the accepted five-minute validity window
- Authenticate refused lifecycle outcomes and traverse protected parent directories
- Assert committed genesis RPC behavior and isolate qualified native executables
- Track asynchronous Programs execution and handle WebSocket control frames
- Give concurrent funded journeys distinct custody evidence directories
- Refresh treasury proof authority after preparation seals and verify refusal artifacts
- Replay authenticated refusal outcomes without misclassifying execution
- Distinguish account proof selector and authority refusals
- Align interoperability dependency resolution with authenticated SDK transport
- Share complete receipt outcome verification across mirror consumers
- Report typed authority errors in the network binding regression
- Package the safe data directory helper with supervisor qualification
- Initialize native authority state in durable admission tests
- Decode sponsored account commitments as fixed digests
- Lock the receipt authority signal dependency
- Drain receipt authority connections on termination
- Report canonical admission refusal details
- Identify failed durable admission crash phases
- Back checkpoint membership with a real USDL vault deposit
- Track acknowledged owner publication through bounded transient refusals
- Retain bounded authority refusal diagnostics in the owner journey
- Wait for durable publication before retaining owner authority evidence
- Distinguish authority endpoint and certificate issuer identities
- Supply authoritative genesis metadata and an end-entity TLS certificate
- Enroll custody test guardians through separate processes
- Declare custody transaction dependencies
- Refresh generated CLI dependency metadata
- Refresh platform dependency metadata for session authority verification
- Derive identity sessions from checkpoint-bound Governance history
- Reconnect the module journey after its deadline wait
- Present Budget account admission during kernel execution
- Commit authenticated module custody accounts through prepared transitions
- Pin maintenance evidence to the configured sequencer identity
- Decode sponsored account commitments as fixed digests
- Retain the verified registration outcome during session resume
- Select native protocol for owner lifecycle receipt fixtures
- Check lifecycle fixture signatures before contract execution
- Retain original native session outcome evidence
- Check native governance receipt projection in session parity
- Version executable session intents while preserving legacy vectors
- Retain browser registration bytes across submission retries
- Revalidate prepared storage capacity after fee accounting
- Pin historical paid-grant receipts in native replay coverage
- Assert the declared program ABI mismatch refusal
- Decode scheduled capabilities using the authenticated call ABI
- Report the exact metered preparation failure
- Activate allowance fixtures after metering genesis materialization
- Compare canonical receipt preimages in legacy allowance replay
- Require the admission timestamp before fee reservation
- Activate allowance accounting through canonical genesis state
- Bound delegated native fees with owner-issued versioned budgets
- Authenticate separately published artifacts in metered daemon journeys
- Retain the complete main qualification ledger prefix
- Package pinned static WASM archives as directly linkable bounded artifacts
- Supply native read deadlines from the process clock boundary
- Describe authenticated native custody credit acceptance
- Share the signed reclaim fixture across complete journey assertions
- Distinguish account proof selector and authority refusals
- Authenticate maintained batch transitions before recording Human terminal outcomes
- Carry signed payer grants and receiver authority through reclaim journeys
- Read native participants from the canonical decoded activity type
- Check canonical receipt positions before unsigned conversion
- Read fee fixture contexts from their committed historical stores
- Commit historical store versions used by RPC fee fixtures
- Assert successful gas price responses before decoding their value
- Exclude proven unconsumed nonces from historical gas calculations
- Check the decoded EVM transaction before nonce evidence assertions
- Recognize absent history anchors through wrapped file errors
- Propagate custody history durability and range failures
- Wait for listening admission sockets before starting daemon clients
- Import the Asset activity contract in fee principal regression coverage
- Wait for the authenticated replica maintenance attachment before checking retained evidence

## 2026-09-12

### Added

- Integrate published batch readiness with withdrawal qualification
- Integrate qualified custody evidence with withdrawal fees
- Add versioned withdrawal pricing to canonical fee schedules
- Integrate signed grant issuance and canonical account migration
- Integrate maintenance verification and qualified workspace repairs
- Integrate strict agent lint and formatting repairs
- Integrate qualified native build repairs
- Serve authenticated native evidence and paginated history through agent reads
- Create the Paxeer runtime home for its service user
- Serve verified native activity and maintenance history pages
- Integrate verified custody publication with availability qualification
- Integrate authenticated custody funding with daemon allowance charging
- Integrate verified TLS dependencies with Comet custody consumers
- Integrate the qualified account proof publication fixture
- Integrate canonical Human qualification and public RPC methods
- Integrate qualified main changes with Comet custody evidence
- Implement published identity registration and faucet RPC methods
- Integrate current main with identity RPC qualification
- Integrate strict platform workspace validation
- Integrate qualified Comet custody transport
- Integrate platform qualification repairs
- Integrate complete availability and Comet proof transport
- Integrate qualified Comet transport and current custody consumers
- Integrate complete availability verification
- Integrate qualified explorer authority configuration
- Integrate the qualified explorer authority configuration
- Expose bounded Comet custody proof reads through the TLS boundary
- Integrate real custody deployment and daemon harnesses
- Integrate current Human disclosure qualification
- Integrate current native and workspace repairs
- Add typed Comet custody state proof verification
- Integrate canonical grant issuance and snapshot migration
- Integrate qualified canonical grant issuance
- Integrate strict workspace lint repairs with canonical grants
- Integrate qualified agent formatting and lint repairs
- Integrate qualified native build repairs
- Integrate canonical grant issuance and generated genesis inputs
- Wire payment, program, journey and approval producers into the internal event source
- Add hosted grant draw qualification checkpoint
- Add native fee estimates and authenticated RPC watches; CLI tests and lint pass
- Add wallet commands and explicit payment capability refusals
- Add the public positional JSON-RPC client with strict response validation
- Add canonical wallet asset payload encoders and fixtures
- Serve the faucet tool through the daemon MCP path under its own operation

### Fixed

- Correct optional dependency metadata and retain strict integration checks
- Resolve the wallet tool dependency lock against its workspace
- Resolve occupancy history through authenticated account ownership
- Repair strict lint failures in enrolment and supply receipt tests
- Restore distinct merged observation headers; beta-ledger-check passes
- Resolve the simulation and emulator authority the way an executing node does

### Changed

- Bind fee replay to committed schedule and parameter revision
- Preserve the receipt response contract after verification
- Bind deployed images to verified release evidence and preserve recoverable outcomes
- Preserve hosted outcomes and validate boundary inputs
- Separate maintained account activity binding checks
- Use the independent digest implementation in maintenance codec tests
- Bind module runtimes and receipt deterministic batch maintenance
- Bind the module runtimes in layerxd and the guarantor and drive the epoch hooks from a kernel epoch transition
- Name native receipt coverage by the verified authority invariants
- Bind history to requested ranges and native account participants
- Separate native read qualification stages
- Bind terminal recovery to the persisted signed receipt reference
- Make availability fixture encoding failures explicit
- Keep native read cursor handling lint-clean
- Preserve balance evidence levels and repair initial journey progress
- Keep native proof storage compact and cover subscription authority
- Bind native credit recovery to finalized custody evidence
- Bind client evidence and preserve durable operation recovery
- Preserve epoch recovery and validate EVM and oracle state transitions
- Bind native proofs and idempotency to their committed operations
- Enforce transfer authority sequences and native input boundaries
- Preserve Stream runtimes in private allowance execution
- Use explicit system trust for public TLS clients (#296)
- Separate certificate issuance steps for strict test lint
- Preserve case-insensitive HTTPS trust selection for simulation clients
- Use active Rustls connectors with explicit system and private trust roots
- Bind Human spend evidence to its committed activity tree
- Use protocol account identifiers for KMS Send authorization
- Bind all monetary disclosure roles at the Human KMS boundary
- Use explicit fixture failures in history window assertions
- Keep custody proof results separate from library diagnostics
- Preserve platform validation while satisfying strict workspace lint
- Separate MCP installation and catalogue assertions
- Separate treasury send input validation from signing
- Use concrete consensus keys in custody verification
- Use the iterator position for availability class checks
- Enforce the native canonical availability chunk partition
- Use the Comet protobuf JSON proof field
- Preserve stricter caller limits for disposable chain qualification
- Bind signed receipt fixtures to the committed activity tree
- Use real send authorization signatures in Human compiler tests
- Preserve stricter caller limits for disposable chain qualification
- Keep served binding verification in a focused assertion helper
- Separate binding publication assertions from session lifecycle checks
- Use canonical Stream custody accounts in allowance fixtures
- Keep the genesis module key prefix length explicit
- Keep event hash domain lengths explicit with terminated literals
- Bind metered transfers to private execution scopes
- Keep the sequencer seed out of sequencer.env and hand it to layerxd from the supervisor
- Keep the upstream ledger and daemon behaviour the rebase had displaced
- Reconcile the rebase onto main with the merged behaviour
- Bind wallet writes to authenticated account snapshots
- Bind program signers to canonical payment accounts and settlement commitments
- Reconcile overlapping public read implementations; qualification pending
- Enforce selected payment commitments without receipt downgrades
- Name payment disclosure fields and payload codecs
- Preserve merged observations under unique IDs; ledger check passes
- Bind payment payloads to complete signer disclosure
- Name the publisher mode at the cluster callsite and bound the publish job's disk

### Removed

- Drop ledger blocks that duplicate upstream records and follow the published contract

### Documentation

- Document the daemon-bound MCP install and serve surface in the wiki
- Document the treasury signer socket in the core wiki and cover its refusal
- Document MCP payment limits and the identity-read blocker; shell examples parse
- Document wallet and token commands in the CLI wiki and install guide
- Document token execution and test wallet trust refusals

### Tests

- Verify withdrawal fee quotes through the running daemon
- Qualify gateway reads with its production event services
- Prove maintenance replay refuses tampered state atomically
- Verify versioned maintenance in publication and historical replay
- Verify signed batch maintenance envelopes in native clients
- Verify native module receipts through their declared state and custody effects
- Prove Programs artifact bindings and streamline terminal evidence verification
- Verify atomic principal updates and isolate availability checks
- Verify offline availability and reconcile Human submission outcomes
- Exercise access list journal rollback with complete slot storage
- Exercise crash boundaries in asynchronous publication workers
- Verify persisted availability bytes and exhausted asset registry sequences
- Exercise custody consumers after authenticated history pruning
- Verify complete availability bundles and bind their canonical records
- Exercise custody attestation against one real Paxeer process
- Qualify signed issuance snapshot migration and native regressions
- Qualify the grant registry and custody genesis input
- Verify complete simulation evidence in metered daemon tests
- Verify RPC receipt commitments with explicit trust policy

### Housekeeping

- Record withdrawal fee and crash replay qualification
- Record the native withdrawal fee encoding fixture
- Complete oracle migration after normal cache iterator exhaustion
- Sync publication files and directories without flushing unrelated data
- Record availability publication and complete workspace qualification
- Record custody consumer checks after TLS dependency integration
- Record public TLS qualification and remaining integration failures
- Format explicit TLS trust configuration and server identity cases
- Format TLS identity qualification cases
- Record Comet custody qualification and withdrawal fee refusal
- Record sealed account proof fixture qualification
- Format sealed receipt proof fixture
- Record Human signing and receipt qualification
- Format Human KMS monetary signing coverage
- Record identity RPC client qualification and remaining contract conflicts
- Format public identity RPC support
- Record platform lint and focused integration qualification
- Format extracted registrar qualification checks
- Format custody verification and render protocol requirements
- Record real Comet proof transport qualification
- Record complete availability verification qualification
- Format availability verification and canonical evidence cases
- Record explorer CA configuration qualification
- Format explorer authority configuration coverage
- Format Comet transport and resolve its existing serde dependency
- Record shared daemon controls and the Paxeer evidence conflict
- Record complete Agent receipt qualification
- Record Human send signature qualification
- Record canonical grant integration qualification
- Record agent lint qualification and remaining receipt blockers
- Format native authority fixtures and Programs transfer paths
- Record grant qualification and reproduced baseline blockers
- Record missing native fee authority for delegated calls
- Record native GCC and Clang qualification
- Record native qualification and regenerate the beta report
- Record the inherited qualification failures found while replaying the wallet CLI onto the current base
- Record RPC qualification and remaining shared integration blockers; ledger check passes
- Regenerate CLI SDK dependency lock; locked metadata and contract test pass
- Record outstanding commitment verification interfaces; ledger check passes
- Record the boundary read contract decisions and the fixture that blocks the gate

### Other

- Replay recovered withdrawal batches through the guarantor
- Load production event credentials from protected files
- Publish complete deployment exports and replay settled payments
- Initialize maintained upgrade batches in a valid sequencer term
- Bound isolated chain and contract build concurrency
- Enable genesis module flags only for their lifecycle scenario
- Run funded daemon scenarios on an isolated beta chain
- Initialize fixture roots and verify maintenance envelope boundaries
- Charge every epoch hook of a transition against one capacity budget
- Identify identity client operations in transport failures
- Share certificate trust with subscriptions and reject incomplete write transcripts
- Validate mirror process outcomes and bound custody intent storage
- Recover terminal submission bookkeeping and enforce feed continuity
- Carry authenticated custody credits through Human intent execution
- Start admission and replica recovery from the recovered publication frontier
- Allocate replay receipt storage for the retained batch count
- Reexecute prepared Programs batches before recovering their publication
- Check the initial durable marker generation after refused group commit
- Roll back replica execution when durable publication fails
- Wait for completed batches in the real availability fixture (#298)
- Report the observed publication frontier on fixture timeout
- Wait for each availability fixture batch to finish publication
- Wait for the sealed availability fixture batch before proof checks
- Encode the complete Programs entrypoint in live allowance calls
- Wait for published preparation state in metered daemon tests
- Retain authenticated simulation evidence in metered daemon tests
- Pass the selected build directory to the metered daemon gate
- Authenticate Paxeer custody with Comet state proofs (#297)
- Reuse the Agent TLS qualification module without duplicate inclusion
- Align maintained native TLS dependencies and build real TLS test prerequisites
- Canonicalize RPC URLs before trust loading and issue valid TLS server certificates
- Align public TLS trust configuration and WebSocket client dependencies
- Wait for sealed batch visibility in the account proof fixture (#295)
- Require the protocol receipt variant in the publication fixture
- Wait for sealed receipt evidence before account proof export
- Encode canonical Human KMS monetary roles and signed qualification inputs
- Retain the generated Human disclosure regression case
- Distinguish custody key and Send fixture inputs
- Isolate principal store initialization in Human startup
- Authenticate Human journey Send and receipt fixtures
- Encode RPC test proof bytes without intermediate strings
- Support identity methods in the shared public RPC client
- Refuse blocking custody key files and retain verified history window fixtures
- Generate Comet custody fixtures from authenticated chain state
- Check real custody verifier CLI returns one JSON document
- Decode published genesis with the canonical Comet codec
- Refresh the CLI fixture from the published RPC contract
- Extract platform validation and integration checks for strict lint
- Respect the native historical proof query rate in transport qualification
- Retain real Comet proof anchor context during qualification
- Require JSON objects at Comet RPC boundaries
- Require explicit CA trust for explorer program reads
- Persist authenticated Comet history and typed custody credits
- Retain public custody header and storage proof responses
- Authenticate disposable custody endpoints through the real TLS boundary
- Bound isolated chain and contract build concurrency
- Enable genesis module flags only for their lifecycle scenario
- Run funded daemon scenarios on an isolated beta chain
- Bound isolated chain and contract build concurrency
- Enable genesis module flags only for their lifecycle scenario
- Run funded daemon scenarios on an isolated beta chain
- Check shared metadata lengths and clean remaining agent lint errors
- Refresh the grant client connection after maintenance
- Issue canonical capability and budget grants through governance
- Migrate retired issuance accounts before canonical acceptance
- Include module identifiers in send allowance tests
- Include batch identity declarations in metered transfer tests
- Initialize the snapshot lineage atomic without a deprecated macro
- Install the guest compilation target for native CI
- Locate the escrow guest in the configured build directory
- Publish the MCP binding from the agent daemon and repair the hosted install journey
- Write the MCP daemon binding during agent enrolment and drop the gateway key from install mcp
- Match the registry producer test to the identity binary and serialise the program journal
- Call the stream journal append as the associated function it is
- Carry the resolved grant's live allowance through every daemon transfer and persist the charged scope
- Route every core boundary spawn through one binary-path helper
- Sign the hosted core's treasury sends through the signer socket
- Re-render the beta go/no-go report after rebasing the Asset withdraw dispatch onto main
- Dispatch Asset withdraw through the real transition and bind pause and unpause supply
- Deliver the sequencer seed to every daemon test launcher that sources sequencer.env
- Load the metadata helper by path in the bootstrap test and refresh the node test line references
- Start a real layerxd from the hosted node harnesses
- Carry achieved verification levels on DID account listings and verify them in the SDK
- Track the published RPC contract after rebasing onto main
- Rebuild the rebased ledger, contract fixture and CLI citations against main
- Enable secure hosted CLI transports
- Lock the wallet RPC dependency graph
- Reproduce inherited Programs aggregate failures at the merge base
- Pin LXT-20 refusal vectors and record payment qualification blockers
- Admit canonical native payment activities
- Publish verified receipts concurrently with funded SENDs
- Instrument and reduce hosted SEND latency
- Reconstruct Asset state from committed registry metadata
- Mount the core receipt event token and resolve the SDK lock dependency
- Connect wallet writes to native signing and identity sequence RPC
- Refresh published RPC contract fixture; positional contract test passes
- Align wallet and token CLI wording with what the commands actually do
- Execute disclosed token writes through the wallet SDK
- Encode independent Send sequences and verify bytes against native C
- Reuse shared asset encoders behind the existing CLI fixture API
- Recognize asset accounts when the DID contains a namespace marker
- Sign canonical asset payments through the shared disclosure signer
- Accept canonical per-asset agent accounts with unchanged account derivation
- Route wallet and token MCP tools through scoped daemon operations
- Point the pay1 fixture observations at the current test line numbers
- Align the genesis and module-registry tests with the advertised Asset ordinals
- Refresh the emulator identity from governance before resolving authority
- Register the emulator's state commitment modules from the genesis table
- Define the batch execution identifier once and make the wire crate select the way the daemon binds
- Describe the empty DID listing refusal in the public read documentation
- Hand the supervised bootstrap its settlement document and refuse an unproven DID listing
- Report public read refusals with the message their code names
- Give the gateway a registrar identity role that cannot mint sessions
- Retain the genesis request beside the genesis artifacts and speak LNI 1.5 in the availability harness

## 2026-09-11

### Added

- Serve the daemon-bound tool catalogue from layerx mcp serve
- Create draw test databases under the system temp directory
- Add WebSocket unsubscribe and a per-topic resume cursor
- Wire the hosted gateway to identity provisioning and to the faucet
- Build the identity provider before the hosted Human provisioning tests

### Changed

- Make the mirror image resolvable and part of the published image set
- Split the payment disclosure decoder into per-activity helpers
- Reconcile the 402LXP lane with the merged transport and asset work
- Keep 402LXP transport failures distinct from pending
- Bind payment payloads to complete signer disclosure
- Enforce selected payment commitments without receipt downgrades

### Tests

- Verify maintenance heads and refuse forged registry ingress before Human assembly

### Housekeeping

- Record the decoder decomposition and the remaining agent clippy denials
- Record the daemon-bound serving path in the beta ledger
- Record the admission harness going green from the shipped genesis metadata fixture
- Record the red genesis manifest target and the aggregate test coverage gap
- Record the widened asset price table and the supply binding decision

### Other

- Map the hosted agentd and middleware examples surfaces to real gates
- Give a repeated entry in a canonical hash set its own result code
- Rebuild the account index when the emulator imports a snapshot
- Grow the account registry instead of refusing at 512 accounts
- Describe the identity-sequence faucet path, asset symbol refusal and harness state guard
- Refuse a corrupted asset symbol and separate the hosted identity sequence
- Leave the published gateway read contract alone and record what the harness cannot qualify
- Admit canonical native payment activities
- Instrument and reduce hosted SEND latency
- Reconstruct Asset state from committed registry metadata
- Allow canonical Asset ordinals through daemon admission and replay
- Mount the core receipt event token and resolve the SDK lock dependency
- Route wallet and token MCP tools through scoped daemon operations
- Wake authenticated receipt lookups when the executor publishes a commit
- Hold the treasury key in a signer process behind a unix socket
- Tie every copy of the LNI version to the schema and refuse an old minor as unsupported
- Emit the accepted asset record shape from the shared genesis metadata fixtures
- Give the mirror image the whole build context
- Meter send and receive history instead of bounding it at sixty-four
- Price asset pause and unpause in the version 2 fee schedule
- Charge the resolved grant on every delegated debit leg
- Persist the identity tenant and scope principals and sessions to it
- Carry container images through the release artifact manifest
- Give the nested programs build its own cargo target directory

## 2026-09-10

### Added

- Add a standalone layerx-verify binary pinned to the genesis trust root
- Implement staged Asset accounts and conserved issuance with passing focused ledger gates
- Add the LXT-20 settlement guest and bound registry interface fixtures
- Add the program-funded merchant split guest
- Add bounded LXT-20 request encoding and runtime parser vectors
- Add program account preparation with runtime encoding parity
- Serve public self-service registration through lx_register
- Add authenticated PAY2 execution evidence and funded coverage
- Add hosted grant draw qualification checkpoint
- Expose disclosed native Send debit signing and shared envelope encoding
- Expose public RPC asset reads, fees and authenticated subscriptions
- Add wallet RPC execution and verified commitment waits
- Add funded gateway SEND latency gate with native sequence blocker
- Expose sequence and inclusion-proof reads with real receipt latency coverage
- Add public RPC read subset and verify core and gateway gates
- Integrate Python grant budgets and environment-driven public payment examples
- Add buyer grant payment headers and activity-bound settlement capture
- Add public RPC and faucet payment examples with exact response contracts
- Add Python HTTP payment handling with persistent receipt replay protection
- Add asset account, revoke and supply payload codecs with passing parity tests
- Add canonical grant and receive codecs for Python and TypeScript
- Add real LNI restart qualification and record the account-open blocker
- Add explicit salt sourced migration for version two Asset records
- Implement staged Asset accounts and conserved issuance with passing focused ledger gates

### Fixed

- Restore vendored Programs test coverage
- Fix strict Platform supervisor lint
- Resolve snapshot payer balances with the activity protocol version
- Restore pinned parity-wasm resources for the existing tests
- Correct payment documentation against the served surface
- Correct payment branch capabilities and asset execution documentation
- Resolve activity authority from persisted grants instead of synthesizing it
- Correct the formatter hunk counts in the core boundary observation
- Restore the inherited formatting of the core boundary genesis metadata calls
- Resolve the metadata helper include when adapting the core fixture
- Restore the send latency diagnostics in the local gateway lifecycle test
- Restore the pending receipt lookup inside the publication wake loop
- Restore the blocker severities dropped from two ledger observations
- Repair rebased daemon helper boundary so both identity admission and asset admission remain
- Restore persisted Asset metadata and enumerate DID accounts; native build and account gates pass

### Changed

- Make Asset pause and unpause real transitions
- Bind guarantor bond state to verified Paxeer receipts
- Keep vendored crates outside Programs test targets
- Bind emulator Programs authority to signer DIDs
- Bind Programs identities to their payment accounts
- Reconcile the agent API workspace lockfile
- Reconcile the platform registry SDK dependency in the lockfile
- Bind wind-down fixtures to signer principals and payment accounts
- Bind program signers to canonical payment accounts and settlement commitments
- Enforce dynamic descriptor limits on the grants available to guest execution
- Bind dynamic interface spending to caller grants at native call admission
- Bind the guarantor membership mirror to the GuarantorBond deployment
- Move the unsupported activity example off the now disclosable mint ordinal
- Separate the colliding observation identifiers in the beta ledger
- Bind wallet preparation to identity sequence RPC
- Preserve generic asset dispatch admission
- Reconcile merchant settlement receipt digests
- Name payment disclosure fields and payload codecs
- Bind 402LXP grants to payer and purpose
- Separate dashboard configuration and bind hosted traffic to destination pods
- Preserve requested RPC commitments and expose identity sequence selection
- Bind payment payloads to complete signer disclosure
- Separate gateway account sequence reads from SEND envelope sequences
- Use an empty DID for the invalid sequence selector test
- Reconcile overlapping public read implementations; qualification pending
- Enforce agent payment commitments before budget settlement
- Bind payment RPC receipts to signed activity and payer facts
- Enforce selected payment commitments without receipt downgrades
- Preserve generic asset dispatch admission
- Use the module value namespace for Asset issuance accounts
- Reconcile native ASCII symbols without relaxing legacy validation; ledger check passes
- Name per-asset accounts and reserved withdraw ordinal in the modules wiki

### Documentation

- Document payments quickstart, public RPC, assets, and commitments
- Document the one-record-per-asset account model
- Document the eighteen MCP wallet and token tools
- Document LNI minor 5 reads and publication waiting; pass schema and ledger checks
- Document native asset ordinals and issuance accounts in the protocol design

### Tests

- Verify account-bound Programs settlement evidence across SDKs
- Exercise native LXT-20 custody and merchant settlement round trips
- Qualify merchant split effects and retain native integration blockers
- Prove public account and balance reads at the core and verify them in the SDK
- Prove payment fixtures with native grant encoder
- Qualify hosted 402LXP grant draws and renewal
- Verify SEND authorization without conflating account and identity sequences
- Exercise exported payment and wait scope refusals without bypassing receipt gates
- Verify canonical receive authorizations and preserve signed receiver sequences
- Qualify funded SEND receipts and reduce transport and notification waits
- Qualify the rebased native payment lane on current main
- Qualify Asset version-one fees, genesis registration and guarantor replay
- Test Asset issuance, prepared accounts, receipt binding and replay; make test passes

### Housekeeping

- Record the qualified gate results for the token and payment branch
- Format the agent-boundary node test call this branch introduced
- Record the AssemblyScript SDK install gap and the stale payments wiki claims
- Record completed PAY5 aggregate qualification
- Record PAY5 payment qualification evidence
- Record the wasmi representation conflict without suppressing tests
- Regenerate the agent API contract with canonical Rust formatting
- Record Programs aggregate qualification and remaining integration failures
- Format the owned Programs packages for the scoped formatter gate
- Record the checkpoint ordering closure and the missing settlement deployment record
- Format the genesis metadata call sites in the core boundary target
- Record the repair of the websocket upgrade request
- Record the duplicate connection header in the websocket upgrade gate
- Record the repairs and the remaining integration contradictions
- Record the workspace lock, fixture adapter and remaining gate findings
- Record the SDK websocket dependency in the root workspace lock
- Record the layerx-crypto dependency of layerx-mcp in the platform lock file
- Record the mint disclosure and withdraw refusal contradiction
- Record the layerx-crypto dependency of layerx-mcp in the agent lock file
- Record payment signer qualification and unresolved integration contracts
- Record PAY6d qualification and runtime blockers
- Record passing payment package gates and inherited conformance failures
- Record pending native metered draw and renewal commands
- Record payment integration qualification and include all payment tests
- Complete HTTP payment retries with verified settlement capture
- Record native Send phase timing comparison
- Record passing native and client gates with unresolved runtime boundaries
- Record native timing diagnostics and the blocked Send comparison
- Record the signed snapshot migration boundary; pass the ledger check
- Checkpoint event driven receipt lookup and native timing probes
- Checkpoint committed Asset and fee reads with typed LNI calls
- Record native account read handoff and public RPC ownership boundary; ledger check passes
- Record obsolete Asset type documentation and required corrections; ledger check passes
- Record remaining daemon registry synthesis and restart gap; ledger check passes
- Record actor-bound Asset derivation and passing refusal test; ledger check passes
- Record existing typed Asset execution and passing focused qualification; ledger check passes
- Record passing native Asset build and aggregate qualification gates
- Record passing Asset build, replay, daemon and ledger gates with remaining coverage boundaries
- Record payment decoder qualification and remaining execution boundaries

### Other

- Check the hosted topology by named edge and run the agentd checks
- Fund a new signer from the public RPC surface through the hosted faucet
- Stamp the executing module identity on ledger receipts
- Speak LNI 1.5 in the native admission client
- Run every module test from the aggregate gate and repair the two link rules
- Derive the genesis request length bound from the certificate threshold
- Publish the beta images only from a gated release, signed and with an SBOM
- Authorize program-owned spend for ordinary program calls
- Re-verify the ledger consumers on this revision and re-render the beta report
- Refuse a terminal account sequence instead of wrapping it
- Give the gateway key/value fixture the asset metadata the registry now requires
- Describe the shipped LXT-20 and payments SDK paths in the programs README
- Renumber the PAY5 qualification observations clear of the merged ledger
- Point the Asset record tamper offset at the version three layout
- Refresh PAY5 program documentation
- Give per-asset accounts distinct ledger semantics
- Apply canonical Rust formatting to aggregate workspaces
- Evaluate Swift decoder refusals outside XCTest autoclosures
- Supply canonical availability and program artifacts to differential WAL tests
- Place the cargo-deny global option before the bans subcommand
- Match sandbox settlement helpers to their host feature consumers
- Apply workspace formatting to Programs-owned Rust sources
- Sign native and per-asset program call fixtures with DID principals
- Support signed Send from canonical per-asset accounts
- Map ERC-20 and SPL flows to the implemented LXT-20 reference
- Compose funding and program payout grants in one canonical SDK set
- Reproduce inherited Programs aggregate failures at the merge base
- Pin LXT-20 refusal vectors and record payment qualification blockers
- Map ERC-20 and SPL flows to the available LXT-20 interfaces
- Dispatch every perpetuals ordinal against canonical module state
- Require durable autobahn consensus state instead of silently running in memory
- Dispatch every stream ordinal against canonical key-value state
- Enrol each recovery guardian in its own process and custody directory
- Run the go/no-go report check from the beta contract check
- Align the public surface documentation with the served contract
- Apply payments documentation review findings
- Refresh payment docs for current testnet contracts
- Point READMEs at the payments wiki and unmerged testnet branches
- Link payments pages from the wiki index and related docs
- Give the service module real dispatch, canonical state and events
- Persist gateway invoice and idempotency state in metered key/value state
- Dispatch every escrow ordinal over module-scoped kernel state
- Dispatch every budget ordinal against canonical module state
- Render the beta go/no-go report from the evidence ledger
- Clear the Python SDK lint findings without suppressing any rule
- Give a refused acknowledged activity a canonical terminal rejection
- Execute and verify a batch before a replica acknowledges it
- Derive genesis module registration from one table
- Publish the hosted agentd readiness surface and check it on the cluster
- Send exactly one connection header on the websocket upgrade request
- Supply the principal tenant in the local gateway lifecycle harness
- Register the governance module in the bridge credit qualification
- Allow fresh actors to open asset accounts
- Emit explicit LXGB v2 metadata; pass native tests and producer signing checks
- Admit canonical native payment activities
- Publish verified receipts concurrently with funded SENDs
- Instrument and reduce hosted SEND latency
- Export 402LXP SDK surfaces and document renewal flow
- Export the shared per-asset account-name builder through wire
- Type public RPC read results against the published object contracts
- Generate native payment and Send authorization fixtures from C fields
- Authorize activity wait through Wait while preserving tenant isolation
- Share per-asset account names and ledger derivation; pass types, wire and SDK tests
- Encode and verify canonical payer grants; pass crypto tests and strict clippy
- Mount the core receipt event token and resolve the SDK lock dependency
- Connect scoped MCP wallet tools to SDK execution and daemon policy
- Disclose identity and source sequences independently; pass crypto tests and clippy
- Route wallet and token MCP tools through scoped daemon operations
- Connect public gateway reads to core and keep WebSocket sessions bounded
- Recognize remaining RPC reads and preserve native capability failures
- Deliver authenticated bounded WebSocket subscriptions from native receipt waits
- Submit public RPC activities through authenticated durable gateway paths
- Export exact verified account proof bytes through public RPC
- Wake authenticated receipt lookups when the executor publishes a commit
- Admit strictly framed Programs transfer and account activities in core
- Require native receive receipts for grant payment examples
- Decode RPC checkpoint certificates with operator-pinned authority
- Launch conformance examples with explicit isolated configuration
- Route prepared grant draws through agent budget reservations
- Persist prepared grant draws and recover uncertain RPC submissions
- Validate grant payment terms and route seller draw requests
- Specify multi-asset 402LXP offers, grant draws and commitments
- Coalesce native Send durability and publication
- Pipeline native batch durability and publication
- Refresh native Send timing at the qualified head
- Authorize retired issuance snapshot migrations
- Allow fresh actors to open asset accounts
- Emit explicit LXGB v2 metadata; pass native tests and producer signing checks
- Reconstruct Asset state from committed registry metadata
- Authenticate Asset supply transitions in versioned receipts
- Encode named Asset prices in version two fee parameters
- Retire the historical byte-pair Asset catalogue
- Spell payload comment bounds without decimal syntax; no-float and ledger gates pass
- Explicitly exclude pause and unpause from native activity support; ledger check passes
- Require named Asset prices and record the fee schedule boundary; ledger check passes
- Require canonical internal issuance accounting without weakening conservation; ledger check passes
- Require explicit receipt supply binding and record the codec boundary; ledger check passes
- Compare restored Asset metadata and grants; snapshot and guarantor tests pass
- Persist Asset salt in version 3 records; registry, roots, snapshots and replay tests pass
- Comment asset payload encodings and the issuer_kind custody_kind split
- Expand Asset kernel refusal, cap and snapshot qualification
- Allow canonical Asset ordinals through daemon admission and replay
- Decode canonical Asset activities and reserve withdrawal ordinal
- Support signed Send from canonical per-asset accounts

## 2026-09-09

### Added

- Add readiness build request keys and record protected Human input refusal
- Add native owner-registration production from protected custody inputs
- Add staged Governance registration handlers and record the genesis blocker
- Expose offline identity provisioning through the real LXIP state path
- Wire internal services into beta cluster provisioning and topology
- Add shared beta image publishing and digest-verified GHCR pulls
- Add readiness build request keys and record protected Human input refusal
- Add guarded deployment proof and journal publication paths
- Expose offline identity provisioning through the real LXIP state path
- Wire internal services into beta cluster provisioning and topology
- Wire internal services into beta cluster provisioning and topology
- Ship the module registry tool in the node image
- Wire Human providers and scoped authority into the node pod
- Wire internal services into beta cluster provisioning and topology
- Wire internal services into beta cluster provisioning and topology
- Add native composite state witness codecs and replay checks
- Build the independent guarantor checkpoint producer
- Add bounded mutual-TLS guarantor attestation transport

### Fixed

- Restore canonical guarantor qualification records
- Repair settlement witness qualification after rebase
- Fix asset deposit test authority fixture and report assertion failures
- Restore agentd formatting and pass fmt, clippy, and test compilation

### Changed

- Use named fields for Human peer authority bindings
- Preserve registry deployment proof integrity after rebase
- Use named fields for Human peer authority bindings
- Reconcile the module registry replay with current main
- Reconcile Human transport qualification with settlement main
- Bind hosted Human peers through strict named fields
- Reconcile Human and qualification locks; verify locked offline Human metadata
- Make the built-in topology YAML parser consume the node manifest
- Reconcile duplicate ledger records and record activation blocker
- Reconcile Human and qualification locks; verify locked offline Human metadata
- Make the built-in topology YAML parser consume the node manifest
- Bind withdrawal ledger admission and pass funded crash replay
- Preserve both qualification records after native tests rebase
- Use named fields for Human peer authority bindings
- Reconcile availability fixtures and missing-class classification

### Documentation

- Document the candidate availability dependency for checkpoint production
- Document owner registration encodings, inputs and qualification limits
- Document the candidate availability dependency for checkpoint production
- Document the candidate availability dependency for checkpoint production
- Document native witness encoding and settlement qualification gaps
- Document guarantor operation and remaining qualification limits
- Document the candidate availability dependency for checkpoint production

### Tests

- Qualify isolated owner checkpoint settlement
- Exercise real owner decoding and Governance lifecycle refusals
- Verify native identity receipt snapshots without upgrading finality claims
- Verify native registry journal pairs with the Human evidence reader
- Exercise topology refusals through the live identity alias
- Verify native settlement signatures with Solidity Ed25519 and pass contract and replay gates
- Qualify distinct-identity guarantor replay after traversal correction
- Qualify disposable custody with repository-built paxd and record remaining gate failures
- Verify signed recipient bindings against native account authority proofs
- Verify account witnesses after every guarantor replay batch
- Prove native account balances through the committed registry root
- Verify native layered state witnesses in Solidity
- Exercise checkpoint production against a real daemon

### Housekeeping

- Close owner-registration rebase safety gaps
- Record the remaining cluster qualification boundary
- Record passing owner formatting and preserve unrelated differences
- Record remaining owner custody, guardian and capability integration gaps
- Record registry request initialization blocking gateway readiness
- Checkpoint protected Human evidence assembly and cluster hook
- Record cluster image progress and Docker registry connection failure
- Record cluster image progress and Docker registry connection failure
- Record Human material blocker after successful hosted image builds and ledger checks
- Record Docker Hub failure in full cluster gate with passing ledger checks
- Record registry image build and load with passing ledger validation
- Record registry request initialization blocking gateway readiness
- Checkpoint protected Human evidence assembly and cluster hook
- Checkpoint protected Human catalog and owner Job staging
- Record passing identity client and service qualification
- Checkpoint identity client interface reconciliation
- Record cluster image progress and Docker registry connection failure
- Record cluster image progress and Docker registry connection failure
- Record registry journal integration blockers; ledger check passes
- Record cluster image progress and Docker registry connection failure
- Record cluster image progress and Docker registry connection failure
- Record cluster image progress and Docker registry connection failure
- Record cluster build disk stop and successful cleanup
- Record multi-guarantor runtime progress and qualification failures
- Record settlement bootstrap threshold failure and passing ledger check
- Record disk-limited cluster qualification and passing RPC isolation
- Record topology qualification and blocked cluster preflight
- Record cluster image progress and Docker registry connection failure
- Record cluster build disk stop and successful cleanup
- Record the unchanged inherited sole-writer conflict
- Record inherited balance-writer conflict with git provenance
- Record settlement proof path-position conflict; ledger and whitespace checks pass
- Record guarantor qualification rerun and remaining external blockers
- Record incompatible native and settlement witness commitments
- Record passing real-daemon guarantor integration and ledger check
- Record peer binding rebase qualification and root harness blocker

### Other

- Align disposable owner chain time and isolate the settlement epoch refusal
- Trace inherited bond failure to the epoch refusal output flag
- Trace availability failures to the merged canonical decoder
- Limit LNI Governance refresh to the protocol that registers it
- Replay owner Governance batches in both guarantor configurations
- Commit versioned session action and expiry evidence
- Produce native owner registration with authenticated custody evidence
- Provision real owner custody before native credit submission
- Admit the exact LXIP owner identity before native execution
- Generate dedicated cluster guardian keys and verify custody bindings
- Require node-verified checkpoint certificates for Human identity and key policies
- Read protected owner email and provision LXIP before registration validation
- Register Governance in fresh genesis and require its snapshot entry
- Rebase the cluster lane onto main with the registry cgroup delegation
- Start the registry without a protocol head at genesis and pin its outbound trust
- Sequence genesis guarantors in sorted activation order
- Stage the Human co-location work in progress before merging main
- Sequence genesis guarantors in sorted activation order
- Host the Human service in the beta cluster
- Decode canonical Human owner authority references
- Require protocol 3 throughout cluster sequencer trust provisioning
- Produce treasury-signed registry deployments from the built escrow artifact
- Require registry ingress deployment before Human evidence assembly
- Materialize shared registry journals in the protected Human work directory
- Mount one protected journal PVC in the registry and Human owner Job
- Forward registry deployments and native proofs through the authenticated boundary
- Authenticate maintained deployment receipts by signed batch inclusion
- Accept protocol 3 in protected sequencer trust histories
- Cache verified builder state for isolated registry readiness
- Derive provisioning counterparties with the protocol account function
- Require tenant binding during identity provisioning
- Validate protected Human owner registration inputs
- Rebase the cluster lane onto main with the registry cgroup delegation
- Start the registry without a protocol head at genesis and pin its outbound trust
- Sequence genesis guarantors in sorted activation order
- Stage the Human co-location work in progress before merging main
- Sequence genesis guarantors in sorted activation order
- Host the Human service in the beta cluster
- Rebase the cluster lane onto main with the registry cgroup delegation
- Start the registry without a protocol head at genesis and pin its outbound trust
- Sequence genesis guarantors in sorted activation order
- Stage the Human co-location work in progress before merging main
- Sequence genesis guarantors in sorted activation order
- Host the Human service in the beta cluster
- Generate hosted module registry with the node image tool
- Produce version-2 module registries and retain beta material
- Rebase the cluster lane onto main with the registry cgroup delegation
- Delegate registry cgroups inside the container and prove worker attachment
- Start the registry without a protocol head at genesis and pin its outbound trust
- Renumber ledger observations that collide with main after rebase
- Normalise the ledger observations added on this branch to numeric identifiers
- Host the Human service in the beta cluster
- Stage the Human co-location work in progress before merging main
- Sequence genesis guarantors in sorted activation order
- Wait for beta RPC reads to recover after forward interruptions
- Supervise beta cluster forwards through connection resets
- Generate sorted genesis guarantors from the settlement threshold
- Renumber ledger observations that collide with main after rebase
- Normalise the ledger observations added on this branch to numeric identifiers
- Host the Human service in the beta cluster
- Register consecutive same-epoch checkpoints and pass native settlement publication gates
- Persist receipt-derived withdrawal identities and pass Human recovery and payout gates
- Disclose native withdrawals and pass crypto signing and refusal gates
- Publish signed native deposit and balance evidence and record the checkpoint epoch conflict
- Compile versioned native withdrawal intents and preserve provider anchors
- Route native withdrawals through daemon replay and record the funded sequence conflict
- Persist native settlement evidence and qualify Rust client and Human journeys
- Check request-anchor ancestry against recorded canonical checkpoint order
- Execute signed native withdrawal requests and verify committed records
- Commit canonical withdrawal requests in the native asset module
- Decode and verify native version-two state witnesses in Rust
- Provision the persistent checkpoint authority and publish its public key
- Provision two bonded guarantors and a checkpoint submitter
- Register exact guarantor certificates with a dedicated submitter
- Route bridge reserve credits through the ledger owner
- Report DA withholding assertions and preserve child exit codes
- Clear finalisable on refusal and report guarantor test failures

## 2026-09-08

### Added

- Add scoped movement protocols and durable EVM custody authorization
- Add scoped human authority routes with explicit evidence refusals
- Implement durable LXSP security provider and verified receipt ingestion
- Implement the LXIP identity provider and test its real client
- Add opt-in encrypted CLI credential storage; pass CLI and ledger gates

### Fixed

- Resolve explorer lint errors while preserving evidence checks

### Changed

- Reconcile ledger records after rebasing the movement provider onto main
- Reconcile the ledger record after rebasing the image reference change onto main
- Use Sidiora Labs hosted image references; pass syntax and contract checks
- Reconcile ledger observation ids after rebasing onto main
- Keep the persistent Comet chain-id refusal case in the disposable custody test

### Tests

- Qualify legacy retirement and rerun availability gates
- Verify daemon availability against real settlement and exclude maintenance activity matches
- Verify altered checkpoint signatures at withdrawal settlement

### Housekeeping

- Record final gate evidence and guarantor harness handoff
- Record availability qualification results and preserved contract conflicts
- Checkpoint real settlement availability harness and kernel replay fixtures
- Checkpoint native availability refusal harness and canonical replay fixtures
- Checkpoint kernel recovery validation and real replay fixture migration
- Checkpoint canonical availability sealing, retention and fetch implementation
- Record the load-sensitive movement provider restart test after the rebase
- Regenerate the human lockfile for the movement provider after rebasing onto main

### Other

- Fetch sealed availability candidates before checkpoint finalization
- Commit complete DA fixtures before withholding classes
- Recover legacy WAL batches without advertising unavailable data
- Publish pinned settlement evidence for deposits, withdrawals and exits
- Version asset metadata and serve verified human balance context
- Require private CA trust and add a Human-owner runtime

## 2026-09-07

### Added

- Add the French README translation
- Add the German README translation
- Add the Brazilian Portuguese README translation
- Add the Simplified Chinese README translation
- Add the Russian README translation
- Add the Japanese README translation
- Add the Spanish README translation
- Add the LayerX network image referenced by the README
- Add a short index of the docs tree
- Add CITATION.cff for Apache-2.0 LayerX
- Add maintainer governance and the spec-first change process
- Add the Contributor Covenant 2.1 code of conduct
- Add pre-commit hooks and refresh editor and Git attributes.
- Add offline Markdown link checks and OpenSSF Scorecard publishing.
- Add a Debian bookworm devcontainer that can build the repository.
- Add the beta-qualify-focused gate runner
- Add the agent runtime guide
- Add the testnet quickstart
- Integrate focused deployment and emulator maintenance repairs
- Serve authenticated maintenance attachments and account-state heads
- Integrate Human qualification locks and emulator native call fixtures
- Build and lint the marketplace guest for its Wasm target
- Integrate Agentd maintenance attachment verification
- Integrate programs workspace strict lint repairs
- Add the client ed25519-dalek edge to the interop lockfile
- Integrate registry account-state, state-leaf bound and test lint repairs
- Integrate registry test lint repairs
- Integrate runtime gauntlet benchmark and registry lint repairs
- Integrate runtime lint repairs for qualification
- Integrate configured maintained authority consumers with terminal V4
- Expose configured sequencer authorization and maintained activity facts
- Serve published receipts without waiting for batch execution
- Serve published receipts without waiting for batch execution
- Serve the configured CometBFT genesis document through the Paxeer boundary
- Add signed custody deployment through pinned TLS origins
- Add an independent Paxeer observer and publish both TLS origins; qualify real syncing and shell syntax

### Fixed

- Repair faucet strict lint findings
- Repair SDK generator strict lint findings
- Repair hosted registry strict lint findings
- Correct two HostedWebhooks citation line numbers
- Repair qualification ledger identifiers, severities and task references
- Repair CLI strict lint findings
- Repair dashboard lint findings and pass strict Clippy and cargo tests
- Repair emulator strict lint findings
- Repair CosmWasm porting strict lint and qualify Programs workspace lint
- Repair EVM porting strict lint with checked emitter conversions
- Repair Solana porting strict lint and preserve test refusals
- Repair sandbox strict lint findings across library and integration tests
- Restore shared signed-call fixtures for sandbox tests
- Repair registry test lint and adapter documentation; registry Clippy, tests and ABI drift pass
- Repair registry test strict Clippy findings
- Repair gateway library lint findings and migrate request callers
- Repair layerx-interop-service strict Clippy findings
- Repair layerx-x402 strict Clippy findings
- Repair layerx-visa-tap strict Clippy findings
- Repair layerx-ucp strict Clippy findings
- Repair layerx-portable strict Clippy findings
- Repair layerx-fiat strict Clippy findings
- Repair layerx-ap2 strict Clippy findings
- Repair layerx-migrate strict Clippy findings
- Repair layerx-mirror strict Clippy findings
- Repair interop gateway route and parser lint findings
- Correct storage scan cursor, page and typed rollback expectations
- Correct signature expectations with independently checked authority vectors
- Repair interpreter conformance strict Clippy findings with all tests passing
- Repair market strict Clippy findings and verify native tests and Wasm build
- Correct BetaCluster wiki citations after fact-check
- Repair interpreter and CosmWasm guest library lints
- Repair independent runtime lint findings and preserve terminal ownership
- Repair strict clippy findings in vendored wasm-instrument
- Repair strict clippy findings in the Rust program SDK
- Repair strict clippy findings in vendored wasmi
- Repair strict clippy findings in vendored parity-wasm
- Restore the vendored Binaryen LLVM configuration header so programs-test builds
- Correct Programs wiki citations and payload layouts after fact-check

### Changed

- Rename the HPX registry Go module; pass build, vet and test commands
- Rename the Go payment sample module and SDK imports; pass build, vet and test commands
- Rename the Go SDK module and metadata; pass build, vet and tests
- Name the product LayerX Network in the README translations.
- Name the product LayerX Network in English docs, templates, and pages.
- Replace remaining inspection-only wording with Apache 2.0
- Reconcile the quickstart with the cluster export and add the program deploy walk
- Reconcile the fuzz lockfile with current runtime dependencies
- Reconcile the fuzz lockfile with current runtime dependencies
- Use the protocol-specific deployment helper in Human withdrawal tests
- Use the protocol-specific deployment helper in Human withdrawal tests
- Reconcile the human and qualify lockfiles with their manifests
- Reconcile the human and qualify lockfiles with their manifests
- Make the boundary conformance runner compile against the activity error types
- Replace the CLI tests' rejected mock override with a private Secret Service
- Refactor reference ramp handlers and pass strict Clippy and tests
- Split canonical account reads into ordered decode stages
- Refactor gateway handlers and pass strict all-target Clippy and tests
- Make stored fixture lookups object-aware and verify native gates
- Use let-else for the required nested test module
- Make occupancy test failures explicit
- Make commitment test failures explicit
- Use let-else for cache test requirements
- Make access declaration test failures explicit
- Make context vector test failures explicit
- Make ABI codec test failures explicit
- Split native call and transfer validation with passing runtime and ASan gates
- Split bigint registration and preserve hash refusal assertions
- Separate current V4 lifecycle generation from stored V3 verification
- Bind disposable custody identity to genesis and preserve host refusals
- Make the Binaryen metering corpus reproducible
- Bind disposable custody to Comet genesis and verify real TLS paths
- Bind custody genesis to verified disposable chain identity

### Removed

- Drop the docs index row for the removed wiki-drafts directory.
- Remove internal audit and metadata files from the repository root
- Remove a stray ledger section left by the rebase
- Drop the unused base64 0.22.1 entry from the verify-receipt sample lock

### Documentation

- State the public testnet as it exists, without launch dates
- Document the hosted node and the agent boundary
- Document the hosted faucet and testnet control services
- Document the Paxeer boundary
- Document the hosted identity service
- Document the hosted internal services
- Document the hosted registry service
- Document the hosted webhooks service
- Document the hosted gateway
- Document the hosted authority service
- Document the hosted core service
- Document the LayerX CLI and its credential handling
- Document Agentd budgets, approvals and protocol evidence
- Document the x402 transport and its conformance matrix
- Document the program porting crates
- Document the portable receipt verifier and its refusal contract
- Document testnet validation errors and pass strict Clippy and tests
- Document the program storage scan host contract
- Document webhook trust refusals and pass strict Clippy and tests
- Document canonical account-state refusal contracts
- Document the sandbox lease lifecycle, refusals and snapshot metering
- Document client SDK terminal verification
- Document the runtime signature and storage test authorities
- Document the disposable custody deployment identity rule
- Document the Programs workspace gates and fixture recipes
- Document the disposable beta cluster bring-up and its outputs
- Document the custody-first settlement dependency and qualification
- Document the Programs module for the beta wiki

### Tests

- Verify the escrow refusal through the canonical V4 terminal envelope
- Verify maintained account evidence over LNI and in client bundles
- Verify occupancy maintenance attachments in the agent boundary artifact consumer
- Verify occupancy maintenance attachments in the Agentd real-authority fixture
- Verify maintained batch outcomes through the maintenance transition roots
- Qualify custody credit with real contracts and native sanitizers
- Qualify the public simulation interface against the native daemon
- Qualify V4 replay and historical receipts while retaining matrix failures
- Verify maintained batch outcomes through the maintenance transition roots
- Qualify custody credit with real contracts and native sanitizers
- Qualify the public simulation interface against the native daemon
- Verify applied terminal transfers in the .NET SDK
- Verify applied terminal transfers in the Swift SDK
- Verify applied terminal transfers in the JVM SDK
- Verify applied terminal transfers in Go
- Verify applied terminal transfers in TypeScript
- Test signed terminal vectors and malformed evidence in Python
- Verify applied terminal transfers in Python
- Verify observer genesis identity across both disposable Paxeer boundaries
- Prove the boundary genesis route against a disposable paxd
- Qualify bond prediction on current-source Paxeer images and verify teardown
- Verify secp256k1 signatures against the supplied digest in the Programs runtime
- Verify blake3 against the official test vectors and correct the hand-written goldens

### Housekeeping

- Record passing Go checks and blocked aggregate sample gates
- Record the public documentation alignment under Unreleased
- Record legacy replay qualification and retain historical gate results
- Record passing native and Agentd gates with boundary and lock blockers
- Record passing aggregate platform lint and registry checks
- Record passing native and Agentd gates with boundary and lock blockers
- Record the emulator lifecycle failure diagnosis
- Record maintenance integration qualification and registry lint blockers
- Record interop lockfile edge qualification
- Record rebased native matrix and retained contract blockers
- Record runtime feature qualification and remaining registry and lifecycle conflicts
- Record strict Clippy passes and remaining contract conflicts
- Close owned runtime test lint findings while retaining every vector and assertion
- Record runtime test qualification and strict Clippy blockers
- Record native matrix results and preserve qualification severities
- Checkpoint runtime lint repairs preserving bounds and execution semantics
- Record consumer qualification and pre-existing native fixture mismatches
- Checkpoint maintained batch identity selection in the authority and wire helper
- Format the availability record verification expression
- Checkpoint maintained-batch evidence and partial crash recovery
- Record native regression and sanitizer passes with finality blockers
- Record occupancy qualification blockers after native and proof checks
- Checkpoint occupancy publication with passing kernel gates; daemon recovery unqualified
- Checkpoint unqualified occupancy maintenance publication implementation
- Record the occupancy account proof publication dependency
- Record occupancy publication and executed fixture contract conflicts
- Checkpoint signed terminal V4 with real executed proof fixtures
- Checkpoint explicit V2 transfer authorization and applied-leg verification source
- Record passing finality authority gate after maintained proof merge
- Checkpoint maintained batch identity selection in the authority and wire helper
- Record the native recovery matrix and remaining consumer failures
- Format the availability record verification expression
- Checkpoint maintained-batch evidence and partial crash recovery
- Record native regression and sanitizer passes with finality blockers
- Record occupancy qualification blockers after native and proof checks
- Checkpoint occupancy publication with passing kernel gates; daemon recovery unqualified
- Checkpoint unqualified occupancy maintenance publication implementation
- Record the occupancy account proof publication dependency
- Record occupancy publication and executed fixture contract conflicts
- Update platform getrandom to 0.4.3 and remove the interop r-efi duplicate
- Record interop dependency blockers and passing build, test and Clippy gates
- Record six SDK terminal verification gates after integration
- Record Python terminal qualification and stored fixture provenance
- Record missing authenticated terminal transfer inputs
- Record principal transfer terminal evidence mismatch
- Record platform-lint remaining failures outside webhook and ramp ownership
- Record fixed-address USDL deployment constraints
- Record the rebased cluster verification failure at the native callback assertion
- Record passing disposable bond prediction traces without claiming the prior failure repaired
- Record cluster builds and the remaining settlement and tooling blockers
- Record published signature vector exposing digest verification defect
- Record unrelated platform workspace test failures
- Record layerx-client sha2 on the interop lockfile
- Record layerx-client sha2 on the verify-receipt sample lockfile
- Record the custody-first genesis dependency cycle and the beta resolution

### Other

- Point Paxeer docs and HPX hosting at the renamed repository
- Point owned documentation at the renamed LayerX-Network repository.
- Point manifests, workflows and release metadata at the renamed repository
- Point interop MCP docs at the crate README that exists
- Align monorepo and qualification docs with the README toolchain
- Index every wiki page from Home and link Home from each page
- Rewrite the changelog in Keep a Changelog format
- Relicense LayerX under the Apache License 2.0
- List committers and how contributor recognition works
- Rewrite support guidance for issues, wiki, and no hosted SLA
- Refresh the security policy for main-branch private reporting
- Refresh the contributing guide for DCO and spec-first work
- Rewrite the README for the public release
- Map area labels to changelog categories and auto-label pull requests.
- Refresh issue forms and the pull request template for public contribution.
- Assign @Sidiora-Labs/core as CODEOWNERS for every published area.
- Pin the qualification replay corpus to the legacy protocol
- Rebuild the ledger as main plus this branch's records after rebase
- Rebuild the ledger as main plus this branch's records after rebase
- Inject clocks into the SDK transport and real authority harness
- Serialize approval decisions through durable persistence
- Rebuild the ledger as main plus this branch's records after rebase
- Renumber the ledger observations on this branch that collide with main after rebase
- Give the boundary conformance observation a numeric ledger identifier
- Renumber ledger observations that collide with main after rebase
- Normalise the ledger observations added on this branch to numeric identifiers
- Tighten the quickstart scope sentence
- Place the cargo-deny fetch flag after the check subcommand
- Normalise the ledger observations added on this branch to numeric identifiers
- Admit dedicated webhook receipt credentials and verify scope refusals
- Normalise the ledger observations added on lane/native to numeric identifiers
- Authenticate maintained lifecycle batch evidence through the real verifier
- Probe a deterministically unavailable core endpoint in the exit tests
- Finalize occupancy maintenance for each native emulator batch
- Align emulator request fixtures with the kernel-generated native call
- Align emulator request fixtures with the kernel-generated native call
- Tighten the HostedWebhooks scope sentence
- Bring the seven newest ledger observations into the checked identifier scheme
- Align the genesis funding text with ordinary genesis
- Pin the Paxeer finality endpoint in the node egress contract
- Distinguish durable-storage faults from admin refusals in the core contract
- Admit the batch_evidence field on maintained authority responses
- Supply boundary test prerequisites and verify local boundary and finality gates
- Provision the private Secret Service dependencies in the platform workflow
- Reject oversized state leaves with checked lengths; registry and adapter tests and rustfmt pass
- Represent canonical account flags with independent two-state enums
- Gate sandbox escrow reservations with the host FFI consumer
- Gate occupancy commit state and storage host helpers while retaining unit coverage
- Gate authenticated execution and scheduling helpers with their host consumers
- Group deployment identity and extract lifecycle history checks
- Consume verified deployment records during catalog admission
- Extract lifecycle parsers and retain verified lifecycle evidence
- Check interface lengths and document typed refusals
- Consolidate corrupt migration marker rejection
- Simplify required benchmark artifact lookup
- Extract gauntlet refusal checks and check cursor lengths
- Match the historical executed fixture exact metadata key contract
- Group authenticated scheduling inputs without changing admission checks
- Group governed fee parameters while preserving const construction and wire order
- Group sandbox escrow requests and use associated transfer settlement
- Check the terminal fixture wrapper length
- Extract the unchanged scan budget refusal assertions
- Check account fixture lengths and retain explicit test failures
- Evaluate the capability transport bound at compile time
- Reduce composition errors and split execution trace accounting
- Match native CALL test requests to the signed ABI 2 fixture
- Release artifact fixture state on first CALL assertion failure
- Select encoding 4 for current terminals and assert stored V3 replay
- Migrate interop HTTP callers and verify the platform workspace build
- Reference independent sequencer authorization files in hosted deployments
- Require configured maintained authority evidence for webhooks
- Authenticate maintained gateway receipts under configured sequencer pins
- Require configured sequencer authorization in ramp consumers
- Capture real maintained authority evidence for consumer verification
- Clarify strict consumer authorization configuration work
- Authorize maintained batches with the maintenance leaf in the authority
- Authenticate initialized genesis recovery before the first checkpoint
- Select maintenance account-proof verification by wire version in the client
- Drive the post-upgrade CALL regression through batch maintenance publication
- Recover maintenance authority and isolated account snapshots on restart
- Generate native CALL and execution evidence fixtures
- Link native state tests with their cryptographic dependencies
- Reject malformed Programs lifecycle payloads before admission
- Authorize maintained batches with the maintenance leaf in the authority
- Authenticate initialized genesis recovery before the first checkpoint
- Select maintenance account-proof verification by wire version in the client
- Drive the post-upgrade CALL regression through batch maintenance publication
- Recover maintenance authority and isolated account snapshots on restart
- Generate native CALL and execution evidence fixtures
- Link native state tests with their cryptographic dependencies
- Reject malformed Programs lifecycle payloads before admission
- Assert exact portable receipt refusals from the vector contract
- Meter canonical sandbox state reconstruction during restore
- Require exact sandbox capability refusals through valid guest calls
- Release sandbox principal capacity at lease expiry
- Derive native binding mismatch values from the fixture
- Import authenticated terminal vectors and native regeneration recipes
- Initialize fee replay meters and link their Programs runtime
- Link native account and journal tests with OpenSSL
- Refresh the beta cluster page for the genesis route and observer identity wiring
- Link the Programs runtime into the bridge deposit and bond test binaries
- Compile the interpreter benchmark against the validated module contract
- Migrate the interop service to the grouped gateway store requests
- Provision consumer sequencer pins from authority trust history
- Spell the elided lifetimes explicitly in vendored wasmi and parity-wasm public iterators
- Group gateway store requests and tidy webhook main lints
- Group ramp request and journal arguments and document error contracts
- Group webhook boundary requests and document hosted error contracts
- Produce custody credits from existing verified cluster RPCs
- Populate sealed builder directories before applying the final read-only seal
- Initialize the node toolchain before parallel native builds
- Bound native and testnet image builds and expose image-only qualification
- Exclude environment files and untracked artifacts from Docker contexts
- Serialize cluster lifecycle goals under parallel make
- Accept validated custody profiles and defer registration until observed
- Compress interpreter relocations for root Cargo builds
- Unify base64 on 0.23.1 so the platform dependency bans pass
- Run cargo deny checks without the removed --disable-fetch flag

## 2026-09-06

### Added

- Add encoder-derived simulation envelope golden; client tests pass
- Build signed custody genesis artifacts and record the funding integration gap

### Fixed

- Repair MCP error contracts and borrows; aggregate agent lint and tests pass
- Fix API generator documentation; strict API lint and schema drift pass
- Repair client strict lints and record retained conflict decisions

### Changed

- Refactor agentd and SDK for strict all-target Clippy

### Tests

- Verify real WETH custody evidence through replicated Anvil origins

### Housekeeping

- Update generated API digest; SDK drift check passes

### Other

- Give duplicated qualification observations unique section IDs
- Recover identical protocol-2 evidence through real native restart
- Cross-check protocol-3 custody accounts against native vectors

## 2026-09-05

### Added

- Add task 6.10 binding a real finality-authority verifier so the sequencer bootstraps
- Serve verified receipt authority facts over TLS from the independent replica
- Add requirement 14 and the wave 6 and 7 tasks that build the trusted-boundary services in-repository

### Changed

- Reconcile the foundation crates' protocol-version assertions with explicit protocol 3

### Housekeeping

- Checkpoint Programs lifecycle, custody funding, and ABI 2 SDK work
- Checkpoint accumulated beta work at owner stop
- Checkpoint in-progress wave 6 work: LNI simulate route, internal services, protocol-3 client and SDK changes
- Checkpoint wave 6 services and protocol 3 integration work

### Other

- Allow immediate Paxeer beta bootstrap with verified governance checks
- Execute protocol-3 Programs calls through the runtime FFI with the occupancy semantics the kernel already applies
- Hand off wave 6 in progress: commit the unqualified boundary services and the takeover notes
- Register the five trusted-boundary service crates in the platform workspace

## 2026-09-03

### Housekeeping

- Record the beta cluster gate as blocked on the owner's builder root and external manifests

### Other

- Retry beta cluster port-forwards until the selected pod is running
- Size registry build slots so the inode quota assertion can hold
- Provision the registry node boundary without CAP_FOWNER and mount build slots with a real autoclear loop

## 2026-09-02

### Added

- Build the dashboard image from the repository root and feed the cluster env to the hosted smoke
- Add the beta cluster bring-up and record the dashboard type-check blocker
- Serve a real authenticated durable admission node in the human test fixture
- Add the beta qualification gates and an executing beta driver

### Fixed

- Correct the registry deployment contract's stale builder digest expectation
- Repair the clean-profile emulator bootstrap

### Changed

- Bind agent tokens to a durable revocation generation
- Bind withdrawal settlement to the recorded asset
- Make the release manifest and the release workflow agree

### Removed

- Drop the Package.resolved the beta driver test writes into the Swift sample

### Housekeeping

- Regenerate the platform SDK projections left stale by the envelope schema change

### Other

- Declare the URLPattern globals the Node 26 typings leave out of the dashboard type check
- Mark beta task 4.2 done after the release check, plan, test and contract gates passed
- Emit the source-bound artifact manifest and verify published bytes before promotion
- Mark beta task 2.2 done after the agentd, MCP, SDK and human service gates passed
- Mark beta task 3.4 done after the hosted registry gate passed
- Publish hosted registry deployments as one sealed envelope
- Mark beta task 3.3 done after the withdrawal and result gates passed
- Mark beta task 3.6 done after the hosted topology and contract gates passed
- Align hosted topology and make testnet readiness journey-specific
- Mark beta task 3.2 done after the checkpoint gates passed across all layers
- Unify checkpoint identity and freshness across C, Rust and Solidity
- Mark beta task 4.3 done after the beta driver test gate passed
- Mark beta task 4.1 done after the release manifest check passed
- Mark beta task 2.3 done after the human activity gate passed
- Run the evidence verifier before any human receipt-verified label
- Mark beta task 2.4 done after the human build and test gates passed
- Carry the structured ApiError in the envelope and type the explorer overload state
- Mark beta task 3.1 done after its snapshot gates passed
- Persist committed Programs blobs through snapshots
- Mark beta task 3.5 done after its ramp journal gate passed
- Stage ramp callback validation before the durable append
- Authenticate and durably admit LNI activities

## 2026-09-01

### Changed

- Reconcile Programs runtime and qualification sources
- Reconcile beta integration and money paths

### Housekeeping

- Record the archived node-boundary include break and union-merge the beta ledger

### Other

- Mark beta task 1.1 done after its ledger and contract gates passed
- Define the beta executed-evidence ledger and the canonical beta contract
- Activate the layerx-beta feature spec and archive the interface specs

## 2026-08-31

### Fixed

- Correct contract safety qualification harnesses

### Tests

- Qualify multichain mirror contracts

### Housekeeping

- Record verified Solana devnet mirror deployment
- Record verified mirror testnet deployments

### Other

- Harden deterministic Paxeer deployment topology
- Pin Solana SBF-compatible dependencies

## 2026-08-30

### Added

- Add adversarial finality evidence recovery coverage
- Add fail-closed Programs monetary qualification gate
- Serve verified batch headers over production LNI
- Add fail-closed release qualification runners
- Wire authenticated human operations through the agent daemon
- Add navigable codebase relationship map
- Add authenticated durable A2A execution
- Add cross-SDK Programs receipt verification
- Add the privileged human component boundary

### Fixed

- Restore Programs receipt and state proof parity
- Correct JVM SDK lookup idempotency
- Repair SDK conformance runners and Swift verification
- Repair accounting gateway release gates
- Restore shared storage qualification coverage
- Correct maximum Programs account vector
- Restore Programs isolation regression coverage
- Repair Programs occupancy state progression
- Restore node preparation and Programs qualification gates
- Handle Programs meter injection refusals
- Fix Programs aggregate build blockers
- Fix Programs receipt verifier widths
- Restore Programs runtime compilation
- Repair Programs SDK Rust qualification path

### Changed

- Make aggregate event exhaustion terminal
- Use authoritative execution usage in composition test
- Preserve typed state and snapshot validation errors
- Preserve batch header protocol versions across proof planes
- Bind Programs simulations to trusted state and time
- Bind wind-down account registration effects
- Bind fee governance tests to production batch context
- Preserve unsupported Programs metering refusals
- Preserve authenticated Programs migration evidence
- Enforce Programs runtime bounds and lock workspace
- Use public Amount type in emulator
- Use canonical emulator resource errors
- Bind Programs evidence in Swift and .NET
- Bind Programs evidence in Rust and Go
- Bind Programs evidence in TypeScript and Python

### Tests

- Verify finality evidence across node and agents
- Exercise monetary law through canonical entrypoints
- Qualify canonical storage recovery
- Exercise lifecycle migration with signed metering genesis
- Verify Programs transfer and occupancy attachments

### Housekeeping

- Complete deterministic sandbox expiry and escrow settlement
- Complete the production Human agent boundary
- Update hosted platform for current Rust APIs
- Regenerate Programs SDK catalogs and build inputs
- Complete JVM Programs attachment verification
- Complete Go Programs attachment verification
- Checkpoint truthful Programs provider recovery
- Checkpoint verified Programs transport for JVM

### Other

- Persist and recover canonical finality evidence
- Rollback trapped call output reservations
- Refresh canonical execution evidence vector
- Isolate composition limits from event byte budget
- Accept current core receipts in the proof plane
- Persist canonical log durability boundaries
- Upgrade public web examples to secure runtimes
- Stabilize Programs call activity execution
- Vendor locked Programs workspace dependencies
- Project exact human identity responses
- Inventory the hosted Programs route contract
- Validate Maven Central release metadata
- Link real Programs hosts into the emulator
- Type Programs discovery across SDKs
- Harden Programs call length arithmetic
- Align portable Programs verification contracts
- Match emulator Programs call media types
- Pin Programs sequencer trust in Rust and Go

## 2026-08-29

### Added

- Add verified Programs transport for Rust
- Add verified Programs transport for Go
- Add verified Programs transports for Swift and .NET
- Add verified Programs transports for TypeScript and Python

### Housekeeping

- Checkpoint authorized program transfer settlement
- Checkpoint canonical program receipts and recovery
- Checkpoint verified program gateway boundary
- Checkpoint hosted program operations and SDK surfaces

### Other

- Align hosted and emulator Programs contracts
- Refresh verified program explorer state
- Generate Rust program operation catalog
- Persist emulator program deployment evidence

## 2026-08-28

### Added

- Implement deterministic execution step commitments
- Ship the production node LNI boundary
- Integrate attested inputs into compute market
- Implement the compute marketplace program
- Implement kernel-settled sandbox lease escrow
- Add refreshed 15-agent codebase audit
- Ship the deterministic interpreter program

### Fixed

- Repair interpreter refusal vector stages

### Changed

- Make programs first-class on the agent plane
- Bind program execution to declared access sets

### Housekeeping

- Complete arbitration execution state commitments
- Record production-boundary repair tasks

### Other

- Allow agents to qualify completed tasks
- Settle sandbox usage incrementally
- Authenticate and isolate hosted program builds
- Persist authorized sandbox continuation snapshots
- Align Programs event and capability bounds
- Settle marketplace usage through challenge windows
- Unify Programs ABI upgrade policy
- Recover daemon logs before reconciliation
- Execute sandbox work under lease capabilities
- Model sandbox leases as protocol state
- Generate typed bindings from published program interfaces
- Publish receipt-bound program interfaces
- Price interpreted programs against compiled execution
- Schedule non-conflicting program activities in parallel
- Govern program fee schedules through protocol state
- Instrument program modules with protocol-owned metering
- Reuse one host linker across program execution
- Cache compiled program modules by versioned code hash

## 2026-08-27

### Added

- Add the Android sample-app Gradle wrapper with dependency verification metadata, adjust transport and proguard config, and update platform conformance tests

### Fixed

- Fix clippy doc markdown and strict optional property errors
- Repair C test link order, apply forge fmt, fix audit script file filtering
- Fix paxeer vet and fmt findings, share tracing backend state, refresh mocks and wasm builder

### Other

- Point CI at hosted tooling: install ripgrep and the pinned Rust toolchain with cargo-deny where jobs need them, and source integration-test images from GHCR
- Rework autobahn test fixtures to sign with committee-bound keys and committee-valid proposals, stamp init-chain block time in node test helpers, and reject EVM messages whose tx data fails to unpack
- Clear golangci findings in paxeer-network: bind integer conversions to their guards, name mismatched-package imports, guard KV cache sizing, and dedupe receipt action strings
- Vendor wasmvm shared and static libraries with gitignore carve-outs and repair the wasm-runtime Makefile
- Extend agent wire crates for protocol-v2 receipts, refresh gate allowlists, and adjust daemon C protocol handling and programs runtime FFI
- Run CI on hosted runners and gate external publishing on configured credentials

## 2026-08-26

### Added

- Add merchant binding to AP2 asset policy and expose authorization helper
- Expose indexable ABI contract accessors
- Add receipt-verified program balance sight
- Expose authenticated program execution context
- Ship program custody reference patterns

### Fixed

- Fix platform CLI seed and request handling
- Fix platform CLI and ramp request parsing
- Fix ramp canonical length encoding
- Fix remaining Rust ownership and helper errors
- Repair surfaced Rust build failures
- Repair Rust workspace build blockers
- Correct ABI v2 reference qualification
- Restore historical ABI constant paths
- Repair ABI v2 selection and parity

### Changed

- Bind ABI v2 context and compute primitives
- Enforce immutable ABI linker closure

### Housekeeping

- Close reference artifacts on read failures
- Record Programs ABI v2 implementation
- Record verified balance sight implementation
- Record execution context implementation

### Other

- repair clean-host CI inputs and Paxeer gates
- add missing kernel headers
- Finish the executable interoperability gateway
- Run the full SDK conformance surface
- Pin generated calldata vector counts
- Finish ABI v2 SDK and porting bindings
- Collect the orphaned end-to-end suites in their runners
- Route porting references through production calls
- Harden ABI v2 kit bindings
- Own current ABI at the crate root
- Parse program ABI metadata strictly
- Route program lint by recorded ABI
- Freeze Programs ABI version two
- Isolate custody guests from host builds

## 2026-08-25

### Added

- Add canonical receipt execution batch hash
- Add Tailwind CSS v4 PostCSS integration and platform-specific binaries
- Add Paxeer documentation site
- Add the HPX public gateway
- Integrate Paxeer Network into the LayerX monorepo

### Fixed

- Guard local Paxeer chain resets
- Fix sequencer handover and fail close budget authority
- Repair checkpoint time and historical attestation boundaries
- Repair Paxeer docs navigation and responsive layout
- Repair mirror source and journal integration
- Repair program authority and legacy state integration
- Restore checksum-bound cc vendor source
- Repair interop workspace dependencies
- Repair Programs wind-down route length
- Repair Programs state record capacity
- Repair Programs occupancy settlement

### Changed

- Keep Paxeer qualification observations unique
- Preserve bounded Giga configuration defaults
- Make integration state and transport bounds durable
- Bind receipts to signed execution batch context
- Bind Agent evidence to daemon trust policy
- Replace Agent verification booleans with proof evidence
- Bind checkpoint verification to settlement policy
- Bind finality policy to checkpoint epoch
- Bind equivocation evidence to settlement domain
- Preserve historical guarantor certificate validity
- Bind deposit proofs to the configured Paxeer chain
- Bind Human deposits to signed Paxeer custody roots
- Keep qualification observations uniquely addressable
- Make AP2 ingress trust deployment context
- Bind fiat callbacks to provider settlement identity
- Preserve deployment trust across sequencer rotation
- Bind program deployment admission to protocol proofs
- Bind verified deployments to executable resolution
- Replace placeholder AP2 mandate vectors
- Make deposit proof policy verifier-owned
- Bind Human proof evidence to Paxeer quorum
- Bind Paxeer deposits to registered custody roots
- Keep gateway coordination out of account copies
- Make gateway ownership initialization safe
- Bind gateway transactions to registry coordination
- Make gateway settlement atomic
- Enforce Platform dependency root coverage
- Separate Platform dependency preparation
- Make workspace build coverage complete and reproducible
- Bind Autobahn signatures to chain committees
- Bind Visa TAP targets and pending retries
- Make Paxeer accounting failures deterministic
- Enforce program lifecycle code integrity
- Keep reconnect and authority state honest
- Make the HPX public release deployable
- Separate HPX runtime and image publication

### Removed

- Remove fabricated Paxeer genesis commitments
- Remove Paxeer SDK compatibility panics
- Remove fabricated engine transaction signer
- Remove obsolete Programs authorization bypasses

### Tests

- Cover authenticated engine message conversion
- Verify pinned Paxeer lint tooling
- Cover deposit credit rollback refusal
- Cover Autobahn quorum replay rejection

### Housekeeping

- Complete Paxeer cached store boundaries
- Record Autobahn qualification boundaries
- Record Platform runtime qualification boundaries
- Complete fixed-capacity runtime admission
- Complete module store count admission
- Record core C boundary repairs
- Close AP2 and journal trust gaps
- Close gateway registry destroy race
- Close Paxeer nested dependency inventories
- Record workspace dependency installation
- Regenerate Human and JVM SDK surfaces
- Complete the public HPX registry deployment
- Record the HPX runtime publication blocker

### Other

- Reject invalid consensus index ranges
- Reject incomplete Paxeer RPC execution data
- Reject unrepresentable Autobahn heights
- Propagate Autobahn payload construction failures
- Validate complete Autobahn certificates
- Bound Autobahn peer handshakes
- Bound Autobahn producer arithmetic
- Authenticate Autobahn state before waiting
- Harden Paxeer precompile boundaries
- Harden Paxeer WASM input boundaries
- Fail closed on unavailable routed state roots
- Harden Paxeer RPC trust boundaries
- Harden Platform durable transaction boundaries
- Redact issued gateway credentials
- Bound mirror verifier process trust
- Harden CLI and emulator runtime boundaries
- Seal policy facts and protect daemon trust inputs
- Seal policy budget evidence behind reconciliation
- Fail closed on budget state and receipt replay
- Harden EVMC execution boundaries
- Harden equivocation encoder admission
- Validate bounded guarantor history before traversal
- Fail closed on unavailable Paxeer membership authority
- Govern guarantor membership independently of bond funding
- Limit transfer decoders to source regressions
- Migrate legacy deposits without truncating chain identity
- Commit DeliverTx hooks atomically with messages
- Reject fabricated synthetic EVM receipt logs
- Fail closed on Paxeer marker audit boundaries
- Harden fixed-capacity module admission
- Bound governance table traversal
- Validate DA bundle structure before rooting
- Protect migration journal storage boundaries
- Harden reproducible workspace tooling
- Require journal-verified deployment evidence
- Fail closed at EVM association and state root boundaries
- Harden Human Paxeer RPC trust boundary
- Couple deposit replay state to canonical credit
- Reclaim gateway registries safely
- Harden gateway ownership and test boundaries
- Inventory complete workspace build gates
- Reject Autobahn proposal arithmetic overflow
- Harden Human web trust boundaries
- Freeze web evidence and authority inputs
- Harden Paxeer bank boundary inputs
- Fail closed across Paxeer module boundaries
- Harden hosted runtime control boundaries
- Harden Visa TAP replay and intent binding
- Fail closed when recovering the mirror archive spool
- Harden program SDK ABI and guest memory boundaries
- Harden core C boundary validation
- Harden migration RPC quorum identities
- Invalidate challenged checkpoints before emergency exit
- Refresh Cargo workspace lockfiles
- move paxeer-docs site (moved to paxeer-network dir)
- Flesh out Configuration and Operators pages from source
- Publish the HPX image without Docker
- Apply the Paxeer design system to the HPX gateway
- docs(programs): README + wiki Protocol/Modules/Finality drafts
- docs: paxeer-network README + MCP/A2A/mirrors + wiki Developers/Security drafts
- docs: monorepo + qualification + wiki drafts after Paxeer import
- Prepare the public HPX distribution and registry

## 2026-08-24

### Added

- Wire program-owned accounts into the kernel registry
- Add downward-only program spending grants
- Implement canonical Programs occupancy settlement
- Build the production market-maker ramp
- Implement durable Ethereum and Solana mirror publishing
- Implement production Ethereum and Solana migration clients
- Ship scoped MCP and A2A installation
- Serve protocol adapters through the interop gateway
- Ship receipt-verified reference applications
- Build the receipt-verifying hosted gateway
- Serve the versioned human API over HTTPS
- Build the hosted testnet control plane and faucet
- Build the hosted testnet control plane and faucet
- Serve the versioned human API over HTTPS
- Add production remote KMS custody boundary
- Add production remote KMS custody boundary
- Wire settings persistence into real-stack browser coverage
- Wire settings persistence into real-stack browser coverage

### Changed

- Bind program registry balances to protocol state
- Reconcile platform spec with implementation reality

### Tests

- Verify evidence directly from mirror sources

### Housekeeping

- Complete mobile and agent framework integrations
- Complete merchant and agent middleware integration
- Complete merchant and agent middleware integration
- Complete managed agent application surfaces
- Complete the web performance machinery
- Complete the web performance machinery
- Complete managed agent application surfaces
- Record reconciliation publication blocker
- Record push authentication failure; commit remains local only

### Other

- Authorize program-owned transfer sources
- Deliver durable hosted webhooks and developer dashboards
- Generate the typed JVM SDK and conformance surface
- Generate the typed JVM SDK and conformance surface
- Log blocked install errors and missing packages from workspace logs; install npm, java/mvn, dotnet via apt; swift unavailable; append qualification note

## 2026-08-23

### Added

- Build and export the explorer core fixture for workspace-wide human test runs
- Build the CLI (task 17.1) (#41)
- Expose the program call activity through the agent layer, CLI and emulator (Task 28.8) (#38)
- Implement the x402 facilitator and transport matrix (task 22.3) (#37)
- Implement Visa Trusted Agent credential verification (#29)
- Implement portable receipt and mandate verification (#28)
- Implement UCP merchant profiles, checkout and orders (Task 23.2) (#27)
- Implement x402 v2 seller and buyer test suites (#22)
- Implement home and move-money wizard with e2e tests (#20)
- Add wide-integer and modular-exponentiation primitives (task 31.5) (#7)
- Implement Ethereum and Solana migration tooling (#25)
- Implement public explorer plane with server-rendered pages (task 13.5) (#18)
- Add deterministic signature verification and recovery (task 31.4) (#8)
- Implement JVM SDK for Java and Kotlin (task 15.4) (#31)
- Implement fiat adapter test suite with fault injection (#24)
- Add e2e tests for support surface states and report-to-support flow (#21)
- Add e2e tests for approval and notification surfaces (#19)
- Implement custody journey surfaces with e2e tests (#17)
- Implement onboarding and account activation journeys (#16)
- Implement e2e tests for activity surfaces (#15)
- Implement settings and preferences e2e test suite (#14)
- Implement AP2 checkout and payment mandate verification with golden vectors
- Implement performance machinery with route budgets, SSR caching, web vitals CI enforcement, RUM pipeline, and 3G testing (#13)
- Add security center e2e tests for step-up authentication gates (#12)
- Implement agent surfaces with e2e test coverage (#11)
- Add shared storage support to SDK and porting kits (#10)
- Implement frozen calldata encoding convention with canonical validation (#9)
- Add deterministic hash primitives for sha256, keccak256 and blake3 (#6)

### Fixed

- Fix core test failures: retain fees on fatal faults, centralise ledger snapshot restore, complete fee meter initialisers
- Fix platform emulator fee wiring and regenerate stale lockfiles
- Fix programs runtime and porting Rust build
- Fix core programs C compile errors

### Changed

- Reconcile interop test fixtures with unified API contracts and remove stale test utilities
- Reconcile human web activity e2e spec with generated API types
- Reconcile human web security e2e spec with real StepUpEvidence contract
- Reconcile human web custody e2e spec with generated API types
- Reconcile human web agents and support e2e specs with generated API types
- Make program deprecation wind-down state replayable from its activity log (#32)

### Tests

- Test the multichain mirror publisher against Ethereum and Solana test networks (#40)
- Cover the real-transition emulator gateway with in-process integration tests and close task 17.2 (#34)

### Housekeeping

- Updated cli
- Complete determinism, differential and fuzz qualification for the programs runtime (task 19.5) (#36)
- Close the deploy/call/upgrade/migration lifecycle for task 20.1 (#35)
- Bump actions/setup-node from 6.5.0 to 7.0.0 (#2)
- Bump actions/checkout from 4.3.1 to 7.0.1 (#1)

### Other

- Polish LayerX public metadata
- CHANGELOG to match the upstream state at c5a61541e13a (2026-08-21 09:35 pulled updates).
- Skip Python bytecode caches in the SDK drift walker and refresh the pipeline lock for the JVM pom
- Promote budgeted activity preparation to a production API and refresh runtime module boundary inventories
- Align gateway fault-injection test with the 503 mapping for core IO refusals
- Platform task 16.1: buyer and seller middleware + non-authority conformance suite (#39)
- Derive program-owned accounts deterministically (task 30.1) (#33)
- Harden Rust, TypeScript and Python SDKs to production (#26)
- Mark task 28.7 as done: program call activity implementation complete

## 2026-08-22

### Added

- Add settings for post-tool hooks in Claude configuration
- Add deterministic storage occupancy accounting
- Add atomic namespace reclamation
- Add bounded resumable storage scans
- Add principal and shared program namespaces

### Changed

- Split the agent workflow into implementation and qualification phases
- Enforce shared storage capability boundaries

### Housekeeping

- Record implementation-only platform task states
- Complete programs module registration boundary

### Other

- Release eleven stalled task claims and open the wave 14 to 17 lanes
- Bring the programs CALL activity ingress onto the trunk
- Seal program monetary settlement authority
- Extend the shared-state isolation gauntlet
- Enable Codify Prod mode for platform implementation

## 2026-08-21

### Added

- Ship isolation and composition adversarial suites
- Add workspace command to platform CLI

### Changed

- Let the Next.js documentation sample build in the workspace
- Bind the module context test to the real transfer and receipt types

### Tests

- Qualify deterministic program execution and metering

### Other

- Admit caller-declared execution budgets
- Carry typed program failure payloads
- Return bounded responses across program calls
- Route program activities through canonical calldata entry
- Modularize the program ABI and host surface
- Harden the program capability ABI and storage isolation
- Release the incomplete performance task claim
- Plan out the programs plane as a real execution layer

## 2026-08-20

### Added

- Add EVM, Solana and CosmWasm porting kits with ported reference contracts
- Ship C and AssemblyScript program authoring SDKs
- Add the Rust program SDK, determinism lint and paid-counter quickstart
- Build the developer documentation site with executable samples
- Add one-command MCP and A2A installation to the platform CLI
- Add the hosted webhooks service and developer dashboards
- Add iOS, Android and agent-framework LayerX integrations
- Add Express, Next.js, FastAPI and Spring integration packages
- Serve the class-name helper from a server-safe entry point
- Add Programs fuzz and versioned replay gates
- Build the receipt verified program registry
- Implement program deployment and migration lifecycle
- Build the hosted gateway policy core
- Build the market maker ramp toolkit
- Build the unified LayerX developer CLI
- Build the multichain mirror publisher
- Build receipt-backed merchant and agent middleware
- Build receipt-gated buyer and seller middleware
- Build portable receipt and mandate verification
- Build the capability ABI and namespaced storage
- Build receipt-backed AP2 mandates
- Build the security center
- Build the production Swift and C sharp SDKs
- Build the x402 facilitator and transport matrix
- Build Visa trusted-agent verification
- Build receipt-backed UCP commerce
- Build the production JVM SDK
- Build verified Ethereum and Solana migration boundaries
- Build the real-transition local emulator
- Build receipt-gated fiat adapter interfaces
- Build receipt-verified x402 roles
- Build the production Go SDK
- Build real support conversations
- Add deterministic program execution metering
- Build receipt-backed activity surfaces
- Add local receipt verification to portable SDKs
- Build approvals and notifications
- Build custody journeys
- Build managed agent journeys
- Add portable proof verification to SDKs
- Build home and move-money journeys
- Build account onboarding and device sign-in
- Build the signed-out public explorer plane
- Build settings and per-user privacy preferences
- Build web performance monitoring foundations
- Build state handling and support reporting
- Build the accessibility and visual foundation
- Build the paired application component kit
- Build capability-aware application shells
- Add verifiable activity exports
- Implement agent key recovery journeys
- Serve receipt-backed activity details
- Implement irreversible agent archive
- Implement receipt-backed move money journey
- Implement receipt-verified agent reclaim paths
- Add the receipt-verified emergency exit journey
- Build the receipt-verified withdrawal and claim journey
- Serve verified public explorer lookups
- Implement the crash-safe deposit journey
- Implement receipt-backed agent controls
- Add the real approval inbox lifecycle
- Add crash-safe receipt-gated journeys
- Build the rebuildable explorer index
- Add deterministic movement routing
- Add durable notification dispatch
- Add the Paxeer emergency exit path
- Add passkey authentication and secure sessions
- Add the KMS-backed custody signer
- Add tamper-evident audit and redacted tracing
- Introduce the all-in-one platform spec spanning the human plane, developer platform, Programs, interop and the multichain surface

### Fixed

- Repair two human web e2e guards that had gone stale
- Restore offline Programs registry builds
- Restore the task ledger wiped by a bad in-place edit in the previous commit
- Resolve notification links and badge counts

### Changed

- Make the copy lint precise enough to be worth enforcing
- Let interop hold the typed-intent boundary it depends on
- Make the human fault gate run the fault suites
- Make the agent unsafe policy actually consult its allowlist
- Name the endpoint fault when the unavailable-core probe fails
- Keep payload encoding inside the human plane's payload authority
- Make the cargo-deny policies enforceable
- Let SDK-built programs compile under the programs unsafe_code gate
- Make all five Rust workspaces compile again
- Make program-to-program calls actually execute as one atomic call graph
- Enforce the Programs monetary law
- Enforce program deprecation wind-down exits
- Enforce external custody ramp labels
- Bind Visa trusted-agent credentials to verified receipts
- Preserve deterministic program metering evidence
- Enforce application kit composition
- Reconcile agent spend from verified receipts
- Make the parallel task limit machine-readable

### Tests

- Verify state from signed mirror archives

### Housekeeping

- Regenerate the platform SDKs for the support failure codes
- Regenerate the SDKs against the current human API schema
- Complete the x402 facilitator transport matrix
- Complete state handling and support reporting
- Complete the accessibility foundation
- Complete the paired application component kit
- Complete capability-aware application shells
- Complete verifiable activity exports
- Complete agent rotation and recovery
- Complete digest-bound approval decisions
- Complete notification link resolution
- Complete receipt-verified wallet binding
- Complete receipt-gated onboarding

### Other

- Bring the QR encoder inside the web runtime dependency budget
- Compose the last two raw screen patterns through the kit
- Bound outbound endpoint I/O without reading an ambient clock
- Unstick two stale platform gate fixtures and refresh the reference docs
- Refresh the frozen human API schema baseline
- Clear the agent workspace clippy gate
- Clear the human web lint errors
- Declare the granular support failure codes in the human API schema
- Clear the clippy gate on four workspaces
- Clear the wallet settings lint and pin the new SDK builds
- Rebuild published program source instead of hashing it
- Reflect the Go and Swift/C# SDK completions in the generated task list
- Manage the Paxeer wallet binding from settings
- Operate the hosted testnet and faucet
- Register the programs module in the core
- Encode portable program execution evidence
- Harden the production SDK runtime contracts
- Persist verified account identity for money journeys
- Refresh the LayerX protocol overview
- Persist redacted web vitals durably
- Stand up the programs workspace on a vendored deterministic WASM engine
- Reserve the Paxeer settlement-domain vocabulary across the custody schemas
- Stand up the interop workspace and the receipt-verified gateway core
- Establish the platform workspace with its policy, drift and release gates
- Raise the parallel work cap and begin the application journeys and platform workspace roots
- Apply digest-bound approval decisions
- Finish the generated human API client gate
- Generate the human-api TypeScript client and gate the build on its freshness
- Assemble the receipt-verified activity feed
- Render approvals from digest-bound disclosures
- Orchestrate receipt-verified agent creation
- Provision scoped agent session keys
- Construct and verify Paxeer withdrawals
- Construct verified Paxeer deposit credits
- Report Paxeer boundary degradation honestly
- Raise the in-progress task limit to 6 and allow parallel sub-agent execution within a wave

## 2026-08-19

### Other

- Give the human service a principal-scoped store with authenticated tenancy, migrations and evidence-pinned retention
- Mark the custody boundary paths and the explorer index in progress

## 2026-08-18

### Added

- Add agent, approval, activity and notification operations to the human API
- Add movement and custody journey operations to the human API
- Add identity, session and wallet-binding operations to the human API
- Add schema-check tool to human workspace CI and mark contract schema task in progress
- Add make entry points for the custody service and Paxeer client suites

### Changed

- Keep agent worktrees out of version control

### Housekeeping

- Close out the human-api contract schema task

### Other

- Report endpoint health and confirmation progress on every finality state
- Accept OR-licensed dependencies, harden the unsafe scan and give CI a Foundry toolchain
- Define the error, streaming and verification contract
- Gate payload encoding behind the intents crate
- Reject unknown intents deterministically and harden the fuzz surface
- update

## 2026-08-16

### Added

- Implement tenant-scoped daemon approval operations
- Add the additive approval contract schema
- Add the real-stack human test harness

### Changed

- Make approval decisions durable and idempotent

### Housekeeping

- Regenerate SDK approval surfaces

### Other

- Lock intent vectors and disclosure checks
- Compile human intents to canonical payloads
- Define the versioned human intent vocabulary
- Stream and audit approval lifecycle events
- Adopt the LayerX UI component library

## 2026-08-15

### Added

- Add the versioned human copy contract
- Add fault-injection and exactly-once qualification
- Add hostile-node no-fabrication qualification
- Add the real-node boundary qualification gate
- Add the differential wire qualification gate
- Add live cross-SDK parity qualification
- Build deterministic SDK generator and drift gate
- Add mode-bound read-only MCP deployment
- Add MCP injection resistance gate
- Add typed MCP write outcomes
- Add verified bounded MCP read tools
- Add audited non-mutating operator controls
- Add forward migration and graceful shutdown gate
- Add honest degraded read mode
- Add fail-closed daemon startup configuration
- Add verifiable tenant audit exports
- Add bounded observability and write health
- Add fail-closed hash-chained audit log
- Ship tenant isolation and deletion gate
- Build self-contained offline proof exports
- Serve verified checkpoint evidence
- Serve bounded cursor-stable history
- Serve proof-gated balance reads
- Implement durable idempotency records
- Implement the durable submission outbox
- Add bounded session-key self-signing
- Ship the adversarial policy harness
- Implement digest-bound approval holds
- Add auditable policy dry runs
- Add versioned policy activation
- Implement deterministic policy evaluation
- Create protocol budgets through signed activities
- Add honest capability enforcement reports
- Add persistent capability attenuation
- Add explicit deterministic capabilities
- Build the agent identity and authority foundation
- Add provider-isolated availability retrieval
- Add ordered event streaming and durable cursors
- Add proof-gated reads and gap-free history
- Add verified receipt lookup and unknown resolution
- Implement byte-exact signed submission
- Add client connection lifecycle and head tracking
- Add bounded LNI transports and multiplexed framing

### Fixed

- Resolve tenant scope only from authenticated tokens
- Resolve unknown submissions by receipt lookup

### Changed

- Enforce human workspace supply-chain boundaries
- Enforce digest-bound MCP approval thresholds
- Bind MCP servers to daemon session scope
- Enforce tenant redaction across outputs
- Make durable storage keys structurally tenant scoped
- Enforce durable tenant quotas and isolated shedding
- Enforce layered shared rate limits
- Bind disclosures to canonical preparations
- Reconcile budgets against protocol state
- Enforce capability narrowing
- Enforce LNI startup handshake and capability gaps

### Removed

- Remove generated boundary build artifacts

### Tests

- Prove limits preserve exactly-once effects
- Verify and persist submission finality
- Verify and preserve protocol receipts
- Qualify the LNI boundary against a real node

### Housekeeping

- Record complete audit decision evidence
- Complete unknown budget recovery
- Close budget reconciliation graph seam
- Close capability attenuation graph seam

### Other

- Establish the human control plane workspace
- Publish SDK compatibility and guarantee gates
- Package the generated Python SDK
- Package the generated TypeScript SDK
- Author the Rust agent SDK surface
- Gate daemon writes on boundary handshake
- Normalize tenant errors and bound observability labels
- Isolate tenant signers channels and runtime config
- Bound request lifetimes without orphaning submissions
- Bound and prioritize core boundary traffic
- Authenticate and bind outbound event delivery
- Block delivery across explicit event gaps
- Deliver durable events across the live seam
- Persist scoped subscriptions and cursors
- Persist ordered core event ingestion
- Gate cached reads on verified evidence
- Audit verified availability retrieval
- Recover durable submissions without duplication
- Expire preparations and release reservations
- Gate submission on exact signature binding
- Prepare canonical activities from core state
- Refresh the policy fuzz lockfile
- Report budget divergence conservatively
- Persist unknown budget reservations across restarts
- Serialize multi-scope budget reservations
- Account capability ceilings from verified receipts
- Propagate authority revocations into sessions
- Define API error and idempotency contracts
- Define subscription and stream contracts
- Define verified read and export contracts
- Define prepare and submission contracts
- Define identity and budget contracts
- Define the generated Agent API schema
- Publish the LNI capability gap report
- Constrain the optional LNI stable ABI
- Define the versioned LayerX node interface schema

## 2026-08-14

### Added

- Implement exact Merkle proof verification
- Implement the remote signer transport and refusal semantics
- Implement the encrypted keystore and session-key issuance
- Implement disclosure-bound signing
- Implement Ed25519 and secp256k1 verification against core vectors
- Ship the codec fuzz targets
- Implement the canonical primitive encoder and decoder
- Build the boundary-purity CI gate
- Create the agent workspace and its build entry points

### Changed

- Enforce secret hygiene as a build gate
- Enforce non-canonical rejection and declared limits as a suite

### Tests

- Verify availability chunks and reassembled roots
- Verify bonded checkpoint certificates
- Verify signed batch inclusion proofs
- Verify signed receipts from canonical bytes

### Other

- Gate proof levels on verified evidence
- Stabilize sanitizer artifact selection and preserve dependency resolution
- Define the signer abstraction and the local signer
- Run the byte-parity differential harness against the C core
- Compute identifiers and signing preimages under domain tags
- Encode and decode envelopes, payloads and receipts
- Load and enforce the protocol conformance vectors
- Define verification status and value provenance
- Define receipts, batch headers and checkpoint certificates
- Define the activity envelope and the module payload types
- Define identifiers, account namespaces and asset types
- Stand up the test, property and fuzz harness
- Define the layer error model and the protocol result-code mapping [done]
- Lock the dependency, unsafe and supply-chain policy [done]
- Prepare LayerX Protocol repository


