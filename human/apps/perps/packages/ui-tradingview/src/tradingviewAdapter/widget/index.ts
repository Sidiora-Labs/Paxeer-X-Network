import { withoutExchangePrefix } from "../../utils/chart.util";
import {
  ChartActionId,
  LoadingScreenOptions,
  Overrides,
  ThemeName,
  type ChartingLibraryWidgetOptions,
} from "../charting_library";
import type {Datafeed} from "../datafeed/datafeed";
import type {
  ChartMode,
  IBrokerConnectionAdapterHost,
  IBrokerTerminal,
  IBrokerWithoutRealtime,
  IChartingLibraryWidget,
  LanguageCode,
  ResolutionString,
  TradingTerminalWidgetOptions,
} from "../type";
import { ChartHack } from "./chart_hack";
import getOptions from "./option";
import {
  defaultSettings,
  getChartData,
  saveChartAdapterSetting,
  saveChartData,
} from "./persistUtils";

const debounce = (func: any, delay: any) => {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const debounced = (...args: any[]) => {
    timer && window.clearTimeout(timer);
    timer = setTimeout(() => {
      func(...args);
    }, delay);
  };
  debounced.cancel = () => {
    timer && window.clearTimeout(timer);
    timer = null;
  };
  return debounced;
};

declare let TradingView: any;

const DEFAULT_SETTINGS_KEY = "chartProp_default";
const DEBOUNCE_SAVE_TIME = 300;
type TradingViewDirection = "ltr" | "rtl";

export interface WidgetOptions {
  fullscreen?: TradingTerminalWidgetOptions["fullscreen"];
  autosize?: TradingTerminalWidgetOptions["autosize"];
  symbol: TradingTerminalWidgetOptions["symbol"];
  overrides?: Overrides;
  studiesOverrides?: Overrides;
  theme?: ThemeName;
  loadingScreen?: LoadingScreenOptions;
  toolbarBg?: string;
  interval: ResolutionString;
  locale: string;
  timezone?: string;
  container: TradingTerminalWidgetOptions["container"];
  libraryPath: TradingTerminalWidgetOptions["library_path"];
  customCssUrl: TradingTerminalWidgetOptions["custom_css_url"];
  customFontFamily: TradingTerminalWidgetOptions["custom_font_family"];
  direction?: TradingViewDirection;
  datafeed: Datafeed;
  positionControlCallback: (...args: any[]) => void;
  getBroker?(
    instance: IChartingLibraryWidget,
    host: IBrokerConnectionAdapterHost,
  ): IBrokerWithoutRealtime | IBrokerTerminal;
  /** Optional custom indicators getter; passed through to TradingView widget as custom_indicators_getter. */
  customIndicatorsGetter?: ChartingLibraryWidgetOptions["custom_indicators_getter"];
}

export interface WidgetProps {
  options: WidgetOptions;
  chartKey?: string;
  mode?: ChartMode;
  onClick?: (e: any) => void;
  /** External enabled features to merge with defaults */
  enabled_features?: string[];
  /** External disabled features to merge with defaults */
  disabled_features?: string[];
}

export class Widget {
  private _instance: IChartingLibraryWidget | null = null;

  private _onClick = null;

  private _broker: any;

  private _datafeed: Datafeed | null = null;

  private _chartKey: string = DEFAULT_SETTINGS_KEY;

  private _adapterSetting: any = defaultSettings;

  private _savedData: any = null;

  private _isLoggedIn: boolean = false;

  constructor(props: WidgetProps) {
    this._create(props);
  }

  public remove() {
    this.unsubscribeClick();
    this._datafeed?.remove();
    this._broker?.remove();
    this._instance?.remove();
    this.debounceSaveChart.cancel();
    this.debounceSaveChartAdapterSetting.cancel();
  }

  public updateOverrides(overrides: Overrides) {
    if (!this.instance) {
      return;
    }
    this.instance.applyOverrides(overrides);
  }

  public setSymbol(symbol: string, interval?: number, callback?: any) {
    try {
      this._instance?.onChartReady(() => {
        let currentInterval =
          interval ?? this._instance?.symbolInterval()?.interval;
        if (!currentInterval) {
          currentInterval = 1;
        }
        this._instance?.setSymbol(symbol, currentInterval as any, callback);
      });
    } catch (e) {
      console.log("set symbol error", e);
    }
  }

  setResolution(resolution: string) {
    if (this._instance) {
      this._instance
        .activeChart()
        .setResolution(resolution as ResolutionString);
    }
  }

  public executeActionById(actionId: ChartActionId) {
    try {
      this._instance?.onChartReady(() => {
        // this._instance?.activeChart().showPropertiesDialog(this._instance.activeChart().getAllShapes()[0].id);

        this._instance?.activeChart().executeActionById(actionId);
      });
    } catch (e) {
      console.log("executeActionId error", e);
    }
  }
  public changeLineType(lineType: any) {
    try {
      this._instance?.onChartReady(() => {
        // this._instance?.activeChart().showPropertiesDialog(this._instance.activeChart().getAllShapes()[0].id);

        this._instance?.activeChart().setChartType(lineType);
      });
    } catch (e) {
      console.log("executeActionId error", e);
    }
  }

  public subscribeClick(onClick: any) {
    this._onClick = onClick;
    this._instance?.onChartReady(() => {
      (this._instance as any)?._iFrame.contentDocument?.addEventListener(
        "click",
        this._onClick,
      );
    });
  }

  public unsubscribeClick() {
    (this._instance as any)?._iFrame.contentDocument?.removeEventListener(
      "click",
      this._onClick,
    );
  }

  get instance() {
    return this._instance;
  }

  private chartHack() {
    this._instance?.onChartReady(() => {
      const iframeDocument = (this._instance as any)._iFrame.contentWindow
        .document;
      new ChartHack({ iframeDocument }).defaultHack();
    });
  }

  private debounceSaveChart = debounce(() => {
    try {
      this._instance?.save((chartProps) => {
        if (!Object.is(this._savedData, chartProps)) {
          this._savedData = chartProps;
          saveChartData(
            this._chartKey,
            JSON.stringify(chartProps),
            this._isLoggedIn,
          );
        }
      });
    } catch (e) {
      console.log("e", e);
    }
  }, DEBOUNCE_SAVE_TIME * 2);

  private debounceSaveChartAdapterSetting = debounce(() => {
    saveChartAdapterSetting(
      this._chartKey,
      JSON.stringify(this._adapterSetting),
      this._isLoggedIn,
    );
  }, DEBOUNCE_SAVE_TIME);

  private subscribeAutoSave() {
    this._instance?.onChartReady(() => {
      this._instance?.subscribe("onAutoSaveNeeded", () => {
        this.debounceSaveChart();
      });

      this._instance
        ?.activeChart()
        .onVisibleRangeChanged()
        .subscribe(null, () => {
          // onAutoSaveNeeded won't be triggered in the new version won't be trigger while time scale changes
          this.debounceSaveChart();
        });
    });
  }

  private async _create({
    options,
    chartKey,
    mode,
    onClick,
    enabled_features,
    disabled_features,
  }: WidgetProps) {
    const getBroker = options.getBroker;

    const widgetOptions: TradingTerminalWidgetOptions & {
      direction?: TradingViewDirection;
    } = {
      fullscreen: options.fullscreen ?? true,
      autosize: options.autosize ?? false,
      timezone: options.timezone as TradingTerminalWidgetOptions["timezone"],
      symbol: options.symbol,
      library_path: options.libraryPath,
      interval: options.interval ?? "1",
      custom_css_url: options.customCssUrl,
      custom_font_family: options.customFontFamily,
      datafeed: options.datafeed as unknown as ChartingLibraryWidgetOptions["datafeed"],
      studies_overrides: options.studiesOverrides,
      locale: options.locale as LanguageCode,
      direction: options.direction,
      theme: options.theme,
      loading_screen: options.loadingScreen,
      toolbar_bg: options.toolbarBg,
      overrides: options.overrides,
      container: options.container,
      favorites: {
        intervals: [
          "1",
          "3",
          "5",
          "15",
          "30",
          "60",
          "240",
          "1d",
          "1W",
          "1M",
        ] as ResolutionString[],
        chartTypes: ["Area", "Line"],
      },
      broker_factory: getBroker
        ? (host) => {
            if (this._broker) {
              this._broker.remove();
            }
            this._broker = getBroker(
              this._instance as IChartingLibraryWidget,
              host,
            );
            return this._broker;
          }
        : undefined,
      custom_indicators_getter: options.customIndicatorsGetter,
    };

    this._datafeed = options.datafeed;

    if (chartKey) {
      this._chartKey = chartKey;
    }

    // Get data from remote first. fallback to localstorage if no data on the server yet
    const { savedData, adapterSetting } = await getChartData(
      this._chartKey,
      this._isLoggedIn,
    );

    // @ts-ignore
    this._adapterSetting = adapterSetting;
    this._savedData = savedData;
    // Pass external enabled_features and disabled_features to getOptions for merging.
    // Explicitly pass custom_indicators_getter so it is never dropped when merging options.
    const mergedOptions = getOptions(
      widgetOptions,
      mode,
      enabled_features,
      disabled_features,
    );

    this._instance = new TradingView.widget({
      ...mergedOptions,
      ...(options.customIndicatorsGetter != null && {
        custom_indicators_getter: options.customIndicatorsGetter,
      }),
      interval:
        adapterSetting["chart.lastUsedTimeBasedResolution"] ??
        widgetOptions.interval,
      saved_data: savedData,
      settings_adapter: {
        initialSettings: adapterSetting,
        setValue: (key: string, value: any) => {
          this._adapterSetting = { ...this._adapterSetting, [key]: value };
          this.debounceSaveChartAdapterSetting();
        },
        removeValue: () => {},
      },
    });

    this._instance!.onChartReady(() => {
      // console.log('-- options symbol', options.symbol, this._instance?.activeChart().symbol());
      if (
        options.symbol &&
        this._instance?.activeChart().symbol() !==
          withoutExchangePrefix(options!.symbol as string)
      ) {
        this.setSymbol(options.symbol as string);
      }
    });
    this.subscribeAutoSave();
    this.subscribeClick(onClick);
    this.chartHack();
  }
}
