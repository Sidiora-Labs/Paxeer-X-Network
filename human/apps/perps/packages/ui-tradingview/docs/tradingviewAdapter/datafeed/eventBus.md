# MultiBroadcastEventBus

## Overview

Simple event bus: subscribe(eventName, callback) returns unsubscribe function; unsubscribe(eventName, callback); publish(eventName, data) invokes all callbacks for that event. Used by Datafeed to broadcast tickerUpdate to quote subscribers.
