export const origin = "https://www.approving-ai.com";
export const repository = "https://github.com/cocofhu/grasp";

export const strings = {
  "zh-CN": {
    help: "帮助中心",
    workflow: "工作流",
    config: "配置",
    skip: "跳到正文",
    language: "切换语言",
    primary: "主导航",
    browse: "文档目录",
    contents: "本页内容",
    search: "搜索文档",
    searchPlaceholder: "搜索功能、配置名或错误信息",
    searchTip: "输入关键词开始搜索。",
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
    footer: { quick: "快速开始", config: "配置参考", source: "源码", legal: "开源软件 · MIT 许可证" },
    quickLookup: "配置速查",
    quickTopics: ["模型组与账号", "沙箱容量与回收", "环境变量索引"],
    prev: "上一篇",
    next: "下一篇",
  },
  en: {
    help: "Help center",
    workflow: "Workflows",
    config: "Configuration",
    skip: "Skip to content",
    language: "Change language",
    primary: "Main navigation",
    browse: "Browse documentation",
    contents: "On this page",
    search: "Search docs",
    searchPlaceholder: "Find a feature, setting, or error",
    searchTip: "Enter a keyword to search.",
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
    footer: { quick: "Quick start", config: "Configuration reference", source: "Source code", legal: "Open source · MIT license" },
    quickLookup: "Configuration shortcuts",
    quickTopics: ["Model groups and accounts", "Capacity and reclaim", "Environment variable index"],
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
        <a class="header-secondary" href="${url(home)}#workflow">${escape(t.workflow)}</a>
        <a class="header-secondary" href="${url(localized(page.locale, "/help/configuration/"))}">${escape(t.config)}</a>
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

function footer(locale, url, t) {
  return `<footer class="site-footer">
    <div class="site-footer__inner">
      <div>${brand(locale, url)}<p>${escape(t.footer.legal)}</p></div>
      <nav aria-label="${escape(t.help)}">
        <a href="${url(localized(locale, "/help/"))}">${escape(t.help)}</a>
        <a href="${url(localized(locale, "/guide/quick-start/"))}">${escape(t.footer.quick)}</a>
        <a href="${url(localized(locale, "/help/configuration/"))}">${escape(t.footer.config)}</a>
        <a href="${repository}">${escape(t.footer.source)}</a>
      </nav>
    </div>
  </footer>`;
}

function searchDialog(t, index) {
  return `<dialog class="search-dialog" aria-labelledby="search-label"
    data-index="${index}" data-tip="${escape(t.searchTip)}" data-empty="${escape(t.searchEmpty)}" data-error="${escape(t.searchError)}">
    <div class="search-dialog__field">
      <span class="search-icon" aria-hidden="true"></span>
      <label class="sr-only" id="search-label" for="doc-search">${escape(t.search)}</label>
      <input id="doc-search" type="search" autocomplete="off" spellcheck="false"
        role="combobox" aria-autocomplete="list" aria-controls="search-results" aria-expanded="false"
        placeholder="${escape(t.searchPlaceholder)}">
      <button class="key-button" type="button" data-search-close aria-label="${escape(t.close)}">Esc</button>
    </div>
    <p class="search-dialog__status" data-search-status role="status">${escape(t.searchTip)}</p>
    <ul id="search-results" role="listbox" aria-label="${escape(t.search)}"></ul>
    <div class="search-dialog__keys" aria-hidden="true"><kbd>↑</kbd><kbd>↓</kbd> ${escape(t.selectResult)} <kbd>↵</kbd> ${escape(t.openResult)}</div>
  </dialog>`;
}

export function layout(page, url, main) {
  const { locale, route } = page;
  const t = strings[locale];
  const pair = localizedPair(route, locale);
  const home = page.kind === "home";
  return `<!doctype html>
<html lang="${locale}"${home ? ' data-home-entry="1"' : ""}>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#f8f7f5">
  <title>${escape(page.title)}${home ? "" : " · Grasp"}</title>
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
  ${home ? `<script src="${url("/js/home.js")}" defer></script>` : ""}
</head>
<body class="${home ? "page-home" : page.kind === "hub" ? "page-hub" : "page-doc"}" data-copy="${escape(t.copy)}" data-copied="${escape(t.copied)}" data-copy-error="${escape(t.copyError)}">
  <a class="skip-link" href="#main">${escape(t.skip)}</a>
  ${header(page, url, t, pair)}
  ${main}
  ${footer(locale, url, t)}
  ${searchDialog(t, url(localized(locale, "/help/search-index.json")))}
</body>
</html>
`;
}

function sections(pages, locale) {
  return Object.keys(strings[locale].docsNav).map(id => ({
    title: strings[locale].docsNav[id],
    pages: pages.filter(p => p.locale === locale && p.section === id).sort((a, b) => a.order - b.order),
  })).filter(section => section.pages.length);
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
    const quickPaths = ["/help/agents-models/#model-groups", "/help/settings/#sandbox-capacity", "/help/configuration-reference/"];
    return layout(page, url, `<main id="main">
      <section class="help-hero">
        <div class="help-hero__inner">
          <div class="help-hero__copy">
            <h1>${escape(page.title)}</h1>
            <p>${escape(page.description)}</p>
            ${searchButton(t, "search-trigger--hero")}
          </div>
          <section class="help-quick" aria-labelledby="help-quick-title">
            <img src="${url("/assets/logo-mark.svg")}" alt="" width="70" height="70">
            <h2 id="help-quick-title">${escape(t.quickLookup)}</h2>
            <ul>${quickPaths.map((route, index) => `<li><a href="${url(localized(page.locale, route))}">${escape(t.quickTopics[index])}</a></li>`).join("")}</ul>
          </section>
        </div>
      </section>
      <div class="help-grid">
        ${groups.map(group => `<section class="help-group">
          <h2>${escape(group.title)}</h2>
          <div class="help-group__items">${group.pages.map(p => `<a class="help-card" href="${url(p.route)}">
            <h3>${escape(p.navTitle)}</h3>
            <p>${escape(p.description)}</p>
          </a>`).join("")}</div>
        </section>`).join("")}
      </div>
    </main>`);
  }

  return layout(page, url, `<div class="docs-layout">
    ${sidebar(page, groups, url, t)}
    <main id="main" class="doc-main">
      <nav class="doc-crumbs" aria-label="${escape(t.browse)}"><a href="${url(localized(page.locale, "/help/"))}">${escape(t.help)}</a><span aria-hidden="true">/</span><span>${escape(t.docsNav[page.section])}</span></nav>
      <header class="doc-title"><h1>${escape(page.title)}</h1><p>${escape(page.description)}</p></header>
      ${headings(page, t, true)}
      <article class="doc-content">${page.html}</article>
      <p class="doc-edit"><a href="${repository}/edit/main/${page.source}">${escape(t.edit)}</a></p>
      ${pagination(page, groups, url, t)}
    </main>
    ${headings(page, t)}
  </div>`);
}
