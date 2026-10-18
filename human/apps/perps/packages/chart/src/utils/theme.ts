export const getThemeColors = () => {
  const root = document.documentElement;
  const computedStyle = getComputedStyle(root);

  const colors = {
    primary: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-primary")
    ),
    primaryLight: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-primary-light")
    ),
    secondary: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-secondary")
    ),
    success: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-success")
    ),
    warning: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-warning")
    ),
    danger: convertToRGB(computedStyle.getPropertyValue("--oui-color-danger")),
    info: convertToRGB(computedStyle.getPropertyValue("--oui-color-info")),
    loss: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-trading-loss")
    ),
    profit: convertToRGB(
      computedStyle.getPropertyValue("--oui-color-trading-profit")
    ),
  };

  return colors;
};

const convertToRGB = (color: string) => {
  return `rgb(${color.split(" ").join(",")})`;
};
