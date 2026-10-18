const BLACK_RGB = "0 0 0";
const WHITE_RGB = "255 255 255";

const parseRgbChannels = (rgb: string): [number, number, number] => {
  const channels = rgb.trim().split(/\s+/).map(Number);

  if (
    channels.length !== 3 ||
    channels.some(
      (channel) => !Number.isFinite(channel) || channel < 0 || channel > 255,
    )
  ) {
    throw new Error(
      `Expected RGB channels in "R G B" format, received "${rgb}".`,
    );
  }

  return channels as [number, number, number];
};

const toLinearChannel = (channel: number) => {
  const normalized = channel / 255;
  return normalized <= 0.04045
    ? normalized / 12.92
    : ((normalized + 0.055) / 1.055) ** 2.4;
};

const getRelativeLuminance = (rgb: string) => {
  const [red, green, blue] = parseRgbChannels(rgb).map(toLinearChannel);
  return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
};

/** Selects whichever of black or white has the stronger WCAG contrast. */
export const getContrastTextRgb = (backgroundRgb: string) => {
  const backgroundLuminance = getRelativeLuminance(backgroundRgb);
  const contrastWithBlack = (backgroundLuminance + 0.05) / 0.05;
  const contrastWithWhite = 1.05 / (backgroundLuminance + 0.05);

  return contrastWithWhite >= contrastWithBlack ? WHITE_RGB : BLACK_RGB;
};
