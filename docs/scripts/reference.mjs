import fs from "node:fs/promises";
import path from "node:path";
import yaml from "js-yaml";

const gateway = [
  { id: "common", source: "## Gateway 公共配置", en: "Gateway common settings", zh: "网关公共设置" },
  { id: "docker", source: "## Docker 配置", en: "Docker settings", zh: "Docker 设置" },
  { id: "kubernetes", source: "## Kubernetes 配置", en: "Kubernetes settings", zh: "Kubernetes 设置" },
  { id: "e2b", source: "### E2B 字段与默认值", en: "E2B settings", zh: "E2B 设置" },
  { id: "runtime", source: "## 沙箱内部配置", en: "Sandbox runtime variables", zh: "沙箱运行时变量" },
];

const englishDefaults = new Map([
  ["空", "Unset"],
  ["空 / `0`（连接时用 `3306`）", "Unset / `0` (connects on `3306`)"],
  ["见各后端示例", "See the backend example"],
  ["必填，无默认", "Required, no default"],
  ["必填", "Required"],
  ["必填，绝对路径且不能为 `/`", "Required absolute path other than `/`"],
  ["必填，无默认模板", "Required, no built-in template"],
  ["单模板自动选择，多模板必填", "Automatic for one template, required for multiple"],
  ["无，启动必填", "Required to boot, no default"],
  ["按 `0` 处理，启动内层 Docker", "`0`, start the inner Docker daemon"],
]);

function tableAfter(text, heading) {
  const lines = text.split(/\r?\n/);
  const start = lines.indexOf(heading);
  if (start < 0) throw new Error("Missing configuration section " + heading);
  const section = lines.slice(start + 1);
  const end = section.findIndex(line => line.startsWith("#"));
  const table = section.slice(0, end < 0 ? section.length : end).findIndex(line => line.startsWith("|"));
  if (table < 0) throw new Error("Missing configuration table after " + heading);
  const rows = [];
  for (const line of section.slice(table)) {
    if (!line.startsWith("|")) break;
    rows.push(line);
  }
  return rows;
}

function translateTable(lines, descriptions, section) {
  const columns = section === "runtime" ? ["Runtime variable", "Standalone default", "Source and effect"] : ["YAML key", "Environment variable", "Default", "Effect"];
  const rows = lines.slice(2).map(line => {
    const cells = line.slice(1, -1).split("|").map(value => value.trim());
    const key = (cells[0] === "—" ? cells[1] : cells[0]).replaceAll("`", "");
    if (cells.length !== columns.length || !Object.hasOwn(descriptions, key)) {
      throw new Error("Update the English configuration reference for " + section + "." + key);
    }
    const defaultIndex = cells.length - 2;
    cells[defaultIndex] = englishDefaults.get(cells[defaultIndex]) ?? cells[defaultIndex];
    if (/\p{Script=Han}/u.test(cells[defaultIndex])) throw new Error("Translate the default for " + key);
    cells[cells.length - 1] = descriptions[key];
    return " | " + cells.join(" | ") + " |";
  });
  return ["| " + columns.join(" | ") + " |", "| " + columns.map(() => "---").join(" | ") + " |", ...rows].join("\n");
}

export async function appendReference(root, page) {
  if (page.reference === "server") {
    const source = await fs.readFile(path.join(root, "../server/CONFIGURATION.md"), "utf8");
    // Use the generated table for authoritative option names, types and defaults.
    const table = source.indexOf("| Environment variable |");
    if (table < 0) throw new Error("Missing server configuration table");
    const body = source.slice(table);
    if (page.locale === "en") return body;

    const descriptions = yaml.load(await fs.readFile(path.join(root, "reference/server.zh-CN.yml"), "utf8"));
    const lines = body.trimEnd().split(/\r?\n/);
    const rows = lines.slice(2).map(line => {
      const cells = line.slice(1, -1).split("|").map(value => value.trim());
      const key = cells[0].replaceAll("`", "");
      if (cells.length !== 6 || !Object.hasOwn(descriptions, key)) {
        throw new Error("Update the Chinese configuration reference for " + key);
      }
      if (cells[3] === "`Not set`") cells[3] = "未设置";
      cells[4] = cells[4] === "Sensitive" ? "敏感" : "公开";
      cells[5] = descriptions[key];
      return "| " + cells.join(" | ") + " |";
    });
    return ["| 环境变量 | YAML 路径 | 类型 | 默认值 | 敏感性 | 用途 |", lines[1], ...rows].join("\n");
  }
  if (page.reference !== "gateway") return "";

  const source = await fs.readFile(path.join(root, "../CONFIGURATION.md"), "utf8");
  const english = page.locale === "en";
  const translations = english ? yaml.load(await fs.readFile(path.join(root, "reference/gateway.en.yml"), "utf8")) : null;
  return gateway.map(section => {
    const rows = tableAfter(source, section.source);
    const table = english ? translateTable(rows, translations[section.id], section.id) : rows.join("\n");
    return "## " + (english ? section.en : section.zh) + " {#" + section.id + "}\n\n" + table;
  }).join("\n\n");
}
