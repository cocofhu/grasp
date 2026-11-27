import { escape, layout, localized, repository } from "./layout.mjs";

const copy = {
  "zh-CN": {
    title: "Grasp · 让 Agent 把工作交付出来",
    description: "用工作流串起需求、代码、测试和人工确认。打开 Solo，在同一个沙箱里继续探索、修改和验证。",
    headline: ["让 Agent 执行。", "让交付有迹可循。"],
    primary: "开始使用",
    secondary: "看工作流演示",
    demo: {
      name: "工作流示例",
      task: "为项目增加搜索",
      stages: [
        { name: "澄清", status: "方案待确认", title: "把需求变成计划", detail: "明确搜索范围和验收条件，再让人确认实现方案。", output: "实现方案", value: "plan.md", proof: ["按标题和摘要搜索项目。", "空关键词保留原列表，未匹配时显示空状态。", "完成后运行搜索和列表的回归测试。"] },
        { name: "执行", status: "测试通过", title: "在沙箱中执行验证", detail: "Agent 继续使用已确认的方案，修改代码并运行项目测试。", output: "测试记录", value: "test-result.md", proof: ["标题匹配与摘要匹配均通过。", "空关键词时列表保持原样。", "无匹配结果时显示空状态。"] },
        { name: "审查", status: "等待人工确认", title: "看清结果，再交付", detail: "在待办中检查改动、测试结果和预览，确认后进入交付节点。", output: "人工门禁", value: "查看确认项", proof: ["对照方案核对搜索范围。", "打开预览验证空结果状态。", "查看测试记录后决定交付或退回。"] },
      ],
      position: "演示进度",
      evidenceTitle: "示例产物",
      close: "关闭",
      play: "播放演示",
      pause: "暂停演示",
      replay: "重新播放",
    },
    use: "两种方式，接入同一套 Agent。",
    modes: [
      { title: "工作流", label: "有固定交付步骤时", detail: "把澄清、实现、测试和审批编排到画布上。每次运行都能查看节点、对话和产物。", link: "了解工作流", route: "/guide/workflows/" },
      { title: "Solo", label: "需要持续探索时", detail: "在一个会话里与 Agent 对话、改代码和查看预览。需要亲手调试时，打开终端或 IDE 接手。", link: "了解 Solo", route: "/guide/solo/" },
    ],
    canvasTitle: "每一个决定，都留在运行记录里。",
    canvasText: "从运行记录打开方案、测试结果和产物。把证据交给下一步，或在需要时让工作流停在人工门禁。",
    canvasLink: "看工作流如何运行",
    canvasAlt: "Grasp 工作流画布。画布中连接需求澄清、实现和测试评审节点。",
    canvasCaption: "Grasp 工作流画布示例",
    deployTitle: "部署在你掌控的环境。",
    deployText: "用 Docker 启动本地演示。要接入现有基础设施，可以配置 Docker、Kubernetes 或 E2B 网关，再为 Agent 和仓库配置各自的授权。",
    deployLink: "自托管指南",
    helpTitle: "找到下一步的操作。",
    helpText: "从首次运行到逐项配置，按任务浏览，也可以直接搜索字段名或错误信息。",
    helpLink: "打开帮助中心",
  },
  en: {
    title: "Grasp · Put agents to work on your terms",
    description: "Connect requirements, code, tests, and human review in a workflow. Open a Solo session to explore and make changes in one sandbox.",
    headline: ["Put agents to work.", "See every step to ship."],
    primary: "Get started",
    secondary: "Try the workflow demo",
    demo: {
      name: "Example workflow",
      task: "Add search to a project",
      stages: [
        { name: "Clarify", status: "Plan ready for review", title: "Turn a request into a plan", detail: "Set the search scope and acceptance criteria, then review the plan before implementation.", output: "Implementation plan", value: "plan.md", proof: ["Search project titles and descriptions.", "Keep the existing list on an empty query and show an empty state on no match.", "Run regression tests for search and the project list."] },
        { name: "Run", status: "Tests passed", title: "Make and test the change", detail: "The agent follows the approved plan, updates the code, and runs the project tests in a sandbox.", output: "Test report", value: "test-result.md", proof: ["Title and description matching passed.", "The existing list stays visible with an empty query.", "Queries without matches render the empty state."] },
        { name: "Review", status: "Waiting for approval", title: "Review the result before shipping", detail: "Open the pending review to inspect the change, test results, and preview before delivery.", output: "Human gate", value: "Review checklist", proof: ["Compare the implemented scope with the plan.", "Open the preview and verify the empty state.", "Read the test report and decide whether the change is ready."] },
      ],
      position: "Demo timeline",
      evidenceTitle: "Example artifact",
      close: "Close",
      play: "Play demo",
      pause: "Pause demo",
      replay: "Replay demo",
    },
    use: "One set of agents. Two ways to work.",
    modes: [
      { title: "Workflows", label: "For repeatable delivery", detail: "Connect clarification, implementation, tests, and approvals on a canvas. Follow the nodes, conversations, and artifacts in every run.", link: "Learn about workflows", route: "/guide/workflows/" },
      { title: "Solo", label: "For open-ended work", detail: "Talk to an agent, edit code, and preview changes in one session. Open a terminal or IDE whenever you want to take over.", link: "Learn about Solo", route: "/guide/solo/" },
    ],
    canvasTitle: "Every decision belongs to the run.",
    canvasText: "Open plans, test results, and artifacts from the run. Pass the evidence to the next step, or pause at a gate for a human decision.",
    canvasLink: "See how workflows run",
    canvasAlt: "Grasp workflow canvas connecting clarification, implementation, and test review nodes.",
    canvasCaption: "Example Grasp workflow canvas",
    deployTitle: "Run Grasp on your infrastructure.",
    deployText: "Start a local demo with Docker. Connect Docker, Kubernetes, or E2B for your deployment, then configure the credentials each agent and repository needs.",
    deployLink: "Self-hosting guide",
    helpTitle: "Find the instructions you need.",
    helpText: "Browse by task, look up a configuration key, or search for an error.",
    helpLink: "Open the help center",
  },
};

function workflowDemo(c, url) {
  const { demo } = c;
  return `<section class="demo" data-demo data-step="0" data-play="${escape(demo.play)}" data-pause="${escape(demo.pause)}" data-replay="${escape(demo.replay)}" aria-label="${escape(demo.name)}">
    <div class="demo__top">
      <div class="demo__caption"><span>${escape(demo.name)}</span><strong>${escape(demo.task)}</strong></div>
      <div class="demo__graph" role="tablist" aria-label="${escape(demo.name)}">
        ${demo.stages.map((stage, index) => `<button type="button" class="demo__step" role="tab" id="demo-tab-${index}" aria-controls="demo-panel-${index}" aria-selected="${index === 0}" tabindex="${index === 0 ? 0 : -1}" disabled>
          ${escape(stage.name)}
        </button>`).join("")}
      </div>
      <button class="demo__play" type="button" data-demo-controls data-play-toggle hidden><span class="play-icon" aria-hidden="true"></span><span data-play-label>${escape(demo.play)}</span></button>
    </div>
    <div class="demo__body">
      <figure class="demo__visual">
        <div class="demo__camera" data-camera>
          <img src="${url("/assets/home-workflow.webp")}" alt="${escape(c.canvasAlt)}" width="1600" height="900" fetchpriority="high">
          <span class="demo__focus" aria-hidden="true"></span>
        </div>
        <figcaption>${escape(c.canvasCaption)}</figcaption>
      </figure>
      <div class="demo__panels">
        ${demo.stages.map((stage, index) => `<div class="demo__panel" role="tabpanel" id="demo-panel-${index}" aria-labelledby="demo-tab-${index}" tabindex="0"${index ? " hidden" : ""}>
          <span class="demo__status"><span aria-hidden="true"></span>${escape(stage.status)}</span>
          <h2>${escape(stage.title)}</h2>
          <p>${escape(stage.detail)}</p>
          <dl class="demo__artifact"><dt>${escape(stage.output)}</dt><dd><button type="button" data-evidence-open disabled>${escape(stage.value)}</button></dd></dl>
          <template data-evidence-template><ul>${stage.proof.map(line => `<li>${escape(line)}</li>`).join("")}</ul></template>
        </div>`).join("")}
        <span class="sr-only" data-demo-announcement aria-live="polite"></span>
      </div>
    </div>
    <div class="demo__timeline" data-demo-controls hidden>
      <label class="sr-only" for="demo-progress">${escape(demo.position)}</label>
      <div class="demo__scrub">
        <input id="demo-progress" type="range" min="0" max="1000" step="1" value="0">
        <b aria-hidden="true"></b><b aria-hidden="true"></b>
      </div>
      <span class="demo__time" data-demo-time aria-hidden="true">0:00 / 0:09</span>
    </div>
    <dialog class="evidence-dialog" aria-labelledby="demo-evidence-title">
      <div class="evidence-dialog__header">
        <div><p>${escape(demo.evidenceTitle)}</p><h2 id="demo-evidence-title"></h2></div>
        <form method="dialog"><button class="key-button" type="submit" aria-label="${escape(demo.close)}">${escape(demo.close)}</button></form>
      </div>
      <div class="evidence-dialog__body" data-evidence-body></div>
    </dialog>
  </section>`;
}

function modeCard(mode, index, href) {
  return `<article class="mode mode--${index}"${index ? ' id="solo"' : ""} data-reveal>
    <div class="mode__copy">
      <p class="mode__label">${escape(mode.label)}</p>
      <h3>${escape(mode.title)}</h3>
      <p>${escape(mode.detail)}</p>
      <a class="text-link" href="${href(mode.route)}">${escape(mode.link)}</a>
    </div>
  </article>`;
}

export function renderHome(locale, url) {
  const c = copy[locale];
  const href = route => url(localized(locale, route));
  const main = `<main id="main">
    <section class="hero">
      <div class="hero__copy home-wrap">
        <h1><span>${escape(c.headline[0])}</span><span>${escape(c.headline[1])}</span></h1>
        <div class="hero__aside">
          <p>${escape(c.description)}</p>
          <div class="hero__actions">
            <a class="button button--solid" href="${href("/guide/quick-start/")}">${escape(c.primary)}</a>
            <a class="button button--bare" href="#workflow">${escape(c.secondary)}</a>
          </div>
        </div>
      </div>
    </section>
    <div class="hero__demo" id="workflow" data-story>
      <div class="story__frame home-wrap" data-story-frame>${workflowDemo(c, url)}</div>
    </div>
    <section class="home-section home-wrap">
      <h2 class="home-heading" data-reveal>${escape(c.use)}</h2>
      <div class="mode-grid">${c.modes.map((mode, index) => modeCard(mode, index, href)).join("")}</div>
    </section>
    <section class="review-section">
      <div class="home-wrap review-grid">
        <h2 class="home-heading" data-reveal>${escape(c.canvasTitle)}</h2>
        <div data-reveal>
          <p>${escape(c.canvasText)}</p>
          <a class="text-link" href="${href("/guide/workflows/#review")}">${escape(c.canvasLink)}</a>
        </div>
      </div>
    </section>
    <section class="home-section home-wrap deploy-grid" id="deploy">
      <div data-reveal>
        <h2 class="home-heading">${escape(c.deployTitle)}</h2>
        <p>${escape(c.deployText)}</p>
        <a class="text-link" href="${href("/help/configuration/")}">${escape(c.deployLink)}</a>
      </div>
      <div class="terminal" data-reveal>
        <div class="terminal__bar"><span aria-hidden="true"></span>Terminal</div>
        <pre><code>git clone ${repository}.git
cd grasp
./start.sh -d</code></pre>
      </div>
    </section>
    <section class="home-wrap home-section help-callout" data-reveal>
      <div>
        <h2 class="home-heading">${escape(c.helpTitle)}</h2>
        <p>${escape(c.helpText)}</p>
        <a class="button button--solid" href="${href("/help/")}">${escape(c.helpLink)}</a>
      </div>
      <img alt="" src="${url("/assets/logo-mark.svg")}" width="144" height="144" loading="lazy">
    </section>
  </main>`;
  return layout({ locale, route: localized(locale, "/"), kind: "home", title: c.title, description: c.description }, url, main);
}
