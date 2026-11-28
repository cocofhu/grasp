export function mountGatewayMap(root, motion) {
  const inputs = [...root.querySelectorAll('input[name="provider"]')];
  const picker = document.querySelector("[data-config-picker]");
  const select = picker.querySelector("select");
  const compact = matchMedia("(max-width: 43rem)");
  let animations = [];

  function stopMotion() {
    animations.forEach(animation => animation.cancel());
    animations = [];
  }
  function sync(animate) {
    const id = inputs.find(input => input.checked).value;
    select.value = id;
    stopMotion();
    if (!animate || motion.matches) return;
    const diagram = root.querySelector(compact.matches ? ".gateway-art--mobile" : ".gateway-art--wide");
    const signal = diagram.querySelector(".gateway-signal");
    const output = root.querySelector(`[data-note="${id}"] .runtime-art`);
    if (!signal.animate) return;
    animations = [
      signal.animate([{ strokeDashoffset: 20, opacity: 0 }, { opacity: .88, offset: .22 }, { strokeDashoffset: -100, opacity: 0 }],
        { duration: 840, easing: "cubic-bezier(.22,.5,.42,1)" }),
      output.animate([{ opacity: .3, transform: "translate(12px,6px)" }, { opacity: 1, transform: "none" }],
        { duration: 440, easing: "cubic-bezier(.19,.78,.25,1)" }),
    ];
  }

  root.addEventListener("change", () => {
    document.querySelector('[name="deploy-example"][value="gateway"]').checked = true;
    sync(true);
  });
  select.addEventListener("change", () => inputs.find(input => input.value === select.value)?.click());
  window.addEventListener("pageshow", () => sync(false));
  motion.addEventListener("change", stopMotion);
  sync(false);
  picker.hidden = false;
  document.querySelector("[data-config-jump]").hidden = true;
}
