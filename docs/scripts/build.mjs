#!/usr/bin/env node
/**
 * Build static site:
 * - copy site/ to public/
 * - convert content markdown files to public/<path>/index.html
 *
 * Locale: zh-CN at site root; English under en/ (content/en/** → public/en/**).
 * BASE_PATH defaults to / for the custom domain (www.approving-ai.com).
 * Legacy CI may set BASE_PATH=/approving-pages; that is remapped to / unless
 * FORCE_PROJECT_PAGES=1 (project Pages fallback).
 */
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";
import MarkdownIt from "markdown-it";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(__dirname, "..");
const siteDir = path.join(root, "site");
const contentDir = path.join(root, "content");
const outDir = path.join(root, "public");

const SITE_CANONICAL = "https://www.approving-ai.com";

// Custom domain is served from /. Legacy ci-docs set BASE_PATH=/approving-pages;
// remap that so CI builds root-relative assets (workflow env update needs workflow scope).
const envBase = process.env.BASE_PATH;
const useLegacyProjectPages =
  envBase === "/approving-pages" && process.env.FORCE_PROJECT_PAGES === "1";
const basePath = normalizeBase(
  useLegacyProjectPages ? envBase : (envBase === "/approving-pages" ? "/" : (envBase ?? "/")),
);
const md = new MarkdownIt({ html: false, linkify: true, typographer: true });

const SHELL = {
  "zh-CN": {
    htmlLang: "zh-CN",
    navAria: "主导航",
    docs: "文档",
    concepts: "概念",
    eyebrow: "文档",
    footerAria: "页脚",
    quickStart: "快速开始",
    configuration: "配置",
    gateway: "网关",
    source: "源码",
    langAria: "语言",
  },
  en: {
    htmlLang: "en",
    navAria: "Main navigation",
    docs: "Docs",
    concepts: "Concepts",
    eyebrow: "Docs",
    footerAria: "Footer",
    quickStart: "Quick start",
    configuration: "Configuration",
    gateway: "Gateway",
    source: "Source",
    langAria: "Language",
  },
};

function normalizeBase(raw) {
  let b = String(raw || "/").trim();
  if (!b.startsWith("/")) b = `/${b}`;
  if (b !== "/" && b.endsWith("/")) b = b.slice(0, -1);
  return b;
}

function withBase(p) {
  if (!p) return basePath === "/" ? "/" : `${basePath}/`;
  if (/^https?:\/\//i.test(p) || p.startsWith("mailto:")) return p;
  const cleaned = p.startsWith("/") ? p : `/${p}`;
  if (basePath === "/") return cleaned;
  return `${basePath}${cleaned}`;
}

function localeFromSlug(slug) {
  return slug === "en" || slug.startsWith("en/") ? "en" : "zh-CN";
}

/** @returns {{ zh: string, en: string }} site-absolute paths ending with / */
function dualPaths(slug) {
  const isEn = slug === "en" || slug.startsWith("en/");
  const bare = isEn ? (slug === "en" ? "" : slug.slice(3)) : slug;
  const zh = bare ? `/${bare}/` : "/";
  const en = bare ? `/en/${bare}/` : "/en/";
  return { zh, en };
}

function canonicalHref(sitePath) {
  return `${SITE_CANONICAL}${sitePath}`;
}

function langSwitcherHtml(locale, dual) {
  const zhCurrent = locale === "zh-CN" ? " is-current" : "";
  const enCurrent = locale === "en" ? " is-current" : "";
  const zhAria = locale === "zh-CN" ? ' aria-current="page"' : "";
  const enAria = locale === "en" ? ' aria-current="page"' : "";
  return `<nav class="lang-switch" aria-label="${escapeHtml(SHELL[locale].langAria)}">
        <a class="lang-switch__link${zhCurrent}" href="${withBase(dual.zh)}" data-locale-set="zh-CN"${zhAria}>中文</a>
        <span class="lang-switch__sep" aria-hidden="true">|</span>
        <a class="lang-switch__link${enCurrent}" href="${withBase(dual.en)}" data-locale-set="en"${enAria}>English</a>
      </nav>`;
}

function hreflangLinks(dual) {
  return `  <link rel="alternate" hreflang="zh-CN" href="${canonicalHref(dual.zh)}">
  <link rel="alternate" hreflang="en" href="${canonicalHref(dual.en)}">
  <link rel="alternate" hreflang="x-default" href="${canonicalHref(dual.zh)}">`;
}

async function rmrf(dir) {
  await fs.rm(dir, { recursive: true, force: true });
}

async function ensureDir(dir) {
  await fs.mkdir(dir, { recursive: true });
}

async function copyDir(src, dest) {
  await ensureDir(dest);
  const entries = await fs.readdir(src, { withFileTypes: true });
  for (const entry of entries) {
    const from = path.join(src, entry.name);
    const to = path.join(dest, entry.name);
    if (entry.isDirectory()) {
      await copyDir(from, to);
    } else {
      await ensureDir(path.dirname(to));
      let data = await fs.readFile(from);
      if (/\.(html|css|js)$/i.test(entry.name)) {
        data = Buffer.from(rewriteBasePlaceholders(data.toString("utf8")), "utf8");
      }
      await fs.writeFile(to, data);
    }
  }
}

function rewriteBasePlaceholders(text) {
  return text
    .replaceAll("{{BASE}}", basePath === "/" ? "" : basePath)
    .replaceAll("{{BASE_SLASH}}", basePath === "/" ? "/" : `${basePath}/`);
}

function docTemplate({ title, description, bodyHtml, relPath }) {
  const locale = localeFromSlug(relPath);
  const t = SHELL[locale];
  const dual = dualPaths(relPath);
  const prefix = locale === "en" ? "/en" : "";
  const homeHref = withBase(locale === "en" ? "/en/" : "/");
  const docsHref = withBase(`${prefix}/guide/quick-start/`);
  const conceptsHref = withBase(`${prefix}/guide/concepts/`);
  const quickStartHref = withBase(`${prefix}/guide/quick-start/`);
  const configHref = withBase(`${prefix}/help/configuration/`);
  const gatewayHref = withBase(`${prefix}/help/gateway/`);
  const dataBase = basePath === "/" ? "" : basePath;
  const pageTitle = title ? `${title} · Grasp` : "Grasp";
  const desc = description || (locale === "en" ? "Grasp help documentation" : "Grasp 帮助文档");

  return `<!DOCTYPE html>
<html lang="${t.htmlLang}" data-base="${escapeHtml(dataBase)}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>${escapeHtml(pageTitle)}</title>
  <meta name="description" content="${escapeHtml(desc)}">
${hreflangLinks(dual)}
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Noto+Sans+SC:wght@400;500;600;700&family=Noto+Serif+SC:wght@500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="${withBase("/css/tokens.css")}">
  <link rel="stylesheet" href="${withBase("/css/site.css")}">
  <link rel="stylesheet" href="${withBase("/css/page.css")}">
  <script src="${withBase("/js/locale.js")}"></script>
</head>
<body class="page-doc">
  <header class="site-header">
    <div class="site-header__inner">
      <a class="site-header__brand" href="${homeHref}">Grasp</a>
      <nav class="site-header__nav" aria-label="${escapeHtml(t.navAria)}">
        <a class="site-header__link" href="${docsHref}">${escapeHtml(t.docs)}</a>
        <a class="site-header__link" href="${conceptsHref}">${escapeHtml(t.concepts)}</a>
        <a class="site-header__link" href="https://github.com/cocofhu/approving" rel="noopener noreferrer" target="_blank">GitHub</a>
        ${langSwitcherHtml(locale, dual)}
      </nav>
    </div>
  </header>
  <main id="main">
    <article class="doc">
      <header class="doc__header">
        <p class="doc__eyebrow">${escapeHtml(t.eyebrow)}</p>
        <h1 class="doc__title">${escapeHtml(title || relPath)}</h1>
        ${description ? `<p class="doc__lead">${escapeHtml(description)}</p>` : ""}
      </header>
      <div class="doc__body">
        ${bodyHtml}
      </div>
    </article>
  </main>
  <footer class="site-footer">
    <div class="site-footer__inner">
      <p class="site-footer__brand">Grasp</p>
      <nav class="site-footer__nav" aria-label="${escapeHtml(t.footerAria)}">
        <a href="${quickStartHref}">${escapeHtml(t.quickStart)}</a>
        <a href="${configHref}">${escapeHtml(t.configuration)}</a>
        <a href="${gatewayHref}">${escapeHtml(t.gateway)}</a>
        <a href="https://github.com/cocofhu/approving" rel="noopener noreferrer" target="_blank">${escapeHtml(t.source)}</a>
      </nav>
      <p class="site-footer__meta">MIT · <a href="https://github.com/cocofhu/approving" rel="noopener noreferrer" target="_blank">cocofhu/approving</a></p>
    </div>
  </footer>
</body>
</html>
`;
}

function escapeHtml(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

/**
 * Split an optional leading YAML block wrapped in ---.
 * Later --- lines stay in the body. A leading UTF-8 BOM is removed.
 * Invalid YAML throws so the build exits non-zero.
 * @returns {{ data: Record<string, unknown>, content: string }}
 */
function parseFrontMatter(raw) {
  let text = raw;
  if (text.charCodeAt(0) === 0xfeff) text = text.slice(1);

  const open = "---";
  if (!text.startsWith(open) || text.charAt(open.length) === "-") {
    return { data: {}, content: text };
  }

  const rest = text.slice(open.length);
  const close = "\n---";
  const closeIndex = rest.indexOf(close);
  if (closeIndex === -1) {
    return { data: {}, content: text };
  }

  const matter = rest.slice(0, closeIndex);
  let content = rest.slice(closeIndex + close.length);
  if (content.startsWith("\r")) content = content.slice(1);
  if (content.startsWith("\n")) content = content.slice(1);

  const block = matter.replace(/^\s*#[^\n]+/gm, "").trim();
  if (block === "") return { data: {}, content };

  const parsed = yaml.load(matter);
  const data =
    parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
      ? parsed
      : {};
  return { data, content };
}

async function walkMarkdown(dir) {
  const out = [];
  async function walk(current) {
    const entries = await fs.readdir(current, { withFileTypes: true });
    for (const entry of entries) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) await walk(full);
      else if (entry.name.endsWith(".md")) out.push(full);
    }
  }
  await walk(dir);
  return out;
}

async function buildMarkdown() {
  const files = await walkMarkdown(contentDir);
  for (const file of files) {
    const raw = await fs.readFile(file, "utf8");
    const rel = path.relative(contentDir, file).replace(/\\/g, "/");
    let data;
    let content;
    try {
      ({ data, content } = parseFrontMatter(raw));
    } catch (err) {
      throw new Error(`failed to parse front matter in ${rel}: ${err.message}`);
    }
    const slug = rel.replace(/\.md$/i, "");
    const outFile = path.join(outDir, slug, "index.html");
    const bodyHtml = md.render(content);
    const html = docTemplate({
      title: data.title || slug,
      description: data.description || "",
      bodyHtml,
      relPath: slug,
    });
    await ensureDir(path.dirname(outFile));
    await fs.writeFile(outFile, html, "utf8");
    console.log(`  md  ${slug}/`);
  }
}

async function main() {
  console.log(`Building Grasp site (BASE_PATH=${basePath || "/"})`);
  await rmrf(outDir);
  await ensureDir(outDir);
  await copyDir(siteDir, outDir);
  await buildMarkdown();
  console.log(`Done → ${path.relative(root, outDir)}/`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
