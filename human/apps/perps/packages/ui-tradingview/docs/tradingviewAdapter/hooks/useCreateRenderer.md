# useCreateRenderer

## Overview

Creates and updates the Renderer instance based on symbol and displayControlSetting. Uses usePositionStream(symbol), useOrderStream (INCOMPLETE and FILLED), useSymbolsInfo; maps positions and orders to chart format and filters by display toggles (position, buySell, limitOrders, stopOrders, tpsl, positionTpsl, trailingStop). Returns [createRenderer, removeRenderer]. createRenderer(instance, host, broker, container) instantiates Renderer; removeRenderer() removes it. Effects call renderer.renderPositions, renderFilledOrders, renderPendingOrders when data or settings change.

## Parameters

symbol: string; displayControlSetting?: DisplayControlSettingInterface.

## Returns

[createRenderer, removeRenderer] as const.
