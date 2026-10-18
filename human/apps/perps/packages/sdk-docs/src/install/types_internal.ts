export type McpServerConfigEntry = {
  command: string;
  args?: string[];
  env?: Record<string, string>;
  [key: string]: unknown;
};
