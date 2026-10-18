# orderInput/index.tsx (OrderInput)

## Overview

Dispatches order input UI by order type: ScaledOrderInput, TrailingStopInput, or (optional TriggerPriceInput +) PriceInput + QtyAndTotalInput for limit/stop orders.

## Props (OrderInputProps)

Extends `PriceInputProps` (without `order_price` and `order_type`). Includes `values: Partial<PaxeerNetworkOrder>`, `priceInputContainerWidth`, `fillMiddleValue`, `bbo` (bboStatus, bboType, onBBOChange, toggleBBO).

## Usage

Rendered by OrderEntry with `formattedOrder`, `priceInputContainerWidth`, `fillMiddleValue`, and `bbo` from script state.
