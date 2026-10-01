/** Shadow-DOM styles for the Live overlay (dark default, `.light` variant). */
export const OVERLAY_CSS =
  ':host{all:initial}' +
  '*{box-sizing:border-box}' +
  '.root{font:13px/1.4 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb}' +
  'button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;border-radius:8px;padding:6px 10px}' +
  'button:hover{background:rgba(255,255,255,.08)}' +
  'button:focus-visible,input:focus-visible,textarea:focus-visible{outline:2px solid #f59e0b;outline-offset:1px}' +
  'button:disabled{opacity:.45;cursor:not-allowed}' +
  // Sits just above preview-pick's bar (right:16px; bottom:16px).
  '.dock{position:fixed;right:16px;bottom:64px;z-index:2147483646;display:flex;flex-direction:column;align-items:flex-end;gap:6px;max-width:calc(100vw - 32px)}' +
  '.sep{width:1px;height:20px;background:#3f3f46;margin:0 2px;flex:none}' +
  '.steer{display:flex;align-items:center;gap:2px;padding:4px 4px 4px 10px;border-radius:10px;background:#18181b;border:1px solid #3f3f46;' +
  'box-shadow:0 10px 30px rgba(0,0,0,.35);min-width:0;max-width:100%}' +
  '.steer input{background:transparent;border:0;color:inherit;font:inherit;width:260px;min-width:60px;padding:6px 0;outline:none}' +
  '.pickbar{display:flex;flex-direction:column;gap:6px;padding:6px;border-radius:12px;background:#18181b;border:1px solid #3f3f46;' +
  'box-shadow:0 10px 30px rgba(0,0,0,.35);width:360px;max-width:100%}' +
  '.pickbar .row{flex-wrap:nowrap}' +
  '.pickbar .pickhint{flex:1;min-width:0;line-height:1.3}' +
  '.pickbar .steer{box-shadow:none;background:#27272a;border-color:transparent}' +
  '.pickbar .steer input{width:auto;flex:1}' +
  '.seg{display:inline-flex;flex:none;gap:2px;padding:2px;border-radius:8px;background:#27272a}' +
  '.seg button{padding:3px 10px;border-radius:6px;font-size:12px;white-space:nowrap}' +
  '.seg button[aria-pressed="true"]{background:#f59e0b;color:#18181b;font-weight:600}' +
  '.field{display:flex;align-items:center;gap:8px}' +
  '.field>.label{width:64px;flex:none}' +
  '.marks{display:flex;flex-direction:column;gap:6px}' +
  '.disclosure{align-self:flex-start;padding:2px 4px;font-size:12px;color:#a1a1aa}' +
  '.link{padding:2px 4px;font-size:12px;color:#a1a1aa}' +
  '.foot{justify-content:space-between}' +
  '.hint{background:#27272a;border:1px solid #3f3f46;border-radius:8px;padding:6px 10px;font-size:12px;max-width:360px}' +
  '.hint button{padding:2px 6px;text-decoration:underline}' +
  '.panel{position:fixed;z-index:2147483646;width:320px;max-width:calc(100vw - 32px);background:#18181b;border:1px solid #3f3f46;border-radius:12px;' +
  'box-shadow:0 14px 40px rgba(0,0,0,.4);padding:10px;display:flex;flex-direction:column;gap:8px}' +
  '.panel.choose{width:auto;min-width:220px}' +
  '.chips{display:flex;flex-wrap:wrap;gap:4px}' +
  '.chip{background:#27272a;padding:4px 8px;border-radius:999px;font-size:12px}' +
  '.chip[aria-pressed="true"]{background:#f59e0b;color:#18181b;font-weight:600}' +
  '.panel textarea{width:100%;min-height:44px;resize:vertical;background:#27272a;border:1px solid #3f3f46;border-radius:8px;color:inherit;font:inherit;padding:6px 8px}' +
  '.row{display:flex;align-items:center;gap:6px;flex-wrap:wrap}' +
  '.label{font-size:11px;color:#a1a1aa}' +
  '.target{font-size:12px;color:#a1a1aa;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}' +
  '.go{background:#f59e0b;color:#18181b;font-weight:700;margin-left:auto}' +
  '.go:hover{background:#fbbf24}' +
  '.sw{position:fixed;z-index:2147483645;display:flex;align-items:center;gap:2px;padding:4px;border-radius:10px;background:#18181b;' +
  'border:1px solid #27272a;box-shadow:0 10px 30px rgba(0,0,0,.35);white-space:nowrap}' +
  '.sw .count{font-variant-numeric:tabular-nums;padding:0 6px;font-weight:600}' +
  '.sw .lab{color:#a1a1aa;font-size:12px;padding-right:4px;max-width:120px;overflow:hidden;text-overflow:ellipsis}' +
  '.sw .accept{background:#f59e0b;color:#18181b;font-weight:700}' +
  '.sw button[aria-pressed="true"]{background:rgba(245,158,11,.2);color:#fbbf24;font-weight:600}' +
  '.sw .accept:hover{background:#fbbf24}' +
  '.sw .state{padding:0 8px;color:#fbbf24}' +
  '.sw .err{padding:0 8px;color:#fca5a5;max-width:260px;overflow:hidden;text-overflow:ellipsis}' +
  '.params{position:fixed;z-index:2147483645;display:flex;flex-wrap:wrap;gap:8px;align-items:center;padding:6px 8px;border-radius:10px;' +
  'background:#18181b;border:1px solid #27272a;font-size:12px;max-width:min(560px,calc(100vw - 32px))}' +
  '.params input[type=range]{width:110px;accent-color:#f59e0b}' +
  '.frame{position:fixed;z-index:2147483644;pointer-events:none;border:2px solid #0f766e;border-radius:6px;box-shadow:0 0 0 4px rgba(15,118,110,.2)}' +
  '.shimmer{position:fixed;z-index:2147483644;pointer-events:none;border-radius:6px;overflow:hidden;' +
  'background:linear-gradient(100deg,rgba(245,158,11,.05) 20%,rgba(245,158,11,.28) 50%,rgba(245,158,11,.05) 80%);background-size:200% 100%;' +
  'animation:shine 1.2s linear infinite;outline:2px dashed rgba(245,158,11,.8);outline-offset:2px}' +
  '@keyframes shine{from{background-position:200% 0}to{background-position:-200% 0}}' +
  '@media (prefers-reduced-motion:reduce){.shimmer{animation:none}}' +
  '.cframe{position:fixed;z-index:2147483644;pointer-events:none;border:1px solid rgba(161,161,170,.4);border-radius:6px}' +
  '.cframe.sel{border:2px solid #f59e0b;box-shadow:0 0 0 3px rgba(245,158,11,.18)}' +
  '.tag{position:fixed;z-index:2147483645;padding:2px 8px;border-radius:999px;font-size:11px;font-weight:600;line-height:16px;' +
  'background:rgba(24,24,27,.78);border:1px solid rgba(63,63,70,.8);opacity:.8;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}' +
  '.tag:hover{background:rgba(24,24,27,.92);opacity:1}' +
  '.tag.sel,.tag.sel:hover{background:#f59e0b;border-color:#f59e0b;color:#18181b;opacity:1}' +
  '[hidden]{display:none!important}' +
  '.light{color:#18181b}' +
  '.light .steer,.light .panel,.light .sw,.light .params,.light .pickbar{background:#fff;border-color:#e4e4e7;box-shadow:0 10px 30px rgba(16,24,40,.14)}' +
  '.light .pickbar .steer{background:#f4f4f5;border-color:transparent;box-shadow:none}' +
  '.light button:hover{background:#f4f4f5}' +
  '.light .chip,.light .panel textarea,.light .seg{background:#f4f4f5;border-color:#e4e4e7}' +
  '.light .seg button:hover{background:#e4e4e7}' +
  '.light .seg button[aria-pressed="true"]{background:#f59e0b}' +
  '.light .disclosure,.light .link{color:#71717a}' +
  '.light .hint{background:#fff;border-color:#e4e4e7}' +
  '.light .sep{background:#e4e4e7}' +
  '.light .label,.light .target,.light .sw .lab{color:#71717a}' +
  '.light .sw .state{color:#b45309}' +
  '.light .sw button[aria-pressed="true"]{color:#b45309}' +
  '.light .sw .err{color:#b91c1c}' +
  '.light .cframe{border-color:rgba(113,113,122,.35)}' +
  '.light .tag{background:rgba(255,255,255,.88);border-color:#e4e4e7}' +
  '.light .tag:hover{background:#fff}' +
  '.light .tag.sel,.light .tag.sel:hover{background:#f59e0b;border-color:#f59e0b}'

/** Page-level styles: hidden variants and the pick hover outline. Compare leaves the page layout alone. */
export const PAGE_CSS =
  '[data-grasp-live]>[data-grasp-variant][hidden]{display:none!important}' +
  '[data-grasp-live-hover]{outline:2px solid #f59e0b!important;outline-offset:2px!important;cursor:crosshair!important}' +
  'html[data-grasp-live-picking],html[data-grasp-live-picking] *{cursor:crosshair!important}'
