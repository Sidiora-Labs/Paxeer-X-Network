<!--
parent:
  order: false
-->

# List of Modules

These are the base modules of the SDK fork under `sdk/x`. The Paxeer X chain application wires all of them in [`node/app.go`](../../node/app.go). Each links to its specification:

- [Auth](auth/spec/README.md) - Authentication of accounts and transactions.
- [Authz](authz/spec/README.md) - Authorization for accounts to perform actions on behalf of other accounts.
- [Bank](bank/spec/README.md) - Token transfer functionalities.
- [Capability](capability/spec/README.md) - Object capability implementation.
- [Distribution](distribution/spec/README.md) - Fee distribution, and staking token provision distribution.
- [Evidence](evidence/spec/README.md) - Evidence handling for double signing, misbehaviour, etc.
- [Feegrant](feegrant/spec/README.md) - Fee allowances granted from one account to another.
- [Governance](gov/spec/README.md) - On-chain proposals and voting.
- [Params](params/spec/README.md) - Globally available parameter store.
- [Slashing](slashing/spec/README.md) - Validator punishment mechanisms ([implementation notes](slashing/README.md)).
- [Staking](staking/spec/README.md) - Proof-of-Stake layer for public blockchains.
- [Upgrade](upgrade/spec/README.md) - Software upgrades handling and coordination.
- `genutil` - Genesis transaction and genesis file utilities (no spec).

The mint module is not here: it lives in [`modules/mint`](../../modules/mint/README.md) with the other chain-specific modules in [`modules/`](../../modules/README.md).

## IBC

IBC is not part of `sdk/x`. It lives in [`interchain/`](../../interchain/README.md).

## FeesParams

To query for current fee params:

```bash
paxd q params feesparams
```

To update the fees params, submit a `param-change` governance proposal with `paxd tx gov submit-proposal param-change <proposal.json> --from <key>` and a proposal file like this:

```json
{
  "title": "Update Global Minimum Prices",
  "description": "This proposal seeks update the global minimum prices for a gas unit.",
  "changes": [
    {
      "subspace": "params",
      "key": "FeesParams",
      "value": {
	  "global_minimum_gas_prices": [
    		{
			"denom": "uhpx",
      		"amount":	 "1.00000000000000000"
    		}
  	]
 	}
    }
  ],
  "deposit": "1000000000uhpx",
  "is_expedited": true
}
```
