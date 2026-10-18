# provider

## Overview

Symbol context and provider: supplies symbol metadata (base_dp, quote_dp, ticks, base, quote, origin, quote_min/max) for the current symbol so cells and inputs can format numbers and validate.

## Files

| File | Description |
|------|-------------|
| [symbolContext](symbolContext.md) | `SymbolContextState` interface, `SymbolContext`, `useSymbolContext`. |
| [symbolProvider](symbolProvider.md) | `SymbolProvider` — wraps children with context value from `useSymbolsInfo()[symbol]`. |
