# track

## Overview

Analytics tracker event names. Used to send consistent event names for withdraw, deposit, sign-in, place order, wallet connect, link device, vault, BBO, close positions, language switch, etc.

## Exports

### TrackerEventName (enum)

Includes virtual events (e.g. `track_identify_user_id`, `track_identify`, `track_custom_event`) and real event names such as:

- `withdraw_request_success` / `withdraw_request_failure`
- `deposit_request_success` / `deposit_request_failure`
- `sign_message_success`
- `place_order_success`
- `connect_wallet_success`
- `click_link_device_button`, `sign_link_device_message_success`, `link_device_modal_click_confirm`
- `social_login_success`
- `click_switch_network`, `click_switch_wallet`, `switch_network_success`
- `click_export_private_key`
- `switch_language`
- `leaderboard_campaign_click_trade_now` / `leaderboard_campaign_click_learn_more`
- `vault_deposit_success` / `vault_deposit_failed`, `vault_withdraw_success` / `vault_withdraw_failed`
- `click_bbo_button`
- `confirm_close_all_positions`

## Usage example

```typescript
import { TrackerEventName } from "@paxeer-network/types";
track(TrackerEventName.placeOrderSuccess, { orderId: "..." });
```
