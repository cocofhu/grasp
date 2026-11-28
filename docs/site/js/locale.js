/**
 * Docs/site locale: grasp-locale (zh-CN | en), migrating approving-locale once.
 * Priority: localStorage > navigator (zh* → zh-CN, en* → en, else zh-CN).
 * Home entry (/ and /en/) may redirect once; deep links never auto-rewrite.
 */
(() => {
  const STORAGE_KEY = "grasp-locale";
  const LEGACY_KEY = "approving-locale";

  function getSaved() {
    try {
      let v = localStorage.getItem(STORAGE_KEY);
      if (v !== "zh-CN" && v !== "en") {
        const legacy = localStorage.getItem(LEGACY_KEY);
        if (legacy === "zh-CN" || legacy === "en") {
          localStorage.setItem(STORAGE_KEY, legacy);
          localStorage.removeItem(LEGACY_KEY);
          v = legacy;
        }
      } else if (localStorage.getItem(LEGACY_KEY) != null) {
        localStorage.removeItem(LEGACY_KEY);
      }
      if (v === "zh-CN" || v === "en") return v;
    } catch {
      /* private mode / opaque origin */
    }
    return null;
  }

  function setSaved(loc) {
    if (loc !== "zh-CN" && loc !== "en") return;
    try {
      localStorage.setItem(STORAGE_KEY, loc);
      localStorage.removeItem(LEGACY_KEY);
    } catch {
      /* ignore */
    }
  }

  function fromNavigator() {
    const lang = (navigator.language || "zh-CN").toLowerCase();
    if (lang.startsWith("zh")) return "zh-CN";
    if (lang.startsWith("en")) return "en";
    return "zh-CN";
  }

  function pageLocale() {
    const lang = (document.documentElement.lang || "zh-CN").toLowerCase();
    if (lang === "en" || lang.startsWith("en-")) return "en";
    return "zh-CN";
  }

  function wireSwitcher() {
    document.querySelectorAll("[data-locale-set]").forEach((el) => {
      el.addEventListener("click", () => {
        const loc = el.getAttribute("data-locale-set");
        if (loc === "zh-CN" || loc === "en") setSaved(loc);
        if (document.documentElement.hasAttribute("data-home-entry")) {
          const next = new URL(el.href);
          next.search = location.search;
          next.hash = location.hash;
          el.href = next.href;
        }
      });
    });
  }

  /**
   * Home entry matrix (at most one redirect):
   * - saved en on / → /en/
   * - saved zh-CN on /en/ → /
   * - no saved: en* on / → /en/; zh* or other stay on /
   * - no saved on /en/: stay (do not bounce zh* away)
   *
   * Resolve relative to the current home so project Pages prefixes also work.
   */
  function maybeRedirectHome() {
    if (document.documentElement.getAttribute("data-home-entry") !== "1") return;

    const page = pageLocale();
    const saved = getSaved();

    function redirect(path) {
      const next = new URL(path, location.href);
      next.search = location.search;
      next.hash = location.hash;
      location.replace(next);
    }

    if (saved === "en" && page === "zh-CN") {
      redirect("en/");
      return;
    }
    if (saved === "zh-CN" && page === "en") {
      redirect("../");
      return;
    }
    if (!saved && page === "zh-CN" && fromNavigator() === "en") {
      redirect("en/");
    }
  }

  // Script loads in <head>; bind switcher after [data-locale-set] exists in body.
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", wireSwitcher);
  } else {
    wireSwitcher();
  }
  maybeRedirectHome();
})();
