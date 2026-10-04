/* Grasp IP-direct preview cooperative script. Runs in the app origin. */
(function () {
  if (window.__graspPreviewPick) return;
  window.__graspPreviewPick = true;
  var scriptSrc = (document.currentScript && document.currentScript.src) || '';

  // Drawer page protocol (web/src/lib/inbox/embedChat.ts).
  var EMBED_PICK = 'grasp-embed:pick';
  var EMBED_READY = 'grasp-embed:ready';
  var EMBED_SESSION = 'grasp-embed:session';
  var EMBED_THEME = 'grasp-embed:theme';
  var EMBED_LANG = 'grasp-embed:lang';
  var EMBED_HASH = '__grasp_embed';
  var EMBED_ORIGIN_PATH = '/__grasp/embed-origin';
  var EMBED_STORE_KEY = '__grasp_embed';
  // Survives a full-page redirect (the preview app sending the user to login)
  // that would otherwise drop the ticket after it was stripped from the URL.
  var EMBED_PENDING_KEY = '__grasp_embed_pending';
  var EMBED_CONTROL = 'grasp-embed:control';
  var EMBED_CMD = 'grasp-embed:cmd';
  var EMBED_CMD_RESULT = 'grasp-embed:cmd-result';
  var PAGE_CONTROL_CAP = 'page-control';
  var TAB_KEY = '__grasp_tab';
  var TAB_CHANNEL = '__grasp_tabs';
  var TAB_PROBE_MS = 150;
  var EXEC_SCRIPT = 'page-control.js';
  // Live variants (web/src/lib/inbox/liveVariants.ts + web/src/liveoverlay).
  var EMBED_LIVE_PREFIX = 'grasp-embed:live';
  var EMBED_LIVE_CAPS = 'grasp-embed:live-caps';
  var LIVE_SCRIPT = 'live-overlay.js';

  var MAX_ITEMS = 20;
  var MAX_TEXT = 120;
  var MAX_HTML = 1024;
  var STORE_KEY = '__grasp_preview_picks';

  var ICON_MOON =
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
  var ICON_SUN =
    '<svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4' +
    'M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>';

  var STRINGS = {
    zh: {
      pick: '取点',
      picking: '取点中 · Esc 退出',
      added: '已添加到 Grasp 对话框',
      artifact: '产物',
      artifactTitle: '产物',
      closeArtifact: '关闭产物',
      chat: '对话',
      chatTitle: 'Grasp · Agent 对话',
      needTicket: '需要从预览页跳转重新获得票据',
      brand: 'Page Harness CoCo',
      close: '收起对话',
      toLight: '切换到浅色',
      toDark: '切换到深色',
      agentOn: 'Agent 可操作此页面',
      agentBusy: 'Agent 正在操作…',
      stop: '停止',
      pickDesign: '点选元素引用或修改，也可以插入区块或整页调整',
      eye: '按住看原版，点击收起选择层',
      eyeShow: '展开选择层',
    },
    en: {
      pick: 'Pick',
      picking: 'Picking · Esc to stop',
      added: 'Added to the Grasp chat',
      artifact: 'Artifact',
      artifactTitle: 'Artifacts',
      closeArtifact: 'Close artifacts',
      chat: 'Chat',
      chatTitle: 'Grasp · Agent chat',
      needTicket: 'Reopen from the preview page in Grasp to get a new ticket.',
      brand: 'Page Harness CoCo',
      close: 'Hide chat',
      toLight: 'Switch to light',
      toDark: 'Switch to dark',
      agentOn: 'Agent can operate this page',
      agentBusy: 'Agent is operating…',
      stop: 'Stop',
      pickDesign: 'Pick an element to quote or edit, insert a block, or adjust the whole page',
      eye: 'Hold to see the original, click to hide the selection layer',
      eyeShow: 'Show the selection layer',
    },
  };

  // Grasp UI language: `zh-CN` | `en`, or '' when unknown.
  function normLang(v) {
    v = typeof v === 'string' ? v : '';
    if (/^zh/i.test(v)) return 'zh-CN';
    if (/^en/i.test(v)) return 'en';
    return '';
  }

  function stringsFor(l) {
    return /^zh/i.test(l || '') ? STRINGS.zh : STRINGS.en;
  }

  // The fragment is still in the address bar here; bootDrawer strips it later.
  function initialLang() {
    try {
      var q = new URLSearchParams((location.hash || '').replace(/^#/, ''));
      if (q.has(EMBED_HASH) && normLang(q.get('lang'))) return normLang(q.get('lang'));
    } catch (e) {}
    var pending = loadPending();
    if (pending && pending.lang) return pending.lang;
    var saved = loadEmbed();
    if (saved && normLang(saved.lang)) return normLang(saved.lang);
    return normLang(navigator.language || '') || 'en';
  }

  var lang = initialLang();
  var T = stringsFor(lang);

  var enabled = false;
  var hoverEl = null;
  var styleEl = null;
  var items = loadItems();
  var host = null;
  var ui = null;
  var noticeTimer = null;
  // Chat drawer: set once Grasp vouches for the origin behind this tab's ticket.
  var drawer = null;
  var drawerOpen = false;
  var drawerReady = false;
  // Artifact modal: separate iframe over the current preview page (not the chat drawer).
  var artifactOpen = false;
  var artifactFrame = null;
  // The drawer reported its session invalid, expired or revoked.
  var sessionDead = false;
  // Custom no-ticket bubble: open only while the gate is hovered or focused.
  var tipHover = false;
  var tipFocus = false;
  var outbox = [];
  // Agent page control, switched on from the drawer.
  var control = { on: false, busy: 0, exec: null, loading: null, pending: {} };
  // Live variants overlay, loaded once the drawer says the node has Live on.
  var live = { enabled: false, api: null, loading: null, inbox: [] };
  var tabId = '';
  var tabReady = resolveTab();

  // Picks staged by older script versions; handed to the drawer once it exists.
  function loadItems() {
    try {
      var v = JSON.parse(sessionStorage.getItem(STORE_KEY) || '[]');
      return Array.isArray(v) ? v.slice(0, MAX_ITEMS) : [];
    } catch (e) {
      return [];
    }
  }

  function saveItems() {
    try {
      if (items.length) sessionStorage.setItem(STORE_KEY, JSON.stringify(items));
      else sessionStorage.removeItem(STORE_KEY);
    } catch (e) {}
  }

  function currentUrl() {
    try {
      return location.href;
    } catch (e) {
      return '';
    }
  }

  function escId(id) {
    if (typeof CSS !== 'undefined' && CSS.escape) return CSS.escape(id);
    return String(id).replace(/([ !"#$%&'()*+,./:;<=>?@[\\\]^`{|}~])/g, '\\$1');
  }

  function seg(e) {
    if (e.id) return '#' + escId(e.id);
    var s = e.tagName.toLowerCase();
    var p = e.parentElement;
    if (!p) return s;
    var same = Array.prototype.filter.call(p.children, function (c) {
      return c.tagName === e.tagName;
    });
    if (same.length > 1) s += ':nth-of-type(' + (same.indexOf(e) + 1) + ')';
    return s;
  }

  function path(e) {
    var parts = [];
    while (e && e.nodeType === 1 && e.tagName.toLowerCase() !== 'html') {
      var s = seg(e);
      parts.unshift(s);
      if (s.charAt(0) === '#') break;
      e = e.parentElement;
    }
    return parts.join(' > ');
  }

  function clip(s, n) {
    s = String(s || '');
    return s.length > n ? s.slice(0, n) + '…' : s;
  }

  function visibleText(e) {
    var s = '';
    try {
      s = e.innerText;
      if (typeof s !== 'string') s = e.textContent || '';
    } catch (err) {
      s = '';
    }
    return clip(s.replace(/\s+/g, ' ').trim(), MAX_TEXT);
  }

  function describe(t) {
    var html = '';
    try {
      html = t.outerHTML || '';
    } catch (e) {}
    return {
      selector: path(t),
      tagName: t.tagName.toLowerCase(),
      text: visibleText(t),
      outerHTML: clip(html, MAX_HTML),
      url: currentUrl(),
    };
  }

  function ensureStyle() {
    if (styleEl) return;
    styleEl = document.createElement('style');
    styleEl.textContent =
      '.__hp-inspect-hover{outline:2px solid #3b82f6!important;outline-offset:1px!important;cursor:crosshair!important;}' +
      'html.__hp-inspecting,html.__hp-inspecting *{cursor:crosshair!important;}';
    (document.head || document.documentElement).appendChild(styleEl);
  }

  function clearHover() {
    if (hoverEl) {
      hoverEl.classList.remove('__hp-inspect-hover');
      hoverEl = null;
    }
  }

  function isOwn(ev) {
    var t = ev.target;
    // The Live overlay draws its own shadow host; never pick it.
    if (t && t.closest && t.closest('[data-grasp-live-overlay]')) return true;
    if (!host) return false;
    if (t === host || host.contains(t)) return true;
    // contains() does not see into the shadow root.
    var p = ev.composedPath ? ev.composedPath() : [];
    return p.indexOf(host) >= 0;
  }

  var MIN_W = 320;
  var MIN_H = 240;
  var DEFAULT_W = 420;
  var DEFAULT_Y = 72;
  var DEFAULT_MARGIN_X = 28;
  var box = { x: 0, y: DEFAULT_Y, w: DEFAULT_W, h: MIN_H };
  var drag = null;
  // Artifact modal: default 920×640 at top-left margin; min 480×320; session-only geometry.
  var ARTIFACT_MIN_W = 480;
  var ARTIFACT_MIN_H = 320;
  var ARTIFACT_DEFAULT_W = 920;
  var ARTIFACT_DEFAULT_H = 640;
  var ARTIFACT_MARGIN = 28;
  var artifactBox = null;
  var artifactDrag = null;

  var BAR_CSS =
    ':host{all:initial}' +
    '.bar{position:fixed;right:16px;bottom:16px;z-index:2147483647;max-width:min(480px,calc(100vw - 32px));' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb;' +
    'background:#111827;border:1px solid #374151;border-radius:10px;' +
    'box-shadow:0 6px 24px rgba(0,0,0,.35);padding:6px}' +
    '.row{display:flex;align-items:center;gap:6px}' +
    'button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;border-radius:6px;padding:4px 8px}' +
    'button:hover{background:#1f2937}' +
    'button:focus-visible{outline:2px solid #3b82f6;outline-offset:1px}' +
    '.toggle{background:#1f2937;font-weight:600}' +
    '.toggle[aria-pressed="true"]{background:#064e3b;color:#6ee7b7}' +
    '.artifact{background:#1f2937;font-weight:600}' +
    '.artifact[aria-expanded="true"]{background:#4338ca;color:#e0e7ff}' +
    '.chat{background:#312e81;color:#e0e7ff;font-weight:600}' +
    '.chat[aria-expanded="true"]{background:#4338ca}' +
    '.live{background:#1f2937;font-weight:600;color:#fbbf24}' +
    '.live[aria-pressed="true"],.live[aria-expanded="true"]{background:#78350f;color:#fde68a}' +
    '.light .live{background:#fef3c7;color:#92400e}' +
    '.light .live[aria-pressed="true"],.light .live[aria-expanded="true"]{background:#fde68a;color:#78350f}' +
    '.notice{margin-top:4px;color:#fbbf24}' +
    '.notice.ok{color:#6ee7b7}' +
    '.mask{position:fixed;inset:0;z-index:2147483645;background:rgba(0,0,0,.45)}' +
    '.modal{position:fixed;display:flex;flex-direction:column;overflow:hidden;min-width:0;' +
    'background:#f3f3f5;color:#18181b;border-radius:14px;box-shadow:0 24px 60px rgba(0,0,0,.4);' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif}' +
    '.mhead{display:flex;align-items:center;gap:8px;height:40px;padding:0 8px 0 12px;flex:none;' +
    'background:#0b0b0c;border-bottom:1px solid #26262c;color:#e5e7eb;' +
    'cursor:grab;user-select:none;touch-action:none}' +
    '.mhead:active{cursor:grabbing}' +
    '.mhead .drag{display:grid;grid-template-columns:repeat(2,3px);grid-auto-rows:3px;gap:3px;' +
    'opacity:.35;flex:none;color:inherit}' +
    '.mhead .drag i{width:3px;height:3px;border-radius:50%;background:currentColor}' +
    '.mhead [data-role="artifact-title"]{flex:1;min-width:0;font-size:13px;font-weight:600}' +
    '.mhead button{width:28px;height:28px;padding:0;display:grid;place-items:center;flex:none;' +
    'border:0;border-radius:6px;background:transparent;color:#9ca3af;font-size:16px;line-height:1;cursor:pointer}' +
    '.mhead button:hover{background:#1f2937;color:#e5e7eb}' +
    '.modal iframe{flex:1;min-height:0;width:100%;border:0;background:#f3f3f5}' +
    '.drawer{position:fixed;z-index:2147483647;display:flex;flex-direction:column;overflow:hidden;' +
    'background:#0b0b0c;border-radius:22px;box-shadow:0 16px 40px rgba(0,0,0,.35);' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb}' +
    '.dhead{display:flex;align-items:center;gap:9px;height:48px;padding:0 10px 0 14px;flex:none;' +
    'cursor:grab;user-select:none;touch-action:none}' +
    '.dhead:active{cursor:grabbing}' +
    '.dhead .drag{display:grid;grid-template-columns:repeat(2,3px);grid-auto-rows:3px;gap:3px;' +
    'opacity:.32;flex:none;color:inherit}' +
    '.dhead .drag i{width:3px;height:3px;border-radius:50%;background:currentColor}' +
    '.dhead .mark{width:22px;height:22px;border-radius:7px;flex:none;display:grid;place-items:center;' +
    'background:linear-gradient(135deg,#6366f1,#8b5cf6);color:#fff;font-size:10px;font-weight:700;letter-spacing:.02em}' +
    '.dhead [data-role="drawer-title"]{flex:1;min-width:0;font-size:14px;font-weight:600;letter-spacing:-0.01em;' +
    'line-height:20px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}' +
    '.dhead button{width:30px;height:30px;padding:0;display:grid;place-items:center;flex:none;' +
    'border:1px solid #26262c;border-radius:9px;background:transparent;color:#9ca3af}' +
    '.drawer iframe{flex:1;min-height:0;width:auto;margin:0 14px 14px;border:0;border-radius:14px;background:#0b0b0c}' +
    '.edge{position:absolute;touch-action:none;z-index:3}' +
    '.edge.n,.edge.s{left:14px;right:14px;height:8px;cursor:ns-resize}' +
    '.edge.n{top:0}.edge.s{bottom:0}' +
    '.edge.e,.edge.w{top:14px;bottom:14px;width:8px;cursor:ew-resize}' +
    '.edge.e{right:0}.edge.w{left:0}' +
    '.edge.nw,.edge.ne,.edge.sw,.edge.se{width:14px;height:14px}' +
    '.edge.nw{top:0;left:0;cursor:nwse-resize}' +
    '.edge.ne{top:0;right:0;cursor:nesw-resize}' +
    '.edge.sw{bottom:0;left:0;cursor:nesw-resize}' +
    '.edge.se{bottom:0;right:0;cursor:nwse-resize}' +
    '.bar.light{color:#18181b;background:#fff;border-color:#e4e4e7;box-shadow:0 6px 24px rgba(16,24,40,.12)}' +
    '.light button:hover{background:#f4f4f5}' +
    '.light .toggle{background:#f4f4f5}' +
    '.light .toggle[aria-pressed="true"]{background:#dcfce7;color:#15803d}' +
    '.light .artifact{background:#f4f4f5}' +
    '.light .artifact[aria-expanded="true"]{background:#dcdcfe;color:#4f46e5}' +
    '.light .chat{background:#eef0ff;color:#4f46e5}' +
    '.light .chat[aria-expanded="true"]{background:#dcdcfe}' +
    '.mask.light .modal{background:#f3f3f5;color:#18181b}' +
    '.mask.light .mhead{background:#fff;border-bottom-color:#e6e6ea;color:#18181b}' +
    '.mask.light .mhead button{color:#71717a}' +
    '.mask.light .mhead button:hover{background:#f4f4f5;color:#71717a}' +
    '.drawer.light{color:#18181b;background:#fff;box-shadow:0 16px 40px rgba(16,24,40,.12)}' +
    '.drawer.light .dhead button{border-color:#e7e7ea;background:transparent;color:#52525b}' +
    '.drawer.light iframe{background:#fafafb}' +
    '.agent{position:fixed;left:50%;top:12px;transform:translateX(-50%);z-index:2147483647;' +
    'display:flex;align-items:center;gap:8px;padding:4px 4px 4px 12px;border-radius:999px;' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e0e7ff;background:#312e81;' +
    'border:1px solid #4338ca;box-shadow:0 6px 24px rgba(0,0,0,.3)}' +
    '.agent.busy{background:#4338ca}' +
    '.agent button{background:#1e1b4b;color:#fff;font-weight:600;border-radius:999px;padding:3px 10px}' +
    '.agent button:hover{background:#111827}' +
    'button:disabled{opacity:.5;cursor:not-allowed;pointer-events:none}' +
    '[data-role="gate"]{position:relative}' +
    '[data-role="gate"].locked{cursor:not-allowed}' +
    '[data-role="gate"].locked:focus-visible{outline:2px solid #3b82f6;outline-offset:2px}' +
    '.tip{position:absolute;right:0;bottom:calc(100% + 10px);z-index:2;box-sizing:border-box;' +
    'width:max-content;max-width:min(220px,calc(100vw - 32px));padding:8px 10px;border-radius:12px;' +
    'background:#111827;color:#e5e7eb;border:1px solid #374151;' +
    'box-shadow:0 6px 24px rgba(0,0,0,.35);white-space:normal;overflow-wrap:break-word;' +
    'pointer-events:none;text-align:start}' +
    '.tip::before,.tip::after{content:"";position:absolute;top:100%;border-style:solid;border-color:transparent}' +
    '.tip::before{right:21px;border-width:7px;border-top-color:#374151}' +
    '.tip::after{right:22px;border-width:6px;border-top-color:#111827}' +
    '.bar.light .tip{background:#fff;color:#18181b;border-color:#e4e4e7;box-shadow:0 6px 24px rgba(16,24,40,.12)}' +
    '.bar.light .tip::before{border-top-color:#e4e4e7}' +
    '.bar.light .tip::after{border-top-color:#fff}' +
    '[hidden]{display:none!important}';

  function viewport() {
    var de = document.documentElement;
    var vw = (de && de.clientWidth) || window.innerWidth || 0;
    var vh = (de && de.clientHeight) || window.innerHeight || 0;
    return { vw: vw, vh: vh };
  }

  function finiteNum(v) {
    return typeof v === 'number' && isFinite(v) ? v : null;
  }

  function clampBox(b) {
    var v = viewport();
    var maxW = v.vw > 0 ? v.vw : MIN_W;
    var maxH = v.vh > 0 ? v.vh : MIN_H;
    var minW = v.vw > 0 && v.vw < MIN_W ? v.vw : MIN_W;
    var minH = v.vh > 0 && v.vh < MIN_H ? v.vh : MIN_H;
    var w = b.w;
    var h = b.h;
    if (!(typeof w === 'number' && isFinite(w))) w = DEFAULT_W;
    if (!(typeof h === 'number' && isFinite(h))) h = minH;
    if (w < minW) w = minW;
    if (h < minH) h = minH;
    if (w > maxW) w = maxW;
    if (h > maxH) h = maxH;
    var x = b.x;
    var y = b.y;
    if (!(typeof x === 'number' && isFinite(x))) x = 0;
    if (!(typeof y === 'number' && isFinite(y))) y = 0;
    var maxX = Math.max(0, (v.vw || w) - w);
    var maxY = Math.max(0, (v.vh || h) - h);
    if (x < 0) x = 0;
    if (y < 0) y = 0;
    if (x > maxX) x = maxX;
    if (y > maxY) y = maxY;
    return { x: Math.round(x), y: Math.round(y), w: Math.round(w), h: Math.round(h) };
  }

  function defaultBox() {
    var v = viewport();
    return clampBox({
      x: (v.vw || DEFAULT_W) - DEFAULT_W - DEFAULT_MARGIN_X,
      y: DEFAULT_Y,
      w: DEFAULT_W,
      h: Math.round((v.vh || 800) * 0.7),
    });
  }

  function boxFromEmbed(e) {
    var d = defaultBox();
    if (!e) return d;
    var x = finiteNum(e.x);
    var y = finiteNum(e.y);
    var w = finiteNum(e.width);
    var h = finiteNum(e.height);
    if (w != null && w <= 0) w = null;
    if (h != null && h <= 0) h = null;
    return clampBox({
      x: x == null ? d.x : x,
      y: y == null ? d.y : y,
      w: w == null ? d.w : w,
      h: h == null ? d.h : h,
    });
  }

  function applyBox() {
    if (!ui) return;
    ui.drawer.style.left = box.x + 'px';
    ui.drawer.style.top = box.y + 'px';
    ui.drawer.style.width = box.w + 'px';
    ui.drawer.style.height = box.h + 'px';
  }

  function persistBox() {
    if (!drawer) return;
    drawer.embed.x = box.x;
    drawer.embed.y = box.y;
    drawer.embed.width = box.w;
    drawer.embed.height = box.h;
    saveEmbed(drawer.embed);
  }

  function setFramePassthrough(on) {
    if (!drawer || !drawer.frame) return;
    drawer.frame.style.pointerEvents = on ? 'none' : '';
  }

  function resizeFrom(start, dx, dy) {
    var dir = start.dir || '';
    var east = dir.indexOf('e') >= 0;
    var south = dir.indexOf('s') >= 0;
    var west = dir.indexOf('w') >= 0;
    var north = dir.indexOf('n') >= 0;
    var x = start.x;
    var y = start.y;
    var w = start.w;
    var h = start.h;
    if (east) w = start.w + dx;
    if (south) h = start.h + dy;
    if (west) {
      w = start.w - dx;
      x = start.x + dx;
    }
    if (north) {
      h = start.h - dy;
      y = start.y + dy;
    }
    var v = viewport();
    var minW = v.vw > 0 && v.vw < MIN_W ? v.vw : MIN_W;
    var minH = v.vh > 0 && v.vh < MIN_H ? v.vh : MIN_H;
    if (w < minW) {
      if (west) x = start.x + (start.w - minW);
      w = minW;
    }
    if (h < minH) {
      if (north) y = start.y + (start.h - minH);
      h = minH;
    }
    // Stop on the viewport edge that is being dragged. The opposite edge stays
    // put; clampBox would slide it inward and the window would jump larger.
    if (east && v.vw > 0 && w > v.vw - x) w = v.vw - x;
    if (west && x < 0) {
      w = w + x;
      x = 0;
    }
    if (south && v.vh > 0 && h > v.vh - y) h = v.vh - y;
    if (north && y < 0) {
      h = h + y;
      y = 0;
    }
    var right = start.x + start.w;
    var bottom = start.y + start.h;
    w = Math.round(w);
    h = Math.round(h);
    if (west) x = Math.round(right) - w;
    else x = Math.round(x);
    if (north) y = Math.round(bottom) - h;
    else y = Math.round(y);
    return { x: x, y: y, w: w, h: h };
  }

  function onPointerMove(ev) {
    if (!drag) return;
    var dx = ev.clientX - drag.sx;
    var dy = ev.clientY - drag.sy;
    if (drag.kind === 'move') box = clampBox({ x: drag.x + dx, y: drag.y + dy, w: drag.w, h: drag.h });
    else box = resizeFrom(drag, dx, dy);
    applyBox();
  }

  function endDrag() {
    if (!drag) return;
    drag = null;
    setFramePassthrough(false);
    window.removeEventListener('pointermove', onPointerMove, true);
    window.removeEventListener('pointerup', endDrag, true);
    window.removeEventListener('pointercancel', endDrag, true);
    persistBox();
  }

  function beginDrag(ev, kind, dir) {
    if (!drawerOpen || drag || artifactDrag) return;
    if (ev.button != null && ev.button !== 0) return;
    ev.preventDefault();
    ev.stopPropagation();
    drag = {
      kind: kind,
      dir: dir || '',
      sx: ev.clientX,
      sy: ev.clientY,
      x: box.x,
      y: box.y,
      w: box.w,
      h: box.h,
    };
    setFramePassthrough(true);
    try {
      if (ev.currentTarget && ev.currentTarget.setPointerCapture && ev.pointerId != null) {
        ev.currentTarget.setPointerCapture(ev.pointerId);
      }
    } catch (e) {}
    window.addEventListener('pointermove', onPointerMove, true);
    window.addEventListener('pointerup', endDrag, true);
    window.addEventListener('pointercancel', endDrag, true);
  }

  function onHeadPointerDown(ev) {
    var t = ev.target;
    if (t && t.closest && t.closest('button')) return;
    beginDrag(ev, 'move', '');
  }

  function onEdgePointerDown(ev) {
    var dir = ev.currentTarget && ev.currentTarget.getAttribute ? ev.currentTarget.getAttribute('data-dir') : '';
    beginDrag(ev, 'resize', dir || '');
  }

  function onViewportResize() {
    if (!ui || drag || artifactDrag) return;
    // Pull the window into the current viewport for display only. The size
    // written on pointerup stays in the session, so a larger viewport can restore it.
    box = drawer ? boxFromEmbed(drawer.embed) : clampBox(box);
    applyBox();
    if (artifactOpen) {
      artifactBox = clampArtifactBox(artifactBox || artifactDefaultBox());
      applyArtifactBox();
    }
  }

  function clampArtifactBox(b) {
    var v = viewport();
    var maxW = v.vw > 0 ? v.vw : ARTIFACT_MIN_W;
    var maxH = v.vh > 0 ? v.vh : ARTIFACT_MIN_H;
    var minW = v.vw > 0 && v.vw < ARTIFACT_MIN_W ? v.vw : ARTIFACT_MIN_W;
    var minH = v.vh > 0 && v.vh < ARTIFACT_MIN_H ? v.vh : ARTIFACT_MIN_H;
    var w = b && b.w;
    var h = b && b.h;
    if (!(typeof w === 'number' && isFinite(w))) w = Math.max(0, maxW - ARTIFACT_MARGIN * 2);
    if (!(typeof h === 'number' && isFinite(h))) h = Math.max(0, maxH - ARTIFACT_MARGIN * 2);
    if (w < minW) w = minW;
    if (h < minH) h = minH;
    if (w > maxW) w = maxW;
    if (h > maxH) h = maxH;
    var x = b && b.x;
    var y = b && b.y;
    if (!(typeof x === 'number' && isFinite(x))) x = ARTIFACT_MARGIN;
    if (!(typeof y === 'number' && isFinite(y))) y = ARTIFACT_MARGIN;
    var maxX = Math.max(0, (v.vw || w) - w);
    var maxY = Math.max(0, (v.vh || h) - h);
    if (x < 0) x = 0;
    if (y < 0) y = 0;
    if (x > maxX) x = maxX;
    if (y > maxY) y = maxY;
    return { x: Math.round(x), y: Math.round(y), w: Math.round(w), h: Math.round(h) };
  }

  function artifactDefaultBox() {
    return clampArtifactBox({
      x: ARTIFACT_MARGIN,
      y: ARTIFACT_MARGIN,
      w: ARTIFACT_DEFAULT_W,
      h: ARTIFACT_DEFAULT_H,
    });
  }

  function applyArtifactBox() {
    if (!ui || !ui.artifactModal) return;
    var b = clampArtifactBox(artifactBox || artifactDefaultBox());
    ui.artifactModal.style.left = b.x + 'px';
    ui.artifactModal.style.top = b.y + 'px';
    ui.artifactModal.style.width = b.w + 'px';
    ui.artifactModal.style.height = b.h + 'px';
  }

  function setArtifactFramePassthrough(on) {
    if (!artifactFrame) return;
    artifactFrame.style.pointerEvents = on ? 'none' : '';
  }

  function artifactResizeFrom(start, dx, dy) {
    var dir = start.dir || '';
    var east = dir.indexOf('e') >= 0;
    var south = dir.indexOf('s') >= 0;
    var west = dir.indexOf('w') >= 0;
    var north = dir.indexOf('n') >= 0;
    var x = start.x;
    var y = start.y;
    var w = start.w;
    var h = start.h;
    if (east) w = start.w + dx;
    if (south) h = start.h + dy;
    if (west) {
      w = start.w - dx;
      x = start.x + dx;
    }
    if (north) {
      h = start.h - dy;
      y = start.y + dy;
    }
    var v = viewport();
    var minW = v.vw > 0 && v.vw < ARTIFACT_MIN_W ? v.vw : ARTIFACT_MIN_W;
    var minH = v.vh > 0 && v.vh < ARTIFACT_MIN_H ? v.vh : ARTIFACT_MIN_H;
    if (w < minW) {
      if (west) x = start.x + (start.w - minW);
      w = minW;
    }
    if (h < minH) {
      if (north) y = start.y + (start.h - minH);
      h = minH;
    }
    if (east && v.vw > 0 && w > v.vw - x) w = v.vw - x;
    if (west && x < 0) {
      w = w + x;
      x = 0;
    }
    if (south && v.vh > 0 && h > v.vh - y) h = v.vh - y;
    if (north && y < 0) {
      h = h + y;
      y = 0;
    }
    var right = start.x + start.w;
    var bottom = start.y + start.h;
    w = Math.round(w);
    h = Math.round(h);
    if (west) x = Math.round(right) - w;
    else x = Math.round(x);
    if (north) y = Math.round(bottom) - h;
    else y = Math.round(y);
    return { x: x, y: y, w: w, h: h };
  }

  function onArtifactPointerMove(ev) {
    if (!artifactDrag) return;
    var dx = ev.clientX - artifactDrag.sx;
    var dy = ev.clientY - artifactDrag.sy;
    if (artifactDrag.kind === 'move') {
      artifactBox = clampArtifactBox({
        x: artifactDrag.x + dx,
        y: artifactDrag.y + dy,
        w: artifactDrag.w,
        h: artifactDrag.h,
      });
    } else {
      artifactBox = artifactResizeFrom(artifactDrag, dx, dy);
    }
    applyArtifactBox();
  }

  function endArtifactDrag() {
    if (!artifactDrag) return;
    artifactDrag = null;
    setArtifactFramePassthrough(false);
    window.removeEventListener('pointermove', onArtifactPointerMove, true);
    window.removeEventListener('pointerup', endArtifactDrag, true);
    window.removeEventListener('pointercancel', endArtifactDrag, true);
    if (ui && ui.artifactModal) {
      artifactBox = {
        x: Number.parseInt(ui.artifactModal.style.left, 10) || 0,
        y: Number.parseInt(ui.artifactModal.style.top, 10) || 0,
        w: Number.parseInt(ui.artifactModal.style.width, 10) || 0,
        h: Number.parseInt(ui.artifactModal.style.height, 10) || 0,
      };
      artifactBox = clampArtifactBox(artifactBox);
    }
  }

  function beginArtifactDrag(ev, kind, dir) {
    if (!artifactOpen || artifactDrag || drag) return;
    if (ev.button != null && ev.button !== 0) return;
    ev.preventDefault();
    ev.stopPropagation();
    var cur = clampArtifactBox(artifactBox || artifactDefaultBox());
    artifactDrag = {
      kind: kind,
      dir: dir || '',
      sx: ev.clientX,
      sy: ev.clientY,
      x: cur.x,
      y: cur.y,
      w: cur.w,
      h: cur.h,
    };
    setArtifactFramePassthrough(true);
    try {
      if (ev.currentTarget && ev.currentTarget.setPointerCapture && ev.pointerId != null) {
        ev.currentTarget.setPointerCapture(ev.pointerId);
      }
    } catch (e) {}
    window.addEventListener('pointermove', onArtifactPointerMove, true);
    window.addEventListener('pointerup', endArtifactDrag, true);
    window.addEventListener('pointercancel', endArtifactDrag, true);
  }

  function onArtifactHeadPointerDown(ev) {
    var t = ev.target;
    if (t && t.closest && t.closest('button')) return;
    beginArtifactDrag(ev, 'move', '');
  }

  function onArtifactEdgePointerDown(ev) {
    var dir = ev.currentTarget && ev.currentTarget.getAttribute ? ev.currentTarget.getAttribute('data-dir') : '';
    beginArtifactDrag(ev, 'resize', dir || '');
  }

  function mountBar() {
    var root = document.body || document.documentElement;
    if (!root) return;
    if (host) {
      // Apps that replace <body> on navigation drop the bar; put it back.
      if (!host.isConnected) root.appendChild(host);
      return;
    }
    host = document.createElement('grasp-preview-pick');
    host.setAttribute('data-grasp-preview-pick', '');
    host.setAttribute('data-page-agent-not-interactive', '');
    var shadow = host.attachShadow ? host.attachShadow({ mode: 'open' }) : host;
    shadow.innerHTML =
      '<style>' + BAR_CSS + '</style>' +
      '<div class="mask" data-role="artifact-mask" hidden>' +
      '<div class="modal" data-role="artifact-modal" role="dialog" aria-modal="true">' +
      '<div class="mhead" data-role="artifact-head">' +
      '<span class="drag" data-role="artifact-grip" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i></span>' +
      '<span data-role="artifact-title"></span>' +
      '<button type="button" data-role="artifact-close" aria-label="">×</button>' +
      '</div>' +
      '<div class="edge n" data-dir="n"></div><div class="edge s" data-dir="s"></div>' +
      '<div class="edge e" data-dir="e"></div><div class="edge w" data-dir="w"></div>' +
      '<div class="edge nw" data-dir="nw"></div><div class="edge ne" data-dir="ne"></div>' +
      '<div class="edge sw" data-dir="sw"></div><div class="edge se" data-dir="se"></div>' +
      '</div></div>' +
      '<div class="drawer" data-role="drawer" hidden>' +
      '<div class="dhead" data-role="drawer-head">' +
      '<span class="drag" data-role="drawer-grip" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i><i></i></span>' +
      '<span class="mark" aria-hidden="true">PH</span>' +
      '<span data-role="drawer-title"></span>' +
      '<button type="button" data-role="drawer-theme"></button>' +
      '<button type="button" data-role="drawer-close" class="close">' +
      '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">' +
      '<rect x="1.5" y="2.5" width="13" height="11" rx="1.5" stroke="currentColor"/>' +
      '<path d="M6 2.5v11M9.2 6.2 7.4 8l1.8 1.8" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"/>' +
      '</svg></button></div>' +
      '<div class="edge n" data-dir="n"></div><div class="edge s" data-dir="s"></div>' +
      '<div class="edge e" data-dir="e"></div><div class="edge w" data-dir="w"></div>' +
      '<div class="edge nw" data-dir="nw"></div><div class="edge ne" data-dir="ne"></div>' +
      '<div class="edge sw" data-dir="sw"></div><div class="edge se" data-dir="se"></div>' +
      '</div>' +
      '<div class="agent" data-role="agent" role="status" hidden><span data-role="agent-text"></span>' +
      '<button type="button" data-role="agent-stop"></button></div>' +
      '<div class="bar" part="bar" data-role="bar">' +
      '<div class="row">' +
      '<span class="row" data-role="gate">' +
      '<span class="tip" data-role="ticket-tip" role="tooltip" id="grasp-ticket-tip" hidden></span>' +
      '<button type="button" class="toggle" data-role="toggle" aria-pressed="false"></button>' +
      '<button type="button" class="live" data-role="eye" aria-pressed="false" hidden>👁</button>' +
      '<button type="button" class="artifact" data-role="artifact" aria-expanded="false"></button>' +
      '<button type="button" class="chat" data-role="chat" aria-expanded="false"></button>' +
      '</span>' +
      '</div>' +
      '<div class="notice" data-role="notice" role="status" hidden></div>' +
      '</div>';
    ui = {
      shadow: shadow,
      bar: shadow.querySelector('[data-role="bar"]'),
      gate: shadow.querySelector('[data-role="gate"]'),
      tip: shadow.querySelector('[data-role="ticket-tip"]'),
      toggle: shadow.querySelector('[data-role="toggle"]'),
      eye: shadow.querySelector('[data-role="eye"]'),
      artifact: shadow.querySelector('[data-role="artifact"]'),
      chat: shadow.querySelector('[data-role="chat"]'),
      notice: shadow.querySelector('[data-role="notice"]'),
      drawer: shadow.querySelector('[data-role="drawer"]'),
      theme: shadow.querySelector('[data-role="drawer-theme"]'),
      agent: shadow.querySelector('[data-role="agent"]'),
      agentText: shadow.querySelector('[data-role="agent-text"]'),
      agentStop: shadow.querySelector('[data-role="agent-stop"]'),
      artifactMask: shadow.querySelector('[data-role="artifact-mask"]'),
      artifactModal: shadow.querySelector('[data-role="artifact-modal"]'),
      artifactTitle: shadow.querySelector('[data-role="artifact-title"]'),
      artifactClose: shadow.querySelector('[data-role="artifact-close"]'),
      drawerTitle: shadow.querySelector('[data-role="drawer-title"]'),
      drawerClose: shadow.querySelector('[data-role="drawer-close"]'),
    };
    ui.agentStop.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      stopControl();
    });
    applyStaticLabels();
    var head = shadow.querySelector('[data-role="drawer-head"]');
    head.addEventListener('pointerdown', onHeadPointerDown);
    Array.prototype.forEach.call(ui.drawer.querySelectorAll('.edge'), function (edge) {
      edge.addEventListener('pointerdown', onEdgePointerDown);
    });
    var artifactHead = shadow.querySelector('[data-role="artifact-head"]');
    artifactHead.addEventListener('pointerdown', onArtifactHeadPointerDown);
    Array.prototype.forEach.call(ui.artifactModal.querySelectorAll('.edge'), function (edge) {
      edge.addEventListener('pointerdown', onArtifactEdgePointerDown);
    });
    ui.toggle.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      if (pickActive()) exitPick();
      else startPickMode();
    });
    ui.artifact.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setArtifactOpen(!artifactOpen);
    });
    // Hold to peek at the original; a short click toggles the selection layer
    // (the same switch as the candidate toolbar's hide control).
    var eyeDownAt = 0;
    var endPeek = function () {
      if (live.api) live.api.setPeek(false);
    };
    ui.eye.addEventListener('pointerdown', function () {
      eyeDownAt = Date.now();
      if (live.api) live.api.setPeek(true);
    });
    ui.eye.addEventListener('pointerup', endPeek);
    ui.eye.addEventListener('pointerleave', endPeek);
    ui.eye.addEventListener('pointercancel', endPeek);
    ui.eye.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      if (live.api && (ev.detail === 0 || Date.now() - eyeDownAt < 300)) live.api.toggleHidden();
    });
    ui.artifactClose.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setArtifactOpen(false);
    });
    // Keep the mask for dimming; do not close on outside click (explicit close / bar toggle only).
    ui.chat.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setDrawerOpen(!drawerOpen);
    });
    ui.theme.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setDrawerTheme(drawerTheme() === 'light' ? 'dark' : 'light');
    });
    ui.gate.addEventListener('mouseenter', function () {
      tipHover = true;
      syncTicketTip();
    });
    ui.gate.addEventListener('mouseleave', function () {
      tipHover = false;
      syncTicketTip();
    });
    ui.gate.addEventListener('focusin', function () {
      tipFocus = true;
      syncTicketTip();
    });
    ui.gate.addEventListener('focusout', function (ev) {
      if (ev.relatedTarget && ui.gate.contains(ev.relatedTarget)) return;
      tipFocus = false;
      syncTicketTip();
    });
    ui.drawerClose.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setDrawerOpen(false);
    });
    root.appendChild(host);
    if (!drawer) box = defaultBox();
    if (drawer) attachDrawer();
    render();
  }

  function syncTicketTip() {
    if (!ui) return;
    var ok = usable();
    ui.gate.removeAttribute('title');
    if (ok) {
      ui.gate.className = 'row';
      ui.gate.removeAttribute('tabindex');
      ui.gate.removeAttribute('aria-describedby');
      ui.tip.textContent = '';
      ui.tip.hidden = true;
      ui.tip.className = 'tip';
      return;
    }
    ui.gate.className = 'row locked';
    ui.gate.setAttribute('tabindex', '0');
    ui.gate.setAttribute('aria-describedby', 'grasp-ticket-tip');
    ui.tip.textContent = T.needTicket;
    var show = tipHover || tipFocus;
    ui.tip.hidden = !show;
    ui.tip.className = show ? 'tip on' : 'tip';
  }

  function applyStaticLabels() {
    if (!ui) return;
    ui.drawerTitle.textContent = T.brand;
    ui.drawerTitle.title = T.brand;
    ui.artifactTitle.textContent = T.artifactTitle;
    ui.artifactClose.setAttribute('aria-label', T.closeArtifact);
    ui.artifactClose.title = T.closeArtifact;
    ui.drawerClose.setAttribute('aria-label', T.close);
    ui.drawerClose.title = T.close;
    if (drawer) drawer.frame.title = T.chatTitle;
    if (artifactFrame) artifactFrame.title = T.artifactTitle;
  }

  function setLang(next) {
    next = normLang(next);
    if (!next) return;
    if (drawer && drawer.embed.lang !== next) {
      drawer.embed.lang = next;
      saveEmbed(drawer.embed);
    }
    if (next === lang) return;
    lang = next;
    T = stringsFor(next);
    applyStaticLabels();
    syncTicketTip();
    render();
    if (live.api && live.api.setLang) live.api.setLang(next);
  }

  function render() {
    if (!ui) return;
    var picking = pickActive();
    ui.toggle.textContent = picking ? T.picking : T.pick;
    ui.toggle.setAttribute('aria-pressed', picking ? 'true' : 'false');
    var ok = usable();
    syncTicketTip();
    ui.toggle.disabled = !ok || control.busy > 0;
    ui.agent.hidden = !control.on;
    ui.agent.className = control.busy > 0 ? 'agent busy' : 'agent';
    ui.agentText.textContent = control.busy > 0 ? T.agentBusy : T.agentOn;
    ui.agentStop.textContent = T.stop;
    var liveOn = !!(live.enabled && live.api);
    var liveOk = liveUsable();
    ui.toggle.title = liveOk ? T.pickDesign : '';
    var chromeHidden = !!(liveOn && live.api.isHidden());
    ui.eye.hidden = !(liveOn && live.api.hasCandidates());
    ui.eye.title = chromeHidden ? T.eyeShow : T.eye;
    ui.eye.setAttribute('aria-label', chromeHidden ? T.eyeShow : T.eye);
    ui.eye.setAttribute('aria-pressed', chromeHidden ? 'true' : 'false');
    ui.artifact.disabled = !ok || !drawerReady;
    ui.artifact.textContent = T.artifact;
    ui.artifact.title = ok && drawerReady ? T.artifactTitle : '';
    ui.artifact.setAttribute('aria-expanded', artifactOpen ? 'true' : 'false');
    ui.artifactMask.hidden = !artifactOpen;
    if (artifactOpen) applyArtifactBox();
    ui.chat.disabled = !ok;
    ui.chat.textContent = T.chat;
    ui.chat.title = ok ? T.chatTitle : '';
    ui.chat.setAttribute('aria-expanded', drawerOpen ? 'true' : 'false');
    ui.drawer.hidden = !drawerOpen;
    var light = drawerTheme() === 'light';
    ui.drawer.className = light ? 'drawer light' : 'drawer';
    ui.bar.className = 'bar' + (light ? ' light' : '');
    ui.artifactMask.className = light ? 'mask light' : 'mask';
    applyBox();
    ui.theme.innerHTML = light ? ICON_MOON : ICON_SUN;
    ui.theme.setAttribute('aria-label', light ? T.toDark : T.toLight);
    ui.theme.title = light ? T.toDark : T.toLight;
  }

  function notice(text, ok) {
    if (!ui) return;
    ui.notice.textContent = text;
    ui.notice.className = ok ? 'notice ok' : 'notice';
    ui.notice.hidden = false;
    if (noticeTimer) clearTimeout(noticeTimer);
    noticeTimer = setTimeout(function () {
      ui.notice.hidden = true;
    }, 2500);
  }

  function usable() {
    return !!drawer && !sessionDead;
  }

  function liveUsable() {
    return !!(live.enabled && live.api) && usable() && drawerReady;
  }

  // Pick mode spans element picking here and, with Live on, the overlay's pick bar
  // (select / insert switch and whole-page input).
  function pickActive() {
    return enabled || !!(live.api && live.api.isPickMode());
  }

  function startPickMode() {
    setEnabled(true);
    if (enabled && liveUsable()) live.api.setPickMode(true);
  }

  function exitPick() {
    setEnabled(false);
    if (live.api) live.api.setPickMode(false);
  }

  function setEnabled(on) {
    // Picking would swallow the agent's clicks.
    if (on && (control.busy > 0 || !usable())) return;
    if (on && live.api) live.api.cancelPick();
    enabled = !!on;
    ensureStyle();
    clearHover();
    var root = document.documentElement;
    if (root) {
      if (enabled) root.classList.add('__hp-inspecting');
      else root.classList.remove('__hp-inspecting');
    }
    render();
  }

  function setDrawerOpen(on) {
    drawerOpen = !!on && usable();
    if (drawer) {
      drawer.embed.open = drawerOpen;
      saveEmbed(drawer.embed);
    }
    render();
  }

  function setArtifactOpen(on) {
    // Wait for the chat iframe to redeem the ticket so the artifact page can
    // reuse the same localStorage session (tickets are one-shot).
    artifactOpen = !!on && usable() && drawerReady;
    if (artifactOpen) {
      ensureArtifactFrame();
      if (!artifactBox) artifactBox = artifactDefaultBox();
      else artifactBox = clampArtifactBox(artifactBox);
    }
    render();
  }

  // ---- chat drawer ----

  function drawerTheme() {
    // Empty when the chat drawer is not attached, so the action card can fall
    // back to the page background. An attached drawer is always light or dark.
    if (!drawer) return '';
    return drawer.embed.theme === 'light' ? 'light' : 'dark';
  }

  function postTheme() {
    if (!drawer) return;
    var theme = drawerTheme();
    if (drawer.frame && drawer.frame.contentWindow) {
      try {
        drawer.frame.contentWindow.postMessage({ type: EMBED_THEME, theme: theme }, drawer.origin);
      } catch (e) {}
    }
    if (artifactFrame && artifactFrame.contentWindow) {
      try {
        artifactFrame.contentWindow.postMessage({ type: EMBED_THEME, theme: theme }, drawer.origin);
      } catch (e) {}
    }
  }

  function setDrawerTheme(t) {
    if (!drawer) return;
    drawer.embed.theme = t;
    saveEmbed(drawer.embed);
    postTheme();
    render();
    if (live.api && live.api.syncTheme) live.api.syncTheme();
  }

  function stashPending(got) {
    try {
      sessionStorage.setItem(
        EMBED_PENDING_KEY,
        JSON.stringify({ run: got.run, node: got.node, ticket: got.ticket, theme: got.theme, lang: got.lang }),
      );
    } catch (e) {}
  }

  function loadPending() {
    try {
      var v = JSON.parse(sessionStorage.getItem(EMBED_PENDING_KEY) || 'null');
      if (v && v.run && v.node && v.ticket) {
        return {
          run: String(v.run),
          node: String(v.node),
          ticket: String(v.ticket),
          theme: v.theme === 'light' ? 'light' : 'dark',
          lang: normLang(v.lang),
        };
      }
    } catch (e) {}
    return null;
  }

  function clearPending() {
    try {
      sessionStorage.removeItem(EMBED_PENDING_KEY);
    } catch (e) {}
  }

  function readEmbedFragment() {
    var raw = '';
    try {
      raw = (location.hash || '').replace(/^#/, '');
    } catch (e) {
      return null;
    }
    if (!raw) return null;
    var q = new URLSearchParams(raw);
    if (!q.has(EMBED_HASH)) return null;
    var got = {
      run: q.get('run') || '',
      node: q.get('node') || '',
      ticket: q.get('ticket') || '',
      theme: q.get('theme') === 'light' ? 'light' : 'dark',
      lang: normLang(q.get('lang')),
    };
    var ok = !!(got.run && got.node && got.ticket);
    if (ok) stashPending(got);
    try {
      // The ticket must not linger in the address bar, history or the app's router.
      history.replaceState(history.state, '', location.pathname + location.search);
    } catch (e) {}
    return ok ? got : null;
  }

  function loadEmbed() {
    try {
      var v = JSON.parse(localStorage.getItem(EMBED_STORE_KEY) || 'null');
      if (v && typeof v.origin === 'string' && typeof v.run === 'string' && typeof v.node === 'string') return v;
    } catch (e) {}
    return null;
  }

  function saveEmbed(v) {
    try {
      localStorage.setItem(EMBED_STORE_KEY, JSON.stringify(v));
    } catch (e) {}
  }

  function chatSrc(e, ticket) {
    var src =
      e.origin +
      '/embed/runs/' +
      encodeURIComponent(e.run) +
      '/nodes/' +
      encodeURIComponent(e.node) +
      '/chat';
    var q = new URLSearchParams();
    if (ticket) q.set('ticket', ticket);
    q.set('theme', e.theme === 'light' ? 'light' : 'dark');
    return src + '#' + q.toString();
  }

  function artifactsSrc(e) {
    var src =
      e.origin +
      '/embed/runs/' +
      encodeURIComponent(e.run) +
      '/nodes/' +
      encodeURIComponent(e.node) +
      '/artifacts';
    var q = new URLSearchParams();
    q.set('theme', e.theme === 'light' ? 'light' : 'dark');
    return src + '#' + q.toString();
  }

  function ensureArtifactFrame() {
    if (!ui || !drawer) return;
    if (!artifactFrame) {
      artifactFrame = document.createElement('iframe');
      artifactFrame.title = T.artifactTitle;
      artifactFrame.setAttribute('referrerpolicy', 'no-referrer');
      artifactFrame.setAttribute('allow', 'clipboard-write');
      artifactFrame.src = artifactsSrc(drawer.embed);
      ui.artifactModal.appendChild(artifactFrame);
      return;
    }
    if (artifactFrame.parentNode !== ui.artifactModal) {
      ui.artifactModal.appendChild(artifactFrame);
    }
  }

  function attachDrawer() {
    if (!ui || !drawer || drawer.frame.parentNode === ui.drawer) return;
    ui.drawer.appendChild(drawer.frame);
  }

  function startDrawer(e, ticket) {
    box = boxFromEmbed(e);
    var frame = document.createElement('iframe');
    frame.title = T.chatTitle;
    frame.setAttribute('referrerpolicy', 'no-referrer');
    frame.setAttribute('allow', 'clipboard-write');
    frame.src = chatSrc(e, ticket);
    drawer = { origin: e.origin, frame: frame, embed: e };
    drawerOpen = !!e.open;
    drawerReady = false;
    sessionDead = false;
    // Picks staged before the drawer existed move to the Grasp chat.
    outbox = outbox.concat(items);
    items = [];
    saveItems();
    attachDrawer();
    render();
  }

  function sendPick(item) {
    if (!drawerReady || !drawer.frame.contentWindow) {
      outbox.push(item);
      return;
    }
    try {
      drawer.frame.contentWindow.postMessage({ type: EMBED_PICK, payload: item }, drawer.origin);
    } catch (e) {}
  }

  function flushOutbox() {
    var queued = outbox;
    outbox = [];
    queued.forEach(sendPick);
  }

  function resumeDrawer() {
    var saved = loadEmbed();
    if (saved && !drawer) startDrawer(saved, '');
  }

  function bootDrawer() {
    var frag = readEmbedFragment() || loadPending();
    if (!frag || typeof fetch !== 'function') {
      resumeDrawer();
      return;
    }
    var q = '?ticket=' + encodeURIComponent(frag.ticket) + '&node=' + encodeURIComponent(frag.node);
    fetch(EMBED_ORIGIN_PATH + q, { credentials: 'omit', cache: 'no-store' })
      .then(function (res) {
        return res.ok ? res.json() : null;
      })
      .then(function (v) {
        if (!v || v.runId !== frag.run || v.nodeId !== frag.node || !/^https?:\/\/[^/?#]+$/.test(v.origin || '')) {
          clearPending();
          resumeDrawer();
          return;
        }
        // Keep the pending ticket until the drawer reports a live session.
        // A login redirect destroys this page before the iframe can redeem it.
        startDrawer(
          { origin: v.origin, run: frag.run, node: frag.node, open: true, theme: frag.theme, lang: frag.lang || lang },
          frag.ticket,
        );
        setDrawerOpen(true);
      })
      .catch(resumeDrawer);
  }

  function onMove(ev) {
    if (!enabled) return;
    var t = ev.target;
    if (!t || t === hoverEl || t === styleEl) return;
    if (t.nodeType !== 1 || isOwn(ev)) {
      clearHover();
      return;
    }
    clearHover();
    hoverEl = t;
    hoverEl.classList.add('__hp-inspect-hover');
  }

  function onClick(ev) {
    if (!enabled) return;
    var t = ev.target;
    if (!t || t.nodeType !== 1 || isOwn(ev)) return;
    ev.preventDefault();
    ev.stopPropagation();
    clearHover();
    if (liveUsable()) {
      setEnabled(false);
      live.api.offer(t);
      return;
    }
    pickToChat(t);
  }

  function pickToChat(t) {
    sendPick(describe(t));
    setDrawerOpen(true);
    notice(T.added, true);
  }

  function onKeydown(ev) {
    if (!pickActive()) return;
    if (ev.key !== 'Escape' && ev.key !== 'Esc') return;
    ev.preventDefault();
    ev.stopPropagation();
    exitPick();
  }

  // ---- agent page control ----

  function randomHex() {
    var b = new Uint8Array(8);
    try {
      crypto.getRandomValues(b);
    } catch (e) {
      for (var i = 0; i < b.length; i++) b[i] = Math.floor(Math.random() * 256);
    }
    return Array.prototype.map
      .call(b, function (x) {
        return (x < 16 ? '0' : '') + x.toString(16);
      })
      .join('');
  }

  // A tab opened from this one starts with a copy of sessionStorage, so the id
  // is only kept when no other live tab answers for it.
  function resolveTab() {
    var id = '';
    try {
      id = sessionStorage.getItem(TAB_KEY) || '';
    } catch (e) {}
    var fresh = function () {
      id = randomHex();
      try {
        sessionStorage.setItem(TAB_KEY, id);
      } catch (e) {}
    };
    if (typeof BroadcastChannel !== 'function') {
      if (!id) fresh();
      tabId = id;
      return Promise.resolve();
    }
    var ch = new BroadcastChannel(TAB_CHANNEL);
    var me = randomHex();
    var taken = false;
    ch.onmessage = function (ev) {
      var d = ev.data || {};
      if (d.from === me) return;
      if (d.kind === 'who' && d.tab && d.tab === (tabId || id)) ch.postMessage({ kind: 'mine', tab: d.tab, to: d.from, from: me });
      else if (d.kind === 'mine' && d.to === me) taken = true;
    };
    if (!id) {
      fresh();
      tabId = id;
      return Promise.resolve();
    }
    ch.postMessage({ kind: 'who', tab: id, from: me });
    return new Promise(function (resolve) {
      setTimeout(function () {
        if (taken) fresh();
        tabId = id;
        resolve();
      }, TAB_PROBE_MS);
    });
  }

  function postDrawer(msg) {
    if (!drawer || !drawer.frame.contentWindow) return;
    try {
      drawer.frame.contentWindow.postMessage(msg, drawer.origin);
    } catch (e) {}
  }

  function liveSrc() {
    if (/preview-pick\.js([?#].*)?$/.test(scriptSrc)) return scriptSrc.replace(/preview-pick\.js([?#].*)?$/, LIVE_SCRIPT);
    return '/__grasp/' + LIVE_SCRIPT;
  }

  function makeLive(api) {
    var inst = api.create({
      post: function (msg) {
        if (!drawerReady || sessionDead) return false;
        postDrawer(msg);
        return true;
      },
      theme: drawerTheme,
      notice: notice,
      stopPick: function () {
        setEnabled(false);
      },
      startPick: function () {
        setEnabled(true);
      },
      lang: lang,
      sendToChat: pickToChat,
      changed: render,
      isOwnUi: function (el) {
        return !!(host && el && (el === host || host.contains(el)));
      },
    });
    var queued = live.inbox;
    live.inbox = [];
    queued.forEach(function (m) {
      inst.onDrawer(m);
    });
    return inst;
  }

  function loadLive() {
    if (live.api) return Promise.resolve(live.api);
    if (live.loading) return live.loading;
    live.loading = new Promise(function (resolve, reject) {
      var api = window.__graspLiveOverlay;
      if (api && api.version === 3) {
        resolve(api);
        return;
      }
      var s = document.createElement('script');
      s.src = liveSrc();
      s.async = true;
      s.setAttribute('data-grasp-live-overlay', '');
      s.onload = function () {
        var loaded = window.__graspLiveOverlay;
        if (loaded && loaded.version === 3) resolve(loaded);
        else reject(new Error('live overlay missing'));
      };
      s.onerror = function () {
        reject(new Error('live overlay failed to load'));
      };
      (document.head || document.documentElement).appendChild(s);
    })
      .then(function (api) {
        live.api = makeLive(api);
        if (enabled && liveUsable()) live.api.setPickMode(true);
        render();
        return live.api;
      })
      .catch(function (e) {
        live.loading = null;
        throw e;
      });
    return live.loading;
  }

  function onLiveMessage(data) {
    if (data.type === 'grasp-embed:live-context-request') {
      var nonce = typeof data.nonce === 'string' ? data.nonce : '';
      if (!nonce || nonce.length > 64) return;
      var answer = function (ok) {
        ok = ok && live.enabled && usable() && drawerReady;
        postDrawer({ type: 'grasp-embed:live-context-result', nonce: nonce, ok: ok, url: ok ? currentUrl() : undefined });
      };
      if (!live.enabled || !usable() || !drawerReady) answer(false);
      else loadLive().then(function () { answer(true); }, function () { answer(false); });
      return;
    }
    if (data.type === EMBED_LIVE_CAPS) {
      live.enabled = data.enabled === true;
      if (live.enabled && live.api) live.api.setEnabled(true);
      else if (live.enabled) loadLive().catch(function () {});
      else if (live.api) live.api.setEnabled(false);
      render();
      return;
    }
    if (live.api) live.api.onDrawer(data);
    else if (live.inbox.length < 50) live.inbox.push(data);
  }

  function execSrc() {
    if (/preview-pick\.js([?#].*)?$/.test(scriptSrc)) return scriptSrc.replace(/preview-pick\.js([?#].*)?$/, EXEC_SCRIPT);
    return '/__grasp/' + EXEC_SCRIPT;
  }

  function makeExec(api) {
    return api.create({
      hideOwnUi: function (hidden) {
        if (host) host.style.display = hidden ? 'none' : '';
      },
    });
  }

  function loadExec() {
    if (control.exec) return Promise.resolve(control.exec);
    if (control.loading) return control.loading;
    control.loading = new Promise(function (resolve, reject) {
      var api = window.__graspPageControl;
      if (api && api.version === 1) {
        resolve(makeExec(api));
        return;
      }
      var s = document.createElement('script');
      s.src = execSrc();
      s.async = true;
      s.setAttribute('data-grasp-page-control', '');
      s.onload = function () {
        var a = window.__graspPageControl;
        if (a && a.version === 1) resolve(makeExec(a));
        else reject(new Error('页面操作脚本版本不匹配,请用户刷新预览页'));
      };
      s.onerror = function () {
        reject(new Error('无法加载页面操作脚本(可能被页面的内容安全策略 CSP 拦截)'));
      };
      (document.head || document.documentElement).appendChild(s);
    }).then(
      function (x) {
        control.exec = x;
        return x;
      },
      function (e) {
        control.loading = null;
        throw e;
      },
    );
    return control.loading;
  }

  function abortPending() {
    for (var k in control.pending) {
      var ac = control.pending[k];
      if (ac && ac.abort) ac.abort();
    }
  }

  function setControl(on) {
    control.on = !!on;
    if (!control.on) {
      abortPending();
      armExec(control.exec);
    } else {
      // Loading early also keeps the first command from waiting on the download.
      loadExec()
        .then(armExec)
        .catch(function () {});
    }
    render();
  }

  function armExec(ex) {
    if (ex && ex.setArmed) ex.setArmed(control.on);
  }

  function stopControl() {
    setControl(false);
    postDrawer({ type: EMBED_CONTROL, stop: true });
  }

  function replyCmd(nonce, r) {
    r = r || {};
    postDrawer({ type: EMBED_CMD_RESULT, nonce: nonce, ok: !!r.ok, error: r.error, note: r.note, state: r.state });
  }

  function onCmd(d) {
    var nonce = typeof d.nonce === 'string' ? d.nonce : '';
    if (!nonce) return;
    if (d.action === 'cancel') {
      var c = control.pending[nonce];
      if (c && c.abort) c.abort();
      return;
    }
    if (!control.on) {
      replyCmd(nonce, { ok: false, error: '用户没有开启页面操作' });
      return;
    }
    if (pickActive()) exitPick();
    var ac = typeof AbortController === 'function' ? new AbortController() : true;
    control.pending[nonce] = ac;
    control.busy++;
    render();
    var args = d.args && typeof d.args === 'object' ? d.args : {};
    loadExec()
      .then(function (ex) {
        return ex.run({ action: String(d.action || ''), args: args }, ac === true ? undefined : ac.signal);
      })
      .then(
        function (r) {
          replyCmd(nonce, r);
        },
        function (e) {
          replyCmd(nonce, { ok: false, error: String((e && e.message) || e) });
        },
      )
      .then(function () {
        delete control.pending[nonce];
        control.busy--;
        render();
      });
  }

  window.addEventListener('message', function (ev) {
    if (!drawer || ev.source !== drawer.frame.contentWindow || ev.origin !== drawer.origin) return;
    var data = ev.data;
    if (!data || typeof data !== 'object') return;
    if (data.type === EMBED_READY) {
      drawerReady = true;
      sessionDead = false;
      clearPending();
      render();
      postTheme();
      flushOutbox();
      tabReady.then(function () {
        postDrawer({ type: EMBED_CONTROL, caps: [PAGE_CONTROL_CAP], tab: tabId });
      });
    } else if (data.type === EMBED_LANG) {
      setLang(data.lang);
    } else if (data.type === EMBED_SESSION && data.ok === false) {
      sessionDead = true;
      clearPending();
      drawerReady = false;
      setEnabled(false);
      setControl(false);
      setDrawerOpen(false);
      setArtifactOpen(false);
      live.enabled = false;
      if (live.api) live.api.setEnabled(false);
      render();
    } else if (typeof data.type === 'string' && data.type.indexOf(EMBED_LIVE_PREFIX) === 0) {
      onLiveMessage(data);
    } else if (data.type === EMBED_CONTROL && typeof data.on === 'boolean') {
      setControl(data.on);
    } else if (data.type === EMBED_CMD) {
      onCmd(data);
    }
  });

  window.addEventListener('resize', onViewportResize);
  document.addEventListener('mousemove', onMove, true);
  document.addEventListener('click', onClick, true);
  document.addEventListener('keydown', onKeydown, true);
  window.addEventListener('popstate', mountBar);
  window.addEventListener('hashchange', mountBar);
  try {
    var push = history.pushState;
    history.pushState = function () {
      var r = push.apply(this, arguments);
      if (host) mountBar();
      return r;
    };
  } catch (e) {}

  bootDrawer();
  if (document.body) mountBar();
  else document.addEventListener('DOMContentLoaded', mountBar);
})();
