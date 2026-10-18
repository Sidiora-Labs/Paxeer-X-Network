export const SETTING_CHAR_LIMIT = 300_000;
export const getChartSettingAdapterKey = (chartKey: string) =>
  `${chartKey}_adapter`;

export const defaultSettings = {
  "trading.chart.proterty": JSON.stringify({
    showSellBuyButtons: 0,
    noConfirmEnabled: 1,
    qweqrq: 0,
    showPricesWithZeroVolume: 1,
    showSpread: 1,
    orderExecutedSoundParams: '{"enabled":0,"name":"alert/alarm_clock"}',
  }),
  "hint.startFocusedZoom": "true",
};

const chartCacheMap = new Map<string, string>();

export const clearChartCache = () => chartCacheMap.clear();

const parseAndCacheChartData = (
  chartKey: string,
  savedDataString: string,
  adapterSettingString: string
) => {
  const settingAdapterKey = getChartSettingAdapterKey(chartKey);
  try {
    const transformedSavedDataString = savedDataString;

    const savedData = transformedSavedDataString
      ? JSON.parse(transformedSavedDataString)
      : undefined;
    const adapterSetting = adapterSettingString
      ? JSON.parse(adapterSettingString)
      : defaultSettings;

    if (transformedSavedDataString) {
      chartCacheMap.set(chartKey, transformedSavedDataString);
    }
    if (adapterSettingString) {
      chartCacheMap.set(settingAdapterKey, adapterSettingString);
    }

    return { savedData, adapterSetting };
  } catch (e) {
    console.error(e);
  }

  return { savedData: undefined, adapterSetting: defaultSettings };
};

// 1. Try to fetch data from cache first.
//    - That make the copying the chart synchronously. cause new chart will find the data from cache
//    - This causes the request only happen 1 time before refreshing the page.
// 2. If there is no data in cache, try to fetch it from api
// 3. If there is no data from backend, try to fetch it from local storage (it's a transition mechanism between localstorage way and beckend storage)
// 4. If data format is not a valid json format or api call failed, it fallback to the default one.
export const getChartData = async (chartKey: string, isLoggedIn: boolean) => {
  const settingAdapterKey = getChartSettingAdapterKey(chartKey);

  const localStorageSavedData = localStorage.getItem(chartKey) || "";
  const localStorageAdapterSetting =
    localStorage.getItem(settingAdapterKey) || "";

  try {
    if (chartCacheMap.has(chartKey) && chartCacheMap.has(settingAdapterKey)) {
      return {
        savedData: JSON.parse(chartCacheMap.get(chartKey)!),
        adapterSetting: JSON.parse(chartCacheMap.get(settingAdapterKey)!),
      };
    }
  } catch (e) {
    console.error(e);
  }

  return parseAndCacheChartData(
    chartKey,
    localStorageSavedData,
    localStorageAdapterSetting
  );
};

export const saveChartData = async (
  chartKey: string,
  setting: string,
  isLoggedIn: boolean
) => {
  if (setting) {
    localStorage.setItem(chartKey, setting);
    chartCacheMap.set(chartKey, setting);
  }
};

export const saveChartAdapterSetting = async (
  chartKey: string,
  setting: string,
  isLoggedIn: boolean
) => {
  const settingAdapterKey = getChartSettingAdapterKey(chartKey);

  if (setting) {
    localStorage.setItem(settingAdapterKey, setting);
    chartCacheMap.set(settingAdapterKey, setting);
  }
};

export const deleteChartData = async (
  chartKeys: string[],
  isLoggedIn: boolean
) => {
  const allKeys = chartKeys.concat(chartKeys.map(getChartSettingAdapterKey));

  allKeys.forEach((key) => {
    if (localStorage.getItem(key)) {
      localStorage.removeItem(key);
    }
    if (chartCacheMap.has(key)) {
      chartCacheMap.delete(key);
    }
  });
};

export const copyChartData = async (
  fromChartKey: string,
  toChartKey: string,
  isLoggedIn: boolean
) => {
  const fromSettingAdapterKey = getChartSettingAdapterKey(fromChartKey);
  const chartData = chartCacheMap.get(fromChartKey);
  const adapterSettingData = chartCacheMap.get(fromSettingAdapterKey);

  if (chartData && adapterSettingData) {
    saveChartData(toChartKey, chartData, isLoggedIn);
    saveChartAdapterSetting(toChartKey, adapterSettingData, isLoggedIn);
  }
};
