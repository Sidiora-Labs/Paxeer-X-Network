# Renderer

## Overview

Orchestrates chart overlay services. Constructor(instance, host, broker) creates PositionLineService, OrderLineService, ExecutionService, TPSLService. renderPositions(positions) waits chartReady and onDataLoaded, then updates position/order/tpsl lines. renderPendingOrders(pendingOrders) draws order lines. renderFilledOrders(filledOrders, basePriceDecimal) draws executions. remove() clears all lines and destroys execution service. chartReady() and onDataLoaded() return Promises for sync.
