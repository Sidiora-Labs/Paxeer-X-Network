import React from 'react';

import type { ColorThemeId } from 'types/settings';

import * as cookies from 'lib/cookies';
import { COLOR_THEMES, getDefaultColorTheme, getThemeHexWithOverrides } from 'lib/settings/colorTheme';
import type { ColorMode } from 'toolkit/chakra/color-mode';
import { useColorMode } from 'toolkit/chakra/color-mode';
import { IconButton } from 'toolkit/chakra/icon-button';
import { Tooltip } from 'toolkit/chakra/tooltip';
import IconSvg from 'ui/shared/IconSvg';

// The color theme lives in two places at once: the color mode next-themes keeps and the page
// background hex the settings menu writes onto the document, so the toggle has to move both.
const applyColorTheme = (themeId: ColorThemeId) => {
  const theme = COLOR_THEMES.find((item) => item.id === themeId);
  const hex = getThemeHexWithOverrides(themeId);

  if (!theme || !hex) {
    return;
  }

  const varName = theme.colorMode === 'light' ? '--chakra-colors-white' : '--chakra-colors-black';
  const varNameBg = theme.colorMode === 'light' ? '--chakra-colors-theme-bg-primary-_light' : '--chakra-colors-theme-bg-primary-_dark';
  window.document.documentElement.style.setProperty(varName, hex);
  window.document.documentElement.style.setProperty(varNameBg, hex);

  cookies.set(cookies.NAMES.COLOR_MODE, theme.colorMode);
  cookies.set(cookies.NAMES.COLOR_THEME, themeId);
  window.localStorage.setItem(cookies.NAMES.COLOR_MODE, theme.colorMode);
};

const ColorModeToggle = () => {
  const { colorMode, setColorMode } = useColorMode();

  const nextColorMode: ColorMode = colorMode === 'dark' ? 'light' : 'dark';
  const label = `Switch to ${ nextColorMode } theme`;

  const handleClick = React.useCallback(() => {
    const nextThemeId = getDefaultColorTheme(nextColorMode);

    setColorMode(nextColorMode);
    applyColorTheme(nextThemeId);
  }, [ nextColorMode, setColorMode ]);

  return (
    <Tooltip content={ label } disableOnMobile>
      <IconButton
        variant="link"
        size="2xs"
        borderRadius="sm"
        aria-label={ label }
        onClick={ handleClick }
      >
        <IconSvg name={ colorMode === 'dark' ? 'sun' : 'moon' }/>
      </IconButton>
    </Tooltip>
  );
};

export default React.memo(ColorModeToggle);
