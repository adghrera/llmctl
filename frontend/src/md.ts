// Minimal, safe markdown → HTML for chat messages (F067).
// All input is HTML-escaped first, so only the constructs handled below can
// produce markup: fenced code, headings, lists, links, bold/italic, code spans.
// No external dependencies (the UI must stay offline-capable).

function esc(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// Inline pass runs on already-escaped text.
function inline(s: string): string {
  // code spans first; their contents are protected from further styling
  const kept: string[] = [];
  const parts = s.split(/`/);
  for (let i = 1; i < parts.length; i += 2) {
    kept.push(`<code>${parts[i]}</code>`);
    parts[i] = `\x00${kept.length - 1}\x00`;
  }
  s = parts.join("");
  // links: [text](http…) only
  s = s.replace(/\[([^\]]+)\]\((https?:[^)\s]+)\)/g, (_all, text, url) => {
    return `<a href="${url}" target="_blank" rel="noreferrer">${text}</a>`;
  });
  s = s.replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>");
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, "$1<i>$2</i>");
  return s.replace(/\x00(\d+)\x00/g, (_all, n) => kept[+n]);
}

export function renderMd(src: string): string {
  const lines = src.split("\n");
  const out: string[] = [];
  let para: string[] = [];
  let list: string[] = [];
  let listTag = "";
  let fence: string[] | null = null;

  const flushList = () => {
    if (list.length) out.push(`<${listTag}>${list.map((li) => `<li>${li}</li>`).join("")}</${listTag}>`);
    list = [];
    listTag = "";
  };
  const flushPara = () => {
    if (para.length) out.push(`<p>${para.map(inline).join("<br/>")}</p>`);
    para = [];
  };

  for (const raw of lines) {
    if (fence !== null) {
      if (/^```\s*$/.test(raw)) {
        out.push(`<pre class="md-code"><code>${esc(fence.join("\n"))}</code></pre>`);
        fence = null;
      } else {
        fence.push(raw);
      }
      continue;
    }
    const line = raw;
    if (/^```/.test(line)) {
      flushPara();
      flushList();
      fence = [];
      continue;
    }
    const h = line.match(/^(#{1,4}) (.*)$/);
    if (h) {
      flushPara();
      flushList();
      out.push(`<h${h[1].length + 2}>${inline(esc(h[2]))}</h${h[1].length + 2}>`);
      continue;
    }
    const ul = line.match(/^\s*[-*] (.*)$/);
    const ol = line.match(/^\s*\d+\. (.*)$/);
    if (ul || ol) {
      flushPara();
      const tag = ul ? "ul" : "ol";
      if (listTag && listTag !== tag) flushList();
      listTag = tag;
      list.push(inline(esc((ul || ol)![1])));
      continue;
    }
    if (line.trim() === "") {
      flushPara();
      flushList();
      continue;
    }
    list && flushList();
    para.push(esc(line));
  }
  if (fence !== null) out.push(`<pre class="md-code"><code>${esc(fence.join("\n"))}</code></pre>`);
  flushPara();
  flushList();
  return out.join("");
}
