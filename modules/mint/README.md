# modules/mint

## Overview

The minting mechanism was designed for creating new tokens according to a predefined schedule. It allows the creation of scheduled token release structures that define the release of tokens over a period of time. The Mint module provides a system for managing token minting, release schedule, and related parameters. Once every scheduled release has been minted, no further tokens are minted.

Minting occurs over a specified period, with a proportion of the total mint amount distributed daily (UTC).

### Minting Mechanism

The minting mechanism is built on a daily distribution model. The total_mint_amount is a predefined amount of tokens set to be minted within the duration specified by a start and end date. This total amount is evenly distributed over each day of the minting period, ensuring a consistent daily distribution of tokens.

### Daily Mint Calculation

The daily mint amount is derived by dividing the `remaining_mint_amount` by the number of days left in the minting period. This calculation is based on the assumption of a uniform distribution of tokens throughout the period, barring any instances where the chain is down for more than a day.

For example, if the `total_mint_amount` is set to 1,000,000 tokens and the minting period is 100 days, the daily mint amount would be 10,000 tokens. However, if there was a network outage and the chain was down for 1 day, the daily mint amount would be recalculated. If 500,000 tokens had already been distributed in the first 50 days, with 49 days remaining and a `remaining_mint_amount` of 500,000 tokens, the revised daily mint amount would be 10,204 tokens. This adjusted amount would be minted daily until the 100th day, when 10,208 tokens would be minted to achieve the total of 1,000,000 tokens.

### Minting Process

Minting runs in the epoch module's `AfterEpochEnd` hook. At the first epoch end of each UTC day inside the release period, the daily mint amount is created and sent to the fee_collector account. From here, it's distributed to stakers in the same manner as transaction fees (percentage-based).

### Updating the Minting Schedule

The minting schedule, including the start date, end date, and `total_mint_amount`, can be updated through a governance proposal. This feature allows network participants to adjust the minting parameters as necessary in response to the network's needs and conditions.

Note: Changes to the `total_mint_amount` or `remaining_mint_amount` after the start date will not impact tokens already minted.

## State

### Minter

The minter holds the current release information. It can be updated through a governance proposal (see below). Dates use the `YYYY-MM-DD` format.

```go
type Minter struct {
    // The day where the mint begins
    StartDate           string `protobuf:"bytes,1,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
    // The day where the mint ends
    EndDate             string `protobuf:"bytes,2,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
    // Denom for the coins minted, defaults to uhpx
    Denom               string `protobuf:"bytes,3,opt,name=denom,proto3" json:"denom,omitempty"`
    // Total amount to be minted
    TotalMintAmount     uint64 `protobuf:"varint,4,opt,name=total_mint_amount,json=totalMintAmount,proto3" json:"total_mint_amount,omitempty"`
    // Remaining amount to be minted
    RemainingMintAmount uint64 `protobuf:"varint,5,opt,name=remaining_mint_amount,json=remainingMintAmount,proto3" json:"remaining_mint_amount,omitempty"`
    // Last amount minted (usually from the day before)
    LastMintAmount      uint64 `protobuf:"varint,6,opt,name=last_mint_amount,json=lastMintAmount,proto3" json:"last_mint_amount,omitempty"`
    // Last day minted
    LastMintDate        string `protobuf:"bytes,7,opt,name=last_mint_date,json=lastMintDate,proto3" json:"last_mint_date,omitempty"`
    // The height of the last mint
    LastMintHeight      uint64 `protobuf:"varint,8,opt,name=last_mint_height,json=lastMintHeight,proto3" json:"last_mint_height,omitempty"`
}
```

### Params

The mint module stores its params in state, it can be updated with governance or the address with authority.

```go
type Params struct {
    // type of coin to mint
    MintDenom string `protobuf:"bytes,1,opt,name=mint_denom,json=mintDenom,proto3" json:"mint_denom,omitempty"`
    // List of token release schedules
    TokenReleaseSchedule []ScheduledTokenRelease `protobuf:"bytes,2,rep,name=token_release_schedule,json=tokenReleaseSchedule,proto3" json:"token_release_schedule" yaml:"token_release_schedule"`
}
...
type ScheduledTokenRelease struct {
    // The day where the mint begins
    StartDate          string `protobuf:"bytes,1,opt,name=start_date,json=startDate,proto3" json:"start_date,omitempty"`
    // The day where the mint ends
    EndDate            string `protobuf:"bytes,2,opt,name=end_date,json=endDate,proto3" json:"end_date,omitempty"`
    // Total amount to be minted
    TokenReleaseAmount uint64 `protobuf:"varint,3,opt,name=token_release_amount,json=tokenReleaseAmount,proto3" json:"token_release_amount,omitempty"`
}

```

### Governance

#### Minter Governance Proposal
Here is an example of how to submit a governance proposal to update the Minter parameters:

First, prepare a proposal in JSON format, like the minter_prop.json file below:

```json
{
  "title": "Test Update Minter",
  "description": "Updating test minter",
  "minter": {
    "start_date": "<YYYY-MM-DD>",
    "end_date": "<YYYY-MM-DD>",
    "denom": "uhpx",
    "total_mint_amount": 100000
  }
}
```

Then, submit the proposal with the following command:

```bash
paxd tx gov submit-proposal update-minter ./minter_prop.json --deposit 20pax --from admin -b block -y --gas 200000 --fees 2000uhpx
```

This command submits a proposal to update the minter. The --deposit flag is used to provide the initial deposit. The proposal is submitted by the address provided with the --from flag.

Before the proposal, the Minter parameters might look like this:

```bash
> paxd q mint minter
denom: uhpx
end_date: "<old end date>"
last_mint_amount: "333333333333"
last_mint_date: "<last mint date>"
last_mint_height: "0"
remaining_mint_amount: "666666666666"
start_date: "<old start date>"
total_mint_amount: "999999999999"
```

After the proposal is passed, the Minter parameters would be updated as per the proposal:

```bash
> paxd q mint minter
denom: uhpx
end_date: "<new end date>"
last_mint_amount: "0"
last_mint_date: ""
last_mint_height: "0"
remaining_mint_amount: "0"
start_date: "<new start date>"
total_mint_amount: "100000"
```

In this example, start_date and end_date take the values from the proposal and total_mint_amount has been reduced to "100000".

### Params Governance Proposal

Here is an example for updating the params for the mint module

```json
{
  "title": "Param Change Proposal",
  "description": "Proposal to change some parameters",
  "changes": [
    {
      "subspace": "mint",
      "key": "MintDenom",
      "value": "uhpx"
    },
    {
      "subspace": "mint",
      "key": "TokenReleaseSchedule",
      "value": [
        {
          "token_release_amount": 500,
          "start_date": "<YYYY-MM-DD>",
          "end_date": "<YYYY-MM-DD>"
        },
        {
          "token_release_amount": 1000,
          "start_date": "<YYYY-MM-DD>",
          "end_date": "<YYYY-MM-DD>"
        }
      ]
    }
  ]
}
```

Submit the proposal

```bash
paxd tx gov submit-proposal param-change ./param_change_prop.json --from admin -b block -y --gas 200000 --fees 200000uhpx
```

## Epoch hook

At the end of each epoch (60s by default), the `AfterEpochEnd` hook picks the active release from the schedule and, if nothing has been minted yet that UTC day, mints that day's share of the remaining amount. On or after the end date, the whole remaining amount is released.

### Minting events

#### Type: Mint

- mint_date: date of the mint
- mint_epoch: epoch of the mint
- amount: amount minted


### Metrics

Each successful mint records the telemetry gauge `pax_mint_coins{denom}` and the OpenTelemetry gauge `mint_coins_minted` (attribute `denom`).
