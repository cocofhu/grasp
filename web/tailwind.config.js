/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts,js}', './e2e/**/*.{html,ts,vue,js}'],
  theme: {
    borderRadius: {
      none: '0px',
      // Role tokens: control 8 / card 12 / shell 16 / full capsule
      sm: '8px',
      DEFAULT: '8px',
      md: '8px',
      lg: '12px',
      xl: '16px',
      '2xl': '16px',
      '3xl': '16px',
      full: '9999px',
    },
    extend: {
      screens: {
        md: '768px',
      },
      colors: {
        base: 'rgb(var(--c-base) / <alpha-value>)',
        surface: 'rgb(var(--c-surface) / <alpha-value>)',
        elevated: 'rgb(var(--c-elevated) / <alpha-value>)',
        overlay: 'rgb(var(--c-overlay) / <alpha-value>)',
        line: 'rgb(var(--c-line) / <alpha-value>)',
        'line-strong': 'rgb(var(--c-line-strong) / <alpha-value>)',
        txt: 'rgb(var(--c-txt) / <alpha-value>)',
        txt2: 'rgb(var(--c-txt2) / <alpha-value>)',
        txt3: 'rgb(var(--c-txt3) / <alpha-value>)',
        accent: '#7B61FF',
        'accent-2': 'rgb(var(--c-accent-2) / <alpha-value>)',
        'accent-hover': 'rgb(var(--c-accent-hover) / <alpha-value>)',
        'accent-dim': 'rgb(var(--c-accent-dim) / <alpha-value>)',
        ok: 'rgb(var(--c-ok) / <alpha-value>)',
        warn: 'rgb(var(--c-warn) / <alpha-value>)',
        err: 'rgb(var(--c-err) / <alpha-value>)',
        info: 'rgb(var(--c-info) / <alpha-value>)',
        // node type accents
        'n-input': '#94A3B8',
        'n-llm': '#A78BFA',
        'n-agent': '#60A5FA',
        'n-clarify': '#22D3EE',
        'n-gate': '#FBBF24',
        'n-ci': '#2DD4BF',
        'n-branch': '#E879F9',
        'n-review': '#34D399',
        'n-artifact': '#F59E0B',
      },
      fontFamily: {
        sans: ['Inter', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      boxShadow: {
        card: '0 1px 0 0 rgba(255,255,255,0.02) inset, 0 8px 24px -12px rgba(0,0,0,0.6)',
        drawer: '-16px 0 48px -24px rgba(0,0,0,0.8)',
        glow: '0 0 0 1px rgba(123,97,255,0.5), 0 0 24px -4px rgba(123,97,255,0.4)',
      },
      keyframes: {
        dash: { to: { 'stroke-dashoffset': '-16' } },
        pulseglow: {
          '0%,100%': { opacity: '0.6' },
          '50%': { opacity: '1' },
        },
      },
      animation: {
        dash: 'dash 0.6s linear infinite',
        pulseglow: 'pulseglow 1.6s ease-in-out infinite',
      },
    },
  },
  plugins: [],
}
