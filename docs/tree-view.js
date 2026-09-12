// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Shared parse-result viewer for the sandbox pages. The parse comes back as a
// preorder event stream whose leaf and skip lengths tile the input in bytes
// (docs/tree-stream.md). decodeEvents turns it into rows: one per open, leaf
// and skip event, each with its depth, byte span and, for leaves and skips,
// its text.
(function (global) {
  function decodeEvents(events, input) {
    const bytes = new TextEncoder().encode(input);
    const decoder = new TextDecoder();
    const slice = (a, b) => decoder.decode(bytes.subarray(a, b));
    const rows = [];
    const open = [];
    let cur = 0;
    for (const e of events) {
      if (e.op === "close") {
        const r = open.pop();
        if (r) r.end = cur;
        continue;
      }
      const row = { op: e.op, kind: e.kind || "", name: e.name || "", depth: open.length, start: cur, end: cur, text: null };
      rows.push(row);
      if (e.op === "open") {
        open.push(row);
      } else {
        cur += e.len || 0;
        row.end = cur;
        row.text = slice(row.start, cur);
      }
    }
    while (open.length) open.pop().end = cur;
    for (const r of rows) if (r.op === "open") r.text = slice(r.start, r.end);
    return rows;
  }

  // Whitespace made visible: a space, tab or newline inside a snippet is
  // easy to miss, and skip rows are nothing else.
  function snippet(text) {
    const frag = document.createDocumentFragment();
    const glyphs = { " ": "\u2423", "\t": "\u21e5", "\n": "\u23ce", "\r": "\u240d" };
    let run = "";
    const flush = () => { if (run) { frag.appendChild(document.createTextNode(run)); run = ""; } };
    for (const ch of text) {
      if (glyphs[ch]) {
        flush();
        const g = document.createElement("span");
        g.className = "ws";
        g.textContent = glyphs[ch];
        frag.appendChild(g);
        if (ch === "\n") frag.appendChild(document.createTextNode("\n"));
      } else {
        run += ch;
      }
    }
    flush();
    return frag;
  }

  function renderResult(out, data, input) {
    out.textContent = "";
    out.classList.remove("ok", "fail");
    out.classList.add(data.ok ? "ok" : "fail");
    const rows = decodeEvents(data.events || [], input);

    const status = document.createElement("div");
    status.className = "status";
    const verdict = document.createElement("span");
    verdict.className = "verdict";
    verdict.textContent = data.ok ? "match" : "no match";
    status.appendChild(verdict);
    const count = document.createElement("span");
    count.className = "count";
    const nodes = rows.filter((r) => r.op !== "skip").length;
    count.textContent = nodes + (nodes === 1 ? " node" : " nodes") + (data.packed ? ", packed " + data.packed : "");
    status.appendChild(count);
    const rawBtn = document.createElement("button");
    rawBtn.type = "button";
    rawBtn.textContent = "events JSON";
    status.appendChild(rawBtn);
    if (!data.ok) {
      const err = document.createElement("div");
      err.className = "error";
      err.textContent = data.error || "failed";
      status.appendChild(err);
    }
    out.appendChild(status);

    const table = document.createElement("table");
    table.className = "tree";
    const tbody = document.createElement("tbody");
    rows.forEach(function (r, i) {
      const tr = document.createElement("tr");
      tr.dataset.depth = r.depth;
      tr.className = r.op === "open" ? "interior" : r.op === "skip" ? "skip" : "leaf";
      const n = document.createElement("td");
      n.className = "n";
      n.style.paddingLeft = "calc(0.5rem + " + r.depth + " * var(--indent))";
      const tg = document.createElement("span");
      tg.className = "tg";
      tg.textContent = r.op === "open" ? "\u25be" : "";
      n.appendChild(tg);
      if (r.op === "skip") {
        n.appendChild(document.createTextNode("\u00b7"));
      } else {
        const kind = document.createElement("span");
        kind.className = "kind";
        kind.textContent = r.kind;
        n.appendChild(kind);
        if (r.name) {
          const name = document.createElement("span");
          name.className = "name";
          name.textContent = r.name;
          n.appendChild(name);
        }
      }
      const t = document.createElement("td");
      t.className = "t";
      if (r.op === "open") {
        t.textContent = r.start + "\u2013" + r.end;
      } else {
        t.appendChild(snippet(r.text));
      }
      tr.appendChild(n);
      tr.appendChild(t);
      if (r.op === "open") {
        tr.addEventListener("click", function () { toggle(i); });
      }
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    out.appendChild(table);

    const collapsed = new Set();
    function toggle(i) {
      if (collapsed.has(i)) collapsed.delete(i); else collapsed.add(i);
      const trs = tbody.children;
      let hideDepth = -1;
      rows.forEach(function (r, j) {
        const tr = trs[j];
        if (hideDepth >= 0 && r.depth > hideDepth) {
          tr.hidden = true;
          return;
        }
        hideDepth = -1;
        tr.hidden = false;
        if (r.op !== "open") return;
        const isCollapsed = collapsed.has(j);
        tr.querySelector(".tg").textContent = isCollapsed ? "\u25b8" : "\u25be";
        const cell = tr.querySelector(".t");
        cell.textContent = "";
        if (isCollapsed) {
          cell.appendChild(snippet(r.text));
          hideDepth = r.depth;
        } else {
          cell.textContent = r.start + "\u2013" + r.end;
        }
      });
    }

    const raw = document.createElement("pre");
    raw.className = "raw";
    raw.hidden = true;
    raw.textContent = (data.events || []).map((e) => JSON.stringify(e)).join("\n");
    out.appendChild(raw);
    rawBtn.addEventListener("click", function () {
      raw.hidden = !raw.hidden;
      rawBtn.textContent = raw.hidden ? "events JSON" : "hide JSON";
    });
  }

  global.xbnfTree = { decodeEvents: decodeEvents, snippet: snippet, renderResult: renderResult };
})(this);
