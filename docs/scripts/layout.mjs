export const origin = "https://www.approving-ai.com";
export const repository = "https://github.com/cocofhu/grasp";

export const strings = {
  "zh-CN": {
    help: "帮助中心",
    sandbox: "运行环境", capabilities: "Agent 实例", deploy: "部署",
    config: "配置",
    skip: "跳到正文",
    language: "切换语言",
    primary: "主导航",
    browse: "文档目录",
    contents: "本页内容",
    search: "搜索文档",
    searchPlaceholder: "搜索功能、配置名或错误信息",
    searchEmpty: "没有找到匹配的内容。试试更短的关键词或配置名。",
    searchError: "搜索暂时不可用。可以从文档目录继续浏览。",
    openResult: "打开",
    selectResult: "选择",
    close: "关闭",
    copy: "复制",
    copied: "已复制",
    copyError: "无法访问剪贴板，请手动选择并复制。",
    edit: "在 GitHub 编辑",
    docsNav: { start: "开始使用", use: "使用 Grasp", deploy: "部署与配置", support: "排查与贡献" },
    footer: { workflows: "工作流与人工审批", quick: "快速开始", config: "配置参考", source: "源码", legal: "开源软件 · MIT 许可证" },
    quickLookup: "常用配置",
    quickTopics: ["模型组与账号", "沙箱容量与回收"],
    guides: "常用指南",
    guideTitles: ["快速开始", "配置项索引", "故障排查"],
    prev: "上一篇",
    next: "下一篇",
  },
  en: {
    help: "Help center",
    sandbox: "Runtimes", capabilities: "Examples", deploy: "Deploy",
    config: "Configuration",
    skip: "Skip to content",
    language: "Change language",
    primary: "Main navigation",
    browse: "Browse documentation",
    contents: "On this page",
    search: "Search docs",
    searchPlaceholder: "Find a feature, setting, or error",
    searchEmpty: "No matches. Try a shorter phrase or a configuration key.",
    searchError: "Search is temporarily unavailable. You can still browse the documentation.",
    openResult: "Open",
    selectResult: "Select",
    close: "Close",
    copy: "Copy",
    copied: "Copied",
    copyError: "Clipboard unavailable. Select the code to copy it manually.",
    edit: "Edit on GitHub",
    docsNav: { start: "Get started", use: "Use Grasp", deploy: "Deploy and configure", support: "Troubleshoot and contribute" },
    footer: { workflows: "Workflows and approvals", quick: "Quick start", config: "Configuration reference", source: "Source code", legal: "Open source · MIT license" },
    quickLookup: "Configuration shortcuts",
    quickTopics: ["Model groups", "Sandbox capacity"],
    guides: "Popular guides",
    guideTitles: ["Quick start", "Configuration index", "Troubleshooting"],
    prev: "Previous",
    next: "Next",
  },
};

export function escape(value) {
  return String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

export function localized(locale, route) {
  return locale === "en" ? `/en${route}` : route;
}

export function localizedPair(route, locale) {
  const bare = locale === "en" ? route.slice(3) : route;
  return { "zh-CN": bare, en: `/en${bare}` };
}

function brand(locale, url) {
  return `<a class="brand" href="${url(localized(locale, "/"))}">
    <img src="${url("/assets/logo-mark.svg")}" alt="" width="34" height="34">Grasp
  </a>`;
}

// Keep ordinary mentions in body text in the reading face.
function displayTitle(title) {
  return escape(title).replace(/\bGrasp\b/g, '<span class="title-wordmark">Grasp</span>');
}

function searchButton(t, variant = "") {
  return `<button type="button" class="search-trigger ${variant}" data-search-open hidden>
    <span class="search-icon" aria-hidden="true"></span>
    <span>${escape(variant ? t.searchPlaceholder : t.search)}</span>
    <kbd aria-hidden="true">/</kbd>
  </button>`;
}

function header(page, url, t, pair) {
  const home = localized(page.locale, "/");
  const help = localized(page.locale, "/help/");
  const other = page.locale === "en" ? "zh-CN" : "en";
  const helpCurrent = page.kind === "home" ? "" : ' aria-current="true"';
  return `<header class="site-header">
    <div class="site-header__inner">
      ${brand(page.locale, url)}
      <nav class="site-nav" aria-label="${escape(t.primary)}">
        <a class="header-secondary" data-section="solo" href="${url(home)}#solo">${escape(t.capabilities)}</a>
        <a class="header-secondary" data-section="sandbox" href="${url(home)}#sandbox">${escape(t.sandbox)}</a>
        ${page.kind === "home" ? `<a class="header-secondary" data-section="deploy" href="#deploy">${escape(t.deploy)}</a>`
          : `<a class="header-secondary" href="${url(localized(page.locale, "/help/configuration/"))}">${escape(t.config)}</a>`}
        <a href="${url(help)}"${helpCurrent}>${escape(t.help)}</a>
      </nav>
      <div class="header-actions">
        ${searchButton(t)}
        <a class="language-link" href="${url(pair[other])}" lang="${other}" data-locale-set="${other}" aria-label="${escape(t.language)} · ${other === "en" ? "English" : "中文"}">${other === "en" ? "EN" : "中文"}</a>
        <a class="source-link" href="${repository}">GitHub</a>
      </div>
    </div>
  </header>`;
}

function footer(locale, url, t, home) {
  return `<footer class="site-footer">
    <div class="site-footer__inner">
      <div>${brand(locale, url)}<p>${escape(t.footer.legal)}</p></div>
      <nav aria-label="${escape(t.help)}">
        <a href="${url(localized(locale, "/help/"))}">${escape(t.help)}</a>
        <a href="${url(localized(locale, "/guide/quick-start/"))}">${escape(t.footer.quick)}</a>
        <a href="${url(localized(locale, "/help/configuration/"))}">${escape(t.footer.config)}</a>
        ${home ? `<a id="workflow" href="${url(localized(locale, "/guide/workflows/"))}">${escape(t.footer.workflows)}</a>` : ""}
        <a href="${repository}">${escape(t.footer.source)}</a>
      </nav>
    </div>
  </footer>`;
}

function searchDialog(t, index) {
  return `<dialog class="search-dialog" aria-labelledby="search-label"
    data-index="${index}" data-empty="${escape(t.searchEmpty)}" data-error="${escape(t.searchError)}">
    <div class="search-dialog__field">
      <span class="search-icon" aria-hidden="true"></span>
      <label class="sr-only" id="search-label" for="doc-search">${escape(t.search)}</label>
      <input id="doc-search" type="search" autocomplete="off" spellcheck="false"
        role="combobox" aria-autocomplete="list" aria-controls="search-results" aria-expanded="false"
        placeholder="${escape(t.searchPlaceholder)}">
      <button class="key-button" type="button" data-search-close aria-label="${escape(t.close)}">Esc</button>
    </div>
    <p class="search-dialog__status" data-search-status role="status" hidden></p>
    <ul id="search-results" role="listbox" aria-label="${escape(t.search)}"></ul>
    <div class="search-dialog__keys" aria-hidden="true"><kbd>↑</kbd><kbd>↓</kbd> ${escape(t.selectResult)} <kbd>↵</kbd> ${escape(t.openResult)}</div>
  </dialog>`;
}

export function layout(page, url, main) {
  const { locale, route } = page;
  const t = strings[locale];
  const pair = localizedPair(route, locale);
  const home = page.kind === "home";
  const title = home || /\bGrasp\b/.test(page.title) ? page.title : `${page.title} · Grasp`;
  return `<!doctype html>
<html lang="${locale}"${home ? ' data-home-entry="1"' : ""}>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#fafafb">
  <title>${escape(title)}</title>
  <meta name="description" content="${escape(page.description)}">
  <link rel="canonical" href="${origin}${route}">
  <link rel="alternate" hreflang="zh-CN" href="${origin}${pair["zh-CN"]}">
  <link rel="alternate" hreflang="en" href="${origin}${pair.en}">
  <link rel="alternate" hreflang="x-default" href="${origin}${pair["zh-CN"]}">
  <link rel="icon" href="${url("/assets/logo-mark.svg")}" type="image/svg+xml">
  <link rel="stylesheet" href="${url("/css/tokens.css")}">
  <link rel="stylesheet" href="${url("/css/site.css")}">
  <link rel="stylesheet" href="${url(home ? "/css/home.css" : "/css/page.css")}">
  <script src="${url("/js/locale.js")}" defer></script>
  <script src="${url("/js/docs.js")}" defer></script>
  ${home ? `<script type="module" src="${url("/js/home.js")}"></script>` : ""}
</head>
<body class="${home ? "page-home" : page.kind === "hub" ? "page-hub" : "page-doc"}" data-copy="${escape(t.copy)}" data-copied="${escape(t.copied)}" data-copy-error="${escape(t.copyError)}">
  <a class="skip-link" href="#main">${escape(t.skip)}</a>
  ${header(page, url, t, pair)}
  ${main}
  ${footer(locale, url, t, home)}
  ${searchDialog(t, url(localized(locale, "/help/search-index.json")))}
</body>
</html>
`;
}

function sections(pages, locale) {
  return Object.keys(strings[locale].docsNav).map(id => ({
    id,
    title: strings[locale].docsNav[id],
    pages: pages.filter(p => p.locale === locale && p.section === id).sort((a, b) => a.order - b.order),
  })).filter(section => section.pages.length);
}

const sectionIcons = {
  start: '<path d="M12 6c-3-2.4-7-2.4-10-1v14c3-1.4 7-1.4 10 1 3-2.4 7-2.4 10-1V5c-3-1.4-7-1.4-10 1Zm0 0v14"/>',
  use: '<rect x="2" y="4" width="20" height="16" rx="2.5"/><path d="m6 9 3 3-3 3m6 0h5"/>',
  deploy: '<path d="M3 7h11m4 0h3M3 17h4m4 0h10"/><circle cx="16" cy="7" r="2.5"/><circle cx="9" cy="17" r="2.5"/>',
  support: '<circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="4.7"/><path d="m5 5 3.7 3.7M15.3 15.3 19 19m0-14-3.7 3.7M8.7 15.3 5 19"/>',
};

// The spine is the link. Keep its title in HTML for keyboard and touch users.
function guideShelf(page, url, t) {
  const routes = ["/guide/quick-start/", "/help/configuration-reference/", "/help/troubleshooting/"];
  return `<nav class="guide-shelf" aria-label="${escape(t.guides)}">
    ${routes.map((route, i) => `<a class="guide-volume" href="${url(localized(page.locale, route))}">
      <svg viewBox="0 0 400 104" aria-hidden="true" focusable="false">
        <path class="volume-cover" d="M12 40 58 5q3-2 8-2h321q7 0 5 5l-41 35H18Z"/>
        <path class="volume-rule" d="M28 37 67 8m270 28 32-26h-30"/>
        <path class="volume-pages" d="m350 44 41-35v46q0 4-4 7l-37 31Z"/>
        <path class="volume-leaves" d="m356 50 30-26m-30 36 30-26m-30 36 30-26m-30 36 30-26"/>
        <path class="volume-board" d="m350 89 41-34v5q0 2-3 5l-37 31-11-1Z"/>
        <path class="volume-spine" d="M17 39h332q5 0 5 5v46q0 6-6 6H17Q5 96 5 84V53q0-14 12-14Z"/>
        <path class="volume-rib" d="M19 45v45m6-45v45"/>
      </svg>
      <span class="guide-volume__label">${escape(t.guideTitles[i])}<span class="link-arrow" aria-hidden="true">→</span></span>
    </a>`).join("")}
  </nav>`;
}

function sidebar(page, groups, url, t) {
  const links = groups.map(group => `<section class="docs-nav__group">
    <h2>${escape(group.title)}</h2>
    <ul>${group.pages.map(p => `<li><a href="${url(p.route)}"${p === page ? ' aria-current="page"' : ""}>${escape(p.navTitle)}</a></li>`).join("")}</ul>
  </section>`).join("");
  return `<aside class="docs-nav"><nav class="docs-nav__desktop" aria-label="${escape(t.browse)}">${links}</nav>
    <details class="docs-nav__mobile"><summary>${escape(t.browse)}</summary>
      <nav aria-label="${escape(t.browse)}">${links}</nav></details></aside>`;
}

function headings(page, t, mobile = false) {
  if (!page.toc.length) return "";
  const list = `<ul>${page.toc.map(heading =>
    `<li class="outline-level-${heading.level}"><a href="#${escape(heading.id)}">${escape(heading.title)}</a></li>`).join("")}</ul>`;
  return mobile
    ? `<details class="mobile-outline"><summary>${escape(t.contents)}</summary>${list}</details>`
    : `<nav class="doc-outline" aria-label="${escape(t.contents)}"><p>${escape(t.contents)}</p>${list}</nav>`;
}

function pagination(page, groups, url, t) {
  const ordered = groups.flatMap(group => group.pages);
  const i = ordered.indexOf(page);
  return `<nav class="doc-next" aria-label="${escape(t.browse)}">
    ${[[ordered[i - 1], t.prev], [ordered[i + 1], t.next]].map(([neighbor, label]) => neighbor
      ? `<a href="${url(neighbor.route)}"><span>${escape(label)}</span><strong>${escape(neighbor.navTitle)}</strong></a>`
      : "<span></span>").join("")}
  </nav>`;
}

export function renderDocumentation(page, pages, url) {
  const t = strings[page.locale];
  const groups = sections(pages, page.locale);
  if (page.kind === "hub") {
    const quickPaths = ["/help/agents-models/#model-groups", "/help/settings/#sandbox-capacity"];
    return layout(page, url, `<main id="main">
      <section class="help-hero">
        <div class="help-hero__inner">
          <div class="help-hero__copy">
            <h1>${displayTitle(page.title)}</h1>
            <p>${escape(page.description)}</p>
            ${searchButton(t, "search-trigger--hero")}
            <nav class="help-shortcuts" aria-label="${escape(t.quickLookup)}">
              ${quickPaths.map((route, i) => `<a href="${url(localized(page.locale, route))}">${escape(t.quickTopics[i])}</a>`).join("")}
            </nav>
          </div>
          ${guideShelf(page, url, t)}
        </div>
      </section>
      <div class="help-grid">
        ${groups.map(group => `<section class="help-group" aria-labelledby="${group.id}">
          <h2 id="${group.id}"><svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">${sectionIcons[group.id]}</svg>${escape(group.title)}</h2>
          <ul class="help-group__items">${group.pages.map(p => `<li><a class="help-card" href="${url(p.route)}">
            <h3>${escape(p.navTitle)}</h3><span class="link-arrow" aria-hidden="true">→</span>
            <p>${escape(p.description)}</p>
          </a></li>`).join("")}</ul>
        </section>`).join("")}
      </div>
    </main>`);
  }

  return layout(page, url, `<div class="docs-layout">
    ${sidebar(page, groups, url, t)}
    <main id="main" class="doc-main">
      <nav class="doc-crumbs" aria-label="${escape(t.browse)}"><a href="${url(localized(page.locale, "/help/"))}">${escape(t.help)}</a><span aria-hidden="true">/</span><a href="${url(localized(page.locale, "/help/"))}#${page.section}">${escape(t.docsNav[page.section])}</a></nav>
      <header class="doc-title"><h1>${displayTitle(page.title)}</h1><p>${escape(page.description)}</p></header>
      ${headings(page, t, true)}
      <article class="doc-content">${page.html}</article>
      <p class="doc-edit"><a href="${repository}/edit/main/${page.source}">${escape(t.edit)}</a></p>
      ${pagination(page, groups, url, t)}
    </main>
    ${headings(page, t)}
  </div>`);
}
