## Abstract

The Paxeer X chain has an `oracle` module that provides asset exchange rates for other modules and contracts (EVM contracts reach it through the `oracle` precompile in [`precompiles/oracle`](../../precompiles/oracle)). Bonded validators are expected to submit exchange-rate votes.

Voting is a single step: every vote period, each validator (or the feeder it delegated to) submits a `MsgAggregateExchangeRateVote` with its proposed rates for the current vote targets. On the last block of each vote period the votes are tallied, and a weighted median (weighted by validator voting power) becomes the exchange rate for each asset.

There are penalties for non-participation and for bad data. Each validator has success, abstain, and miss counters. At the end of every slash window, a validator whose valid vote rate is below the minimum is slashed and jailed.

## Contents

## Concepts

### Voting Procedure

Votes are collected per denom into ballots. In the mid-block step of the last block of each vote period (`VotePeriod` blocks):

1. Among the vote targets whose ballots reach `VoteThreshold` of bonded voting power, the one with the highest voting power becomes the reference denom. Ballots below the threshold are not tallied; voting in them counts as a win.
2. The reference rate is the weighted median of its ballot. Other ballots are converted to cross rates against the reference denom, tallied, and converted back.
3. Each accepted rate is stored as the base exchange rate and emits an `exchange_rate_update` event.
4. Ballots are cleared and the vote targets are re-synced with the `Whitelist` param.

### Reward Band

A vote counts as a win when it falls within `RewardBand` around the weighted median (half the band on each side, widened to the ballot's standard deviation when that is larger).

### Slashing

On the last block of each slash window (`SlashWindow` blocks), the end blocker computes each validator's valid vote rate as `success / (success + abstain + miss)`. If it is below `MinValidPerWindow`, a bonded, unjailed validator is slashed by `SlashFraction` and jailed. The counters are then reset and an `end_slash_window` event reports them.

### Abstaining from Voting

A vote with a non-positive exchange rate is an abstain and carries zero voting power in the tally. At the end of each vote period, a validator that won every vote target has its success counter incremented; one that submitted no vote has its abstain counter incremented; any other validator has its miss counter incremented.

## Messages

- `MsgAggregateExchangeRateVote` (`exchange_rates`, `feeder`, `validator`) — submits the validator's rates for all vote targets.
- `MsgDelegateFeedConsent` — lets a validator delegate voting to a separate feeder address.

## Events

`exchange_rate_update`, `aggregate_vote`, `feed_delegate`, and `end_slash_window`; see [`types/events.go`](types/events.go).

## Parameters

| Param | Default |
| ----- | ------- |
| `VotePeriod` | 2 blocks |
| `VoteThreshold` | 0.667 |
| `RewardBand` | 0.02 |
| `Whitelist` | `uatom`, `ueth` |
| `SlashFraction` | 0 |
| `SlashWindow` | two days of blocks |
| `MinValidPerWindow` | 0 |
| `LookbackDuration` | 3600 seconds |

## Transactions

```bash
paxd tx oracle aggregate-vote [exchange-rates] [validator]
paxd tx oracle set-feeder [feeder]
```

## Queries

```bash
paxd q oracle exchange-rates [denom]
paxd q oracle price-snapshot-history
paxd q oracle twaps [lookback-seconds]
paxd q oracle actives
paxd q oracle params
paxd q oracle feeder [validator]
paxd q oracle vote-penalty-counter [validator]
paxd q oracle vote-targets
```

The detailed spec lives in [`spec/`](spec/README.md).
