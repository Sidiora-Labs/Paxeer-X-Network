# Rewards Calculation Module

This module provides functionality for calculating estimated rewards and estimated tickets earned.

## Features

### 1. Estimated Rewards

- Supports multiple prize pool configurations
- Supports different metrics based on trading volume and PnL (Profit and Loss)
- Supports fixed position rewards and position range rewards
- Automatically estimates user ranking and calculates corresponding rewards

### 2. Estimated Tickets

- Supports tiered mode: Awards different ticket amounts based on different trading volume tiers
- Supports linear mode: Earn Y tickets for every X trading volume

## Type Definitions

### Prize Pool Configuration

```typescript
interface PrizePool {
  pool_id: string; // Prize pool ID
  label: string; // Prize pool label
  total_prize: number; // Total prize amount
  currency: string; // Reward currency
  metric: "volume" | "pnl"; // Evaluation metric
  tiers: PrizePoolTier[]; // Tier configuration
}
```

### Ticket Rules

```typescript
interface TicketRules {
  total_prize: number; // Total ticket prize amount
  currency: string; // Currency
  metric: "volume" | "pnl"; // Evaluation metric
  mode: "tiered" | "linear"; // Mode
  tiers?: TicketTierRule[]; // Tiered mode configuration
  linear?: TicketLinearRule; // Linear mode configuration
}
```

## Usage Examples

### Basic Usage

```typescript
import {
  calculateEstimatedRewards,
  calculateEstimatedTickets,
  CampaignConfig,
  UserData,
} from "./utils";

const userdata: UserData = {
  account_id: "user_001",
  trading_volume: 50000,
  pnl: 1500,
  current_rank: 5,
  total_participants: 1000,
};

const campaign: CampaignConfig = {
  // ... campaign configuration
};

// Calculate estimated rewards
const rewards = calculateEstimatedRewards(userdata, campaign);
console.log(`Estimated rewards: ${rewards?.amount} ${rewards?.currency}`);

// Calculate estimated tickets
if (campaign.ticket_rules) {
  const tickets = calculateEstimatedTickets(userdata, campaign.ticket_rules);
  console.log(`Estimated tickets: ${tickets}`);
}
```

### Usage in React Components

```typescript
import { RewardsDesktopUI } from './rewards.desktop.ui';

function MyComponent() {
  return (
    <RewardsDesktopUI
      campaign={campaignConfig}
      userdata={userData}
    />
  );
}
```

## Configuration Examples

### Tiered Mode Ticket Configuration

```typescript
ticket_rules: {
  total_prize: 2000,
  currency: "WIF",
  metric: "volume",
  mode: "tiered",
  tiers: [
    { value: 25000, tickets: 10 }, // ≥ 25,000 volume → 10 tickets
    { value: 10000, tickets: 5 },  // ≥ 10,000 volume → 5 tickets
    { value: 5000, tickets: 1 }    // ≥ 5,000 volume → 1 ticket
  ]
}
```

### Linear Mode Ticket Configuration

```typescript
ticket_rules: {
  total_prize: 1000,
  currency: "WIF",
  metric: "volume",
  mode: "linear",
  linear: {
    every: 5000, // Every 5000 trading volume
    tickets: 1   // Earn 1 ticket
  }
}
```

### Prize Pool Configuration Example

```typescript
prize_pools: [
  {
    pool_id: "general",
    label: "General Pool",
    total_prize: 10000,
    currency: "USDC",
    metric: "volume",
    tiers: [
      { position: 1, amount: 3000 }, // 1st place: 3000 USDC
      { position: 2, amount: 2000 }, // 2nd place: 2000 USDC
      { position: 3, amount: 1000 }, // 3rd place: 1000 USDC
      { position_range: [4, 10], amount: 500 }, // 4th-10th place: 500 USDC each
      { position_range: [11, 50], amount: 100 }, // 11th-50th place: 100 USDC each
    ],
  },
];
```

## Calculation Logic

### Ranking Estimation

The system estimates user ranking based on trading performance:

- If `current_rank` is provided, it is used directly
- Otherwise, a simple estimation is made based on trading volume/PnL:
  - ≥ 100,000: Rank 1
  - ≥ 50,000: Top 5%
  - ≥ 10,000: Top 20%
  - ≥ 1,000: Top 50%
  - < 1,000: Bottom 80%

### Reward Calculation

1. Iterate through all prize pools
2. Get user data based on the pool's metric (volume/pnl)
3. Estimate user ranking for that metric
4. Find matching tier and accumulate rewards

### Ticket Calculation

- **Tiered Mode**: Find the highest tier that matches the user's trading volume
- **Linear Mode**: Calculate proportionally using `Math.floor(volume / every) * tickets`

## Important Notes

1. Ranking estimation is based on simplified logic; it's recommended to use real leaderboard data in actual applications
2. When user trading volume or PnL is 0 or negative, some calculations may return empty results
3. It's recommended to integrate real API data sources in production environments
