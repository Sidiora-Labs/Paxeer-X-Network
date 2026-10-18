# persistUtils

## Overview

Chart and adapter setting persistence: in-memory cache (chartCacheMap) plus localStorage. getChartData(chartKey, isLoggedIn) reads cache first, then localStorage, returns { savedData, adapterSetting }. saveChartData/saveChartAdapterSetting write to localStorage and cache. deleteChartData(chartKeys, isLoggedIn) clears keys and adapter keys. copyChartData(fromChartKey, toChartKey, isLoggedIn) copies from cache to new key. clearChartCache() clears cache. getChartSettingAdapterKey(chartKey) returns `${chartKey}_adapter`.
