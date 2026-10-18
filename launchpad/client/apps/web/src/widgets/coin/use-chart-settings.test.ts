// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  CHART_DEFAULT_HEIGHT, CHART_INTERVALS, CHART_MAX_HEIGHT, CHART_MIN_HEIGHT, CHART_SETTINGS_KEY,
  chartSettings, clampChartHeight, defaultChartSettings, isChartInterval, parseChartSettings, resetChartSettingsCache, serializeChartSettings, updateChartSettings,
} from './use-chart-settings';

beforeEach(() => { window.localStorage.clear(); resetChartSettingsCache(); });
afterEach(() => { window.localStorage.clear(); resetChartSettingsCache(); });

describe('chart interval vocabulary', () => {
  it('exposes 1m 5m 1h 4h D inline and 15m 1W behind the caret, all valid UDF resolutions', () => {
    expect(CHART_INTERVALS.filter(option => option.primary).map(option => option.label)).toEqual(['1m', '5m', '1h', '4h', 'D']);
    expect(CHART_INTERVALS.filter(option => !option.primary).map(option => option.label)).toEqual(['15m', '1W']);
    for (const option of CHART_INTERVALS) expect(isChartInterval(option.value)).toBe(true);
    expect(isChartInterval('30')).toBe(false);
    expect(isChartInterval(5)).toBe(false);
  });
});

describe('chart height clamp', () => {
  it('keeps heights inside the published bounds and rejects garbage', () => {
    expect(clampChartHeight(CHART_MIN_HEIGHT - 100)).toBe(CHART_MIN_HEIGHT);
    expect(clampChartHeight(CHART_MAX_HEIGHT + 100)).toBe(CHART_MAX_HEIGHT);
    expect(clampChartHeight(412.6)).toBe(413);
    expect(clampChartHeight('500')).toBe(500);
    expect(clampChartHeight(Number.NaN)).toBe(CHART_DEFAULT_HEIGHT);
    expect(clampChartHeight(undefined)).toBe(CHART_DEFAULT_HEIGHT);
    expect(clampChartHeight('')).toBe(CHART_DEFAULT_HEIGHT);
  });
});

describe('chart settings codec', () => {
  it('round-trips through serialize and parse', () => {
    const settings = { height: 540, interval: '240' as const, mode: 'mcap' as const, markers: false };
    expect(parseChartSettings(serializeChartSettings(settings))).toEqual(settings);
  });
  it('falls back field by field on corrupt input', () => {
    expect(parseChartSettings(null)).toEqual(defaultChartSettings);
    expect(parseChartSettings('not json')).toEqual(defaultChartSettings);
    expect(parseChartSettings('[1,2]')).toEqual(defaultChartSettings);
    expect(parseChartSettings(JSON.stringify({ height: 99999, interval: '30', mode: 'usd', markers: 'yes' }))).toEqual({ ...defaultChartSettings, height: CHART_MAX_HEIGHT });
    expect(parseChartSettings(JSON.stringify({ interval: '1W' }))).toEqual({ ...defaultChartSettings, interval: '1W' });
  });
});

describe('chart settings store', () => {
  it('reads defaults when nothing is stored and persists updates to localStorage', () => {
    expect(chartSettings()).toEqual(defaultChartSettings);
    const next = updateChartSettings({ height: 600, interval: '60' });
    expect(next).toEqual({ ...defaultChartSettings, height: 600, interval: '60' });
    expect(parseChartSettings(window.localStorage.getItem(CHART_SETTINGS_KEY))).toEqual(next);
    resetChartSettingsCache();
    expect(chartSettings()).toEqual(next);
  });
  it('clamps heights written through the store', () => {
    expect(updateChartSettings({ height: 10 }).height).toBe(CHART_MIN_HEIGHT);
    expect(updateChartSettings({ height: 5000 }).height).toBe(CHART_MAX_HEIGHT);
  });
  it('ignores a stored value another tab corrupted', () => {
    window.localStorage.setItem(CHART_SETTINGS_KEY, '{"height":"tall","interval":null');
    expect(chartSettings()).toEqual(defaultChartSettings);
  });
});
