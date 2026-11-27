const shapes = {
  expand: '<path d="M14 4h6v6M10 20H4v-6m15-9-6 6M5 19l6-6"/>',
  focus: '<path d="M8 3H3v5m13-5h5v5M3 16v5h5m13-5v5h-5"/><circle cx="12" cy="12" r="2.6"/>',
};
export function graphicDefs() {
  return `<svg class="graphic-defs" width="0" height="0" aria-hidden="true"><defs>${Object.entries(shapes).map(([name, shape]) =>
    `<symbol viewBox="0 0 24 24" id="graphic-${name}">${shape}</symbol>`).join("")}</defs></svg>`;
}
export function glyph(name) {
  return `<svg class="glyph" viewBox="0 0 24 24" aria-hidden="true" focusable="false"><use href="#graphic-${name}"></use></svg>`;
}

// Each strand is one continuous route through the drawn gateway. The smaller
// composition is diagonal so the three endpoints retain their scale on a phone.
export function gatewayTraces(compact = false) {
  const prefix = compact ? "mobile" : "wide";
  const strands = Array.from({ length: 23 }, (_, i) => {
    const t = i - 11;
    const d = compact
      ? `M88 ${89+t*.35} C163 ${87+t*3.8} 112 ${144+t*4.5} 188 ${181+t*.7} S252 ${268+t*3.8} 321 ${267+t*.4}`
      : `M164 ${166+t*.6} C292 ${166+t*1.5} 363 ${69+t*7.2} 485 ${119+t*3.8} S564 ${166+t*1.1} 594 ${166+t*.83} C705 ${165+t*4.5} 766 ${256+t*5.6} 878 ${206+t*1.9} S988 ${166+t*.8} 1042 ${166+t*.6}`;
    return `<path d="${d}" pathLength="100"/>`;
  }).join("");
  const gate = compact ? "translate(188 181) rotate(-28) scale(.67)" : "translate(594 166) rotate(-11)";
  const rings = Array.from({ length: 6 }, (_, i) => `<ellipse cx="${i*5-12.5}" cy="0" rx="24" ry="88"/>`).join("");
  return `<svg class="gateway-art gateway-art--${prefix}" viewBox="0 0 ${compact ? "420 380" : "1200 358"}" aria-hidden="true" focusable="false">
    <defs><linearGradient id="flow-${prefix}" x1="0" y1="0" x2="1" y2=".15" gradientUnits="objectBoundingBox"><stop stop-color="#94b8ee"/><stop offset=".48" stop-color="#7962dc"/><stop offset="1" stop-color="#8dacf2"/></linearGradient>
      <g id="strands-${prefix}">${strands}</g></defs>
    <g class="gateway-rings" transform="${gate}">${rings}</g>
    <use href="#strands-${prefix}" class="gateway-wire" stroke="url(#flow-${prefix})"/>
    <use href="#strands-${prefix}" class="gateway-signal"/>
  </svg>`;
}

// The built-in SSH gateway connects an IDE to the same workspace as the agent.
export function sshDrawing(labels, logo) {
  const route = "M160 125H467";
  return `<figure class="ssh-drawing">
    <svg viewBox="0 0 640 252" aria-hidden="true" focusable="false">
      <path class="ssh-guide" d="${route}"/><path class="ssh-trace" pathLength="1" d="${route}"/>
      <g class="ssh-device"><rect x="36" y="73" width="123" height="84" rx="7"/><path d="M45 82h105v65H45zM36 157l-13 13h149l-13-13"/><path class="ssh-feature" d="m76 103 11 10-11 10m18 0h16"/></g>
      <g class="ssh-device"><rect x="284" y="89" width="72" height="72" rx="15"/><image href="${logo}" x="296" y="100" width="48" height="48"/></g>
      <g class="ssh-device"><rect x="468" y="75" width="133" height="94" rx="7"/><path d="M468 95h133m-99 0v74m-23-83h3m5 0h3"/><path class="ssh-feature" d="M515 110h49m-49 13h32m-32 13h54"/></g>
    </svg><figcaption>${labels.map(label => `<span>${label}</span>`).join("")}</figcaption>
  </figure>`;
}
