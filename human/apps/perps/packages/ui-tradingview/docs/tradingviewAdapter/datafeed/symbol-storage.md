# SymbolsStorage

## Overview

Resolves and searches symbols for the datafeed. Fetches exchange/symbol list from API, builds symbol info (LibrarySymbolInfo) with supported resolutions, pricescale, etc. searchSymbols(userInput, exchange, symbolType, limit) and resolveSymbol(symbolName, currencyCode?, unitId?) return results for TradingView.
