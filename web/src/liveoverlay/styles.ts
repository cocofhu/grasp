/** Shadow-DOM styles for the Live overlay (dark default, `.light` variant). */
export const OVERLAY_CSS =
  ':host{all:initial}' +
  '*{box-sizing:border-box}' +
  '.root{font:13px/1.4 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb}' +
  'button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;border-radius:8px;padding:6px 10px}' +
  'button:hover{background:rgba(255,255,255,.08)}' +
  'button:focus-visible,input:focus-visible,textarea:focus-visible{outline:2px solid #f59e0b;outline-offset:1px}' +
  'button:disabled{opacity:.45;cursor:not-allowed}' +
  '.bar{position:fixed;left:50%;bottom:16px;transform:translateX(-50%);z-index:2147483646;display:flex;align-items:center;gap:4px;' +
  'padding:6px;border-radius:12px;background:#18181b;border:1px solid #27272a;box-shadow:0 10px 30px rgba(0,0,0,.35);max-width:calc(100vw - 32px)}' +
  '.mark{width:22px;height:22px;border-radius:6px;background:#f59e0b;color:#18181b;display:grid;place-items:center;font-weight:800;margin:0 4px;position:relative;flex:none}' +
  '.mark.busy::after{content:"";position:absolute;right:-3px;top:-3px;width:8px;height:8px;border-radius:50%;background:#fbbf24;animation:pulse 1s infinite}' +
  '@keyframes pulse{50%{opacity:.3}}' +
  '.act[aria-pressed="true"]{background:#3f3f46}' +
  '.sep{width:1px;height:20px;background:#3f3f46;margin:0 2px;flex:none}' +
  '.steer{display:flex;align-items:center;gap:2px;background:#27272a;border-radius:8px;padding:0 2px 0 8px;min-width:0}' +
  '.steer input{background:transparent;border:0;color:inherit;font:inherit;width:180px;min-width:60px;padding:6px 0;outline:none}' +
  '.hint{position:fixed;left:50%;bottom:68px;transform:translateX(-50%);z-index:2147483646;background:#27272a;border:1px solid #3f3f46;' +
  'border-radius:8px;padding:6px 10px;font-size:12px;max-width:calc(100vw - 32px)}' +
  '.hint button{padding:2px 6px;text-decoration:underline}' +
  '.panel{position:fixed;z-index:2147483646;width:320px;max-width:calc(100vw - 32px);background:#18181b;border:1px solid #3f3f46;border-radius:12px;' +
  'box-shadow:0 14px 40px rgba(0,0,0,.4);padding:10px;display:flex;flex-direction:column;gap:8px}' +
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
  '@media (prefers-reduced-motion:reduce){.shimmer,.mark.busy::after{animation:none}}' +
  '.badge{position:fixed;z-index:2147483645;display:flex;align-items:center;gap:4px;padding:3px;border-radius:8px;background:#18181b;' +
  'border:1px solid #3f3f46;font-size:12px;white-space:nowrap}' +
  '.badge b{padding:0 6px}' +
  '.badge .accept{background:#f59e0b;color:#18181b;font-weight:700;padding:3px 8px}' +
  '.badge button{padding:3px 8px}' +
  '[hidden]{display:none!important}' +
  '.light{color:#18181b}' +
  '.light .bar,.light .panel,.light .sw,.light .params,.light .badge{background:#fff;border-color:#e4e4e7;box-shadow:0 10px 30px rgba(16,24,40,.14)}' +
  '.light button:hover{background:#f4f4f5}' +
  '.light .act[aria-pressed="true"]{background:#e4e4e7}' +
  '.light .steer,.light .chip,.light .panel textarea{background:#f4f4f5;border-color:#e4e4e7}' +
  '.light .hint{background:#fff;border-color:#e4e4e7}' +
  '.light .sep{background:#e4e4e7}' +
  '.light .label,.light .target,.light .sw .lab{color:#71717a}' +
  '.light .sw .state{color:#b45309}' +
  '.light .sw .err{color:#b91c1c}'

/** Page-level styles: pick hover outline and the compare grid. */
export const PAGE_CSS =
  '[data-grasp-live]>[data-grasp-variant][hidden]{display:none!important}' +
  '[data-grasp-live-hover]{outline:2px solid #f59e0b!important;outline-offset:2px!important;cursor:crosshair!important}' +
  'html[data-grasp-live-picking],html[data-grasp-live-picking] *{cursor:crosshair!important}' +
  '[data-grasp-live][data-grasp-compare]{display:grid!important;grid-template-columns:repeat(auto-fit,minmax(min(100%,300px),1fr));gap:20px;align-items:start}' +
  '[data-grasp-live][data-grasp-compare][data-grasp-compare="stack"]{grid-template-columns:1fr}' +
  '[data-grasp-live][data-grasp-compare]>[data-grasp-variant]{outline:1px dashed rgba(245,158,11,.7);outline-offset:4px;margin-top:28px!important}'
