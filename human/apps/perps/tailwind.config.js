/** @type {import('tailwindcss').Config} */
module.exports = {
    darkMode: ["class"],
    content: [
      "./src/**/*.{js,ts,jsx,tsx,mdx}"
    ],
    theme: {
    	extend: {
    		fontFamily: {
    			sans: ['"Paxeer Sans Rounded"', '-apple-system', 'BlinkMacSystemFont', 'sans-serif'],
    			mono: ['"Paxeer Sans Rounded"', 'sans-serif']
    		},
    		borderRadius: {
    			sm: 'var(--radius-sm)',
    			md: 'var(--radius-md)',
    			lg: 'var(--radius-lg)',
    			xl: 'var(--radius-xl)',
    			'2xl': 'var(--radius-xl)',
    			pill: 'var(--radius-pill)',
    			// Design-port additions (DESIGN-AUDIT §3.5)
    			card: 'var(--radius-card)',
    			hero: 'var(--radius-hero)',
    		},
    		fontSize: {
    			// Design-port scale (DESIGN-AUDIT §3.3) — mono labels + display
    			'2xs': '9px',
    			'xs-label': '10px',
    			'sm-mono': '11px',
    			'display-xs': ['22px', { lineHeight: '1', letterSpacing: '-0.02em' }],
    			'display-sm': ['32px', { lineHeight: '1', letterSpacing: '-0.025em' }],
    			'display-md': ['42px', { lineHeight: '1', letterSpacing: '-0.03em' }],
    			'display-lg': ['56px', { lineHeight: '1', letterSpacing: '-0.035em' }],
    			'display-xl': ['92px', { lineHeight: '1', letterSpacing: '-0.04em' }],
    		},
    		letterSpacing: {
    			// Design-port (DESIGN-AUDIT §3.4) — 0.14em is the signature detail
    			label: '0.14em',
    			'label-tight': '0.04em',
    			'label-loose': '0.1em',
    			display: '-0.02em',
    			'display-tight': '-0.035em',
    		},
    		boxShadow: {
    			// Design-port (DESIGN-AUDIT §3.6)
    			'04': 'var(--shadow-04)',
    			'05': 'var(--shadow-05)',
    			'06': 'var(--shadow-06)',
    			card: 'var(--shadow-04)',
    			'brand-lift': 'var(--shadow-05)',
    			'brand-glow': 'var(--shadow-06)',
    		},
    		transitionTimingFunction: {
    			// Design-port (DESIGN-AUDIT §3.7)
    			data: 'cubic-bezier(0.4, 0, 0.2, 1)',
    			emphatic: 'cubic-bezier(0.16, 1, 0.3, 1)',
    		},
    		colors: {
    			grey: {
    				0: 'var(--grey-0)',
    				10: 'var(--grey-10)',
    				20: 'var(--grey-20)',
    				50: 'var(--grey-50)',
    				100: 'var(--grey-100)',
    				200: 'var(--grey-200)',
    				300: 'var(--grey-300)',
    				400: 'var(--grey-400)',
    				500: 'var(--grey-500)',
    				600: 'var(--grey-600)',
    				650: 'var(--grey-650)',
    				700: 'var(--grey-700)',
    				800: 'var(--grey-800)',
    				825: 'var(--grey-825)',
    				850: 'var(--grey-850)',
    				875: 'var(--grey-875)',
    				900: 'var(--grey-900)',
    				950: 'var(--grey-950)',
    				1000: 'var(--grey-1000)',
    				1100: 'var(--grey-1100)',
    				1200: 'var(--grey-1200)'
    			},
    			blue: {
    				0: 'var(--blue-0)',
    				10: 'var(--blue-10)',
    				20: 'var(--blue-20)',
    				50: 'var(--blue-50)',
    				100: 'var(--blue-100)',
    				200: 'var(--blue-200)',
    				300: 'var(--blue-300)',
    				400: 'var(--blue-400)',
    				500: 'var(--blue-500)',
    				600: 'var(--blue-600)',
    				700: 'var(--blue-700)',
    				800: 'var(--blue-800)',
    				900: 'var(--blue-900)',
    				1000: 'var(--blue-1000)',
    				1100: 'var(--blue-1100)',
    				1200: 'var(--blue-1200)'
    			},
    			red: {
    				0: 'var(--red-0)',
    				10: 'var(--red-10)',
    				20: 'var(--red-20)',
    				50: 'var(--red-50)',
    				100: 'var(--red-100)',
    				200: 'var(--red-200)',
    				300: 'var(--red-300)',
    				400: 'var(--red-400)',
    				500: 'var(--red-500)',
    				600: 'var(--red-600)',
    				700: 'var(--red-700)',
    				800: 'var(--red-800)',
    				900: 'var(--red-900)',
    				1000: 'var(--red-1000)',
    				1100: 'var(--red-1100)',
    				1200: 'var(--red-1200)'
    			},
    			green: {
    				0: 'var(--green-0)',
    				10: 'var(--green-10)',
    				20: 'var(--green-20)',
    				50: 'var(--green-50)',
    				100: 'var(--green-100)',
    				200: 'var(--green-200)',
    				300: 'var(--green-300)',
    				400: 'var(--green-400)',
    				500: 'var(--green-500)',
    				600: 'var(--green-600)',
    				700: 'var(--green-700)',
    				800: 'var(--green-800)',
    				900: 'var(--green-900)',
    				1000: 'var(--green-1000)',
    				1100: 'var(--green-1100)',
    				1200: 'var(--green-1200)'
    			},
    			yellow: {
    				0: 'var(--yellow-0)',
    				10: 'var(--yellow-10)',
    				20: 'var(--yellow-20)',
    				50: 'var(--yellow-50)',
    				100: 'var(--yellow-100)',
    				200: 'var(--yellow-200)',
    				300: 'var(--yellow-300)',
    				400: 'var(--yellow-400)',
    				500: 'var(--yellow-500)',
    				600: 'var(--yellow-600)',
    				700: 'var(--yellow-700)',
    				800: 'var(--yellow-800)',
    				900: 'var(--yellow-900)',
    				1000: 'var(--yellow-1000)',
    				1100: 'var(--yellow-1100)',
    				1200: 'var(--yellow-1200)'
    			},
    			purple: {
    				0: 'var(--purple-0)',
    				10: 'var(--purple-10)',
    				20: 'var(--purple-20)',
    				50: 'var(--purple-50)',
    				100: 'var(--purple-100)',
    				200: 'var(--purple-200)',
    				300: 'var(--purple-300)',
    				400: 'var(--purple-400)',
    				500: 'var(--purple-500)',
    				600: 'var(--purple-600)',
    				700: 'var(--purple-700)',
    				800: 'var(--purple-800)',
    				900: 'var(--purple-900)',
    				1000: 'var(--purple-1000)',
    				1100: 'var(--purple-1100)',
    				1200: 'var(--purple-1200)'
    			},
    			pink: {
    				0: 'var(--pink-0)',
    				10: 'var(--pink-10)',
    				20: 'var(--pink-20)',
    				50: 'var(--pink-50)',
    				100: 'var(--pink-100)',
    				200: 'var(--pink-200)',
    				300: 'var(--pink-300)',
    				400: 'var(--pink-400)',
    				500: 'var(--pink-500)',
    				600: 'var(--pink-600)',
    				700: 'var(--pink-700)',
    				800: 'var(--pink-800)',
    				900: 'var(--pink-900)',
    				1000: 'var(--pink-1000)',
    				1100: 'var(--pink-1100)',
    				1200: 'var(--pink-1200)'
    			},
    			orange: {
    				50: 'var(--orange-50)',
    				100: 'var(--orange-100)',
    				200: 'var(--orange-200)',
    				300: 'var(--orange-300)',
    				400: 'var(--orange-400)',
    				500: 'var(--orange-500)',
    				600: 'var(--orange-600)',
    				700: 'var(--orange-700)',
    				800: 'var(--orange-800)',
    				900: 'var(--orange-900)'
    			},
    			background: 'hsl(var(--background))',
    			foreground: 'hsl(var(--foreground))',
			long: 'var(--color-long)',
			longDark: 'var(--color-long-dark)',
        		short: 'var(--color-short)',
			shortDark: 'var(--color-short-dark)',
			warning: 'var(--color-warning)',
			longShortButton: 'var(--color-long-short-button)',
			longShortButtonHover: 'var(--color-long-short-button-hover)',
			surface: {
				0: 'hsl(var(--surface-0))',
				1: 'hsl(var(--surface-1))',
				2: 'hsl(var(--surface-2))',
				glass: 'var(--surface-glass)',
			},
    			card: {
    				DEFAULT: 'hsl(var(--card))',
    				foreground: 'hsl(var(--card-foreground))'
    			},
    			popover: {
    				DEFAULT: 'hsl(var(--popover))',
    				foreground: 'hsl(var(--popover-foreground))'
    			},
    			primary: {
    				DEFAULT: 'hsl(var(--primary))',
    				foreground: 'hsl(var(--primary-foreground))'
    			},
    			secondary: {
    				DEFAULT: 'hsl(var(--secondary))',
    				foreground: 'hsl(var(--secondary-foreground))'
    			},
    			muted: {
    				DEFAULT: 'hsl(var(--muted))',
    				foreground: 'hsl(var(--muted-foreground))'
    			},
    			accent: {
    				DEFAULT: 'hsl(var(--accent))',
    				foreground: 'hsl(var(--accent-foreground))'
    			},
    			destructive: {
    				DEFAULT: 'hsl(var(--destructive))',
    				foreground: 'hsl(var(--destructive-foreground))'
    			},
    			border: 'hsl(var(--border))',
    			input: 'hsl(var(--input))',
    			ring: 'hsl(var(--ring))',
    			chart: {
    				'1': 'hsl(var(--chart-1))',
    				'2': 'hsl(var(--chart-2))',
    				'3': 'hsl(var(--chart-3))',
    				'4': 'hsl(var(--chart-4))',
    				'5': 'hsl(var(--chart-5))'
    			},
    			sidebar: {
    				DEFAULT: 'hsl(var(--sidebar-background))',
    				foreground: 'hsl(var(--sidebar-foreground))',
    				primary: 'hsl(var(--sidebar-primary))',
    				'primary-foreground': 'hsl(var(--sidebar-primary-foreground))',
    				accent: 'hsl(var(--sidebar-accent))',
    				'accent-foreground': 'hsl(var(--sidebar-accent-foreground))',
    				border: 'hsl(var(--sidebar-border))',
    				ring: 'hsl(var(--sidebar-ring))'
    			}
    		},
    		keyframes: {
    			'slide-up-mobile': {
    				'0%': {
    					transform: 'translateY(100%)',
    					opacity: '0.3'
    				},
    				'100%': {
    					transform: 'translateY(0)',
    					opacity: '1'
    				}
    			}
    		},
    		animation: {
    			'slide-up-mobile': 'slide-up-mobile 0.4s cubic-bezier(0.16, 1, 0.3, 1)'
    		}
    	}
    },
    plugins: [
      require("tailwindcss-animate")
    ]
}