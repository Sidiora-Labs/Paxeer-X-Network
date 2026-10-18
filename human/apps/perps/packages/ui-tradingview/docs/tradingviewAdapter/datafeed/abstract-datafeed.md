# AbstractDatafeed

## Overview

Base datafeed: requests configuration from tv/config, sets up SymbolsStorage when supports_group_request or !supports_search, implements getBars (via HistoryProvider, cursor-based), onReady, searchSymbols, resolveSymbol. Abstract: subscribeBars, unsubscribeBars, getQuotes, subscribeQuotes, unsubscribeQuotes, remove. Default config includes supported_resolutions, supports_marks: false, supports_timescale_marks: false.
