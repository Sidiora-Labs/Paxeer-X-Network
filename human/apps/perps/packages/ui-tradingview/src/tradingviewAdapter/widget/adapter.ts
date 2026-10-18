/*
  Tradingview Widget can be initiated with setting_adpater,
  we try to detect any setting changes and persist them into local storage
*/

const defaultSettings = {
  "chart.lastUsedTimeBasedResolution": "1",
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

const loadAdapterSettings = (adapterKey: string) => {
  const settingsString = localStorage.getItem(adapterKey);
  let settings;

  if (settingsString) {
    try {
      settings = JSON.parse(settingsString);
    } catch (err) {
      console.error(err);
    }
  }

  return settings || defaultSettings;
};

const persistAdapterSettings = (
  adapterKey: string,
  key: string,
  value: any
) => {
  const settings = loadAdapterSettings(adapterKey);
  localStorage.setItem(
    adapterKey,
    JSON.stringify({ ...settings, [key]: value })
  );
};

export const getSettingAdapter = (adapterKey: string) => {
  const settings = loadAdapterSettings(adapterKey);

  return {
    initialSettings: settings,
    setValue: (key: string, value: any) =>
      persistAdapterSettings(adapterKey, key, value),
    removeValue: () => {},
  };
};
