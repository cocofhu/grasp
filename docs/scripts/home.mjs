import { escape, layout, localized, repository } from "./layout.mjs";
import { gatewayTraces, graphicDefs, glyph, sshDrawing } from "./home-graphics.mjs";
import { runtimeArt } from "./runtime-art.mjs";

const providers = ["e2b", "kubernetes", "docker", "cloud"];
// Agent Studio accepts these eight backends (server/internal/runtime/acp_backend.go).
export const codingTools = [
  ["codex", "Codex"], ["claude", "Claude Code"], ["cursor", "Cursor"], ["opencode", "OpenCode"],
  ["codebuddy", "CodeBuddy"], ["trae", "Trae"], ["deepseek", "DeepSeek Harness"], ["kimi", "Kimi Code"],
];
// Additional providers registered by the standalone in-sandbox bridge.
const runtimeAdapters = ["Antigravity", "DevEco", "Gemini CLI", "GitHub Copilot CLI", "Grok", "Hermes", "Kiro", "OpenClaw", "Pi", "Qoder"];
const connections = [
  ["e2b-v1", "E2B_GATEWAY_KEY"], ["k8s-v1", "K8S_GATEWAY_KEY"],
  ["docker-v1", "DOCKER_GATEWAY_KEY"], ["cloud-v1", "CLOUD_GATEWAY_KEY"],
];
const storyModes = ["solo", "preview", "ssh"];
const copy = {
  "zh-CN": {
    title: "Grasp · 自托管的云端 Coding Agent 基础设施",
    description: "自托管 Grasp，把 Coding Agent 连接到 E2B、Kubernetes、Docker 或通过兼容网关接入的公有云沙箱。",
    headline: ["让 Coding Agent", "在自己的云上运行。"],
    intro: "自托管 Grasp，让 Agent 写代码、跑测试，你随时接手。",
    deploy: "部署 Grasp", explore: "看开发现场", deckTitle: "选择工作区的运行环境", inspectConfig: "接入方式",
    tools: "可用编程工具", toolGuide: "配置工具", moreTools: "另有 10 种适配",
    adapterNote: "独立沙箱运行时需要对应的 CLI 和凭据。", adapterGuide: "接入说明",
    runtimeSummaries: ["模板启动，快照恢复。", "集群中的 Pod 与持久卷。", "在 Linux 主机上运行容器。", "通过兼容网关接入。"],
    backends: [["E2B", "E2B 部署指南"], ["Kubernetes", "Kubernetes 部署指南"], ["Docker", "Docker 部署指南"], ["公有云沙箱", "公有云接入契约"]],
    access: "云端开发工作区", overview: "全图", inspect: "聚焦界面细节",
    scenes: [
      ["Solo", "交给 Agent。", "从一条请求开始。", "查看 Solo 界面"],
      ["应用预览", "指给 Agent 看。", "在运行的页面上标注要改的位置。", "预览指南"],
      ["本地 IDE", "用自己的 IDE 接手。", "通过 SSH 继续编辑同一份代码。", "连接指南"],
    ],
    request: "给设置页加上深色模式。", seeResult: "查看这次请求的页面预览",
    soloAlt: "Grasp Solo 界面中的诊断结果和修复选项。", previewAlt: "Grasp 预览中的外观设置和页面标注工具。",
    focus: [["设置页", "检查 Agent 交付的主题设置。"], ["标注", "选中页面上的位置，发回给 Agent。"]],
    ssh: ["本地 IDE", "Grasp SSH 网关", "云端工作区"], close: "关闭大图",
    integrations: "连接你的基础设施。", bridge: "命名网关", control: "控制面", choose: "选择运行环境",
    config: "配置示例", boot: "启动服务", register: "网关配置", setupTabs: "部署示例",
    switchRuntime: "运行环境", changeRuntime: "更换运行环境",
    setup: "适用于已安装 Docker 和 Compose 的 Linux 主机。", envNote: "替换网关地址，并在 Grasp 服务环境中设置对应密钥。",
    lookup: "帮助与配置", lookupHint: "搜索配置或功能", handbook: "帮助中心",
  },
  en: {
    title: "Grasp · Self-hosted infrastructure for cloud coding agents",
    description: "Run coding agents on your own control plane with E2B, Kubernetes, Docker, or public-cloud sandboxes behind a compatible gateway.",
    headline: ["Coding agents,", "on your own cloud."],
    intro: "Self-host Grasp. Let agents write and test code. Step in whenever you like.",
    deploy: "Deploy Grasp", explore: "Explore the workspace", deckTitle: "Choose a workspace runtime", inspectConfig: "Connection details",
    tools: "Coding tools", toolGuide: "Configure tools", moreTools: "10 more adapters",
    adapterNote: "The standalone sandbox runtime needs the matching CLI and credentials.", adapterGuide: "Integration guide",
    runtimeSummaries: ["Start from templates. Resume from snapshots.", "Pods and persistent volumes in your cluster.", "Containers on your own Linux host.", "Connect through a compatible gateway."],
    backends: [["E2B", "E2B deployment guide"], ["Kubernetes", "Kubernetes deployment guide"], ["Docker", "Docker deployment guide"], ["Public cloud", "Public-cloud integration guide"]],
    access: "Cloud development workspace", overview: "Overview", inspect: "Inspect interface details",
    scenes: [
      ["Solo", "Give it to an agent.", "Start with a single request.", "View the Solo interface"],
      ["App preview", "Point to the change.", "Mark the exact spot on the running page.", "Preview guide"],
      ["Local IDE", "Take over in your IDE.", "Connect over SSH to the same working copy.", "Connection guide"],
    ],
    request: "Add a dark mode setting.", seeResult: "See the page preview for this request",
    soloAlt: "Performance findings and proposed fixes in the Grasp Solo interface.", previewAlt: "The Appearance settings and annotation tools in Grasp preview.",
    focus: [["Appearance", "Inspect the theme choices the agent delivered."], ["Annotate", "Pick a spot on the page to send back to the agent."]],
    ssh: ["Local IDE", "Grasp SSH gateway", "Cloud workspace"], close: "Close image",
    integrations: "Your infrastructure, connected.", bridge: "Named gateway", control: "Control plane", choose: "Choose a runtime",
    config: "Configuration examples", boot: "Start Grasp", register: "Gateway settings", setupTabs: "Deployment examples",
    switchRuntime: "Runtime", changeRuntime: "Change runtime",
    setup: "For Linux hosts with Docker and Compose installed.", envNote: "Replace the gateway URL and set its key in the Grasp server environment.",
    lookup: "Help and configuration", lookupHint: "Search settings or features", handbook: "Help center",
  },
};

function runtimeDeck(c, guides) {
  return `<div class="runtime-deck" data-runtime-deck>
    <fieldset class="runtime-tabs"><legend class="sr-only">${c.deckTitle}</legend>
      ${providers.map((id, i) => `<label><input type="radio" name="cover-runtime" value="${id}"${id === "docker" ? " checked" : ""}><span>${c.backends[i][0]}</span></label>`).join("")}
    </fieldset>
    <div class="runtime-sheets" data-sheets>
      ${providers.map((id, i) => `<article class="runtime-sheet" data-sheet="${id}" aria-label="${c.backends[i][0]}">
        <div class="sheet-copy"><h2>${c.backends[i][0]}</h2><p>${c.runtimeSummaries[i]}</p>
          <a class="sheet-link" href="${guides[i]}" data-sheet-config>${c.inspectConfig}<span aria-hidden="true">↗</span></a>
        </div>${runtimeArt(id)}
      </article>`).join("")}
    </div>
  </div>`;
}

function infrastructure(c, url, guides) {
  return `<section class="runtime-section" id="sandbox" aria-labelledby="runtime-title"><div class="home-wrap">
    <figure class="network" data-network>
      <div class="network-heading"><h2 id="runtime-title">${c.integrations}</h2>
        <fieldset class="destinations"><legend class="sr-only">${c.choose}</legend>
          ${providers.map((id, i) => `<label class="destination"><input type="radio" name="provider" value="${id}"${id === "docker" ? " checked" : ""}><span>${c.backends[i][0]}</span></label>`).join("")}
        </fieldset>
      </div>
      <div class="network__plot" data-plot>
        ${gatewayTraces()}${gatewayTraces(true)}
        <div class="network-origin"><img src="${url("/assets/logo-mark.svg")}" width="118" height="118" alt="">
          <strong>Grasp</strong><span>${c.control}</span></div>
        <span class="gate-caption">${c.bridge}</span>
        <div class="network-output" aria-live="polite">${providers.map((id,i) => `<div class="provider-note" data-note="${id}">
          ${runtimeArt(id)}<strong>${c.backends[i][0]}</strong><a href="${guides[i]}">${c.backends[i][1]} <span aria-hidden="true">↗</span></a>
        </div>`).join("")}</div>
      </div>
    </figure>
    ${deployment(c)}
  </div></section>`;
}

// Focus points in the 1600 × 1000 preview capture: center, scale, marker.
const previewFocus = [[.81, .45, 2.15, .961, .263], [.806, .148, 1.95, .947, .101]];
function inspection(c, image) {
  return `<div class="inspection" data-inspection>
    <div class="inspection__view">
      <a href="${image}" class="product-shot" data-enlarge aria-label="${c.previewAlt}">
        <img src="${image}" alt="${c.previewAlt}" width="1600" height="1000" loading="lazy"><span class="enlarge-indicator" aria-hidden="true">${glyph("expand")}</span>
      </a>
      <div class="inspection__markers" data-markers hidden>
        ${previewFocus.map(([, , , pinX, pinY], j) => `<button type="button" data-pin="${j+1}" style="left:${pinX * 100}%;top:${pinY * 100}%" aria-label="${c.focus[j][0]}" title="${c.focus[j][0]}">${glyph("focus")}</button>`).join("")}
      </div>
    </div>
    <div class="inspection__controls" data-inspect-controls hidden role="group" aria-label="${c.inspect}">
      <button type="button" data-pose="0" aria-pressed="true">${c.overview}</button>
      ${previewFocus.map(([x, y, scale], j) => `<button type="button" data-pose="${j+1}" data-x="${x}" data-y="${y}" data-scale="${scale}" data-detail="${escape(c.focus[j][1])}" aria-pressed="false">${c.focus[j][0]}</button>`).join("")}
    </div>
  </div>`;
}

function access(c, href, url) {
  const composer = `<div class="composer-scene"><div class="composer-mark">
    <img src="${url("/assets/logo-mark.svg")}" width="98" height="98" alt="Grasp"><i aria-hidden="true"></i></div>
    <div class="composer-request"><p>${c.request}</p><div><span><img src="${url("/assets/coding-tools/claude.svg")}" width="18" height="18" alt=""> Claude Code</span>
    <a href="#case-preview" aria-label="${c.seeResult}" title="${c.seeResult}">↑</a></div></div></div>`;
  const graphics = [composer, inspection(c, url("/assets/home-preview.webp")), sshDrawing(c.ssh, url("/assets/logo-mark.svg"))];
  const links = [url("/assets/home-solo.webp"), href("/help/gateway/#preview"), href("/help/local-ide/")];
  return `<section class="evidence-band" id="solo" aria-label="${c.access}" data-access><div class="home-wrap access">
    <div class="story-flow" data-story>
      <div class="story-screen" data-screen hidden>
        <div class="story-screen__window" data-window></div>
        <nav class="story-nav" aria-label="${c.access}">${storyModes.map((id, i) =>
          `<a href="#case-${id}">${c.scenes[i][0]}</a>`).join("")}</nav>
      </div>
      <div class="story-chapters">${storyModes.map((id, i) => `<article id="case-${id}" data-interface="${id}" class="story-step" aria-labelledby="title-${id}">
        <header><p class="story-role">${c.scenes[i][0]}</p><h2 id="title-${id}">${c.scenes[i][1]}</h2></header>
        <div class="case-visual" data-visual="${id}">${graphics[i]}</div>
        <div class="case-detail">${i ? `<p>${c.scenes[i][2]}</p>` : ""}<a class="text-link" href="${links[i]}"${i ? "" : ` data-enlarge data-image-alt="${c.soloAlt}" aria-label="${c.scenes[i][3]}"`}>${c.scenes[i][3]}</a></div>
      </article>`).join("")}</div>
    </div>
    <dialog class="product-dialog" data-image-dialog aria-label="${c.access}"><button type="button" data-close-image>${c.close}</button><img alt="" width="1600" height="1000"></dialog>
  </div></section>`;
}

function configurationSample(i) {
  const [id, key] = connections[i];
  return `<pre><code><span class="code-key">sandbox:</span>
  <span class="code-key">default_gateway:</span> ${id}
  <span class="code-key">gateways:</span>
    - <span class="code-key">id:</span> ${id}
      <span class="code-key">url:</span> https://gateway.example.test
      <span class="code-key">api_key_env:</span> ${key}
      <span class="code-key">transport:</span> gateway</code></pre>`;
}

function deployment(c) {
  return `<details class="deploy-section" id="deploy">
    <summary>${c.config}<span aria-hidden="true"></span></summary>
    <div class="deployment">
      <fieldset class="deployment__tabs"><legend class="sr-only">${c.setupTabs}</legend>
        <label><input type="radio" name="deploy-example" value="gateway" checked>${c.register}</label>
        <label><input type="radio" name="deploy-example" value="start">${c.boot}</label>
      </fieldset>
      <div class="terminal" data-snippet="start"><div class="terminal__bar">Terminal</div><pre><code><span class="code-key">git clone</span> ${repository}.git
<span class="code-key">cd</span> grasp
./start.sh -d</code></pre><p class="deployment__note">${c.setup}</p></div>
      <div class="terminal" data-snippet="gateway">
        <div class="config-picker">
          <label data-config-picker hidden>${c.switchRuntime}<select name="deploy-provider">${providers.map((id, i) => `<option value="${id}">${c.backends[i][0]}</option>`).join("")}</select></label>
          <a data-config-jump href="#sandbox">${c.changeRuntime}</a>
        </div>
        ${providers.map((id, i) => `<div data-example="${id}" class="provider-example"><div class="terminal__bar">config.yaml</div>${configurationSample(i)}</div>`).join("")}
        <p class="deployment__note">${c.envNote}</p>
      </div>
    </div>
  </details>`;
}

function helpLookup(c, href) {
  return `<section class="reference-band" id="reference"><div class="home-wrap lookup">
    <h2>${c.lookup}</h2><div><button type="button" class="search-trigger lookup__search" data-search-open hidden>
      <span class="search-icon" aria-hidden="true"></span>${c.lookupHint}<kbd aria-hidden="true">/</kbd>
    </button><a class="text-link" href="${href("/help/")}">${c.handbook} ↗</a></div>
  </div></section>`;
}

export function renderHome(locale, url) {
  const c = copy[locale];
  const href = route => url(localized(locale, route));
  const gatewayGuides = ["e2b", "k8s", "docker"].map(id => `${repository}/blob/main/sandbox-gateway/deploy/${id}/README.md`);
  gatewayGuides.push(href("/help/gateway/#cloud"));
  const main = `<main id="main" class="home-main">${graphicDefs()}
    <section class="hero" aria-labelledby="hero-title"><div class="home-wrap">
      <header class="hero-intro">
        <h1 id="hero-title">${c.headline.map(line => `<span>${escape(line)}</span>`).join(" ")}</h1>
        <p>${escape(c.intro)}</p>
        <div class="hero-actions"><a class="button hero-deploy" href="${href("/guide/quick-start/")}">${c.deploy}<span aria-hidden="true">↗</span></a>
          <a class="hero-architecture" href="#solo">${c.explore}<span aria-hidden="true">↓</span></a></div>
      </header>
      ${runtimeDeck(c, gatewayGuides)}
    </div></section>
    <section class="tool-rail" id="coding-tools" aria-labelledby="tools-title"><div class="home-wrap tool-rail__inner">
      <h2 id="tools-title">${c.tools}</h2>
      <ul class="tool-logos">${codingTools.map(([file, name]) => `<li><img${file === "kimi" ? ' class="color-mark"' : ""} src="${url("/assets/coding-tools/" + file + ".svg")}" width="25" height="25" alt="">${name}</li>`).join("")}</ul>
      <div class="tool-links"><a href="${href("/help/agents-models/#model-groups")}">${c.toolGuide}</a><details class="tool-adapters"><summary>${c.moreTools}</summary><p>${c.adapterNote}</p><ul>${runtimeAdapters.map(name => `<li>${name}</li>`).join("")}</ul><a href="${repository}/blob/main/sandbox-gateway/sandbox/docs/BACKEND.md">${c.adapterGuide}</a></details></div>
    </div></section>
    ${access(c, href, url)}
    ${infrastructure(c, url, gatewayGuides)}
    ${helpLookup(c, href)}
  </main>`;
  return layout({ locale, route: localized(locale, "/"), kind: "home", title: c.title, description: c.description }, url, main);
}
