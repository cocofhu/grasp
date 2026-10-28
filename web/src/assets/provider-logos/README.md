# Provider logo sources

These SVGs are bundled locally so the credentials UI does not make runtime
requests to an image host.

- `claude.svg`, `codebuddy.svg`, `cursor.svg`, `trae.svg`, `opencode.svg`,
  `github.svg`, `gitlab.svg`, `deepseek.svg`, `openrouter.svg`, `google.svg`
  (slug `googlegemini`), `moonshot.svg` (slug `moonshotai`), and
  `bytedance.svg` come from Simple Icons v16. Their source metadata and license are maintained in the
  [Simple Icons repository](https://github.com/simple-icons/simple-icons),
  and the pinned assets were downloaded from the corresponding
  `cdn.jsdelivr.net/npm/simple-icons@v16/icons/<slug>.svg` URLs.
- `openai.svg` comes from the [OpenAI 2025 logo package](https://cdn.openai.com/brand/OpenAI-Logos-2025.zip).
  `codex.svg` uses the same official OpenAI blossom because OpenAI does not
  publish a separate Codex mark in that package or in the official Codex CLI
  repository.
- `xai.svg` is the xAI mark published from the brand source at
  [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:XAI-Logo.svg),
  whose file identifies x.ai as the source.
- `kimi.svg` and `kimi-dark.svg` are the unmodified K-only marks from the
  [official Kimi brand guide](https://moonshotai.github.io/Branding-Guide/),
  at `scenarios/04-k-only/k-only-light.svg` and `k-only-dark.svg`. Use the
  matching light/dark asset so the signature blue remains unchanged.
- The following marks come from [Lobe Icons](https://github.com/lobehub/lobe-icons)
  at commit `7d7d0379f18b114a4009dc5cb85a7c375284f31b`. The SVGs are unmodified;
  their MIT notice is included in [LICENSE.lobe-icons](LICENSE.lobe-icons).

  | Local asset | Upstream asset |
  | --- | --- |
  | `qwen.svg` | [qwen-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/qwen-color.svg) |
  | `doubao.svg` | [doubao-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/doubao-color.svg) |
  | `siliconflow.svg` | [siliconcloud-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/siliconcloud-color.svg) |
  | `minimax.svg` | [minimax-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/minimax-color.svg) |
  | `zhipu.svg` | [zhipu-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/zhipu-color.svg) |
  | `zai.svg` | [zai.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/zai.svg) |
  | `gemini.svg` | [gemini-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/gemini-color.svg) |
  | `hunyuan.svg` | [hunyuan-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/hunyuan-color.svg) |
  | `qianfan.svg` | [baiducloud-color.svg](https://github.com/lobehub/lobe-icons/blob/7d7d0379f18b114a4009dc5cb85a7c375284f31b/packages/static-svg/icons/baiducloud-color.svg) |

  SiliconFlow uses its SiliconCloud service mark. Baidu Qianfan uses the
  Baidu AI Cloud provider mark. Color fills and gradients are retained in
  both themes. Marks use the surrounding themed surface, or stay transparent
  in compact model pickers; only monochrome marks are inverted.

The marks remain trademarks of their respective owners. They are shown only
to identify credential providers and model brands.
