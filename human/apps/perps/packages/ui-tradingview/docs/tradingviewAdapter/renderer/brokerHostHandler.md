# brokerHostHandler

## Overview

Patches the broker connection adapter host: preventDefaultRenderHack(host) overrides setBrokerConnectionAdapter to inject a no-op _ordersCache and _waitForOrderModification. forceSilentOrdersPlacement(instance, host) subscribes to silentOrdersPlacement and forces it true, and hides sell/buy buttons. Exported default function brokerHostHandler(instance, host) runs both.
