# HistoryProvider

## Overview

Fetches historical bars for the chart: maps resolution to API format, calls tv/history or v1/tv/kline_history via Requester, returns GetBarsResult (bars, meta). Handles limited response length and expectedOrder. PeriodParamsWithOptionalCountback extends PeriodParams with optional countBack.
