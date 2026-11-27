#!/usr/bin/env node
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import yaml from "js-yaml";
import MarkdownIt from "markdown-it";
import { localized, localizedPair, renderDocumentation, strings } from "./layout.mjs";
import { renderHome } from "./home.mjs";
import { appendReference } from "./reference.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const out = path.join(root, "public");
const content = path.join(root, "content");
const requestedBase = process.env.BASE_PATH || "/";
const base = requestedBase === "/approving-pages" && process.env.FORCE_PROJECT_PAGES !== "1"
  ? ""
  : requestedBase.replace(/^\/?/, "/").replace(/\/+$/, "");
if (base && !/^\/[\w/-]+$/.test(base)) throw new Error("Invalid BASE_PATH");
const siteUrl = route => base + route;
const md = new MarkdownIt({ html: false, linkify: true, typographer: true });
const languages = ["zh-CN", "en"];
md.renderer.rules.table_open = (tokens, i, options, env, renderer) =>
  '<div class="table-scroll" tabindex="0">' + renderer.renderToken(tokens, i, options);
md.renderer.rules.table_close = () => "</table></div>\n";

async function walk(dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  const groups = await Promise.all(entries.map(entry => {
    const full = path.join(dir, entry.name);
    return entry.isDirectory() ? walk(full) : [full];
  }));
  return groups.flat().sort();
}

async function loadPages() {
  const files = (await walk(content)).filter(file => file.endsWith(".md"));
  return Promise.all(files.map(async file => {
    const text = (await fs.readFile(file, "utf8")).replace(/^\uFEFF/, "");
    const match = text.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
    if (!match) throw new Error(`Missing front matter in ${file}`);
    const meta = yaml.load(match[1]);
    const slug = path.relative(content, file).split(path.sep).join("/").replace(/\.md$/, "");
    const route = `/${slug}/`;
    const locale = slug.startsWith("en/") ? "en" : "zh-CN";
    if (!meta?.title || !meta.description || (meta.kind !== "hub" && !Object.hasOwn(strings[locale].docsNav, meta.section))) {
      throw new Error(`Set title, description and a valid section in ${file}`);
    }
    return {
      ...meta, file, route, locale, navTitle: meta.navTitle || meta.title,
      order: meta.order ?? 0,
      source: `docs/content/${slug}.md`,
      body: text.slice(match[0].length),
    };
  }));
}

function textOf(token) {
  return token.children?.map(child => {
    if (child.type === "text" || child.type === "code_inline") return child.content;
    if (child.type === "softbreak" || child.type === "hardbreak") return " ";
    return "";
  }).join("").trim() || "";
}

function anchorFor(title) {
  return title.normalize("NFKC").toLowerCase().replace(/[^\p{L}\p{N}\p{M}\s-]/gu, "")
    .trim().replace(/\s+/g, "-") || "section";
}

function rewriteLink(href, page, pagesByFile) {
  if (!href || href.startsWith("//") || /^[a-z][a-z\d+.-]*:/i.test(href)) return href;
  const [filename, fragment] = href.split("#", 2);
  if (filename.endsWith(".md") && !filename.startsWith("/")) {
    const other = pagesByFile.get(path.resolve(path.dirname(page.file), decodeURIComponent(filename)));
    if (!other) throw new Error(`Unknown Markdown link ${href} in ${page.source}`);
    return `${siteUrl(other.route)}${fragment ? "#" + fragment : ""}`;
  }
  return href.startsWith("/") ? siteUrl(href) : href;
}

async function renderMarkdown(page, pagesByFile) {
  const body = page.body + "\n\n" + await appendReference(root, page);
  const env = {};
  const tokens = md.parse(body, env);
  const seen = new Set();
  const toc = [];
  let group = "";
  const search = [{ title: page.title, id: "", text: page.description }];
  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i];
    const inline = tokens[i + 1];
    if (token.type === "heading_open" && inline?.type === "inline") {
      const last = inline.children?.at(-1);
      const marker = last?.type === "text" && last.content.match(/\s+\{#([a-z][\w-]*)\}$/);
      if (marker) last.content = last.content.slice(0, marker.index);
      const level = Number(token.tag.slice(1));
      const title = textOf(inline);
      const candidate = marker ? marker[1] : anchorFor(title);
      let id = candidate;
      for (let n = 2; seen.has(id); n++) {
        if (marker) throw new Error(`Duplicate anchor #${id} in ${page.source}`);
        id = `${candidate}-${n}`;
      }
      seen.add(id);
      group = id;
      token.attrSet("id", id);
      if (level === 2 || level === 3) toc.push({ level, title, id });
      search.push({ title, id, text: "" });
      continue;
    }
    if (page.reference && token.type === "tr_open" && inline?.type === "td_open") {
      const first = textOf(tokens[i + 2]);
      const field = first === "—" ? textOf(tokens[i + 5]) : first;
      const backend = ["docker", "kubernetes", "e2b"].includes(group) ? group + "." : "";
      const title = field.split(" / ").map(key => backend + key).join(" / ");
      const slug = field.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
      const id = "option-" + (group ? group + "-" : "") + slug;
      if (title && !seen.has(id)) {
        token.attrSet("id", id);
        seen.add(id);
        search.push({ title, id, text: "" });
      }
    }
    if (token.type !== "inline") continue;
    for (const child of token.children || []) {
      if (child.type === "link_open") child.attrSet("href", rewriteLink(child.attrGet("href"), page, pagesByFile));
      if (child.type === "image") child.attrSet("src", rewriteLink(child.attrGet("src"), page, pagesByFile));
    }
    search.at(-1).text += " " + textOf(token);
  }

  return {
    toc,
    search,
    html: md.renderer.render(tokens, md.options, env),
  };
}

async function write(route, body) {
  const filename = path.join(out, route.endsWith("/") ? route + "index.html" : route);
  await fs.mkdir(path.dirname(filename), { recursive: true });
  await fs.writeFile(filename, body);
}

function indexEntries(pages, locale) {
  return pages.filter(page => page.locale === locale).flatMap(page => page.search.map(section => ({
    title: section.title,
    page: page.title,
    url: siteUrl(page.route) + (section.id ? "#" + encodeURIComponent(section.id) : ""),
    text: section.text.trim(),
    keywords: page.keywords?.join(" ") ?? "",
  })));
}

function htmlAttribute(raw) {
  return raw.replaceAll("&amp;", "&").replaceAll("&quot;", '"').replaceAll("&#39;", "'");
}

async function validateOutput() {
  const files = (await walk(out)).map(file => "/" + path.relative(out, file).split(path.sep).join("/"));
  const outputFiles = new Set(files);
  const pages = new Map(await Promise.all(files.filter(file => file.endsWith(".html")).map(async file => {
    const html = await fs.readFile(path.join(out, file), "utf8");
    const ids = new Set([...html.matchAll(/\bid="([^"]+)"/g)].map(match => htmlAttribute(match[1])));
    return [file, { html, ids }];
  })));

  const errors = [];
  for (const [file, page] of pages) {
    for (const match of page.html.matchAll(/\b(?:href|src)="([^"]+)"/g)) {
      const href = htmlAttribute(match[1]);
      if (href.startsWith("//") || /^[a-z][a-z\d+.-]*:/i.test(href)) continue;
      const url = new URL(href, "https://grasp.invalid" + siteUrl(file));
      const local = decodeURIComponent(url.pathname);
      let target = local.slice(base.length);
      if (target.endsWith("/")) target += "index.html";
      else if (!outputFiles.has(target) && outputFiles.has(target + "/index.html")) target += "/index.html";
      if ((base && !local.startsWith(base + "/")) || !outputFiles.has(target)) {
        errors.push(`${file} → ${href} (missing file)`);
      } else if (url.hash && pages.has(target) && !pages.get(target).ids.has(decodeURIComponent(url.hash.slice(1)))) {
        errors.push(`${file} → ${href} (missing anchor)`);
      }
    }
  }
  if (errors.length) throw new Error("Broken site links:\n" + errors.join("\n"));
  return pages.size;
}

async function main() {
  const pages = await loadPages();
  const byRoute = new Map(pages.map(page => [page.route, page]));
  const byFile = new Map(pages.map(page => [page.file, page]));
  for (const page of pages) {
    for (const route of Object.values(localizedPair(page.route, page.locale))) {
      if (!byRoute.has(route)) throw new Error(`Missing translation for ${page.route}: ${route}`);
    }
    Object.assign(page, await renderMarkdown(page, byFile));
  }
  await fs.rm(out, { recursive: true, force: true });
  await fs.cp(path.join(root, "site"), out, { recursive: true });
  await Promise.all([
    ...pages.map(page => write(page.route, renderDocumentation(page, pages, siteUrl))),
    ...languages.flatMap(locale => [
      write(localized(locale, "/"), renderHome(locale, siteUrl)),
      write(localized(locale, "/help/search-index.json"), JSON.stringify(indexEntries(pages, locale))),
    ]),
  ]);
  const count = await validateOutput();
  console.log(`Built and checked ${count} pages in public/ (BASE_PATH=${base || "/"})`);
}

main().catch(error => {
  console.error(error.message);
  process.exitCode = 1;
});
