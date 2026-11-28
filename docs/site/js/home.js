import { mountGatewayMap } from "./gateway-map.js";
import { mountStory } from "./home-story.js";
import { mountRuntimeDeck } from "./runtime-deck.js";

const motion = matchMedia("(prefers-reduced-motion: reduce)");
const network = document.querySelector("[data-network]");
mountGatewayMap(network, motion);
mountRuntimeDeck(document.querySelector("[data-runtime-deck]"), network, motion);

const configuration = document.getElementById("deploy");
function followConfigLink() {
  if (location.hash === "#deploy") configuration.open = true;
}
for (const link of document.querySelectorAll('a[href="#deploy"]')) {
  link.addEventListener("click", () => { configuration.open = true; });
}
window.addEventListener("hashchange", followConfigLink);
followConfigLink();

const access = document.querySelector("[data-access]");
const dialog = access.querySelector("[data-image-dialog]");
const fullImage = dialog.querySelector("img");
let origin = null;
let zoom = null;
let closing = false;

for (const figure of access.querySelectorAll("[data-inspection]")) {
  const viewport = figure.querySelector(".inspection__view");
  const image = viewport.querySelector("img");
  const controls = figure.querySelector("[data-inspect-controls]");
  const markers = figure.querySelector("[data-markers]");
  const buttons = [...controls.querySelectorAll("[data-pose]")];
  const summary = figure.closest("[data-interface]").querySelector(".case-detail p");
  const original = summary.textContent;
  let selected = buttons[1];

  function position() {
    const box = viewport.getBoundingClientRect();
    if (!box.width) return;
    const requested = Number(selected.dataset.scale || 1);
    const scale = requested === 1 ? 1 : requested * Math.min(1.46, Math.max(1, 560 / box.width));
    const offset = (coordinate, zoom) => Math.max(1 - zoom, Math.min(0, .5 - Number(coordinate || .5) * zoom)) * 100;
    const x = selected.dataset.x, y = selected.dataset.y;
    if (figure.hasAttribute("data-guided")) {
      const start = Math.max(1, scale * .63);
      const along = (from, to, unit) => `calc(${from}${unit} + var(--scene-progress, 1) * ${to - from}${unit})`;
      image.style.transform = `translate(${along(offset(x,start),offset(x,scale),"%")},${along(offset(y,start),offset(y,scale),"%")}) scale(${along(start,scale,"")})`;
    } else {
      image.style.transform = `translate(${offset(x, scale)}%, ${offset(y, scale)}%) scale(${scale})`;
    }
  }

  function select(button, guided = false) {
    figure.toggleAttribute("data-guided", guided);
    selected = button;
    buttons.forEach(item => item.setAttribute("aria-pressed", String(item === selected)));
    figure.toggleAttribute("data-inspecting", selected !== buttons[0]);
    summary.textContent = selected.dataset.detail || original;
    position();
  }
  for (const button of buttons) button.addEventListener("click", () => select(button));
  for (const pin of markers.querySelectorAll("[data-pin]")) {
    pin.addEventListener("click", () => {
      const button = buttons[Number(pin.dataset.pin)];
      select(button);
      button.focus({ preventScroll: true });
    });
  }
  figure.addEventListener("keydown", event => {
    if (event.key !== "Escape" || selected === buttons[0]) return;
    select(buttons[0]);
    buttons[0].focus({ preventScroll: true });
  });
  summary.setAttribute("aria-live", "polite");
  select(selected, true);
  controls.hidden = false;
  markers.hidden = false;
  new ResizeObserver(position).observe(viewport);
}

mountStory(access.querySelector("[data-story]"), motion);

// Zoom from the actual thumbnail. Native dialog drives focus, Escape and modality.
function imageMotion(shrink) {
  if (motion.matches || !fullImage.animate) return null;
  const thumbnail = origin.querySelector("img");
  if (!thumbnail) return null;
  const thumb = thumbnail.getBoundingClientRect();
  if (!thumb.width || !thumb.height) return null;
  const full = fullImage.getBoundingClientRect();
  if (!full.width || !full.height) return null;
  const small = `translate(${thumb.left - full.left}px, ${thumb.top - full.top}px) scale(${thumb.width / full.width}, ${thumb.height / full.height})`;
  const clip = origin.getBoundingClientRect();
  const inset = (difference, size) => Math.max(0, difference / size * 100) + "%";
  const crop = `inset(${inset(clip.top - thumb.top, thumb.height)} ${inset(thumb.right - clip.right, thumb.width)} ${inset(thumb.bottom - clip.bottom, thumb.height)} ${inset(clip.left - thumb.left, thumb.width)})`;
  return fullImage.animate([{ transform: small, clipPath: crop }, { transform: "none", clipPath: "inset(0%)" }], {
    duration: shrink ? 200 : 330, direction: shrink ? "reverse" : "normal", easing: "cubic-bezier(.2,.74,.2,1)",
  });
}
function closeImage() {
  if (!dialog.open || closing) return;
  closing = true;
  zoom?.cancel();
  zoom = imageMotion(true);
  if (zoom) zoom.finished.then(() => dialog.close(), () => { if (dialog.open) dialog.close(); });
  else dialog.close();
}
if (typeof dialog.showModal === "function") {
  for (const link of document.querySelectorAll("[data-enlarge]")) {
    link.addEventListener("click", event => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      origin = link;
      fullImage.src = link.href;
      fullImage.alt = link.dataset.imageAlt || link.querySelector("img").alt;
      dialog.setAttribute("aria-label", link.getAttribute("aria-label"));
      dialog.showModal();
      zoom = imageMotion(false);
    });
  }
  dialog.querySelector("[data-close-image]").addEventListener("click", closeImage);
  dialog.addEventListener("cancel", event => { event.preventDefault(); closeImage(); });
  dialog.addEventListener("click", event => { if (event.target === dialog) closeImage(); });
  dialog.addEventListener("close", () => {
    closing = false;
    zoom?.cancel();
    zoom = null;
    origin?.focus({ preventScroll: true });
  });
}
