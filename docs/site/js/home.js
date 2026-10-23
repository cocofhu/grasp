(() => {
  const hero = document.querySelector(".hero");
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const pageLang = (document.documentElement.lang || "zh-CN").toLowerCase();
  const isEn = pageLang === "en" || pageLang.startsWith("en-");

  const COPY = isEn
    ? {
        heroTitles: ["Self-improve · Orchestration", "Delivery · Orchestration", "Visual review · Orchestration"],
        heroTitleIdle: "Delivery · Orchestration",
        heroLive: {
          idle: "● idle",
          running: "● running",
          waiting: "● waiting",
          revise: "● revise",
          approved: "● approved",
          done: "● done",
        },
        parAsk: "Run sign-in, billing, and notifications in parallel — start now.",
        parReply: "Got it. Parallel workflows for the three requirements are up and running.",
        pmPlaceholder: "Say the next thing to GraspPM…",
        idleMeta: "Idle",
        bootingMeta: "Booting",
        readyMeta: "Ready",
        busyMeta: "Running",
        waitingGateMeta: "Waiting on gate",
        doneMeta: "Done",
        capParStart: "IM confirmed — running in parallel…",
        capParBoot: "Confirmed — spinning up parallel runtimes",
        capParRun: "Three requirements running in parallel — research / plan",
        capParImpl: "Implement writing code in parallel — no interference",
        capParTest: "Tests keep running; billing waits at a gate — parallel, not blocked",
        capParShip: "Two MRs submitted; billing still awaiting approval",
        capParGate: "Gate cleared — billing submits its MR",
        capParDone: "IM start → parallel run → closed out",
        approveCaptions: [
          "Several workflows run in parallel; some nodes will need clarify or approval.",
          "Todos from different Runs ingest into Gates Inbox (clarify 2 + approve 2).",
          "Central clarify: pick an answer → confirm; item leaves; that Run waiting → running.",
          "Handle the next clarify item; when count hits 0, switch to Approve.",
          "Approval detail: annotate → reject → approve (folded homepage UI Demo).",
          "Light-approve the next item; clear the approve queue; Run identity remains.",
          "Both queues at 0; ~3 workflows resume running — centralized handling, separate resume.",
        ],
        approveLanes: [
          { id: "a", name: "Landing page refresh" },
          { id: "b", name: "API contract review" },
          { id: "c", name: "Sandbox preview QA" },
        ],
        approveClarify: [
          {
            id: "c1",
            run: "a",
            src: "Landing · Run #a1",
            q: "Which tone for the primary CTA?",
            opts: ["Action-first (Try now)", "Value-first (See how it collaborates)", "Decide later"],
          },
          {
            id: "c2",
            run: "b",
            src: "API contract · Run #b2",
            q: "Unify error bodies on problem+json?",
            opts: ["Yes, problem+json everywhere", "Keep per-endpoint status quo", "Public APIs only"],
          },
        ],
        approveGates: [
          { id: "g1", run: "c", src: "Sandbox · Run #c3", title: "Sign-in · UI DEMO", mode: "full" },
          { id: "g2", run: "a", src: "Landing · Run #a1", title: "Release notes blurb", mode: "light" },
        ],
        approveEmptyClarify: "Clarify queue empty",
        approveEmptyGate: "Approve queue empty",
        approveEmptyDetail: "Select an item for detail",
        approveEmptyFinale: "Queues cleared · each Run continues",
        approveConfirm: "Confirm",
        approveBtn: "Approve",
        approveReject: "Reject",
        approveLight: "Approve",
        approveStatusRunning: "running",
        approveStatusWaiting: "waiting",
        approveFunnelTitle: "Multi-run todos ingest",
        approveFunnelText:
          "Clarify and gate items from different workflows enter one Inbox, then are handled in order.",
        approveUiTitle: "Welcome back",
        approveUiHint: "Continue with your work account",
        approveUiCancel: "Cancel",
        approveUiBad: "Delete account",
        approveDesc:
          "Review an interactive demo — annotate issues, then reject or approve.",
        approveRejectComment:
          "“Delete account” on the sign-in screen is easy to mis-tap. Move it to a secondary action.",
        approvePassComment: "Dangerous action relocated; hierarchy is clear. Approved.",
        approveLightMsg: "Release notes look fine — approve.",
        approveAside: "Review comment",
        approveAnnot: "Issue",
        pmAsk:
          "How do we guarantee quality? Research industry harness practices and software engineering experience for this open-source project.",
        pmConfirm: "Confirmed — start the workflow with this plan.",
      }
    : {
        heroTitles: ["自我迭代 · 编排", "需求交付 · 编排", "视觉评审 · 编排"],
        heroTitleIdle: "需求交付 · 编排",
        heroLive: {
          idle: "● 待机",
          running: "● 运行中",
          waiting: "● 待审批",
          revise: "● 退回修改",
          approved: "● 已批准",
          done: "● 完成",
        },
        parAsk: "并行推进登录鉴权、计费结算、消息通知这三个需求，现在启动。",
        parReply: "收到。已按三条需求拉起并行工作流，开始执行。",
        pmPlaceholder: "跟 GraspPM 说下一句…",
        idleMeta: "待机",
        bootingMeta: "拉起中",
        readyMeta: "就绪",
        busyMeta: "执行中",
        waitingGateMeta: "等待门禁",
        doneMeta: "完成",
        capParStart: "IM 确认启动，并行推进中…",
        capParBoot: "已确认启动 — 拉起并行执行环境",
        capParRun: "三条需求并行开跑 — research / plan",
        capParImpl: "Implement 并行写代码 — 互不干扰",
        capParTest: "测试继续跑；计费停在门禁 — 并行不互相堵",
        capParShip: "两条已提交 MR；计费仍等批准",
        capParGate: "门禁通过 — 计费补交 MR",
        capParDone: "IM 启动 → 并行执行 → 收口完成",
        approveCaptions: [
          "多条工作流并行运行；部分节点即将需要人工澄清或审批。",
          "待办从不同 Run 汇入统一 Gates Inbox（澄清 2 + 审批 2）。",
          "集中澄清：选择答案 → 确认；该项消失，对应 Run waiting → running。",
          "继续处理下一条澄清；计数减至 0 后切到审批 Tab。",
          "审批详情：标注问题 → 拒绝 → 批准（收编原首页 UI Demo）。",
          "下一项轻量批准，清空审批队列；每项保留 Run 身份。",
          "双队列为 0；约 3 条工作流恢复 running —— 集中处理，分流恢复。",
        ],
        approveLanes: [
          { id: "a", name: "官网落地页改版" },
          { id: "b", name: "API 契约评审" },
          { id: "c", name: "沙箱预览验收" },
        ],
        approveClarify: [
          {
            id: "c1",
            run: "a",
            src: "官网落地页 · Run #a1",
            q: "主 CTA 文案偏好哪种语气？",
            opts: ["直接行动（立即试用）", "价值导向（看看如何协作）", "稍后决定"],
          },
          {
            id: "c2",
            run: "b",
            src: "API 契约 · Run #b2",
            q: "错误响应是否统一使用 problem+json？",
            opts: ["是，统一 problem+json", "保持现状分接口约定", "仅对外 API 统一"],
          },
        ],
        approveGates: [
          { id: "g1", run: "c", src: "沙箱预览 · Run #c3", title: "登录鉴权 · UI DEMO", mode: "full" },
          { id: "g2", run: "a", src: "官网落地页 · Run #a1", title: "发布说明摘要", mode: "light" },
        ],
        approveEmptyClarify: "澄清队列已清空",
        approveEmptyGate: "审批队列已清空",
        approveEmptyDetail: "选择一项查看详情",
        approveEmptyFinale: "队列已清空 · 各 Run 继续推进",
        approveConfirm: "确认并继续",
        approveBtn: "批准",
        approveReject: "拒绝",
        approveLight: "批准",
        approveStatusRunning: "running",
        approveStatusWaiting: "waiting",
        approveFunnelTitle: "多 Run 待办汇入",
        approveFunnelText: "待澄清与待审批从不同工作流汇入统一入口，再按队列连续处理。",
        approveUiTitle: "欢迎回来",
        approveUiHint: "使用企业账号继续",
        approveUiCancel: "取消",
        approveUiBad: "删除账号",
        approveDesc: "审阅可交互 Demo — 框选问题区域后可拒绝或批准。",
        approveRejectComment: "「删除账号」放在登录页不合适，容易误触。请改为次要操作或移出。",
        approvePassComment: "已修正危险操作位置，视觉层级清晰，批准通过。",
        approveLightMsg: "发布说明摘要看起来没问题，可直接批准。",
        approveAside: "审批意见",
        approveAnnot: "问题",
        pmAsk:
          "有什么办法可以保证质量？调研一下业界的 harness 和软件工程经验，看看怎么保证这个开源项目的质量。",
        pmConfirm: "确认，按这个方案启动工作流。",
      };

  if (hero) {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => hero.classList.add("is-ready"));
    });
  }

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const lanes = ["auth", "billing", "notify"];

  const heroTitles = COPY.heroTitles;
  const pick = (arr) => arr[Math.floor(Math.random() * arr.length)];
  const jitter = (base, spread = 0.35) => Math.round(base * (1 - spread + Math.random() * spread * 2));

  async function runHeroFlow(root, signal) {
    const live = within(root, "[data-hero-live]");
    const title = within(root, "[data-hero-title]");
    const loop = within(root, "[data-hf-loop]");
    const nodeIds = ["research", "clarify", "visual", "gate", "plan"];
    const edgeIds = ["e1", "e2", "e3", "e4"];
    const actions = ["approve", "revise"];

    const setLive = (text) => {
      if (live) live.textContent = text;
    };

    const setNode = (id, state) => {
      const el = within(root, `[data-hf="${id}"]`);
      el?.classList.remove("is-running", "is-done", "is-waiting");
      if (state) el?.classList.add(`is-${state}`);
    };

    const setEdge = (id, state) => {
      const el = within(root, `[data-hf-e="${id}"]`);
      el?.classList.remove("is-active", "is-done");
      if (state) el?.classList.add(`is-${state}`);
    };

    const setAction = (id, on) => {
      within(root, `[data-hf-action="${id}"]`)?.classList.toggle("is-hot", Boolean(on));
    };

    const reset = () => {
      nodeIds.forEach((id) => setNode(id, null));
      edgeIds.forEach((id) => setEdge(id, null));
      actions.forEach((id) => setAction(id, false));
      loop?.classList.remove("is-show", "is-active");
      setLive(COPY.heroLive.idle);
    };

    const runNode = async (id, ms, waiting = false) => {
      setNode(id, waiting ? "waiting" : "running");
      await sleep(ms);
      if (signal.aborted) return;
      setNode(id, "done");
    };

    while (!signal.aborted) {
      if (title) title.textContent = pick(heroTitles);
      reset();
      await sleep(jitter(260, 0.2));
      if (signal.aborted) break;

      setLive(COPY.heroLive.running);
      setEdge("e1", "active");
      await runNode("research", jitter(700));
      if (signal.aborted) break;
      setEdge("e1", "done");

      setEdge("e2", "active");
      await runNode("clarify", jitter(700));
      if (signal.aborted) break;
      setEdge("e2", "done");

      setEdge("e3", "active");
      await runNode("visual", jitter(740));
      if (signal.aborted) break;
      setEdge("e3", "done");

      setLive(COPY.heroLive.waiting);
      setNode("gate", "waiting");
      await sleep(jitter(900, 0.25));
      if (signal.aborted) break;

      if (Math.random() > 0.45) {
        setAction("revise", true);
        loop?.classList.add("is-show", "is-active");
        setLive(COPY.heroLive.revise);
        await sleep(jitter(850, 0.2));
        if (signal.aborted) break;

        setAction("revise", false);
        loop?.classList.remove("is-active");
        setNode("gate", null);
        setNode("visual", null);
        setEdge("e3", null);

        setEdge("e3", "active");
        await runNode("visual", jitter(640));
        if (signal.aborted) break;
        setEdge("e3", "done");

        setNode("gate", "waiting");
        setLive(COPY.heroLive.waiting);
        await sleep(jitter(680, 0.25));
        if (signal.aborted) break;
        loop?.classList.remove("is-show");
      }

      setAction("approve", true);
      setLive(COPY.heroLive.approved);
      await sleep(jitter(420, 0.2));
      if (signal.aborted) break;
      setAction("approve", false);
      setNode("gate", "done");

      setEdge("e4", "active");
      await runNode("plan", jitter(720));
      if (signal.aborted) break;
      setEdge("e4", "done");

      setLive(COPY.heroLive.done);
      await sleep(jitter(1400, 0.25));
    }
  }

  const heroFlow = document.querySelector("[data-hero-flow]");
  let heroFlowCtrl = null;
  if (heroFlow && !reduceMotion) {
    const startHeroFlow = () => {
      if (heroFlowCtrl) return;
      heroFlowCtrl = new AbortController();
      runHeroFlow(heroFlow, heroFlowCtrl.signal);
    };
    const stopHeroFlow = () => {
      heroFlowCtrl?.abort();
      heroFlowCtrl = null;
    };
    const heroSection = document.querySelector(".hero") || heroFlow;
    if ("IntersectionObserver" in window) {
      const heroIo = new IntersectionObserver(
        (entries) => {
          for (const entry of entries) {
            if (entry.isIntersecting) startHeroFlow();
            else stopHeroFlow();
          }
        },
        { threshold: 0.15 },
      );
      heroIo.observe(heroSection);
    } else {
      startHeroFlow();
    }
  } else if (heroFlow && reduceMotion) {
    const titleEl = within(heroFlow, "[data-hero-title]");
    const liveEl = within(heroFlow, "[data-hero-live]");
    if (titleEl) titleEl.textContent = COPY.heroTitleIdle;
    if (liveEl) liveEl.textContent = COPY.heroLive.done;
    ["research", "clarify", "visual", "gate", "plan"].forEach((id) => {
      within(heroFlow, `[data-hf="${id}"]`)?.classList.add("is-done");
    });
    ["e1", "e2", "e3", "e4"].forEach((id) => {
      within(heroFlow, `[data-hf-e="${id}"]`)?.classList.add("is-done");
    });
  }

  function within(root, sel) {
    return root.querySelector(sel);
  }

  function clearNode(el) {
    el?.classList.remove("is-running", "is-done", "is-waiting");
  }

  function setNode(root, id, state) {
    const el = within(root, `[data-node="${id}"]`);
    clearNode(el);
    if (state) el?.classList.add(`is-${state}`);
  }

  function setEdge(root, id, state) {
    const el = within(root, `[data-edge="${id}"]`);
    el?.classList.remove("is-active", "is-done", "is-wait");
    if (state) el?.classList.add(`is-${state}`);
  }

  function setLanePill(root, lane, state, label) {
    const pill = within(root, `[data-lane-pill="${lane}"]`);
    const row = within(root, `[data-lane="${lane}"]`);
    if (pill) {
      pill.className = `demo-lane__pill${state ? ` is-${state}` : ""}`;
      pill.textContent = label;
    }
    row?.classList.remove("is-focus", "is-waiting", "is-done");
    if (state === "running") row?.classList.add("is-focus");
    if (state === "waiting") row?.classList.add("is-waiting");
    if (state === "done") row?.classList.add("is-done");
  }

  function setGates(root, count) {
    const badge = within(root, "[data-gates-badge] b");
    const chip = within(root, "[data-gates-badge]");
    const empty = within(root, "[data-gates-empty]");
    const item = within(root, "[data-gates-item]");
    const gateStat = within(root, "[data-gate-stat]");
    if (badge) badge.textContent = String(count);
    chip?.classList.toggle("is-hot", count > 0);
    empty?.classList.toggle("is-hide", count > 0);
    item?.classList.toggle("is-show", count > 0);
    if (gateStat) gateStat.textContent = `${count} waiting`;
  }

  function setShipStat(root, n) {
    const el = within(root, "[data-run-stat]");
    if (el) el.textContent = `${n} / 3 shipping`;
  }

  function moveCursor(stage, cursor, el) {
    if (!cursor || !el || !stage) return;
    const root = stage.getBoundingClientRect();
    const rect = el.getBoundingClientRect();
    cursor.style.left = `${rect.left - root.left + rect.width * 0.65}px`;
    cursor.style.top = `${rect.top - root.top + rect.height * 0.55}px`;
    cursor.classList.add("is-visible");
  }

  function placeCursorAt(stage, cursor, x, y) {
    if (!cursor || !stage) return;
    cursor.style.left = `${x}px`;
    cursor.style.top = `${y}px`;
    cursor.classList.add("is-visible");
  }

  /** Marquee final box in stage coords: left 52% / top 70% / w 42% / h 18% of .demo-ui */
  function marqueeBoxInStage(stage, ui) {
    const sr = stage.getBoundingClientRect();
    const ur = ui.getBoundingClientRect();
    const left = ur.left - sr.left;
    const top = ur.top - sr.top;
    return {
      x0: left + ur.width * 0.52,
      y0: top + ur.height * 0.7,
      x1: left + ur.width * (0.52 + 0.42),
      y1: top + ur.height * (0.7 + 0.18),
    };
  }

  function animateCursorTo(cursor, x1, y1, duration) {
    return new Promise((resolve) => {
      const x0 = parseFloat(cursor.style.left) || 0;
      const y0 = parseFloat(cursor.style.top) || 0;
      const prev = cursor.style.transition;
      cursor.style.transition = "none";
      let start = null;
      function frame(t) {
        if (start === null) start = t;
        const p = Math.min(1, (t - start) / duration);
        const e = 1 - Math.pow(1 - p, 3);
        cursor.style.left = `${x0 + (x1 - x0) * e}px`;
        cursor.style.top = `${y0 + (y1 - y0) * e}px`;
        if (p < 1) {
          requestAnimationFrame(frame);
        } else {
          cursor.style.transition = prev;
          resolve();
        }
      }
      requestAnimationFrame(frame);
    });
  }

  function hideCursor(cursor) {
    cursor?.classList.remove("is-visible", "is-click");
  }

  async function clickEl(stage, cursor, el) {
    moveCursor(stage, cursor, el);
    await sleep(500);
    cursor?.classList.add("is-click");
    el?.classList.add("is-pressed");
    await sleep(180);
    cursor?.classList.remove("is-click");
    el?.classList.remove("is-pressed");
    hideCursor(cursor);
  }

  async function typeText(el, text, signal, ms = 26) {
    if (!el) return;
    el.textContent = "";
    el.classList.add("is-caret");
    for (let i = 0; i < text.length; i += 1) {
      if (signal.aborted) {
        el.classList.remove("is-caret");
        return;
      }
      el.textContent += text[i];
      await sleep(ms);
    }
    el.classList.remove("is-caret");
  }

  /* Parallel showcase — IM start → runtime containers → lanes */
  const parallelNodes = {
    auth: ["auth-research", "auth-impl", "auth-test", "auth-ship"],
    billing: ["billing-plan", "billing-impl", "billing-gate", "billing-ship"],
    notify: ["notify-research", "notify-impl", "notify-test", "notify-ship"],
  };

  const PAR_ASK = COPY.parAsk;
  const PAR_REPLY = COPY.parReply;
  const TERM_LINES = {
    auth: ["$ boot runtime", "$ mount workspace", "$ agent research …"],
    billing: ["$ boot runtime", "$ mount workspace", "$ agent plan …"],
    notify: ["$ boot runtime", "$ mount workspace", "$ agent research …"],
  };

  function setLiveChip(root, text) {
    const el = within(root, "[data-chip-live]");
    if (el) el.textContent = text;
  }

  function setSbChip(root, n) {
    const el = within(root, "[data-sb-chip] b");
    if (el) el.textContent = String(n);
  }

  function setTermLines(root, id, lines) {
    const card = within(root, `[data-sb="${id}"]`);
    if (!card) return;
    const spans = card.querySelectorAll("[data-sb-line]");
    spans.forEach((el, i) => {
      el.textContent = lines[i] || "";
    });
  }

  function setSandbox(root, id, state, status, meta, lines) {
    const card = within(root, `[data-sb="${id}"]`);
    if (!card) return;
    card.classList.remove("is-creating", "is-running", "is-busy", "is-done");
    if (state) card.classList.add(`is-${state}`);
    const st = within(card, "[data-sb-status]");
    const m = within(card, "[data-sb-meta]");
    if (st) st.textContent = status;
    if (m) m.textContent = meta;
    if (lines) setTermLines(root, id, lines);
  }

  function resetSandboxes(root) {
    for (const id of lanes) {
      setSandbox(root, id, "", "idle", COPY.idleMeta, ["", "", ""]);
    }
    setSbChip(root, 0);
  }

  function hideParMsgs(root) {
    root.querySelectorAll("[data-par-msg]").forEach((el) => {
      el.classList.remove("is-show");
      const out = within(el, "[data-par-typeout]");
      if (out) {
        out.textContent = "";
        out.classList.remove("is-caret");
      }
    });
  }

  function resetParComposer(root) {
    const composer = within(root, "[data-par-composer]");
    if (!composer) return;
    composer.classList.remove("is-typing");
    composer.textContent = "";
    composer.setAttribute("data-placeholder", COPY.pmPlaceholder);
  }

  async function typeParComposer(root, text, signal) {
    const composer = within(root, "[data-par-composer]");
    if (!composer) return;
    composer.classList.add("is-typing");
    composer.removeAttribute("data-placeholder");
    await typeText(composer, text, signal, 28);
    if (signal.aborted) return;
    await sleep(200);
  }

  async function typeParMsg(root, n, signal) {
    const msg = within(root, `[data-par-msg="${n}"]`);
    if (!msg) return;
    const bubble = within(msg, "[data-par-text]");
    const out = within(msg, "[data-par-typeout]");
    const full = bubble?.dataset.parText || "";
    if (out) out.textContent = "";
    msg.classList.add("is-show");
    await typeText(out, full, signal, 20);
  }

  function resetLaunch(root) {
    const sheet = within(root, "[data-launch-sheet]");
    sheet?.classList.add("is-open");
    hideParMsgs(root);
    resetParComposer(root);
  }

  function resetParallel(root) {
    for (const lane of lanes) {
      parallelNodes[lane].forEach((id) => clearNode(within(root, `[data-node="${id}"]`)));
      for (let i = 1; i <= 3; i += 1) setEdge(root, `${lane}-${i}`, "");
      setLanePill(root, lane, "", "queued");
    }
    setGates(root, 0);
    setShipStat(root, 0);
    resetSandboxes(root);
    resetLaunch(root);
    setLiveChip(root, "● idle");
  }

  async function playParallel(root, signal) {
    const caption = within(root, '[data-caption="parallel"]');
    const sheet = within(root, "[data-launch-sheet]");

    while (!signal.aborted) {
      resetParallel(root);
      if (caption) caption.textContent = COPY.capParStart;
      await sleep(300);
      if (signal.aborted) break;

      await typeParComposer(root, PAR_ASK, signal);
      if (signal.aborted) break;

      await typeParMsg(root, 1, signal);
      resetParComposer(root);
      await sleep(280);
      if (signal.aborted) break;

      await typeParMsg(root, 2, signal);
      setLiveChip(root, "● starting");
      if (caption) caption.textContent = COPY.capParBoot;
      await sleep(500);
      if (signal.aborted) break;

      for (const id of lanes) {
        setSandbox(root, id, "creating", "booting", COPY.bootingMeta, [
          "$ init runtime",
          "$ allocate cpu/mem",
          "…",
        ]);
      }
      setSbChip(root, 3);
      await sleep(650);
      if (signal.aborted) break;

      sheet?.classList.remove("is-open");
      setLiveChip(root, "● live run");
      for (const id of lanes) {
        setSandbox(root, id, "running", "ready", COPY.readyMeta, TERM_LINES[id]);
      }
      if (caption) caption.textContent = COPY.capParRun;
      await sleep(400);
      if (signal.aborted) break;

      setNode(root, "auth-research", "running");
      setNode(root, "billing-plan", "running");
      setNode(root, "notify-research", "running");
      setEdge(root, "auth-1", "active");
      setEdge(root, "billing-1", "active");
      setEdge(root, "notify-1", "active");
      setLanePill(root, "auth", "running", "running");
      setLanePill(root, "billing", "running", "running");
      setLanePill(root, "notify", "running", "running");
      setSandbox(root, "auth", "busy", "busy", COPY.busyMeta, ["$ agent research", "writing brief…", "ok"]);
      setSandbox(root, "billing", "busy", "busy", COPY.busyMeta, ["$ agent plan", "draft model…", "ok"]);
      setSandbox(root, "notify", "busy", "busy", COPY.busyMeta, ["$ agent research", "channel pick…", "ok"]);
      await sleep(900);
      if (signal.aborted) break;

      setNode(root, "auth-research", "done");
      setNode(root, "billing-plan", "done");
      setNode(root, "notify-research", "done");
      setEdge(root, "auth-1", "done");
      setEdge(root, "billing-1", "done");
      setEdge(root, "notify-1", "done");
      setNode(root, "auth-impl", "running");
      setNode(root, "billing-impl", "running");
      setNode(root, "notify-impl", "running");
      setEdge(root, "auth-2", "active");
      setEdge(root, "billing-2", "active");
      setEdge(root, "notify-2", "active");
      setSandbox(root, "auth", "busy", "busy", COPY.busyMeta, ["$ implement", "edit middleware…", "tests…"]);
      setSandbox(root, "billing", "busy", "busy", COPY.busyMeta, ["$ implement", "edit ledger…", "tests…"]);
      setSandbox(root, "notify", "busy", "busy", COPY.busyMeta, ["$ implement", "edit webhook…", "tests…"]);
      if (caption) caption.textContent = COPY.capParImpl;
      await sleep(1000);
      if (signal.aborted) break;

      setNode(root, "auth-impl", "done");
      setNode(root, "billing-impl", "done");
      setNode(root, "notify-impl", "done");
      setEdge(root, "auth-2", "done");
      setEdge(root, "billing-2", "done");
      setEdge(root, "notify-2", "done");

      setNode(root, "auth-test", "running");
      setNode(root, "notify-test", "running");
      setNode(root, "billing-gate", "waiting");
      setEdge(root, "auth-3", "active");
      setEdge(root, "notify-3", "active");
      setEdge(root, "billing-3", "wait");
      setLanePill(root, "billing", "waiting", "waiting human");
      setGates(root, 1);
      setSandbox(root, "auth", "busy", "busy", COPY.busyMeta, ["$ e2e", "playwright…", "green"]);
      setSandbox(root, "billing", "running", "paused", COPY.waitingGateMeta, ["$ gate", "waiting human…", ""]);
      setSandbox(root, "notify", "busy", "busy", COPY.busyMeta, ["$ contract test", "retries…", "ok"]);
      if (caption) caption.textContent = COPY.capParTest;
      await sleep(1100);
      if (signal.aborted) break;

      setNode(root, "auth-test", "done");
      setNode(root, "notify-test", "done");
      setEdge(root, "auth-3", "done");
      setEdge(root, "notify-3", "done");
      setNode(root, "auth-ship", "running");
      setNode(root, "notify-ship", "running");
      setShipStat(root, 2);
      setSandbox(root, "auth", "busy", "busy", COPY.busyMeta, ["$ submit MR", "!482", "done"]);
      setSandbox(root, "notify", "busy", "busy", COPY.busyMeta, ["$ submit MR", "!491", "done"]);
      if (caption) caption.textContent = COPY.capParShip;
      await sleep(900);
      if (signal.aborted) break;

      setNode(root, "auth-ship", "done");
      setNode(root, "notify-ship", "done");
      setLanePill(root, "auth", "done", "shipped");
      setLanePill(root, "notify", "done", "shipped");
      setSandbox(root, "auth", "done", "done", COPY.doneMeta, ["$ idle", "", ""]);
      setSandbox(root, "notify", "done", "done", COPY.doneMeta, ["$ idle", "", ""]);
      setNode(root, "billing-gate", "done");
      setEdge(root, "billing-3", "done");
      setGates(root, 0);
      setNode(root, "billing-ship", "running");
      setLanePill(root, "billing", "running", "shipping");
      setSandbox(root, "billing", "busy", "busy", COPY.busyMeta, ["$ submit MR", "push…", "done"]);
      setShipStat(root, 3);
      if (caption) caption.textContent = COPY.capParGate;
      await sleep(700);
      if (signal.aborted) break;

      setNode(root, "billing-ship", "done");
      setLanePill(root, "billing", "done", "shipped");
      setSandbox(root, "billing", "done", "done", COPY.doneMeta, ["$ idle", "", ""]);
      if (caption) caption.textContent = COPY.capParDone;
      await sleep(1500);
    }
  }

  /* Approve showcase — unified Inbox: ingest → clarify ×2 → gate full+light → resume */
  async function typeComment(el, text, signal, ms = 22) {
    if (!el) return;
    el.textContent = "";
    el.classList.add("is-typing");
    for (let i = 0; i < text.length; i += 1) {
      if (signal.aborted) {
        el.classList.remove("is-typing");
        return;
      }
      el.textContent += text[i];
      await sleep(ms);
    }
    el.classList.remove("is-typing");
  }

  function approveLaneStatuses(stage) {
    if (stage === 0) return { a: "running", b: "running", c: "running" };
    if (stage <= 2) return { a: "waiting", b: "waiting", c: "waiting" };
    if (stage === 3) return { a: "running", b: "waiting", c: "waiting" };
    if (stage === 4) return { a: "running", b: "running", c: "waiting" };
    return { a: "running", b: "running", c: "running" };
  }

  function approveClarifyDone(stage) {
    if (stage <= 2) return [];
    if (stage === 3) return ["c1"];
    return ["c1", "c2"];
  }

  function approveGateDone(stage) {
    if (stage < 5) return [];
    if (stage === 5) return ["g1"];
    return ["g1", "g2"];
  }

  function approveActiveTab(stage) {
    return stage >= 4 ? "gate" : "clarify";
  }

  function approveCounts(stage) {
    if (stage === 0 || stage >= 6) return { c: 0, g: 0, total: 0 };
    const c = 2 - approveClarifyDone(stage).length;
    const g = 2 - approveGateDone(stage).length;
    return { c, g, total: c + g };
  }

  function renderApproveLanes(root, stage) {
    const host = within(root, "[data-approve-lanes]");
    if (!host) return;
    const st = approveLaneStatuses(stage);
    host.innerHTML = COPY.approveLanes
      .map(
        (L) =>
          `<div class="approve-lane" data-lane-id="${L.id}" data-status="${st[L.id]}">` +
          `<div class="approve-lane__name">${L.name}</div>` +
          `<div class="approve-lane__status">${
            st[L.id] === "waiting" ? COPY.approveStatusWaiting : COPY.approveStatusRunning
          }</div></div>`,
      )
      .join("");
  }

  function renderApproveCounts(root, stage) {
    const c = approveCounts(stage);
    const total = within(root, "[data-approve-total]");
    const clarify = within(root, "[data-approve-clarify-count]");
    const gate = within(root, "[data-approve-gate-count]");
    if (total) {
      total.textContent = String(c.total);
      total.className = `approve-count${c.total === 0 ? " zero" : " has"}`;
    }
    if (clarify) clarify.textContent = String(c.c);
    if (gate) gate.textContent = String(c.g);
  }

  function setApproveTab(root, which) {
    root.querySelectorAll("[data-approve-tab]").forEach((btn) => {
      btn.classList.toggle("is-active", btn.dataset.approveTab === which);
    });
  }

  function renderApproveList(root, stage) {
    const pane = within(root, "[data-approve-list]");
    if (!pane) return;
    const tab = approveActiveTab(stage);
    setApproveTab(root, tab);
    if (stage === 0) {
      pane.innerHTML = `<div class="approve-list-empty">—</div>`;
      return;
    }
    if (tab === "clarify") {
      if (approveClarifyDone(stage).length === 2 && stage >= 4) {
        pane.innerHTML = `<div class="approve-list-empty">${COPY.approveEmptyClarify}</div>`;
        return;
      }
      pane.innerHTML = COPY.approveClarify
        .map((it, idx) => {
          const gone = approveClarifyDone(stage).includes(it.id);
          const active =
            (stage === 2 && it.id === "c1") ||
            (stage === 3 && it.id === "c2") ||
            (stage === 1 && idx === 0);
          return (
            `<button type="button" class="approve-list-item${active ? " is-active" : ""}${
              gone ? " is-gone" : ""
            }" tabindex="-1">` +
            `<div class="approve-list-item__src">${it.src}</div>` +
            `<div class="approve-list-item__q">${it.q}</div></button>`
          );
        })
        .join("");
    } else {
      if (approveGateDone(stage).length === 2) {
        pane.innerHTML = `<div class="approve-list-empty">${COPY.approveEmptyGate}</div>`;
        return;
      }
      pane.innerHTML = COPY.approveGates
        .map((it) => {
          const gone = approveGateDone(stage).includes(it.id);
          const active = (stage === 4 && it.id === "g1") || (stage === 5 && it.id === "g2");
          return (
            `<button type="button" class="approve-list-item${active ? " is-active" : ""}${
              gone ? " is-gone" : ""
            }" tabindex="-1">` +
            `<div class="approve-list-item__src">${it.src}</div>` +
            `<div class="approve-list-item__q">${it.title}</div></button>`
          );
        })
        .join("");
    }
  }

  function renderApproveFullGate(root, gatePhase) {
    const pane = within(root, "[data-approve-detail]");
    if (!pane) return;
    const g = COPY.approveGates[0];
    let pillCls = "approve-window__pill";
    let comment = "";
    let rejPressed = "";
    let okPressed = "";
    let marquee = "";
    if (gatePhase === "annotate") {
      marquee = " is-show";
    } else if (gatePhase === "reject") {
      marquee = " is-show";
      pillCls += " is-rejected";
      comment = COPY.approveRejectComment;
      rejPressed = " is-pressed";
    } else if (gatePhase === "approve") {
      pillCls += " is-approved";
      comment = COPY.approvePassComment;
      okPressed = " is-pressed";
    }
    /* gatePhase === "pending": open window, no marquee yet */
    const pillText =
      gatePhase === "approve" ? "approved" : gatePhase === "reject" ? "rejected" : "pending";
    pane.innerHTML =
      `<div class="approve-window is-open" data-approve-window>` +
      `<header class="approve-window__head">` +
      `<div><p class="approve-window__eyebrow">human_gate · waiting_human</p>` +
      `<h3 class="approve-window__title">${g.title}</h3></div>` +
      `<span class="${pillCls}" data-approve-pill>${pillText}</span>` +
      `</header>` +
      `<div class="approve-window__main">` +
      `<div class="approve-window__content">` +
      `<div class="preview-board">` +
      `<div class="preview-board__meta">` +
      `<span class="preview-chip">artifact</span>` +
      `<span class="preview-chip">ui-demo</span>` +
      `<span class="preview-chip preview-chip--ok">interactive</span>` +
      `</div>` +
      `<p class="preview-board__desc">${COPY.approveDesc}</p>` +
      `<div class="demo-stage" data-demo-stage>` +
      `<div class="demo-ui" data-demo-case="reject">` +
      `<div class="demo-ui__brand">Grasp</div>` +
      `<h4 class="demo-ui__title">${COPY.approveUiTitle}</h4>` +
      `<p class="demo-ui__hint">${COPY.approveUiHint}</p>` +
      `<div class="demo-ui__field"><span></span></div>` +
      `<div class="demo-ui__field"><span></span></div>` +
      `<div class="demo-ui__actions">` +
      `<span class="demo-ui__btn">${COPY.approveUiCancel}</span>` +
      `<span class="demo-ui__btn is-bad" data-annotate-target>${COPY.approveUiBad}</span>` +
      `</div>` +
      `<div class="demo-marquee${marquee}" data-marquee data-label="${COPY.approveAnnot}" aria-hidden="true"></div>` +
      `</div></div></div></div>` +
      `<aside class="approve-window__aside">` +
      `<p class="approve-aside__label">${COPY.approveAside}</p>` +
      `<div class="approve-aside__comment" data-approve-comment>${comment}</div>` +
      `<div class="approve-aside__actions">` +
      `<button type="button" class="approve-btn approve-btn--reject${rejPressed}" data-reject-btn tabindex="-1">${COPY.approveReject}</button>` +
      `<button type="button" class="approve-btn${okPressed}" data-approve-btn tabindex="-1">${COPY.approveBtn}</button>` +
      `</div></aside></div></div>`;
  }

  function renderApproveDetail(root, stage, gatePhase = "annotate") {
    const pane = within(root, "[data-approve-detail]");
    if (!pane) return;
    if (stage === 0) {
      pane.innerHTML = `<div class="approve-detail-empty">${COPY.approveEmptyDetail}</div>`;
      return;
    }
    if (stage === 1) {
      pane.innerHTML = `<div class="approve-detail-empty">${COPY.approveFunnelText}</div>`;
      return;
    }
    if (stage >= 6) {
      pane.innerHTML = `<div class="approve-detail-empty">${COPY.approveEmptyFinale}</div>`;
      return;
    }
    if (stage === 2 || stage === 3) {
      const it = COPY.approveClarify[stage === 2 ? 0 : 1];
      const selected = stage === 2 || stage === 3 ? 0 : -1;
      pane.innerHTML =
        `<div class="approve-detail-meta">${it.src} · waiting → running</div>` +
        `<div class="approve-detail-q">${it.q}</div>` +
        `<div class="approve-options">` +
        it.opts
          .map(
            (o, i) =>
              `<button type="button" class="approve-opt${i === selected ? " is-selected" : ""}" data-approve-opt="${i}" tabindex="-1">${o}</button>`,
          )
          .join("") +
        `</div>` +
        `<button type="button" class="approve-confirm" data-approve-confirm tabindex="-1">${COPY.approveConfirm}</button>`;
      return;
    }
    if (stage === 4) {
      renderApproveFullGate(root, gatePhase);
      return;
    }
    if (stage === 5) {
      const g2 = COPY.approveGates[1];
      pane.innerHTML =
        `<div class="approve-detail-meta">${g2.src} · gate · light</div>` +
        `<div class="approve-detail-q">${g2.title}</div>` +
        `<div class="approve-light-card"><p>${COPY.approveLightMsg}</p></div>` +
        `<button type="button" class="approve-btn" data-approve-btn tabindex="-1" style="width:auto;padding:0.55rem 1rem">${COPY.approveLight}</button>`;
    }
  }

  function renderApproveStage(root, stage, gatePhase = "annotate") {
    renderApproveLanes(root, stage);
    renderApproveCounts(root, stage);
    renderApproveList(root, stage);
    renderApproveDetail(root, stage, gatePhase);
    const funnel = within(root, "[data-approve-funnel]");
    funnel?.classList.toggle("is-on", stage === 1);
    const caption = within(root, '[data-caption="approve"]');
    if (caption) caption.textContent = COPY.approveCaptions[stage] || "";
    const funnelTitle = within(root, ".approve-funnel__title");
    const funnelText = within(root, ".approve-funnel__text");
    if (funnelTitle) funnelTitle.textContent = COPY.approveFunnelTitle;
    if (funnelText) funnelText.textContent = COPY.approveFunnelText;
  }

  function resetApproveDemo(root) {
    hideCursor(within(root, '[data-cursor="approve"]'));
    renderApproveStage(root, 0);
  }

  async function playApproveFullGate(root, signal) {
    const inbox = within(root, "[data-approve-inbox]");
    const cursor = within(root, '[data-cursor="approve"]');
    renderApproveStage(root, 4, "pending");
    await sleep(400);
    if (signal.aborted) return;

    const target = within(root, "[data-annotate-target]");
    const demoUi = target?.closest(".demo-ui") || within(root, '[data-demo-case="reject"]');
    const marquee = within(root, "[data-marquee]");

    if (target && cursor && inbox && demoUi) {
      const box = marqueeBoxInStage(inbox, demoUi);
      const prevTransition = cursor.style.transition;
      cursor.style.transition = "none";
      placeCursorAt(inbox, cursor, box.x0 - 28, box.y0 - 24);
      void cursor.offsetWidth;
      cursor.style.transition = prevTransition;
      await animateCursorTo(cursor, box.x0, box.y0, 380);
      await sleep(120);
      if (signal.aborted) return;
      marquee?.classList.add("is-show");
      await animateCursorTo(cursor, box.x1, box.y1, 550);
      await sleep(280);
      if (signal.aborted) return;
      hideCursor(cursor);
    } else {
      marquee?.classList.add("is-show");
    }
    await sleep(350);
    if (signal.aborted) return;

    const comment = within(root, "[data-approve-comment]");
    await typeComment(comment, COPY.approveRejectComment, signal);
    if (signal.aborted) return;
    await sleep(200);
    if (signal.aborted) return;

    const rejectBtn = within(root, "[data-reject-btn]");
    await clickEl(inbox, cursor, rejectBtn);
    if (signal.aborted) return;
    renderApproveStage(root, 4, "reject");
    await sleep(700);
    if (signal.aborted) return;

    renderApproveStage(root, 4, "pending");
    const comment2 = within(root, "[data-approve-comment]");
    if (comment2) comment2.textContent = "";
    const marquee2 = within(root, "[data-marquee]");
    marquee2?.classList.remove("is-show");
    await typeComment(comment2, COPY.approvePassComment, signal);
    if (signal.aborted) return;
    await sleep(200);
    if (signal.aborted) return;

    const approveBtn = within(root, "[data-approve-btn]");
    await clickEl(inbox, cursor, approveBtn);
    if (signal.aborted) return;
    renderApproveStage(root, 4, "approve");
    await sleep(900);
  }

  async function playApproveClarifyItem(root, signal, stage) {
    const inbox = within(root, "[data-approve-inbox]");
    const cursor = within(root, '[data-cursor="approve"]');
    renderApproveStage(root, stage);
    await sleep(450);
    if (signal.aborted) return;

    const opt = within(root, '[data-approve-opt="0"]');
    await clickEl(inbox, cursor, opt);
    if (signal.aborted) return;
    opt?.classList.add("is-selected");
    await sleep(350);
    if (signal.aborted) return;

    const confirm = within(root, "[data-approve-confirm]");
    await clickEl(inbox, cursor, confirm);
    if (signal.aborted) return;
    confirm?.classList.add("is-pressed");
    await sleep(550);
  }

  async function playApproveLightGate(root, signal) {
    const inbox = within(root, "[data-approve-inbox]");
    const cursor = within(root, '[data-cursor="approve"]');
    renderApproveStage(root, 5);
    await sleep(500);
    if (signal.aborted) return;
    const btn = within(root, "[data-approve-btn]");
    await clickEl(inbox, cursor, btn);
    if (signal.aborted) return;
    btn?.classList.add("is-pressed");
    await sleep(700);
  }

  async function playApprove(root, signal) {
    while (!signal.aborted) {
      resetApproveDemo(root);
      await sleep(900);
      if (signal.aborted) break;

      /* stage 1 — ingest */
      renderApproveStage(root, 1);
      await sleep(1600);
      if (signal.aborted) break;

      /* stage 2 — clarify ① */
      await playApproveClarifyItem(root, signal, 2);
      if (signal.aborted) break;
      renderApproveStage(root, 3);
      await sleep(400);
      if (signal.aborted) break;

      /* stage 3 — clarify ② (already at stage 3 visually for list; animate confirm) */
      await playApproveClarifyItem(root, signal, 3);
      if (signal.aborted) break;
      renderApproveStage(root, 4, "pending");
      await sleep(350);
      if (signal.aborted) break;

      /* stage 4 — full UI demo */
      await playApproveFullGate(root, signal);
      if (signal.aborted) break;
      renderApproveStage(root, 5);
      await sleep(300);
      if (signal.aborted) break;

      /* stage 5 — light approve */
      await playApproveLightGate(root, signal);
      if (signal.aborted) break;

      /* stage 6 — resume finale */
      renderApproveStage(root, 6);
      await sleep(1800);
    }
  }

  function applyApproveReducedMotion(root) {
    hideCursor(within(root, '[data-cursor="approve"]'));
    renderApproveStage(root, 6);
  }

  /* PM showcase — typewriter IM, no bottom caption */
  const PM_PLACEHOLDER = COPY.pmPlaceholder;

  function scrollPmThread(root, behavior = "smooth") {
    const thread = within(root, "[data-pm-thread]");
    if (!thread) return;
    thread.scrollTo({ top: thread.scrollHeight, behavior });
  }


  async function typeComposer(root, text, signal) {
    const composer = within(root, "[data-pm-composer]");
    if (!composer) return;
    composer.classList.add("is-typing");
    composer.removeAttribute("data-placeholder");
    await typeText(composer, text, signal, 30);
    if (signal.aborted) return;
    await sleep(220);
  }

  function resetComposer(root) {
    const composer = within(root, "[data-pm-composer]");
    if (!composer) return;
    composer.classList.remove("is-typing");
    composer.textContent = "";
    composer.setAttribute("data-placeholder", PM_PLACEHOLDER);
  }

  async function typeMsg(root, n, signal) {
    const msg = within(root, `[data-pm-msg="${n}"]`);
    if (!msg) return;
    const bubble = within(msg, "[data-pm-text]");
    const out = within(msg, "[data-pm-typeout]");
    const extra = within(msg, "[data-pm-extra]");
    const full = bubble?.dataset.pmText || "";
    if (out) out.textContent = "";
    if (extra) extra.hidden = true;
    msg.classList.add("is-show");
    requestAnimationFrame(() => scrollPmThread(root, "auto"));
    await typeText(out, full, signal, 22);
    if (signal.aborted) return;
    if (extra) {
      extra.hidden = false;
      scrollPmThread(root);
    } else {
      scrollPmThread(root);
    }
  }

  function hideAllMsgs(root) {
    root.querySelectorAll("[data-pm-msg]").forEach((el) => {
      el.classList.remove("is-show");
      const out = within(el, "[data-pm-typeout]");
      if (out) {
        out.textContent = "";
        out.classList.remove("is-caret");
      }
      const extra = within(el, "[data-pm-extra]");
      if (extra) extra.hidden = true;
    });
    const thread = within(root, "[data-pm-thread]");
    if (thread) thread.scrollTop = 0;
  }

  function setCounts(root, running, waiting) {
    const r = within(root, "[data-pm-running]");
    const w = within(root, "[data-pm-waiting]");
    if (r) r.textContent = String(running);
    if (w) w.textContent = String(waiting);
  }

  function setStep(root, n, state, label) {
    const li = within(root, `[data-pm-step="${n}"]`);
    const tag = within(root, `[data-pm-step-tag="${n}"]`);
    if (li) {
      li.classList.remove("is-proposed", "is-running", "is-waiting", "is-done");
      if (state) li.classList.add(`is-${state}`);
    }
    if (tag) tag.textContent = label;
  }

  function resetSteps(root) {
    for (let i = 1; i <= 3; i += 1) setStep(root, i, "", "proposed");
    for (let i = 4; i <= 6; i += 1) setStep(root, i, "", "queued");
  }

  async function playPm(root, signal) {
    const ask = COPY.pmAsk;
    const confirm = COPY.pmConfirm;

    while (!signal.aborted) {
      hideAllMsgs(root);
      resetSteps(root);
      setCounts(root, 0, 0);
      resetComposer(root);
      await sleep(350);
      if (signal.aborted) break;

      await typeComposer(root, ask, signal);
      if (signal.aborted) break;

      await typeMsg(root, 1, signal);
      resetComposer(root);
      await sleep(280);
      if (signal.aborted) break;

      await typeMsg(root, 2, signal);
      await sleep(400);
      if (signal.aborted) break;

      await typeMsg(root, 3, signal);
      setStep(root, 1, "proposed", "proposed");
      setStep(root, 2, "proposed", "proposed");
      setStep(root, 3, "proposed", "proposed");
      scrollPmThread(root);
      await sleep(500);
      if (signal.aborted) break;

      await typeMsg(root, 4, signal);
      setCounts(root, 0, 1);
      await sleep(400);
      if (signal.aborted) break;

      await typeComposer(root, confirm, signal);
      if (signal.aborted) break;

      await typeMsg(root, 5, signal);
      resetComposer(root);
      setCounts(root, 0, 0);
      await sleep(280);
      if (signal.aborted) break;

      await typeMsg(root, 6, signal);
      setStep(root, 4, "running", "running");
      setStep(root, 5, "running", "running");
      setStep(root, 6, "queued", "queued");
      setCounts(root, 2, 0);
      scrollPmThread(root);
      await sleep(700);
      if (signal.aborted) break;

      setStep(root, 4, "done", "done");
      setStep(root, 5, "running", "60%");
      setStep(root, 6, "waiting", "pending");
      setCounts(root, 1, 1);
      await typeMsg(root, 7, signal);
      await sleep(1500);
    }
  }

  const players = {
    parallel: playParallel,
    approve: playApprove,
    pm: playPm,
  };

  const controllers = new Map();

  function startShowcase(el) {
    const key = el.dataset.showcase;
    const play = players[key];
    if (!play || controllers.has(key)) return;

    if (reduceMotion) {
      if (key === "parallel") {
        resetParallel(el);
        within(el, "[data-launch-sheet]")?.classList.remove("is-open");
        Object.values(parallelNodes).flat().forEach((id) => setNode(el, id, "done"));
        for (const lane of lanes) {
          for (let i = 1; i <= 3; i += 1) setEdge(el, `${lane}-${i}`, "done");
          setLanePill(el, lane, "done", "shipped");
          setSandbox(el, lane, "done", "done", COPY.doneMeta, ["$ idle", "", ""]);
        }
        setShipStat(el, 3);
        setSbChip(el, 3);
        setLiveChip(el, "● live run");
      } else if (key === "approve") {
        applyApproveReducedMotion(el);
      } else if (key === "pm") {
        el.querySelectorAll("[data-pm-msg]").forEach((m) => {
          m.classList.add("is-show");
          const bubble = m.querySelector("[data-pm-text]");
          const out = m.querySelector("[data-pm-typeout]");
          const extra = m.querySelector("[data-pm-extra]");
          if (out && bubble) out.textContent = bubble.dataset.pmText || "";
          if (extra) extra.hidden = false;
        });
        for (let i = 1; i <= 3; i += 1) setStep(el, i, "proposed", "proposed");
        for (let i = 4; i <= 6; i += 1) setStep(el, i, "done", "done");
        setCounts(el, 0, 0);
        resetComposer(el);
        scrollPmThread(el, "auto");
      }
      return;
    }

    const ac = new AbortController();
    controllers.set(key, ac);
    play(el, ac.signal);
  }

  function stopShowcase(el) {
    const key = el.dataset.showcase;
    const ac = controllers.get(key);
    if (ac) {
      ac.abort();
      controllers.delete(key);
    }
  }

  const sections = document.querySelectorAll("[data-showcase]");
  if (!("IntersectionObserver" in window)) {
    sections.forEach((el) => startShowcase(el));
    return;
  }

  const io = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting && entry.intersectionRatio >= 0.35) {
          startShowcase(entry.target);
        } else {
          stopShowcase(entry.target);
        }
      }
    },
    { threshold: [0, 0.35, 0.6] },
  );

  sections.forEach((el) => io.observe(el));
})();
