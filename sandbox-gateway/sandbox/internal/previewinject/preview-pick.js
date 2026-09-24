/* Grasp IP-direct preview cooperative script. Runs in the app origin. */
(function () {
  if (window.__graspPreviewPick) return;
  window.__graspPreviewPick = true;

  var READY = 'direct-preview-ready';
  var URL_MSG = 'direct-preview-url';
  var PICKED = 'direct-preview-picked';
  var CANCELED = 'direct-preview-canceled';
  var INSPECT = 'direct-preview-inspect';
  var INSPECT_STATE = 'direct-preview-inspect-state';
  var NAV = 'direct-preview-nav';
  var PING = 'direct-preview-ping';
  var HOST = 'direct-preview-host';

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
      }
    : {
        pick: 'Pick',
        picking: 'Picking · Esc to stop',
        picked: 'Picked',
        remove: 'Remove',
        full: 'Up to ' + MAX_ITEMS + ' picks. Remove some first.',
        added: 'Added to the Grasp chat',
        empty: 'Turn on Pick, then click an element',
      };

  var hasParent = false;
  try {
    hasParent = window.parent && window.parent !== window;
  } catch (e) {}

  // Embedded only after the Grasp frame says hello; any other embedder is treated
  // as a standalone window so picks never leave the page for an unknown parent.
  var embedded = false;
  var enabled = false;
  var hoverEl = null;
  var styleEl = null;
  var items = loadItems();
  var host = null;
  var ui = null;
  var noticeTimer = null;

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

  function post(msg) {
    if (!hasParent) return;
    // Always '*' : HTTPS Grasp embedding http://IP:port often strips
    // document.referrer (strict-origin-when-cross-origin), and a wrong
    // targetOrigin fails silently with no exception.
    try {
      parent.postMessage(msg, '*');
    } catch (e) {}
  }

  function currentUrl() {
    try {
      return location.href;
    } catch (e) {
      return '';
    }
  }

  function postUrl(type) {
    if (host) mountBar();
    var u = currentUrl();
    if (!u) return;
    post({ type: type || URL_MSG, url: u });
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
    '.count{color:#9ca3af}' +
    '.list{list-style:none;margin:6px 0 0;padding:0;max-height:220px;overflow:auto}' +
    '.list li{display:flex;align-items:center;gap:6px;padding:3px 2px;border-top:1px solid #1f2937}' +
    '.list code{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' +
    'font:11px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;color:#6ee7b7}' +
    '.list button{color:#9ca3af;padding:2px 6px}' +
    '.notice{margin-top:4px;color:#fbbf24}' +
    '.notice.ok{color:#6ee7b7}' +
    '[hidden]{display:none!important}';

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
      '<div class="bar" part="bar">' +
      '<div class="row">' +
      '<button type="button" class="toggle" data-role="toggle" aria-pressed="false"></button>' +
      '<span class="count" data-role="count"></span>' +
      '</div>' +
      '<ul class="list" data-role="list"></ul>' +
      '<div class="notice" data-role="notice" role="status" hidden></div>' +
      '</div>';
    ui = {
      shadow: shadow,
      toggle: shadow.querySelector('[data-role="toggle"]'),
      count: shadow.querySelector('[data-role="count"]'),
      list: shadow.querySelector('[data-role="list"]'),
      notice: shadow.querySelector('[data-role="notice"]'),
    };
    ui.toggle.addEventListener('click', function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      setEnabled(!enabled, true);
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
    render();
  }

  function render() {
    if (!ui) return;
    ui.toggle.textContent = enabled ? T.picking : T.pick;
    ui.toggle.setAttribute('aria-pressed', enabled ? 'true' : 'false');
    ui.count.textContent = embedded ? '' : items.length ? T.picked + ' ' + items.length : T.empty;
    ui.list.hidden = embedded || !items.length;
    ui.list.textContent = '';
    if (embedded) return;
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

  function setEnabled(on, announce) {
    var next = !!on;
    var changed = next !== enabled;
    enabled = next;
    ensureStyle();
    clearHover();
    var root = document.documentElement;
    if (root) {
      if (enabled) root.classList.add('__hp-inspecting');
      else root.classList.remove('__hp-inspecting');
    }
    render();
    if (announce && changed) post({ type: INSPECT_STATE, on: enabled });
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

  function postPicked(item) {
    post({
      type: PICKED,
      selector: item.selector,
      tagName: item.tagName,
      text: item.text,
      outerHTML: item.outerHTML,
      url: item.url,
    });
  }

  function onClick(ev) {
    if (!enabled) return;
    var t = ev.target;
    if (!t || t.nodeType !== 1 || isOwn(ev)) return;
    ev.preventDefault();
    ev.stopPropagation();
    clearHover();
    var item = describe(t);
    if (embedded) {
      postPicked(item);
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
    setEnabled(false, false);
    post({ type: CANCELED });
  }

  window.addEventListener('message', function (ev) {
    if (!hasParent || ev.source !== window.parent) return;
    var data = ev.data;
    if (!data || typeof data !== 'object') return;
    if (data.type === HOST) {
      if (!embedded) {
        embedded = true;
        // Picks made before the handshake move to the Grasp chat.
        items.forEach(postPicked);
        items = [];
        saveItems();
        render();
      }
      postUrl(READY);
      return;
    }
    if (data.type === PING) {
      postUrl(READY);
      return;
    }
    if (data.type === INSPECT) {
      setEnabled(!!data.on, false);
      return;
    }
    if (data.type === NAV) {
      var action = data.action;
      try {
        if (action === 'back') history.back();
        else if (action === 'forward') history.forward();
        else if (action === 'reload') location.reload();
      } catch (e) {}
    }
  });

  document.addEventListener('mousemove', onMove, true);
  document.addEventListener('click', onClick, true);
  document.addEventListener('keydown', onKeydown, true);
  window.addEventListener('popstate', function () {
    postUrl();
  });
  window.addEventListener('hashchange', function () {
    postUrl();
  });

  try {
    var push = history.pushState;
    history.pushState = function () {
      var r = push.apply(this, arguments);
      postUrl();
      return r;
    };
    var replace = history.replaceState;
    history.replaceState = function () {
      var r = replace.apply(this, arguments);
      postUrl();
      return r;
    };
  } catch (e) {}

  if (document.body) mountBar();
  else document.addEventListener('DOMContentLoaded', mountBar);

  postUrl(READY);
  // Parent may arm its wait on iframe "load", which fires after this script.
  // Re-announce so a late listener still clears the missing-script tip.
  if (document.readyState === 'complete') {
    postUrl(READY);
  } else {
    window.addEventListener('load', function () {
      postUrl(READY);
    });
  }
})();
