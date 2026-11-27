(() => {
  const dialog = document.querySelector(".search-dialog");
  const input = dialog.querySelector("input");
  const list = dialog.querySelector("#search-results");
  const status = dialog.querySelector("[data-search-status]");
  const triggers = [...document.querySelectorAll("[data-search-open]")];
  let loading;
  let index;
  let results = [];
  let active = 0;
  let restoreFocus;

  function normalize(value) {
    return value.normalize("NFKC").toLocaleLowerCase().trim();
  }

  function loadIndex() {
    loading ??= fetch(dialog.dataset.index)
      .then(response => {
        if (!response.ok) throw new Error("Search index unavailable");
        return response.json();
      })
      .then(entries => entries.map(entry => ({
        ...entry,
        heading: normalize(entry.title),
        pageTitle: normalize(entry.page),
        terms: normalize(entry.keywords),
        content: normalize(entry.text),
      })))
      .then(entries => {
        index = entries;
        if (dialog.open) renderResults();
      }).catch(() => {
        loading = undefined;
        if (dialog.open) showStatus(dialog.dataset.error);
      });
  }

  function showStatus(message) {
    status.textContent = message;
    status.hidden = !message;
  }

  function rank(entry, words, query) {
    const haystack = [entry.heading, entry.pageTitle, entry.terms, entry.content];
    if (!words.every(word => haystack.some(field => field.includes(word)))) return 0;
    let score = entry.heading === query ? 32 : entry.heading.includes(query) ? 16 : 0;
    if (entry.pageTitle.includes(query)) score += 8;
    if (entry.content.includes(query)) score += 3;
    for (const word of words) {
      if (entry.heading.includes(word)) score += 5;
      if (entry.terms.includes(word)) score += 2;
      if (entry.content.includes(word)) score++;
    }
    return score || 1;
  }

  function snippet(entry, words) {
    const found = words.map(word => entry.content.indexOf(word)).filter(position => position >= 0);
    const start = found.length ? Math.max(0, Math.min(...found) - 36) : 0;
    const text = entry.text.slice(start, start + 126);
    return (start ? "…" : "") + text + (entry.text.length > start + text.length ? "…" : "");
  }

  function choose(index, scroll = false) {
    active = index;
    for (const [i, row] of [...list.children].entries()) {
      row.setAttribute("aria-selected", String(i === index));
    }
    const row = list.children[index];
    if (row) {
      input.setAttribute("aria-activedescendant", row.id);
      if (scroll) row.scrollIntoView({ block: "nearest" });
    } else {
      input.removeAttribute("aria-activedescendant");
    }
  }

  function renderResults() {
    const query = normalize(input.value);
    const words = query.split(/\s+/).filter(Boolean);
    results = query && index ? index
      .map(entry => ({ entry, score: rank(entry, words, query) }))
      .filter(item => item.score)
      .sort((a, b) => b.score - a.score)
      .slice(0, 10)
      .map(item => item.entry) : [];

    list.replaceChildren(...results.map((entry, i) => {
      const item = document.createElement("li");
      item.id = `doc-search-result-${i}`;
      item.setAttribute("role", "option");
      const title = document.createElement("strong");
      title.textContent = entry.title;
      item.append(title);
      const context = document.createElement("span");
      context.textContent = entry.page === entry.title ? snippet(entry, words) : entry.page + " · " + snippet(entry, words);
      item.append(context);
      item.addEventListener("pointermove", () => choose(i));
      item.addEventListener("click", () => openResult(entry.url));
      return item;
    }));
    input.setAttribute("aria-expanded", String(results.length > 0));
    showStatus(query && index && !results.length ? dialog.dataset.empty : "");
    choose(0);
  }

  function closeSearch() {
    dialog.close();
    const target = restoreFocus && restoreFocus !== document.body ? restoreFocus : triggers[0];
    target?.focus({ preventScroll: true });
  }

  function openResult(url) {
    closeSearch();
    location.assign(url);
  }

  function openSearch() {
    if (dialog.open) return;
    restoreFocus = document.activeElement;
    dialog.showModal();
    input.focus();
    input.select();
    renderResults();
    loadIndex();
  }

  if (typeof dialog.showModal === "function") {
    for (const trigger of triggers) {
      trigger.hidden = false;
      trigger.addEventListener("click", openSearch);
    }
    dialog.querySelector("[data-search-close]").addEventListener("click", closeSearch);
    dialog.addEventListener("cancel", event => {
      event.preventDefault();
      closeSearch();
    });
    dialog.addEventListener("click", event => {
      if (event.target === dialog) closeSearch();
    });
    input.addEventListener("input", renderResults);
    input.addEventListener("keydown", event => {
      if (event.isComposing) return;
      if (event.key === "Escape") {
        event.preventDefault();
        closeSearch();
        return;
      }
      if (!results.length) return;
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        choose((active + (event.key === "ArrowDown" ? 1 : results.length - 1)) % results.length, true);
      } else if (event.key === "Enter") {
        event.preventDefault();
        openResult(results[active].url);
      }
    });
    document.addEventListener("keydown", event => {
      const typing = !dialog.contains(event.target) && event.target.closest?.("input, textarea, select, [contenteditable=true]");
      const shortcut = (event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k";
      if (!document.querySelector("dialog[open]") && (shortcut || (event.key === "/" && !typing && !event.altKey))) {
        event.preventDefault();
        openSearch();
      }
    });
  }

  async function copyCode(text) {
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch {
        // An HTTP preview can still support copying via selection.
      }
    }
    const field = document.createElement("textarea");
    field.className = "sr-only";
    field.value = text;
    field.readOnly = true;
    const previous = document.activeElement;
    document.body.append(field);
    field.select();
    let copied = false;
    try {
      copied = document.execCommand("copy");
    } finally {
      field.remove();
      previous?.focus();
    }
    return copied;
  }

  for (const pre of document.querySelectorAll(".doc-content pre, .terminal pre")) {
    const code = pre.querySelector("code");
    if (!code) continue;
    const frame = document.createElement("div");
    frame.className = "code-frame";
    const action = document.createElement("button");
    action.type = "button";
    action.className = "code-copy";
    action.textContent = document.body.dataset.copy;
    const message = document.createElement("span");
    message.className = "sr-only";
    message.setAttribute("role", "status");
    pre.before(frame);
    frame.append(pre, action, message);
    let reset;
    action.addEventListener("click", async () => {
      const copied = await copyCode(code.textContent).catch(() => false);
      message.textContent = copied ? document.body.dataset.copied : document.body.dataset.copyError;
      action.textContent = copied ? document.body.dataset.copied : document.body.dataset.copy;
      if (!copied) {
        const range = document.createRange();
        range.selectNodeContents(code);
        const selection = getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
      }
      clearTimeout(reset);
      reset = setTimeout(() => {
        action.textContent = document.body.dataset.copy;
      }, 1600);
    });
  }

  const outline = [...document.querySelectorAll(".doc-outline a")];
  if (outline.length) {
    const headings = outline.map(link => document.getElementById(decodeURIComponent(link.hash.slice(1))));
    const updateOutline = () => {
      const firstBelow = headings.findIndex(heading => heading.getBoundingClientRect().top > 130);
      const current = firstBelow < 0 ? headings.length - 1 : Math.max(0, firstBelow - 1);
      outline.forEach((link, i) => {
        if (i === current) link.setAttribute("aria-current", "location");
        else link.removeAttribute("aria-current");
      });
    };
    let pending = false;
    window.addEventListener("scroll", () => {
      if (pending) return;
      pending = true;
      requestAnimationFrame(() => {
        updateOutline();
        pending = false;
      });
    }, { passive: true });
    updateOutline();
  }
})();
