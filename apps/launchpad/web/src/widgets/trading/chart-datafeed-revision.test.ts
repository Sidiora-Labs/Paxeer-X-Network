import { describe, it, expect, vi } from 'vitest';
import { createReplayUdfDatafeed, CHART_RESOLUTIONS, resolutionToSeconds, resolutionToWsTimeframe, exactChartRatio } from './udf-datafeed';
describe('strict shared resolution aliases', () => {
 it('supports exactly nine aliases with matching REST windows and WS series', () => {
  expect(Object.keys(CHART_RESOLUTIONS)).toHaveLength(9);
  for (const [alias, expected] of Object.entries(CHART_RESOLUTIONS)) {
   expect(resolutionToSeconds(alias)).toBe(expected.seconds);
   expect(resolutionToWsTimeframe(alias)).toBe(expected.timeframe);
  }
 });
 it('rejects unknown aliases before subscribing or fetching', () => {
  const feed=createReplayUdfDatafeed('0x'+'a'.repeat(40));const error=vi.fn();
  for (const alias of ['1M','99','1d','1Dgarbage','','toString']) {
   expect(()=>resolutionToSeconds(alias)).toThrow('Unsupported');
   expect(()=>resolutionToWsTimeframe(alias)).toThrow('Unsupported');
   expect(()=>feed.subscribeBars({},alias,()=>{},'invalid')).toThrow('Unsupported');
   feed.getBars({},alias,{from:0,to:1},()=>{},error);
  }
  expect(error).toHaveBeenCalledTimes(6);expect(feed.resourceCounts()).toEqual({ subscriptions: 0, requests: 0, sockets: 0, timers: 0 });feed.dispose();
 });
});

describe('accessible exact canonical price and volume representation', () => {
 it('retains every raw decimal digit and represents recurring prices exactly', () => {
  expect(exactChartRatio('123456789012345678901234567891', '1000000')).toBe('123456789012345678901234.567891');
  expect(exactChartRatio('1', '8')).toBe('0.125');
  expect(exactChartRatio('1', '3')).toBe('1/3');
  expect(exactChartRatio('0', '1000000')).toBe('0');
  expect(() => exactChartRatio('1', '0')).toThrow();
  expect(() => exactChartRatio('1e6', '1')).toThrow();
 });
});
