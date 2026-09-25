/* Grasp IP-direct preview cooperative script. Runs in the app origin. */
(function () {
  if (window.__graspPreviewPick) return;
  window.__graspPreviewPick = true;

  // Drawer page protocol (web/src/lib/inbox/embedChat.ts).
  var EMBED_PICK = 'grasp-embed:pick';
  var EMBED_READY = 'grasp-embed:ready';
  var EMBED_THEME = 'grasp-embed:theme';
  var EMBED_HASH = '__grasp_embed';
  var EMBED_ORIGIN_PATH = '/__grasp/embed-origin';
  var EMBED_STORE_KEY = '__grasp_embed';

  var MAX_ITEMS = 20;
  var MAX_TEXT = 120;
  var MAX_HTML = 1024;
  var STORE_KEY = '__grasp_preview_picks';

  var zh = /^zh/i.test((navigator.language || '') + '');
  var T = zh
    ? {
        pick: '取点',
        picking: '取点中 · Esc 退出',
        picked: '已选',
        remove: '移除',
        full: '最多 ' + MAX_ITEMS + ' 个，请先移除一些',
        added: '已添加到 Grasp 对话框',
        empty: '点「取点」后点击页面元素',
        chat: '对话',
        chatTitle: 'Grasp · Agent 对话',
        brand: 'Grasp',
        close: '收起对话',
        toLight: '切换到浅色',
        toDark: '切换到深色',
      }
    : {
        pick: 'Pick',
        picking: 'Picking · Esc to stop',
        picked: 'Picked',
        remove: 'Remove',
        full: 'Up to ' + MAX_ITEMS + ' picks. Remove some first.',
        added: 'Added to the Grasp chat',
        empty: 'Turn on Pick, then click an element',
        chat: 'Chat',
        chatTitle: 'Grasp · Agent chat',
        brand: 'Grasp',
        close: 'Hide chat',
        toLight: 'Switch to light',
        toDark: 'Switch to dark',
      };

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
  var outbox = [];

  // Standalone picks survive full page loads within this tab (multi-page apps).
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
    if (!host) return false;
    var t = ev.target;
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

  var BAR_CSS =
    ':host{all:initial}' +
    '.bar{position:fixed;right:16px;bottom:16px;z-index:2147483647;max-width:320px;' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb;' +
    'background:#111827;border:1px solid #374151;border-radius:10px;' +
    'box-shadow:0 6px 24px rgba(0,0,0,.35);padding:6px}' +
    '.row{display:flex;align-items:center;gap:6px}' +
    'button{font:inherit;color:inherit;background:transparent;border:0;cursor:pointer;border-radius:6px;padding:4px 8px}' +
    'button:hover{background:#1f2937}' +
    'button:focus-visible{outline:2px solid #3b82f6;outline-offset:1px}' +
    '.toggle{background:#1f2937;font-weight:600}' +
    '.toggle[aria-pressed="true"]{background:#064e3b;color:#6ee7b7}' +
    '.chat{background:#312e81;color:#e0e7ff;font-weight:600}' +
    '.chat[aria-expanded="true"]{background:#4338ca}' +
    '.count{color:#9ca3af}' +
    '.list{list-style:none;margin:6px 0 0;padding:0;max-height:220px;overflow:auto}' +
    '.list li{display:flex;align-items:center;gap:6px;padding:3px 2px;border-top:1px solid #1f2937}' +
    '.list code{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' +
    'font:11px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;color:#6ee7b7}' +
    '.list button{color:#9ca3af;padding:2px 6px}' +
    '.notice{margin-top:4px;color:#fbbf24}' +
    '.notice.ok{color:#6ee7b7}' +
    '.drawer{position:fixed;z-index:2147483646;display:flex;flex-direction:column;overflow:hidden;' +
    'background:#0b0b0c;border-radius:22px;box-shadow:0 16px 40px rgba(0,0,0,.35);' +
    'font:12px/1.4 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e5e7eb}' +
    '.dhead{display:flex;align-items:center;gap:8px;height:64px;padding:0 16px 0 22px;flex:none;' +
    'cursor:grab;user-select:none;touch-action:none}' +
    '.dhead span{flex:1;min-width:0;font-size:28px;font-weight:650;letter-spacing:-0.03em;line-height:1;color:#a5b4fc}' +
    '.dhead button{width:36px;height:36px;padding:0;display:grid;place-items:center;flex:none;' +
    'border:1px solid #374151;border-radius:8px;background:#111827}' +
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
    '.light .chat{background:#eef0ff;color:#4f46e5}' +
    '.light .chat[aria-expanded="true"]{background:#dcdcfe}' +
    '.drawer.light{color:#18181b;background:#fff;box-shadow:0 16px 40px rgba(16,24,40,.12)}' +
    '.drawer.light .dhead span{color:#4f46e5}' +
    '.drawer.light .dhead button{border-color:#e4e4e7;background:#fff;color:#3f3f46}' +
    '.drawer.light iframe{background:#fafafb}' +
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
    var dir = start.dir;
    var x = start.x;
    var y = start.y;
    var w = start.w;
    var h = start.h;
    if (dir.indexOf('e') >= 0) w = start.w + dx;
    if (dir.indexOf('s') >= 0) h = start.h + dy;
    if (dir.indexOf('w') >= 0) {
      w = start.w - dx;
      x = start.x + dx;
    }
    if (dir.indexOf('n') >= 0) {
      h = start.h - dy;
      y = start.y + dy;
    }
    var v = viewport();
    var minW = v.vw > 0 && v.vw < MIN_W ? v.vw : MIN_W;
    var minH = v.vh > 0 && v.vh < MIN_H ? v.vh : MIN_H;
    if (w < minW) {
      if (dir.indexOf('w') >= 0) x = start.x + (start.w - minW);
      w = minW;
    }
    if (h < minH) {
      if (dir.indexOf('n') >= 0) y = start.y + (start.h - minH);
      h = minH;
    }
    return clampBox({ x: x, y: y, w: w, h: h });
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
    if (!drawerOpen || drag) return;
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
    if (!ui) return;
    var before = box.x + ',' + box.y + ',' + box.w + ',' + box.h;
    box = clampBox(box);
    applyBox();
    if (drawer && before !== box.x + ',' + box.y + ',' + box.w + ',' + box.h) persistBox();
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
    var shadow = host.attachShadow ? host.attachShadow({ mode: 'open' }) : host;
    shadow.innerHTML =
      '<style>' + BAR_CSS + '</style>' +
      '<div class="drawer" data-role="drawer" hidden>' +
      '<div class="dhead" data-role="drawer-head"><span data-role="drawer-title"></span>' +
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
      '<div class="bar" part="bar" data-role="bar">' +
      '<div class="row">' +
      '<button type="button" class="toggle" data-role="toggle" aria-pressed="false"></button>' +
      '<button type="button" class="chat" data-role="chat" aria-expanded="false" hidden></button>' +
      '<span class="count" data-role="count"></span>' +
      '</div>' +
      '<ul class="list" data-role="list"></ul>' +
      '<div class="notice" data-role="notice" role="status" hidden></div>' +
      '</div>';
    ui = {
      shadow: shadow,
      bar: shadow.querySelector('[data-role="bar"]'),
      toggle: shadow.querySelector('[data-role="toggle"]'),
      chat: shadow.querySelector('[data-role="chat"]'),
      count: shadow.querySelector('[data-role="count"]'),
      list: shadow.querySelector('[data-role="list"]'),
      notice: shadow.querySelector('[data-role="notice"]'),
      drawer: shadow.querySelector('[data-role="drawer"]'),
      theme: shadow.querySelector('[data-role="drawer-theme"]'),
    };
    shadow.querySelector('[data-role="drawer-title"]').textContent = T.brand;
    var head = shadow.querySelector('[data-role="drawer-head"]');
    head.addEventListener('pointerdown', onHeadPointerDown);
    Array.prototype.forEach.call(shadow.querySelectorAll('.edge'), function (edge) {
      edge.addEventListener('pointerdown', onEdgePointerDown);
    });
    var closeBtn = shadow.querySelector('[data-role="drawer-close"]');
    closeBtn.setAttribute('aria-label', T.close);
    closeBtn.title = T.close;
    ui.toggle.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setEnabled(!enabled);
    });
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
    closeBtn.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setDrawerOpen(false);
    });
    ui.list.addEventListener('click', function (ev) {
      var btn = ev.target && ev.target.closest ? ev.target.closest('button[data-index]') : null;
      if (!btn) return;
      ev.preventDefault();
      ev.stopPropagation();
      items.splice(Number(btn.getAttribute('data-index')), 1);
      saveItems();
      render();
    });
    root.appendChild(host);
    if (!drawer) box = defaultBox();
    if (drawer) attachDrawer();
    render();
  }

  function render() {
    if (!ui) return;
    ui.toggle.textContent = enabled ? T.picking : T.pick;
    ui.toggle.setAttribute('aria-pressed', enabled ? 'true' : 'false');
    ui.chat.hidden = !drawer;
    ui.chat.textContent = T.chat;
    ui.chat.setAttribute('aria-expanded', drawerOpen ? 'true' : 'false');
    ui.drawer.hidden = !drawerOpen;
    var light = drawerTheme() === 'light';
    ui.drawer.className = light ? 'drawer light' : 'drawer';
    ui.bar.className = 'bar' + (light ? ' light' : '');
    applyBox();
    ui.theme.textContent = light ? '☾' : '☀';
    ui.theme.setAttribute('aria-label', light ? T.toDark : T.toLight);
    ui.theme.title = light ? T.toDark : T.toLight;
    ui.count.textContent = drawer ? '' : items.length ? T.picked + ' ' + items.length : T.empty;
    ui.list.hidden = !!drawer || !items.length;
    ui.list.textContent = '';
    if (drawer) return;
    items.forEach(function (it, i) {
      var li = document.createElement('li');
      var code = document.createElement('code');
      code.textContent = it.text ? it.tagName + ' · ' + it.text : it.selector;
      code.title = it.selector + '\n' + it.url;
      var rm = document.createElement('button');
      rm.type = 'button';
      rm.setAttribute('data-index', String(i));
      rm.setAttribute('aria-label', T.remove + ' ' + it.selector);
      rm.textContent = '×';
      li.appendChild(code);
      li.appendChild(rm);
      ui.list.appendChild(li);
    });
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

  function setEnabled(on) {
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
    drawerOpen = !!on && !!drawer;
    if (drawer) {
      drawer.embed.open = drawerOpen;
      saveEmbed(drawer.embed);
    }
    render();
  }

  // ---- chat drawer ----

  function drawerTheme() {
    return drawer && drawer.embed.theme === 'light' ? 'light' : 'dark';
  }

  function postTheme() {
    if (!drawer || !drawer.frame.contentWindow) return;
    try {
      drawer.frame.contentWindow.postMessage({ type: EMBED_THEME, theme: drawerTheme() }, drawer.origin);
    } catch (e) {}
  }

  function setDrawerTheme(t) {
    if (!drawer) return;
    drawer.embed.theme = t;
    saveEmbed(drawer.embed);
    postTheme();
    render();
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
    };
    try {
      // The ticket must not linger in the address bar, history or the app's router.
      history.replaceState(history.state, '', location.pathname + location.search);
    } catch (e) {}
    return got.run && got.node && got.ticket ? got : null;
  }

  function loadEmbed() {
    try {
      var v = JSON.parse(sessionStorage.getItem(EMBED_STORE_KEY) || 'null');
      if (v && typeof v.origin === 'string' && typeof v.run === 'string' && typeof v.node === 'string') return v;
    } catch (e) {}
    return null;
  }

  function saveEmbed(v) {
    try {
      sessionStorage.setItem(EMBED_STORE_KEY, JSON.stringify(v));
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
    var frag = readEmbedFragment();
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
          resumeDrawer();
          return;
        }
        startDrawer({ origin: v.origin, run: frag.run, node: frag.node, open: true, theme: frag.theme }, frag.ticket);
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
    var item = describe(t);
    if (drawer) {
      sendPick(item);
      setDrawerOpen(true);
      notice(T.added, true);
      return;
    }
    for (var i = 0; i < items.length; i++) {
      if (items[i].selector === item.selector && items[i].url === item.url) return;
    }
    if (items.length >= MAX_ITEMS) {
      notice(T.full, false);
      return;
    }
    items.push(item);
    saveItems();
    render();
  }

  function onKeydown(ev) {
    if (!enabled) return;
    if (ev.key !== 'Escape' && ev.key !== 'Esc') return;
    ev.preventDefault();
    ev.stopPropagation();
    setEnabled(false);
  }

  window.addEventListener('message', function (ev) {
    if (!drawer || ev.source !== drawer.frame.contentWindow || ev.origin !== drawer.origin) return;
    var data = ev.data;
    if (!data || typeof data !== 'object' || data.type !== EMBED_READY) return;
    drawerReady = true;
    postTheme();
    flushOutbox();
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
