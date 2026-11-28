// Keep each example complete in the document. Wide screens share a sticky
// viewing area; small screens and reduced-motion layouts keep the images inline.
export function mountStory(root, motion) {
  const wide = matchMedia("(min-width: 56rem) and (min-height: 38rem) and (prefers-reduced-motion: no-preference)");
  const screen = root.querySelector("[data-screen]");
  const viewport = screen.querySelector("[data-window]");
  const steps = [...root.querySelectorAll("[data-interface]")];
  const visuals = steps.map(step => step.querySelector("[data-visual]"));
  const links = [...screen.querySelectorAll("a")];
  const header = document.querySelector(".site-header");
  const nav = [...header.querySelectorAll("[data-section]")].map(link => ({
    link, section: document.getElementById(link.dataset.section),
  }));
  const reference = document.getElementById("reference");
  let active = -1;
  let frame = 0;
  let lastBlend;

  // Two neighboring screenshots share the same sticky window during a
  // transition. Its clipping edge is tied directly to scroll position.
  function show(from, to, progress) {
    const index = progress < .5 ? from : to;
    const blending = from !== to;
    const key = from + "/" + to + "/" + Math.round(progress * 1000);
    if (key === lastBlend) return;
    lastBlend = key;
    visuals.forEach((visual, i) => {
      visual.hidden = i !== from && i !== to;
      visual.inert = i !== index;
      visual.toggleAttribute("data-incoming", blending && i === to);
      visual.toggleAttribute("data-outgoing", blending && i === from);
    });
    if (blending) {
      viewport.style.setProperty("--curtain", (100 - progress * 100).toFixed(1) + "%");
      viewport.style.setProperty("--recede", (1 - progress * .035).toFixed(4));
    }
    if (active !== index) {
      links.forEach((link, i) => {
        if (i === index) link.setAttribute("aria-current", "step");
        else link.removeAttribute("aria-current");
      });
      active = index;
    }
  }

  function update() {
    frame = 0;
    const headerBottom = header.getBoundingClientRect().bottom;
    const marker = headerBottom + (innerHeight - headerBottom) * .44;
    if (wide.matches) {
      const boxes = steps.map(step => step.getBoundingClientRect());
      const distance = Math.min(264, innerHeight * .32);
      const start = marker + distance / 2;
      const next = Math.max(0, boxes.findLastIndex(box => box.top <= start));
      const fraction = Math.min(1, (start - boxes[next].top) / distance);
      if (next && fraction < 1) show(next - 1, next, fraction);
      else show(next, next, 1);
      const fill = Math.max(.18, Math.min(1, (marker - boxes[active].top) / boxes[active].height));
      screen.style.setProperty("--chapter-fill", fill.toFixed(3));
      // The composer lifts into view, the preview moves closer, and the SSH
      // route draws from the IDE to the workspace. Scrolling back retraces it.
      visuals.forEach((visual, i) => {
        if (visual.hidden) return;
        const progress = Math.max(0, Math.min(1, (marker - boxes[i].top) / (boxes[i].height * .72)));
        visual.style.setProperty("--scene-progress", progress.toFixed(3));
      });
    } else if (!motion.matches) {
      for (const visual of visuals) {
        const box = visual.getBoundingClientRect();
        if (box.bottom < 0 || box.top > innerHeight) continue;
        const progress = Math.min(1, Math.max(0, (innerHeight - box.top) / (box.height + innerHeight * .26)));
        visual.style.setProperty("--scene-progress", progress.toFixed(3));
      }
    }
    const current = reference.getBoundingClientRect().top <= marker ? null :
      nav.filter(({ section }) => section.getBoundingClientRect().top <= marker).at(-1);
    for (const item of nav) {
      if (item === current) item.link.setAttribute("aria-current", "location");
      else item.link.removeAttribute("aria-current");
    }
  }

  function schedule() {
    if (!frame) frame = requestAnimationFrame(update);
  }

  function layout() {
    lastBlend = null;
    active = -1;
    if (wide.matches) {
      visuals.forEach(visual => viewport.append(visual));
      screen.hidden = false;
      root.setAttribute("data-pinned", "");
    } else {
      root.removeAttribute("data-pinned");
      visuals.forEach((visual, i) => {
        visual.style.removeProperty("--scene-progress");
        visual.hidden = false;
        visual.inert = false;
        visual.removeAttribute("data-incoming");
        visual.removeAttribute("data-outgoing");
        steps[i].querySelector(".case-detail").before(visual);
      });
      screen.hidden = true;
    }
    update();
  }

  root.addEventListener("focusin", event => {
    const step = event.target.closest(".story-step");
    if (wide.matches && step) {
      const index = steps.indexOf(step);
      show(index, index, 1);
    }
  });
  wide.addEventListener("change", layout);
  motion.addEventListener("change", layout);
  window.addEventListener("scroll", schedule, { passive: true });
  window.addEventListener("hashchange", schedule);
  document.addEventListener("focusin", schedule);
  window.addEventListener("resize", schedule);
  window.addEventListener("pageshow", schedule);
  const observer = new ResizeObserver(schedule);
  for (const element of [root, header, document.getElementById("deploy")]) observer.observe(element);
  layout();
}
