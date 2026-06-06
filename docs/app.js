const root = document.documentElement;
const body = document.body;
const navToggle = document.querySelector(".nav-toggle");
const themeToggle = document.querySelector(".theme-toggle");
const themeLabel = document.querySelector(".theme-label");
const themeMeta = document.querySelector('meta[name="theme-color"]');
const sidebar = document.querySelector("#sidebar");
const searchInput = document.querySelector("#doc-search");
const searchStatus = document.querySelector("#search-status");
const sections = Array.from(document.querySelectorAll(".doc-section"));
const navLinks = Array.from(document.querySelectorAll(".sidebar-nav a"));
const codeBlocks = Array.from(document.querySelectorAll("pre code"));
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");

let emptyState = null;
let activeHashLockUntil = 0;

function preferredTheme() {
  const saved = localStorage.getItem("tiny-docs-theme");
  if (saved === "light" || saved === "dark") return saved;
  return systemDark.matches ? "dark" : "light";
}

function applyTheme(theme, persist = true) {
  root.dataset.theme = theme;
  themeToggle?.setAttribute("aria-pressed", String(theme === "dark"));
  themeToggle?.setAttribute("aria-label", `Switch to ${theme === "dark" ? "light" : "dark"} mode`);
  if (themeLabel) themeLabel.textContent = theme === "dark" ? "Dark" : "Light";
  if (themeMeta) themeMeta.setAttribute("content", theme === "dark" ? "#0d1413" : "#f6f4ee");
  if (persist) localStorage.setItem("tiny-docs-theme", theme);
}

applyTheme(preferredTheme(), false);

systemDark.addEventListener("change", () => {
  if (!localStorage.getItem("tiny-docs-theme")) applyTheme(preferredTheme(), false);
});

themeToggle?.addEventListener("click", () => {
  applyTheme(root.dataset.theme === "dark" ? "light" : "dark");
});

function closeNav() {
  body.classList.remove("nav-open");
  navToggle?.setAttribute("aria-expanded", "false");
}

function setActiveLink(hash) {
  navLinks.forEach((link) => {
    link.classList.toggle("active", link.getAttribute("href") === hash);
  });
}

navToggle?.addEventListener("click", () => {
  const isOpen = body.classList.toggle("nav-open");
  navToggle.setAttribute("aria-expanded", String(isOpen));
});

sidebar?.addEventListener("click", (event) => {
  const link = event.target.closest("a");
  if (link) {
    const href = link.getAttribute("href");
    setActiveLink(href);
    activeHashLockUntil = Date.now() + 1800;
    closeNav();
  }
});

window.addEventListener("hashchange", () => {
  if (location.hash) {
    setActiveLink(location.hash);
    activeHashLockUntil = Date.now() + 1800;
  }
});

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") closeNav();
  if (event.key === "/" && document.activeElement !== searchInput && !event.ctrlKey && !event.metaKey) {
    event.preventDefault();
    searchInput?.focus();
  }
});

function escapeHtml(value) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function renderInlineCode() {
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (!node.nodeValue.includes("`")) return NodeFilter.FILTER_REJECT;
      if (node.parentElement?.closest("pre, code, script, style, textarea")) {
        return NodeFilter.FILTER_REJECT;
      }
      return NodeFilter.FILTER_ACCEPT;
    },
  });
  const nodes = [];

  while (walker.nextNode()) nodes.push(walker.currentNode);

  nodes.forEach((node) => {
    const parts = node.nodeValue.split(/(`[^`\n]+`)/g);
    if (parts.length < 2) return;

    const fragment = document.createDocumentFragment();
    parts.forEach((part) => {
      if (part.startsWith("`") && part.endsWith("`")) {
        const code = document.createElement("code");
        code.className = "inline-code";
        code.textContent = part.slice(1, -1);
        fragment.appendChild(code);
      } else if (part) {
        fragment.appendChild(document.createTextNode(part));
      }
    });

    node.parentNode.replaceChild(fragment, node);
  });
}

function tokenClass(token) {
  if (token.startsWith("//") || token.startsWith("#")) return "tok-comment";
  if (token.startsWith("`")) return "tok-template";
  if (token.startsWith('"') || token.startsWith("'")) return "tok-string";
  if (/^\d/.test(token)) return "tok-number";
  if (/^(string|number|bool|object|array|function|buffer|error|any)$/.test(token)) return "tok-type";
  if (/^[+\-*/%=!<>?:|&.]+$/.test(token)) return "tok-operator";
  if (/^[A-Za-z_]\w*$/.test(token) && !keywordSet.has(token)) return "tok-function";
  return "tok-keyword";
}

const keywordSet = new Set([
  "fn", "native", "go", "import", "std", "export", "const", "let", "return",
  "if", "else", "while", "for", "in", "break", "continue", "match", "enum",
  "interface", "class", "field", "embed", "try", "catch", "finally", "throw",
  "defer", "spawn", "await", "lock", "new", "true", "false", "null", "this",
  "as", "from", "private", "public", "iota", "and", "or", "not", "instanceof",
  "typeof",
]);

function highlightCode(source) {
  const tokenPattern =
    /(\/\/.*|#.*|`(?:\\.|[^`])*`|"(?:\\.|[^"])*"|'(?:\\.|[^'])*'|\b(?:fn|native|go|import|std|export|const|let|return|if|else|while|for|in|break|continue|match|enum|interface|class|field|embed|try|catch|finally|throw|defer|spawn|await|lock|new|true|false|null|this|as|from|private|public|iota|and|or|not|instanceof|typeof)\b|\b(?:string|number|bool|object|array|function|buffer|error|any)\b|\b[A-Za-z_]\w*(?=\s*\()|\b\d+(?:\.\d+)?\b|[+\-*\/%=!<>?:|&.]+)/g;
  let highlighted = "";
  let cursor = 0;

  for (const match of source.matchAll(tokenPattern)) {
    const token = match[0];
    const index = match.index;
    highlighted += escapeHtml(source.slice(cursor, index));
    highlighted += `<span class="${tokenClass(token)}">${escapeHtml(token)}</span>`;
    cursor = index + token.length;
  }

  highlighted += escapeHtml(source.slice(cursor));
  return highlighted;
}

function inferCodeLabel(block, source) {
  const explicit = block.parentElement?.dataset.codeLabel || block.dataset.codeLabel;
  if (explicit) return explicit;

  const trimmed = source.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) return "json";
  if (/^(#|cd |tiny|\.\/tiny|build\.bat|chmod |\.\/build\.sh)/m.test(trimmed)) return "terminal";
  if (trimmed.includes("package main") || trimmed.includes("func ") || trimmed.includes('import "C"')) return "plugin.go";
  if (trimmed.includes("TinyPluginCall") || trimmed.includes("TinyPluginFree")) return "plugin ABI";
  if (trimmed.includes("tiny.json") && trimmed.includes("src/")) return "project tree";
  if (trimmed.includes("import plugin")) return "wrapper.tiny";
  if (trimmed.includes("native fn")) return "native.tiny";
  if (trimmed.includes("enum ")) return "main.tiny";
  if (trimmed.includes("import std") || trimmed.includes("fn ") || trimmed.includes("let ")) return "main.tiny";
  return "code";
}

async function copyText(value) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(value);
    return;
  }

  const textarea = document.createElement("textarea");
  textarea.value = value;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  document.execCommand("copy");
  textarea.remove();
}

function setupCodeBlock(block, source) {
  const pre = block.closest("pre");
  if (!pre || pre.dataset.enhanced === "true") return;

  pre.dataset.enhanced = "true";
  const label = inferCodeLabel(block, source);
  const button = document.createElement("button");
  button.type = "button";
  button.className = "copy-button";
  button.textContent = "Copy";
  button.setAttribute("aria-label", `Copy ${label} code`);

  button.addEventListener("click", async () => {
    try {
      await copyText(source);
      button.textContent = "Copied";
      button.classList.add("copied");
      window.setTimeout(() => {
        button.textContent = "Copy";
        button.classList.remove("copied");
      }, 1400);
    } catch {
      button.textContent = "Failed";
      window.setTimeout(() => {
        button.textContent = "Copy";
      }, 1400);
    }
  });

  const heroHead = pre.closest(".hero-panel")?.querySelector(".panel-head");
  if (heroHead) {
    heroHead.appendChild(button);
    return;
  }

  const toolbar = document.createElement("div");
  toolbar.className = "code-toolbar";
  toolbar.innerHTML = `<span>${escapeHtml(label)}</span>`;
  toolbar.appendChild(button);

  pre.classList.add("code-with-toolbar");

  if (pre.parentElement?.classList.contains("split")) {
    const wrapper = document.createElement("div");
    wrapper.className = "code-block-wrap";
    pre.parentNode.insertBefore(wrapper, pre);
    wrapper.appendChild(toolbar);
    wrapper.appendChild(pre);
    return;
  }

  pre.parentNode.insertBefore(toolbar, pre);
}

function buildSectionPagers() {
  const pages = navLinks
    .map((link) => {
      const id = link.getAttribute("href")?.slice(1);
      const section = id ? document.getElementById(id) : null;
      return section ? { id, label: link.textContent.trim(), section } : null;
    })
    .filter(Boolean);

  pages.forEach((page, index) => {
    const pager = document.createElement("nav");
    pager.className = "section-pager";
    pager.setAttribute("aria-label", `${page.label} section navigation`);

    const previous = pages[index - 1];
    const next = pages[index + 1];

    if (previous) {
      const previousLink = document.createElement("a");
      previousLink.className = "pager-link previous";
      previousLink.href = `#${previous.id}`;
      previousLink.innerHTML = `<span>Previous</span><strong>${escapeHtml(previous.label)}</strong>`;
      pager.appendChild(previousLink);
    }

    if (next) {
      const nextLink = document.createElement("a");
      nextLink.className = "pager-link next";
      nextLink.href = `#${next.id}`;
      nextLink.innerHTML = `<span>Next</span><strong>${escapeHtml(next.label)}</strong>`;
      pager.appendChild(nextLink);
    }

    page.section.appendChild(pager);
  });
}

renderInlineCode();

codeBlocks.forEach((block) => {
  const source = block.textContent;
  block.dataset.source = source;
  block.innerHTML = highlightCode(source);
  setupCodeBlock(block, source);
});

buildSectionPagers();

const observer = new IntersectionObserver(
  (entries) => {
    if (location.hash && Date.now() < activeHashLockUntil) {
      setActiveLink(location.hash);
      return;
    }

    const visible = entries
      .filter((entry) => entry.isIntersecting && entry.target.id && !entry.target.classList.contains("hidden"))
      .sort((a, b) => Math.abs(a.boundingClientRect.top) - Math.abs(b.boundingClientRect.top))[0];

    if (!visible) return;

    setActiveLink(`#${visible.target.id}`);
  },
  {
    rootMargin: "-12% 0px -70% 0px",
    threshold: [0.01, 0.08, 0.2],
  }
);

sections.forEach((section) => observer.observe(section));

function ensureEmptyState() {
  if (emptyState) return emptyState;

  emptyState = document.createElement("div");
  emptyState.className = "empty-state";
  emptyState.hidden = true;
  emptyState.textContent = "No matching docs sections. Try syntax, http server, type hints, pack, native, or webview.";

  const content = document.querySelector(".content");
  const footer = document.querySelector(".footer");
  content?.insertBefore(emptyState, footer);

  return emptyState;
}

function normalize(value) {
  return value
    .toLowerCase()
    .replace(/[`"'(){}[\],.;:]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function sectionText(section) {
  const codeText = Array.from(section.querySelectorAll("pre code"))
    .map((block) => block.dataset.source || block.textContent || "")
    .join(" ");
  return normalize(`${section.textContent || ""} ${section.dataset.search || ""} ${codeText}`);
}

function runSearch() {
  const query = normalize(searchInput?.value || "");
  const terms = query ? query.split(" ").filter(Boolean) : [];
  const empty = ensureEmptyState();
  let visibleCount = 0;

  sections.forEach((section) => {
    const text = sectionText(section);
    const matches = terms.length === 0 || terms.every((term) => text.includes(term));
    section.classList.toggle("hidden", !matches);
    if (matches) visibleCount += 1;
  });

  navLinks.forEach((link) => {
    const id = link.getAttribute("href")?.slice(1);
    const section = id ? document.getElementById(id) : null;
    link.hidden = Boolean(section?.classList.contains("hidden"));
  });

  empty.hidden = visibleCount !== 0;

  if (searchStatus) {
    if (terms.length === 0) {
      searchStatus.textContent = "Showing all sections";
    } else {
      searchStatus.textContent = `${visibleCount} section${visibleCount === 1 ? "" : "s"} found`;
    }
  }
}

searchInput?.addEventListener("input", runSearch);
