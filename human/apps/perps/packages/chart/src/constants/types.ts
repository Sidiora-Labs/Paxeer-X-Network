export type Margin = {
  top: number;
  right: number;
  bottom: number;
  left: number;
};

export type Size = {
  width: number;
  height: number;
};

export type LineChart = {
  /**
   * The name of the x-axis field in the data
   */
  x: string | ((d: any) => any);
  /**
   * The name of the y-axis field in the data
   */
  y: string | ((d: any) => any);
};

export type ChartProps<T> = {
  id?: string;
  margin?: Margin;
  data: T[];
  width?: number;
  height?: number;
  loading?: boolean;
} & LineChart;
