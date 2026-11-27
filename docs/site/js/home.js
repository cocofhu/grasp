(() => {
  const story = document.querySelector("[data-story]");
  const frame = story.querySelector("[data-story-frame]");
  const demo = story.querySelector("[data-demo]");
  const camera = demo.querySelector("[data-camera]");
  const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)");
  const pin = matchMedia("(min-width: 59rem) and (min-height: 48rem) and (prefers-reduced-motion: no-preference)");
  const tabs = [...demo.querySelectorAll("[role=tab]")];
  const panels = [...demo.querySelectorAll("[role=tabpanel]")];
  const play = demo.querySelector("[data-play-toggle]");
  const playLabel = play.querySelector("[data-play-label]");
  const announcement = demo.querySelector("[data-demo-announcement]");
  const slider = demo.querySelector("#demo-progress");
  const elapsed = demo.querySelector("[data-demo-time]");
  const evidence = demo.querySelector(".evidence-dialog");
  const duration = 9000;
  const chapterStarts = [0, .34, .69];
  const chapterStops = [.11, .5, 1];
  // The coordinates follow the clarification, implementation and review nodes
  // in the product screenshot. All coordinates are percentages of the canvas.
  const shots = [
    { at: 0, x: 50, y: 50, scale: 1 },
    { at: .2, x: 47.7, y: 52.5, scale: 1.23 },
    { at: .39, x: 56, y: 52.5, scale: 1.35 },
    { at: .62, x: 60.2, y: 52.5, scale: 1.56 },
    { at: .86, x: 71.5, y: 53.2, scale: 1.57 },
    { at: 1, x: 71.5, y: 53.2, scale: 1.46 },
  ];
  let active = -1;
  let progress = 0;
  let frameRequest = 0;
  let scrollRequest = 0;
  let playing = false;

  const clamp = (value, low = 0, high = 1) => Math.min(high, Math.max(low, value));
  const mix = (from, to, delta) => from + (to - from) * delta;

  function selectStage(next) {
    if (next === active) return;
    active = next;
    demo.dataset.step = String(next);
    tabs.forEach((tab, index) => {
      const current = index === next;
      tab.setAttribute("aria-selected", String(current));
      tab.tabIndex = current ? 0 : -1;
      panels[index].hidden = !current;
    });
    if (playing) announcement.textContent = panels[next].querySelector("h2").textContent;
  }

  function render(value, stage) {
    progress = clamp(value);
    selectStage(stage ?? chapterStarts.findLastIndex(start => progress >= start));
    slider.value = String(Math.round(progress * 1000));
    slider.style.setProperty("--progress", (progress * 100).toFixed(1) + "%");
    const second = Math.floor(progress * duration / 1000);
    const label = "0:" + String(second).padStart(2, "0") + " / 0:09";
    if (label !== elapsed.textContent) elapsed.textContent = label;
    const spoken = tabs[active].textContent.trim() + " · " + label;
    if (slider.getAttribute("aria-valuetext") !== spoken) slider.setAttribute("aria-valuetext", spoken);
    if (reducedMotion.matches) {
      camera.style.transform = "";
      return;
    }
    const next = shots.findIndex(shot => shot.at >= progress && shot.at > 0);
    const from = shots[next - 1];
    const to = shots[next];
    const point = (progress - from.at) / (to.at - from.at);
    const smooth = point * point * (3 - 2 * point);
    const scale = mix(from.scale, to.scale, smooth);
    const x = clamp(50 - mix(from.x, to.x, smooth) * scale, (1 - scale) * 100, 0);
    const y = clamp(53 - mix(from.y, to.y, smooth) * scale, (1 - scale) * 100, 0);
    camera.style.transform = `translate3d(${x.toFixed(3)}%, ${y.toFixed(3)}%, 0) scale(${scale.toFixed(4)})`;
  }

  function labelPlayback() {
    playLabel.textContent = playing ? demo.dataset.pause : progress >= 1 ? demo.dataset.replay : demo.dataset.play;
  }

  function pause() {
    cancelAnimationFrame(frameRequest);
    frameRequest = 0;
    playing = false;
    delete demo.dataset.playing;
    labelPlayback();
  }

  function animate(target, milliseconds, stage) {
    const origin = progress;
    const start = performance.now();
    function tick(now) {
      const fraction = clamp((now - start) / milliseconds);
      const easing = playing ? fraction : 1 - (1 - fraction) ** 3;
      render(mix(origin, target, easing), stage);
      if (fraction < 1) frameRequest = requestAnimationFrame(tick);
      else pause();
    }
    frameRequest = requestAnimationFrame(tick);
  }

  function goToStage(next, focus) {
    pause();
    selectStage(next);
    if (focus) tabs[next].focus({ preventScroll: true });
    if (reducedMotion.matches) {
      render(chapterStops[next], next);
      labelPlayback();
    } else {
      animate(chapterStops[next], 500, next);
    }
  }

  play.addEventListener("click", () => {
    if (playing) {
      pause();
      return;
    }
    pause();
    if (progress >= 1) render(0);
    playing = true;
    demo.dataset.playing = "true";
    labelPlayback();
    animate(1, (1 - progress) * duration);
  });

  tabs.forEach((tab, index) => {
    tab.disabled = false;
    tab.addEventListener("click", () => goToStage(index, false));
    tab.addEventListener("keydown", event => {
      const keys = { ArrowLeft: index - 1, ArrowRight: index + 1, Home: 0, End: tabs.length - 1 };
      if (!Object.hasOwn(keys, event.key)) return;
      event.preventDefault();
      goToStage((keys[event.key] + tabs.length) % tabs.length, true);
    });
  });
  slider.addEventListener("input", () => {
    pause();
    render(slider.valueAsNumber / 1000);
    labelPlayback();
  });
  for (const control of demo.querySelectorAll("[data-demo-controls]")) control.hidden = false;
  if (typeof evidence.showModal === "function") {
    const title = evidence.querySelector("h2");
    const content = evidence.querySelector("[data-evidence-body]");
    for (const button of demo.querySelectorAll("[data-evidence-open]")) {
      const template = button.closest("[role=tabpanel]").querySelector("[data-evidence-template]");
      button.disabled = false;
      button.addEventListener("click", () => {
        pause();
        title.textContent = button.textContent;
        content.replaceChildren(template.content.cloneNode(true));
        evidence.showModal();
      });
    }
  }

  function updateScroll() {
    scrollRequest = 0;
    if (!story.classList.contains("story--pinned") || frameRequest || evidence.open) return;
    const top = parseFloat(getComputedStyle(frame).top) || 0;
    const rect = story.getBoundingClientRect();
    frame.style.setProperty("--arrival", clamp((window.innerHeight - rect.top) / (window.innerHeight - top)).toFixed(3));
    const distance = story.offsetHeight - frame.offsetHeight;
    if (rect.top > top || rect.bottom < top || distance <= 0) return;
    render((top - rect.top) / distance);
    labelPlayback();
  }

  function applyPin() {
    const header = document.querySelector(".site-header");
    const fits = frame.offsetHeight <= window.innerHeight - header.offsetHeight + 4;
    story.classList.toggle("story--pinned", pin.matches && fits);
    updateScroll();
  }
  window.addEventListener("scroll", () => {
    if (pin.matches && !scrollRequest) scrollRequest = requestAnimationFrame(updateScroll);
  }, { passive: true });
  pin.addEventListener("change", applyPin);
  window.addEventListener("resize", applyPin, { passive: true });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden && frameRequest) pause();
  });
  render(0);
  applyPin();

  if (!reducedMotion.matches && "IntersectionObserver" in window) {
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("revealed");
        observer.unobserve(entry.target);
      }
    }, { threshold: 0.13 });
    for (const node of document.querySelectorAll("[data-reveal]")) {
      if (node.getBoundingClientRect().top < window.innerHeight) continue;
      node.classList.add("reveal-ready");
      observer.observe(node);
    }
  }
})();
