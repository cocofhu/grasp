// The cover and the configuration diagram select the same example. Without
// script each printed sheet still has a link to its backend's deployment guide.
export function mountRuntimeDeck(deck, network, motion) {
  const tabs = [...deck.querySelectorAll('input[name="cover-runtime"]')];
  const routes = [...network.querySelectorAll('input[name="provider"]')];
  const sheets = [...deck.querySelectorAll("[data-sheet]")];
  const stage = deck.querySelector("[data-sheets]");
  const positions = ["front", "right", "back", "left"];
  let press;
  let dragged = false;
  let current = -1;

  function render() {
    const active = routes.findIndex(input => input.checked);
    if (active < 0) return;
    tabs[active].checked = true;
    sheets.forEach((sheet, index) => {
      const position = (index - active + sheets.length) % sheets.length;
      sheet.dataset.slot = positions[position];
      sheet.setAttribute("aria-hidden", String(index !== active));
      const link = sheet.querySelector("[data-sheet-config]");
      link.tabIndex = index === active ? 0 : -1;
      link.href = "#deploy";
    });
    if (current !== -1 && current !== active && !motion.matches) {
      for (const leader of sheets[active].querySelectorAll(".leader")) {
        leader.animate?.([{ strokeDasharray: "1", strokeDashoffset: 1 }, { strokeDasharray: "1", strokeDashoffset: 0 }],
          { duration: 360, delay: 190, easing: "ease-out", fill: "backwards" });
      }
    }
    current = active;
  }

  function choose(id) {
    const route = routes.find(input => input.value === id);
    if (route && !route.checked) route.click();
  }
  deck.addEventListener("change", event => choose(event.target.value));
  network.addEventListener("change", render);
  window.addEventListener("pageshow", render);
  stage.addEventListener("pointerdown", event => {
    if (event.isPrimary && event.button === 0) {
      dragged = false;
      press = { id: event.pointerId, x: event.clientX, y: event.clientY };
    }
  });
  stage.addEventListener("pointerup", event => {
    if (!press || event.pointerId !== press.id) return;
    const dx = event.clientX - press.x;
    const dy = event.clientY - press.y;
    press = null;
    if (Math.abs(dx) < 40 || Math.abs(dx) < 1.45 * Math.abs(dy)) return;
    dragged = true;
    const current = tabs.findIndex(input => input.checked);
    choose(tabs[(current + (dx < 0 ? 1 : -1) + tabs.length) % tabs.length].value);
  });
  stage.addEventListener("pointercancel", () => { press = null; });
  stage.addEventListener("click", event => {
    if (dragged && event.detail > 0) { event.preventDefault(); dragged = false; return; }
    const sheet = event.target.closest("[data-sheet]");
    if (!sheet) return;
    if (sheet.dataset.slot !== "front") {
      event.preventDefault();
      choose(sheet.dataset.sheet);
    } else if (event.target.closest("[data-sheet-config]")) {
      document.querySelector('[name="deploy-example"][value="gateway"]').checked = true;
    }
  });
  render();
  deck.setAttribute("data-stacked", "");
}
