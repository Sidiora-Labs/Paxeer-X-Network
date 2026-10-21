import { defaultsDeep } from 'es-toolkit/compat';

import config from 'configs/app';

// The product design tokens, one set per appearance.
const LIGHT = {
  surfaceContainer: '#F8F9FC',
  surface: '#FFFFFF',
  surfaceContainerHigh: '#EFF2F7',
  onSurface: '#121317',
  onSurfaceVariant: '#45474D',
  iconSecondary: '#80868B',
  outline: 'rgba(33, 34, 38, 0.12)',
  dataTableBorder: '#BDC1C6',
  primary: '#121317',
  onPrimary: '#FFFFFF',
  primaryHover: '#2B2D31',
  tonal: '#E6EAF0',
  accent: '#2B4FDA',
  accentStrong: '#1E2867',
  accentSoft: '#DCF1FF',
  graphStart: 'rgba(43, 79, 218, 0.3)',
  graphStop: 'rgba(43, 79, 218, 0)',
  success: '#005143',
  successSoft: '#DAF9D4',
  destructive: '#BE1C1B',
  destructiveSoft: '#FFEEF7',
  warning: '#9E3D02',
  warningSoft: '#FCFFAD',
};

const DARK = {
  surfaceContainer: '#121317',
  surface: '#18191D',
  surfaceContainerHigh: '#212226',
  onSurface: '#F8F9FC',
  onSurfaceVariant: '#B2BBC5',
  iconSecondary: '#80868B',
  outline: 'rgba(230, 234, 240, 0.12)',
  dataTableBorder: '#45474D',
  primary: '#F8F9FC',
  onPrimary: '#121317',
  primaryHover: '#DEDFE2',
  tonal: '#2F3034',
  accent: '#9DD2FF',
  accentStrong: '#DCF1FF',
  accentSoft: '#1E2867',
  selectedOption: '#3C90FF',
  graphStart: 'rgba(157, 210, 255, 0.3)',
  graphStop: 'rgba(157, 210, 255, 0)',
  success: '#9BE69A',
  successSoft: '#005143',
  destructive: '#FFACC2',
  destructiveSoft: '#990D0E',
  warning: '#FFF350',
  warningSoft: '#9E3D02',
};

const DEFAULT_THEME_COLORS = {
  bg: {
    primary: {
      _light: { value: LIGHT.surfaceContainer },
      _dark: { value: DARK.surfaceContainer },
    },
    surface: {
      _light: { value: LIGHT.surface },
      _dark: { value: DARK.surface },
    },
    sunken: {
      _light: { value: LIGHT.surfaceContainerHigh },
      _dark: { value: DARK.surfaceContainerHigh },
    },
    overlay: {
      _light: { value: LIGHT.surface },
      _dark: { value: DARK.surfaceContainerHigh },
    },
  },
  text: {
    primary: {
      _light: { value: LIGHT.onSurface },
      _dark: { value: DARK.onSurface },
    },
    secondary: {
      _light: { value: LIGHT.onSurfaceVariant },
      _dark: { value: DARK.onSurfaceVariant },
    },
    muted: {
      _light: { value: LIGHT.onSurfaceVariant },
      _dark: { value: DARK.onSurfaceVariant },
    },
  },
  border: {
    divider: {
      _light: { value: LIGHT.outline },
      _dark: { value: DARK.outline },
    },
    strong: {
      _light: { value: LIGHT.dataTableBorder },
      _dark: { value: DARK.dataTableBorder },
    },
  },
  accent: {
    primary: {
      _light: { value: LIGHT.accent },
      _dark: { value: DARK.accent },
    },
    strong: {
      _light: { value: LIGHT.accentStrong },
      _dark: { value: DARK.accentStrong },
    },
    soft: {
      _light: { value: LIGHT.accentSoft },
      _dark: { value: DARK.accentSoft },
    },
  },
  hover: {
    _light: { value: LIGHT.accentStrong },
    _dark: { value: DARK.accentStrong },
  },
  selected: {
    control: {
      text: {
        _light: { value: LIGHT.onSurface },
        _dark: { value: DARK.onSurface },
      },
      bg: {
        _light: { value: LIGHT.tonal },
        _dark: { value: DARK.tonal },
      },
    },
    option: {
      bg: {
        _light: { value: LIGHT.accent },
        _dark: { value: DARK.selectedOption },
      },
    },
  },
  icon: {
    primary: {
      _light: { value: LIGHT.onSurfaceVariant },
      _dark: { value: DARK.onSurfaceVariant },
    },
    secondary: {
      _light: { value: LIGHT.iconSecondary },
      _dark: { value: DARK.iconSecondary },
    },
  },
  button: {
    primary: {
      _light: { value: LIGHT.primary },
      _dark: { value: DARK.primary },
      text: {
        _light: { value: LIGHT.onPrimary },
        _dark: { value: DARK.onPrimary },
      },
      hover: {
        _light: { value: LIGHT.primaryHover },
        _dark: { value: DARK.primaryHover },
      },
    },
  },
  link: {
    primary: {
      _light: { value: LIGHT.accent },
      _dark: { value: DARK.accent },
    },
  },
  graph: {
    line: {
      _light: { value: LIGHT.accent },
      _dark: { value: DARK.accent },
    },
    gradient: {
      start: {
        _light: { value: LIGHT.graphStart },
        _dark: { value: DARK.graphStart },
      },
      stop: {
        _light: { value: LIGHT.graphStop },
        _dark: { value: DARK.graphStop },
      },
    },
  },
  navigation: {
    bg: {
      selected: {
        _light: { value: LIGHT.surfaceContainerHigh },
        _dark: { value: DARK.surfaceContainerHigh },
      },
    },
    text: {
      selected: {
        _light: { value: LIGHT.onSurface },
        _dark: { value: DARK.onSurface },
      },
    },
  },
  stats: {
    bg: {
      _light: { value: LIGHT.surface },
      _dark: { value: DARK.surface },
    },
  },
  topbar: {
    bg: {
      _light: { value: LIGHT.surface },
      _dark: { value: DARK.surface },
    },
  },
  tabs: {
    text: {
      primary: {
        _light: { value: LIGHT.onSurface },
        _dark: { value: DARK.onSurface },
      },
    },
  },
  table: {
    header: {
      bg: {
        _light: { value: LIGHT.surfaceContainer },
        _dark: { value: DARK.surfaceContainerHigh },
      },
      fg: {
        _light: { value: LIGHT.onSurfaceVariant },
        _dark: { value: DARK.onSurfaceVariant },
      },
    },
  },
  feedback: {
    success: {
      fg: {
        _light: { value: LIGHT.success },
        _dark: { value: DARK.success },
      },
      bg: {
        _light: { value: LIGHT.successSoft },
        _dark: { value: DARK.successSoft },
      },
    },
    error: {
      fg: {
        _light: { value: LIGHT.destructive },
        _dark: { value: DARK.destructive },
      },
      bg: {
        _light: { value: LIGHT.destructiveSoft },
        _dark: { value: DARK.destructiveSoft },
      },
    },
    warning: {
      fg: {
        _light: { value: LIGHT.warning },
        _dark: { value: DARK.warning },
      },
      bg: {
        _light: { value: LIGHT.warningSoft },
        _dark: { value: DARK.warningSoft },
      },
    },
  },
};

const colors = {
  // BASE COLORS
  green: {
    '50': { value: '#F0FFF4' },
    '100': { value: '#C6F6D5' },
    '200': { value: '#9AE6B4' },
    '300': { value: '#68D391' },
    '400': { value: '#48BB78' },
    '500': { value: '#38A169' },
    '600': { value: '#25855A' },
    '700': { value: '#276749' },
    '800': { value: '#22543D' },
    '900': { value: '#1C4532' },
  },
  blue: {
    '50': { value: '#EBF8FF' },
    '100': { value: '#BEE3F8' },
    '200': { value: '#90CDF4' },
    '300': { value: '#63B3ED' },
    '400': { value: '#4299E1' },
    '500': { value: '#3182CE' },
    '600': { value: '#2B6CB0' },
    '700': { value: '#2C5282' },
    '800': { value: '#2A4365' },
    '900': { value: '#1A365D' },
  },
  red: {
    '50': { value: '#FFF5F5' },
    '100': { value: '#FED7D7' },
    '200': { value: '#FEB2B2' },
    '300': { value: '#FC8181' },
    '400': { value: '#F56565' },
    '500': { value: '#E53E3E' },
    '600': { value: '#C53030' },
    '700': { value: '#9B2C2C' },
    '800': { value: '#822727' },
    '900': { value: '#63171B' },
  },
  orange: {
    '50': { value: '#FFFAF0' },
    '100': { value: '#FEEBCB' },
    '200': { value: '#FBD38D' },
    '300': { value: '#F6AD55' },
    '400': { value: '#ED8936' },
    '500': { value: '#DD6B20' },
    '600': { value: '#C05621' },
    '700': { value: '#9C4221' },
    '800': { value: '#7B341E' },
    '900': { value: '#652B19' },
  },
  yellow: {
    '50': { value: '#FFFFF0' },
    '100': { value: '#FEFCBF' },
    '200': { value: '#FAF089' },
    '300': { value: '#F6E05E' },
    '400': { value: '#ECC94B' },
    '500': { value: '#D69E2E' },
    '600': { value: '#B7791F' },
    '700': { value: '#975A16' },
    '800': { value: '#744210' },
    '900': { value: '#5F370E' },
  },
  gray: {
    '50': { value: '#F8F9FC' },
    '100': { value: '#EFF2F7' },
    '200': { value: '#E1E6EC' },
    '300': { value: '#CDD4DC' },
    '400': { value: '#B2BBC5' },
    '500': { value: '#80868B' },
    '600': { value: '#45474D' },
    '700': { value: '#2F3034' },
    '800': { value: '#212226' },
    '900': { value: '#18191D' },
  },
  teal: {
    '50': { value: '#E6FFFA' },
    '100': { value: '#B2F5EA' },
    '200': { value: '#81E6D9' },
    '300': { value: '#4FD1C5' },
    '400': { value: '#38B2AC' },
    '500': { value: '#319795' },
    '600': { value: '#2C7A7B' },
    '700': { value: '#285E61' },
    '800': { value: '#234E52' },
    '900': { value: '#1D4044' },
  },
  cyan: {
    '50': { value: '#EDFDFD' },
    '100': { value: '#C4F1F9' },
    '200': { value: '#9DECF9' },
    '300': { value: '#76E4F7' },
    '400': { value: '#0BC5EA' },
    '500': { value: '#00B5D8' },
    '600': { value: '#00A3C4' },
    '700': { value: '#0987A0' },
    '800': { value: '#086F83' },
    '900': { value: '#065666' },
  },
  purple: {
    '50': { value: '#FAF5FF' },
    '100': { value: '#E9D8FD' },
    '200': { value: '#D6BCFA' },
    '300': { value: '#B794F4' },
    '400': { value: '#9F7AEA' },
    '500': { value: '#805AD5' },
    '600': { value: '#6B46C1' },
    '700': { value: '#553C9A' },
    '800': { value: '#44337A' },
    '900': { value: '#322659' },
  },
  pink: {
    '50': { value: '#FFF5F7' },
    '100': { value: '#FED7E2' },
    '200': { value: '#FBB6CE' },
    '300': { value: '#F687B3' },
    '400': { value: '#ED64A6' },
    '500': { value: '#D53F8C' },
    '600': { value: '#B83280' },
    '700': { value: '#97266D' },
    '800': { value: '#702459' },
    '900': { value: '#521B41' },
  },
  black: { value: '#121317' },
  white: { value: '#ffffff' },
  whiteAlpha: {
    '50': { value: 'RGBA(255, 255, 255, 0.04)' },
    '100': { value: 'RGBA(255, 255, 255, 0.06)' },
    '200': { value: 'RGBA(255, 255, 255, 0.08)' },
    '300': { value: 'RGBA(255, 255, 255, 0.16)' },
    '400': { value: 'RGBA(255, 255, 255, 0.24)' },
    '500': { value: 'RGBA(255, 255, 255, 0.36)' },
    '600': { value: 'RGBA(255, 255, 255, 0.48)' },
    '700': { value: 'RGBA(255, 255, 255, 0.64)' },
    '800': { value: 'RGBA(255, 255, 255, 0.80)' },
    '900': { value: 'RGBA(255, 255, 255, 0.92)' },
  },
  blackAlpha: {
    '50': { value: 'RGBA(33, 34, 38, 0.04)' },
    '100': { value: 'RGBA(33, 34, 38, 0.06)' },
    '200': { value: 'RGBA(33, 34, 38, 0.08)' },
    '300': { value: 'RGBA(33, 34, 38, 0.16)' },
    '400': { value: 'RGBA(33, 34, 38, 0.24)' },
    '500': { value: 'RGBA(33, 34, 38, 0.36)' },
    '600': { value: 'RGBA(33, 34, 38, 0.48)' },
    '700': { value: 'RGBA(33, 34, 38, 0.64)' },
    '800': { value: 'RGBA(33, 34, 38, 0.80)' },
    '900': { value: 'RGBA(33, 34, 38, 0.92)' },
  },

  datavis: {
    blue: {
      lowest: { value: '#DCF1FF' },
      low: { value: '#9DD2FF' },
      mid: { value: '#3C90FF' },
      high: { value: '#2B4FDA' },
      highest: { value: '#1E2867' },
    },
    green: {
      lowest: { value: '#DAF9D4' },
      low: { value: '#9BE69A' },
      mid: { value: '#0EBC5F' },
      high: { value: '#008052' },
      highest: { value: '#005143' },
    },
    grey: {
      lowest: { value: '#EFF2F7' },
      low: { value: '#E1E6EC' },
      mid: { value: '#B2BBC5' },
      high: { value: '#45474D' },
      highest: { value: '#212226' },
    },
    pink: {
      lowest: { value: '#FFDBF5' },
      low: { value: '#FFB5E8' },
      mid: { value: '#FF88D3' },
      high: { value: '#DE249E' },
      highest: { value: '#72004A' },
    },
    purple: {
      lowest: { value: '#E0E7FF' },
      low: { value: '#B8C0FF' },
      mid: { value: '#7372FE' },
      high: { value: '#4D34CF' },
      highest: { value: '#2E1C6D' },
    },
    red: {
      lowest: { value: '#FFEEF7' },
      low: { value: '#FFACC2' },
      mid: { value: '#FF4C45' },
      high: { value: '#BE1C1B' },
      highest: { value: '#7D0304' },
    },
    yellow: {
      lowest: { value: '#FCFFAD' },
      low: { value: '#FFF350' },
      mid: { value: '#FFCF03' },
      high: { value: '#F29900' },
      highest: { value: '#D05600' },
    },
  },

  // BRAND COLORS
  github: { value: '#171923' },
  telegram: { value: '#2775CA' },
  linkedin: { value: '#1564BA' },
  discord: { value: '#9747FF' },
  slack: { value: '#1BA27A' },
  twitter: { value: '#000000' },
  opensea: { value: '#2081E2' },
  facebook: { value: '#4460A0' },
  medium: { value: '#231F20' },
  reddit: { value: '#FF4500' },
  celo: { value: '#FCFF52' },
  clusters: { value: '#DE6061' },

  // THEME COLORS
  theme: defaultsDeep(config.UI.colorTheme.overrides, DEFAULT_THEME_COLORS),
};

export default colors;
