# CosmWasm bindings

This package connects CosmWasm contracts on the Paxeer X chain to the chain's own modules through custom queries and messages.

Queries ([`queries.go`](queries.go), [`query_plugin.go`](query_plugin.go)):

- Oracle: exchange rates and TWAPs
- Epoch: the current epoch
- Token Factory: denom authority metadata and denoms from a creator
- EVM: static calls; ERC-20, ERC-721 and ERC-1155 helpers; Pax and EVM address mapping; interface support checks
- Staking extension: unbonding delegations

Messages ([`encoder.go`](encoder.go), [`message_plugin.go`](message_plugin.go)):

- Token Factory: create denom, mint, burn, change admin, set metadata
- EVM: `call_evm` and `delegate_call_evm`, dispatched as `MsgInternalEVMCall` and `MsgInternalEVMDelegateCall`
