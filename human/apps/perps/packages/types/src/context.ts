export type RouteOption = {
  href: string;
  name: string;
  scope?: string;
  target?: string;
};

export type RouterAdapter = {
  onRouteChange: (option: RouteOption) => void;
  currentPath?: string;
};
