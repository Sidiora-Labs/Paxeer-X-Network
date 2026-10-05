## Abstract

The Paxeer X chain has an `oracle` module that provides asset exchange rates for other modules and contracts. Bonded validators are expected to submit exchange-rate votes.

Voting is a single step. Every vote period, each validator (or its delegated feeder) submits a `MsgAggregateExchangeRateVote` with its proposed rates. The votes are tallied in the module's mid-block step on the last block of the vote period: a weighted median (weighted by validator voting power) becomes the exchange rate for each asset, so transactions later in the same block see the new rates.

There are penalties for non-participation and for bad data. Each validator has success, abstain, and miss counters. At the end of every slash window, the end blocker slashes and jails a validator whose valid vote rate is below `MinValidPerWindow`, then resets the counters.

The current behavior, parameters, CLI, and defaults are summarized in the module [README](../README.md).

## Contents

1. **[Concepts](01_concepts.md)**
2. **[State](02_state.md)**
3. **[End Block](03_end_block.md)**
4. **[Messages](04_messages.md)**
5. **[Events](05_events.md)**
6. **[Parameters](06_params.md)**
7. **[MidBlock design](MidBlock.md)**
