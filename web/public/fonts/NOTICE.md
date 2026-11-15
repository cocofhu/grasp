# Self-hosted font subsets

WOFF2 Latin subsets under this directory are derived from:

| Family | Upstream | License |
|--------|----------|---------|
| Inter | https://github.com/rsms/inter | SIL Open Font License 1.1 |
| Fredoka | https://github.com/hafontia/Fredoka-One (via `@fontsource/fredoka` 5.3.0, Copyright 2016 The Fredoka Project Authors) | SIL Open Font License 1.1 |
| JetBrains Mono | https://github.com/JetBrains/JetBrainsMono | SIL Open Font License 1.1 |

Full OFL text: see `OFL.txt` in this folder.

Subsets were produced for Grasp brand/login critical rendering (preload + `font-display: swap`).
Fredoka 600 is used only for the Grasp wordmark (`.brand-logo__name`).
Do not redistribute these binaries without retaining this notice and the OFL.
