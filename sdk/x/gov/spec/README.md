<!--
order: 0
title: Gov Overview
parent:
  title: "gov"
-->

# `gov`

## Abstract

This paper specifies the Governance module of the Cosmos-SDK, which was first
described in the Cosmos whitepaper.

The module enables Cosmos-SDK based blockchain to support an on-chain governance
system. In this system, holders of the native staking token of the chain can vote
on proposals on a 1 token 1 vote basis. Next is a list of features the module
currently supports:

- **Proposal submission:** Users can submit proposals with a deposit. Once the
minimum deposit is reached, proposal enters voting period
- **Vote:** Participants can vote on proposals that reached MinDeposit
- **Inheritance and penalties:** Delegators inherit their validator's vote if
they don't vote themselves.
- **Deposits:** When voting ends, deposits are refunded unless quorum was not
reached or the veto threshold was exceeded, in which case they are burned.
Deposits on a proposal that never reaches the minimum deposit are burned when
its deposit period ends.
- **Expedited proposals:** A proposal submitted with `is_expedited` uses the
expedited minimum deposit, voting period, quorum and threshold. If an
expedited proposal does not pass, it is converted to a regular proposal and
voting continues for the regular voting period.

In this repository the module is wired into the Paxeer X chain application in [`node/app.go`](../../../../node/app.go).
Features that may be added in the future are described in [Future Improvements](05_future_improvements.md).

## Contents

The following specification uses *ATOM* as the native staking token. The module
can be adapted to any Proof-Of-Stake blockchain by replacing *ATOM* with the native
staking token of the chain.

1. **[Concepts](01_concepts.md)**
    - [Proposal submission](01_concepts.md#proposal-submission)
    - [Vote](01_concepts.md#vote)
    - [Software Upgrade](01_concepts.md#software-upgrade)
2. **[State](02_state.md)**
    - [Parameters and base types](02_state.md#parameters-and-base-types)
    - [Deposit](02_state.md#deposit)
    - [ValidatorGovInfo](02_state.md#validatorgovinfo)
    - [Proposals](02_state.md#proposals)
    - [Stores](02_state.md#stores)
    - [Proposal Processing Queue](02_state.md#proposal-processing-queue)
3. **[Messages](03_messages.md)**
    - [Proposal Submission](03_messages.md#proposal-submission)
    - [Deposit](03_messages.md#deposit)
    - [Vote](03_messages.md#vote)
4. **[Events](04_events.md)**
    - [EndBlocker](04_events.md#endblocker)
    - [Handlers](04_events.md#handlers)
5. **[Future Improvements](05_future_improvements.md)**
6. **[Parameters](06_params.md)**
