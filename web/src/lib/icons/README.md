# Product icons

`definitions.json` is the trusted SVG source for the Vue app, sandbox chat and
injected preview tools. `catalog.ts` exports `IconName`, `iconNames` and
`iconDefinitions`. Add a definition and matching, scoped part animation in
`src/styles/icon-motion.css` together. Do not accept SVG markup from API or user
input into this registry.

Use `Icon.vue` in Vue templates. It binds to the closest button, link, menu item,
focusable control or explicit `data-icon-motion-host` / `grasp-status-host` row.
Metadata outside a host stays static. Feedback runs once for hover or keyboard
focus and can replay after leaving. A nested control owns its own feedback.

- `busy` enables a real ongoing state. Only loading glyphs have repeating busy
  motion; `spinner` defaults to busy. Pass the business state explicitly for
  other loading indicators.
- `:interactive="false"` suppresses hover/focus feedback for stateful chevrons,
  theme controls and metadata inside a larger control.
- `motionKey` changing after mount requests one event animation. Keep the initial
  server snapshot quiet and update the key only for a new event. `check-draw`
  deliberately ignores event triggers and stays complete until hover/focus.
- `disabled` and native disabled/inert ancestors suppress decorative feedback.
  Reduced-motion preferences always show the stable glyph.

`runtime.ts` is the native DOM adapter for sandbox and injected pages. It uses
the same geometry and CSS, with delegated interaction handling. Its generated
copies are refreshed by `npm run build:live-overlay`; edit the source and run
that command instead of editing generated copies.

Provider logos and QQ retain their source geometry. Their enclosing controls
may highlight; the marks do not morph. Mascots, charts and the explicit flow
confirmation ceremony retain their own animation systems.
