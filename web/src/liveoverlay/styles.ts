/**
 * Shadow-DOM styles for the Live overlay (dark default, `.light` variant).
 * Colours follow the Grasp theme tokens (web/src/styles/global.css) so the
 * overlay reads as part of the preview toolbar, not as the host app.
 */
export const OVERLAY_CSS =
  ':host{all:initial}' +
  '*{box-sizing:border-box}' +
  '.root{--bg:#141417;--bg2:#1c1c20;--line:#26262b;--line2:#36363e;--txt:#ededf0;--txt2:#a1a1aa;--txt3:#6e6e78;' +
  '--acc:#7b61ff;--acc-hover:#a78bfa;--acc-txt:#b4a5ff;--acc-dim:rgba(123,97,255,.16);--acc-ring:rgba(123,97,255,.28);--err:#fca5a5;' +
  '--shadow:0 12px 32px rgba(0,0,0,.45);' +
  'font:13px/1.45 ui-sans-serif,system-ui,-apple-system,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;color:var(--txt)}' +
  '.root.light{--bg:#fff;--bg2:#f4f4f6;--line:#e5e5e8;--line2:#d4d4d8;--txt:#18181b;--txt2:#52525b;--txt3:#a1a1aa;' +
  '--acc:#7b61ff;--acc-hover:#5424d8;--acc-txt:#5b3ff0;--acc-dim:#eef0ff;--acc-ring:rgba(123,97,255,.22);--err:#b91c1c;' +
  '--shadow:0 12px 32px rgba(16,24,40,.12),0 2px 6px rgba(16,24,40,.06)}' +
  'button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;border-radius:8px;padding:6px 10px;transition:background-color .12s,border-color .12s,color .12s}' +
  'button:hover{background:var(--bg2)}' +
  'button:focus-visible,input:focus-visible,textarea:focus-visible{outline:2px solid var(--acc);outline-offset:1px}' +
  'button:disabled{opacity:.45;cursor:not-allowed}' +
  // Sits just above preview-pick's bar (right:16px; bottom:16px).
  // Every overlay layer stays below the chat drawer (preview-pick .drawer is 2147483647)
  // so a later-mounted host cannot cover the chat.
  '.dock{position:fixed;right:16px;bottom:64px;z-index:2147483646;display:flex;flex-direction:column;align-items:flex-end;gap:6px;max-width:calc(100vw - 32px)}' +
  '.sep{width:1px;height:18px;background:var(--line2);margin:0 3px;flex:none}' +
  '.steer{display:flex;align-items:center;gap:2px;padding:4px 4px 4px 10px;border-radius:10px;background:var(--bg);border:1px solid var(--line);' +
  'box-shadow:var(--shadow);min-width:0;max-width:100%}' +
  '.steer input{background:transparent;border:0;color:inherit;font:inherit;width:260px;min-width:60px;padding:6px 0;outline:none}' +
  '.steer input::placeholder,.panel textarea::placeholder{color:var(--txt3)}' +
  '.pickbar{display:flex;flex-direction:column;gap:6px;padding:6px;border-radius:12px;background:var(--bg);border:1px solid var(--line);' +
  'box-shadow:var(--shadow);width:360px;max-width:100%}' +
  '.pickbar .row{flex-wrap:nowrap}' +
  '.pickbar .pickhint{flex:1;min-width:0;line-height:1.3}' +
  '.pickbar .steer{box-shadow:none;background:var(--bg2);border-color:transparent}' +
  '.pickbar .steer input{width:auto;flex:1}' +
  '.seg{display:inline-flex;flex:none;gap:2px;padding:2px;border-radius:8px;background:var(--bg2);border:1px solid var(--line)}' +
  '.seg button{padding:3px 10px;border-radius:6px;font-size:12px;color:var(--txt2);white-space:nowrap}' +
  '.seg button:hover{background:transparent;color:var(--txt)}' +
  '.seg button[aria-pressed="true"]{background:var(--bg);color:var(--acc-txt);font-weight:600;box-shadow:0 1px 2px rgba(0,0,0,.18),0 0 0 1px var(--acc-ring)}' +
  '.field{display:flex;align-items:center;gap:10px}' +
  '.field>.label{width:40px;flex:none;font-size:12px}' +
  '.marks{display:flex;flex-direction:column;gap:6px}' +
  '.disclosure{align-self:flex-start;padding:2px 4px;margin-left:-4px;font-size:12px;color:var(--txt2)}' +
  '.disclosure:hover{background:transparent;color:var(--txt)}' +
  '.link{padding:2px 4px;font-size:12px;color:var(--txt2)}' +
  '.foot{justify-content:space-between;padding-top:8px;margin-top:2px;border-top:1px solid var(--line)}' +
  '.foot>button:not(.go){color:var(--txt2)}' +
  '.hint{background:var(--bg);border:1px solid var(--line);border-radius:8px;padding:6px 10px;font-size:12px;max-width:360px;box-shadow:var(--shadow)}' +
  '.hint button{padding:2px 6px;color:var(--acc-txt)}' +
  '.hint button:hover{background:var(--acc-dim)}' +
  '.panel{position:fixed;z-index:2147483646;width:340px;max-width:calc(100vw - 32px);background:var(--bg);border:1px solid var(--line);border-radius:12px;' +
  'box-shadow:var(--shadow);padding:12px;display:flex;flex-direction:column;gap:10px}' +
  '.panel.choose{width:auto;min-width:220px}' +
  '.chips{display:flex;flex-wrap:wrap;gap:6px}' +
  '.chip{background:transparent;border:1px solid var(--line2);color:var(--txt2);padding:3px 10px;border-radius:999px;font-size:12px}' +
  '.chip:hover{background:transparent;border-color:var(--acc);color:var(--txt)}' +
  '.chip[aria-pressed="true"]{background:var(--acc-dim);border-color:var(--acc);color:var(--acc-txt);font-weight:600}' +
  '.panel textarea{width:100%;min-height:52px;resize:vertical;background:var(--bg2);border:1px solid var(--line);border-radius:8px;color:inherit;font:inherit;padding:7px 10px;outline:none}' +
  '.panel textarea:focus{border-color:var(--acc);box-shadow:0 0 0 3px var(--acc-ring)}' +
  '.panel textarea:focus-visible{outline:none}' +
  '.row{display:flex;align-items:center;gap:6px;flex-wrap:wrap}' +
  '.label{font-size:11px;color:var(--txt2)}' +
  '.target{font-size:12px;color:var(--txt3);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}' +
  '.go{background:var(--acc);color:#fff;font-weight:600;padding:6px 16px;margin-left:auto}' +
  '.go:hover{background:var(--acc-hover);color:#fff}' +
  '.go:focus,.go:focus-visible{color:#fff}' +
  '.light .go:hover{background:var(--acc-hover);color:#fff}' +
  '.chip.go,.chip.go:hover,.chip.go:focus,.chip.go:focus-visible{background:var(--acc);color:#fff}' +
  '.chip.go:hover,.chip.go:focus:hover,.chip.go:focus-visible:hover{background:var(--acc-hover);color:#fff}' +
  '.light .chip.go,.light .chip.go:hover,.light .chip.go:focus,.light .chip.go:focus-visible{color:#fff}' +
  '.light .chip.go:hover,.light .chip.go:focus:hover,.light .chip.go:focus-visible:hover{background:var(--acc-hover);color:#fff}' +
  '.sw{position:fixed;z-index:2147483645;display:flex;align-items:center;gap:2px;padding:4px;border-radius:10px;background:var(--bg);' +
  'border:1px solid var(--line);box-shadow:var(--shadow);white-space:nowrap}' +
  '.sw .count{font-variant-numeric:tabular-nums;padding:0 6px;font-weight:600}' +
  '.sw .lab{color:var(--txt2);font-size:12px;padding-right:4px;max-width:120px;overflow:hidden;text-overflow:ellipsis}' +
  '.sw .accept{background:var(--acc);color:#fff;font-weight:600}' +
  '.sw .accept:hover{background:var(--acc-hover)}' +
  '.sw button[aria-pressed="true"]{background:var(--acc-dim);color:var(--acc-txt);font-weight:600}' +
  '.sw .state{padding:0 8px;color:var(--acc-txt)}' +
  '.sw .err{padding:0 8px;color:var(--err);max-width:260px;overflow:hidden;text-overflow:ellipsis}' +
  '.params{position:fixed;z-index:2147483645;display:flex;flex-wrap:wrap;gap:8px;align-items:center;padding:6px 8px;border-radius:10px;' +
  'background:var(--bg);border:1px solid var(--line);box-shadow:var(--shadow);font-size:12px;max-width:min(560px,calc(100vw - 32px))}' +
  '.params input[type=range]{width:110px;accent-color:var(--acc)}' +
  '.frame{position:fixed;z-index:2147483644;pointer-events:none;border:2px solid var(--acc);border-radius:6px;box-shadow:0 0 0 4px var(--acc-ring)}' +
  '.shimmer{position:fixed;z-index:2147483644;pointer-events:none;border-radius:6px;overflow:hidden;' +
  'background:linear-gradient(100deg,rgba(123,97,255,.04) 20%,rgba(123,97,255,.22) 50%,rgba(123,97,255,.04) 80%);background-size:200% 100%;' +
  'animation:shine 1.2s linear infinite;outline:2px dashed rgba(123,97,255,.75);outline-offset:2px}' +
  '@keyframes shine{from{background-position:200% 0}to{background-position:-200% 0}}' +
  '@media (prefers-reduced-motion:reduce){.shimmer{animation:none}button{transition:none}}' +
  '.cframe{position:fixed;z-index:2147483644;pointer-events:none;border:1px solid var(--line2);border-radius:6px}' +
  '.cframe.sel{border:2px solid var(--acc);box-shadow:0 0 0 3px var(--acc-ring)}' +
  '.tag{position:fixed;z-index:2147483645;padding:2px 8px;border-radius:999px;font-size:11px;font-weight:600;line-height:16px;' +
  'background:var(--bg);border:1px solid var(--line2);color:var(--txt2);opacity:.85;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}' +
  '.tag:hover{background:var(--bg);color:var(--txt);opacity:1}' +
  '.tag.sel,.tag.sel:hover{background:var(--acc);border-color:var(--acc);color:#fff;opacity:1}' +
  '[hidden]{display:none!important}'

/** Page-level styles: hidden variants and the pick hover outline. Compare leaves the page layout alone. */
export const PAGE_CSS =
  '[data-grasp-live]>[data-grasp-variant][hidden]{display:none!important}' +
  '[data-grasp-live-hover]{outline:2px solid #7b61ff!important;outline-offset:2px!important;cursor:crosshair!important}' +
  'html[data-grasp-live-picking],html[data-grasp-live-picking] *{cursor:crosshair!important}'
