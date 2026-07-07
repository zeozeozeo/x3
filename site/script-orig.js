(function () {
  const cfg = JSON.parse(
    document.getElementById("x3-site-bootstrap").textContent,
  );

  window.alert = function () {};
  window.confirm = function () {
    return false;
  };
  window.prompt = function () {
    return "";
  };

  let currentPageId = cfg.page_id;
  let role = "follower";
  let ws;
  let cursorFrame = null;
  let cursorPayload = null;
  let progressTimer = null;
  let hasSeenState = false;
  let treeData = Array.isArray(cfg.tree) ? cfg.tree : [];
  let treeQuery = "";
  let activeViewers = 1;

  const overlay = document.createElement("div");
  overlay.id = "x3-site-owner-cursor";
  overlay.style.cssText =
    "position:fixed;left:0;top:0;width:14px;height:14px;border-radius:999px;background:#ff5a36;box-shadow:0 0 0 3px rgba(255,90,54,.25);transform:translate(-9999px,-9999px);pointer-events:none;z-index:2147483647;transition:transform .08s linear";
  document.documentElement.appendChild(overlay);

  const progress = document.createElement("div");
  progress.id = "x3-site-progress";
  progress.style.cssText =
    "position:fixed;left:0;top:0;height:3px;width:0;background:linear-gradient(90deg,#46c2ff,#91ffbe);box-shadow:0 0 12px rgba(70,194,255,.7);z-index:2147483647;opacity:0;transition:width .18s linear,opacity .25s ease";
  document.documentElement.appendChild(progress);

  const spinner = document.createElement("div");
  spinner.id = "x3-site-progress-spinner";
  spinner.style.cssText =
    "position:fixed;right:14px;top:10px;width:16px;height:16px;border-radius:999px;border:2px solid rgba(145,255,190,.18);border-top-color:#91ffbe;border-left-color:#46c2ff;box-shadow:0 0 18px rgba(70,194,255,.35);z-index:2147483647;opacity:0;pointer-events:none;transition:opacity .2s ease;animation:x3-site-spin .7s linear infinite";
  document.documentElement.appendChild(spinner);

  const runtimeStyle = document.createElement("style");
  runtimeStyle.textContent = `
    @keyframes x3-site-spin {
      from { transform: rotate(0deg); }
      to { transform: rotate(360deg); }
    }

    #x3-site-tree button:focus-visible,
    #x3-site-tree-toggle:focus-visible {
      outline: 2px solid rgba(145,255,190,.78);
      outline-offset: 2px;
    }

    #x3-site-tree-list {
      scrollbar-width: thin;
      scrollbar-color: rgba(145,255,190,.35) transparent;
    }
  `;
  document.head.appendChild(runtimeStyle);

  const toastWrap = document.createElement("div");
  toastWrap.id = "x3-site-toasts";
  toastWrap.style.cssText =
    "position:fixed;right:16px;bottom:16px;display:flex;flex-direction:column;gap:10px;z-index:2147483647;pointer-events:none";
  document.documentElement.appendChild(toastWrap);

  const menuButton = document.createElement("button");
  menuButton.id = "x3-site-tree-toggle";
  menuButton.type = "button";
  menuButton.setAttribute("aria-label", "Toggle generated pages");
  menuButton.style.cssText =
    "position:fixed;right:16px;top:42px;width:44px;height:44px;border:1px solid rgba(255,255,255,.08);border-radius:14px;background:rgba(18,22,32,.9);color:#f8fafc;display:flex;align-items:center;justify-content:center;padding:0;box-shadow:0 18px 50px rgba(0,0,0,.28);z-index:2147483647;cursor:pointer;backdrop-filter:blur(12px)";
  menuButton.innerHTML =
    '<svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true"><path d="M3 4.5h12M3 9h12M3 13.5h12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';
  document.documentElement.appendChild(menuButton);

  const treePanel = document.createElement("aside");
  treePanel.id = "x3-site-tree";
  treePanel.setAttribute("aria-label", "Generated pages");
  treePanel.style.cssText =
    "position:fixed;right:16px;top:94px;width:min(380px,calc(100vw - 32px));max-height:min(72vh,680px);background:linear-gradient(180deg,rgba(16,20,31,.97),rgba(9,12,20,.97));color:#e6edf7;border:1px solid rgba(255,255,255,.08);border-radius:20px;box-shadow:0 24px 70px rgba(0,0,0,.35);z-index:2147483646;overflow:hidden;transform:translateX(calc(100% + 24px));opacity:0;transition:transform .22s ease,opacity .22s ease;backdrop-filter:blur(18px)";
  document.documentElement.appendChild(treePanel);

  const treeHeader = document.createElement("div");
  treeHeader.style.cssText =
    "padding:14px 16px 12px;border-bottom:1px solid rgba(255,255,255,.08);display:flex;align-items:center;justify-content:space-between;gap:12px";
  treeHeader.innerHTML =
    '<div><div style="font:700 13px/1.2 system-ui,sans-serif;letter-spacing:.08em;text-transform:uppercase;color:rgba(230,237,247,.68)">Generated Pages</div><div style="font:500 12px/1.4 system-ui,sans-serif;color:rgba(230,237,247,.58);margin-top:4px">Searchable page history</div></div>';
  treePanel.appendChild(treeHeader);

  const treeClose = document.createElement("button");
  treeClose.type = "button";
  treeClose.setAttribute("aria-label", "Close generated pages panel");
  treeClose.style.cssText =
    "width:32px;height:32px;border:0;border-radius:10px;background:rgba(255,255,255,.05);color:#dbe8fb;display:grid;place-items:center;cursor:pointer";
  treeClose.innerHTML =
    '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><path d="M3.5 3.5l7 7m0-7l-7 7" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';
  treeHeader.appendChild(treeClose);

  const treeSearchWrap = document.createElement("div");
  treeSearchWrap.style.cssText =
    "padding:12px 12px 10px;border-bottom:1px solid rgba(255,255,255,.05)";
  treePanel.appendChild(treeSearchWrap);

  const treeSearch = document.createElement("input");
  treeSearch.type = "search";
  treeSearch.placeholder = "Search pages or history";
  treeSearch.setAttribute("aria-label", "Search generated pages");
  treeSearch.style.cssText =
    "width:100%;height:40px;border:1px solid rgba(255,255,255,.08);border-radius:12px;background:rgba(255,255,255,.04);color:#eef5ff;padding:0 14px;font:500 13px/1 system-ui,sans-serif;outline:none";
  treeSearchWrap.appendChild(treeSearch);

  const downloadZipButton = document.createElement("button");
  downloadZipButton.type = "button";
  downloadZipButton.setAttribute(
    "aria-label",
    "Download all generated pages as zip",
  );
  downloadZipButton.style.cssText =
    "width:100%;height:40px;margin-top:10px;border:1px solid rgba(145,255,190,.16);border-radius:12px;background:linear-gradient(135deg,rgba(36,63,84,.75),rgba(18,36,54,.88));color:#eef5ff;padding:0 14px;font:600 13px/1 system-ui,sans-serif;display:flex;align-items:center;justify-content:center;gap:8px;cursor:pointer";
  downloadZipButton.innerHTML =
    '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><path d="M7 2.25v5.5m0 0 2.1-2.1M7 7.75 4.9 5.65M2.5 9.75v.75c0 .69.56 1.25 1.25 1.25h6.5c.69 0 1.25-.56 1.25-1.25v-.75" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg><span>Download as ZIP</span>';
  treeSearchWrap.appendChild(downloadZipButton);

  const treeList = document.createElement("div");
  treeList.id = "x3-site-tree-list";
  treeList.setAttribute("role", "list");
  treeList.style.cssText =
    "padding:10px 10px 14px;overflow:auto;max-height:min(calc(72vh - 120px),560px);display:flex;flex-direction:column;gap:8px";
  treePanel.appendChild(treeList);

  function endpoint(path) {
    return (
      "/site/" +
      cfg.site_id +
      "/" +
      path +
      "?t=" +
      encodeURIComponent(cfg.token)
    );
  }

  function pageURL(pageId) {
    return (
      "/site/" +
      cfg.site_id +
      "/" +
      encodeURIComponent(pageId) +
      "?t=" +
      encodeURIComponent(cfg.token)
    );
  }

  function toggleTree(force) {
    const open =
      typeof force === "boolean" ? force : treePanel.dataset.open !== "true";
    treePanel.dataset.open = open ? "true" : "false";
    treePanel.style.transform = open
      ? "translateX(0)"
      : "translateX(calc(100% + 24px))";
    treePanel.style.opacity = open ? "1" : "0";
  }

  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const wsPath =
      "/site/" +
      cfg.site_id +
      "/ws?t=" +
      encodeURIComponent(cfg.token) +
      "&page=" +
      encodeURIComponent(currentPageId);
    ws = new WebSocket(proto + "://" + location.host + wsPath);
    ws.onmessage = onMessage;
    ws.onclose = function () {
      setTimeout(connect, 1500);
    };
  }

  function onMessage(ev) {
    const msg = JSON.parse(ev.data);

    if (msg.type === "state") {
      const prevRole = role;
      const prevPageId = currentPageId;
      role = msg.role || "follower";

      if (typeof msg.active_viewers === "number") {
        activeViewers = msg.active_viewers;
      }

      if (Array.isArray(msg.tree)) {
        treeData = msg.tree;
        renderTree();
      }

      if (role !== "owner" && (!hasSeenState || prevRole !== "follower")) {
        showToast(
          "Read-only mode: you are watching someone else control this page.",
          "info",
          5000,
        );
      }

      hasSeenState = true;
      if (msg.page_id) {
        currentPageId = msg.page_id;
      }

      if (
        role !== "owner" &&
        msg.page_id &&
        msg.page_id !== prevPageId &&
        msg.page_url
      ) {
        finishProgress();
        location.assign(msg.page_url);
        return;
      }
      return;
    }

    if (msg.type === "cursor" && role !== "owner") {
      let targetX = msg.x;
      let targetY = msg.y;
      let found = false;

      if (msg.selector) {
        try {
          const el = document.querySelector(msg.selector);
          if (el) {
            const rect = el.getBoundingClientRect();
            targetX = rect.left + msg.rx * rect.width;
            targetY = rect.top + msg.ry * rect.height;
            found = true;
          }
        } catch (e) {}
      }

      if (
        !found &&
        typeof msg.x_pct === "number" &&
        typeof msg.y_pct === "number"
      ) {
        targetX = msg.x_pct * window.innerWidth;
        targetY = msg.y_pct * window.innerHeight;
      }

      overlay.style.transform =
        "translate(" +
        Math.round(targetX) +
        "px," +
        Math.round(targetY) +
        "px)";
      return;
    }

    if (msg.type === "generation_start") {
      startProgress(msg.estimated_ms || cfg.default_estimate_ms || 9000);
      return;
    }

    if (msg.type === "generation_error") {
      finishProgress(true);
      showToast(msg.error || "Failed to generate the page.", "error");
      return;
    }

    if (msg.type === "ownership_changed") {
      role = msg.role || role;
      if (role === "owner") {
        showToast(
          msg.toast || "You are now controlling the page.",
          "success",
          5000,
        );
      }
      return;
    }

    if (msg.type === "expired") {
      finishProgress(true);
      document.body.innerHTML =
        '<main style="font-family:sans-serif;padding:40px;max-width:720px;margin:0 auto"><h1>Site expired</h1><p>' +
        escapeHtml(msg.error || "This dynamic site has expired.") +
        "</p></main>";
    }
  }

  function send(msg) {
    if (ws && ws.readyState === 1) {
      ws.send(JSON.stringify(msg));
    }
  }

  function escapeHtml(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
  }

  function iconSVG(kind) {
    if (kind === "success") {
      return '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><path d="M2.5 7.5l2.7 2.7 6-6" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    }
    if (kind === "error") {
      return '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><path d="M7 4v3.2m0 2.6h.01M7 1.75l5.4 9.35a.85.85 0 0 1-.74 1.27H2.34a.85.85 0 0 1-.74-1.27L7 1.75Z" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    }
    return '<svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><circle cx="7" cy="7" r="5.2" stroke="currentColor" stroke-width="1.35"/><path d="M7 6.2V9m0-3.95h.01" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';
  }

  function pageIconSVG(active) {
    return (
      '<svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">' +
      '<path d="M5 2.5h4.9L13 5.6v6.9c0 .8-.6 1.5-1.4 1.5H5c-.8 0-1.5-.7-1.5-1.5V4c0-.8.7-1.5 1.5-1.5Z" stroke="' +
      (active ? "#91ffbe" : "rgba(230,237,247,.72)") +
      '" stroke-width="1.2" stroke-linejoin="round"/>' +
      '<path d="M9.8 2.6V5.2H12.4" stroke="' +
      (active ? "#91ffbe" : "rgba(230,237,247,.72)") +
      '" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"/>' +
      "</svg>"
    );
  }

  function historySummary(history) {
    if (!Array.isArray(history) || history.length === 0) {
      return "Root page";
    }
    if (history.length <= 3) {
      return history.join(" -> ");
    }
    return history.slice(-3).join(" -> ");
  }

  function safeFilename(value) {
    const normalized = String(value || "")
      .trim()
      .replace(/[<>:"/\\|?*\x00-\x1f]+/g, "-")
      .replace(/\s+/g, " ")
      .slice(0, 64);
    return normalized || "page";
  }

  function pageFilename(node, index) {
    const prefix = String(index + 1).padStart(3, "0");
    const label = safeFilename(node.label || "page");
    return prefix + "-" + label + "-" + node.id + ".html";
  }

  function documentToHTML(doc) {
    const doctype = doc.doctype
      ? "<!DOCTYPE " +
        doc.doctype.name +
        (doc.doctype.publicId ? ' PUBLIC "' + doc.doctype.publicId + '"' : "") +
        (!doc.doctype.publicId && doc.doctype.systemId ? " SYSTEM" : "") +
        (doc.doctype.systemId ? ' "' + doc.doctype.systemId + '"' : "") +
        ">\n"
      : "<!doctype html>\n";
    return doctype + doc.documentElement.outerHTML;
  }

  function stripBootstrapRuntime(html) {
    const doc = new DOMParser().parseFromString(html, "text/html");
    const bootstrap = doc.getElementById("x3-site-bootstrap");
    if (bootstrap) {
      let next = bootstrap.nextSibling;
      bootstrap.remove();
      while (next && next.nodeType !== 1) {
        const candidate = next;
        next = next.nextSibling;
        candidate.remove();
      }
      if (
        next &&
        next.tagName === "SCRIPT" &&
        /x3-site-tree-toggle|x3-site-progress|Read-only mode/.test(
          next.textContent || "",
        )
      ) {
        next.remove();
      }
    }
    return documentToHTML(doc);
  }

  function encodeUTF8(text) {
    return new TextEncoder().encode(text);
  }

  const crcTable = (function () {
    const table = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) {
        c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      }
      table[n] = c >>> 0;
    }
    return table;
  })();

  function crc32(data) {
    let crc = 0xffffffff;
    for (let i = 0; i < data.length; i++) {
      crc = crcTable[(crc ^ data[i]) & 0xff] ^ (crc >>> 8);
    }
    return (crc ^ 0xffffffff) >>> 0;
  }

  function dosDateTime(date) {
    const year = Math.max(1980, date.getFullYear());
    const dosTime =
      ((date.getHours() & 31) << 11) |
      ((date.getMinutes() & 63) << 5) |
      (Math.floor(date.getSeconds() / 2) & 31);
    const dosDate =
      (((year - 1980) & 127) << 9) |
      (((date.getMonth() + 1) & 15) << 5) |
      (date.getDate() & 31);
    return { dosTime: dosTime, dosDate: dosDate };
  }

  function setUint16(view, offset, value) {
    view.setUint16(offset, value & 0xffff, true);
  }

  function setUint32(view, offset, value) {
    view.setUint32(offset, value >>> 0, true);
  }

  function zipEntry(name, data, offset, modifiedAt) {
    const nameBytes = encodeUTF8(name);
    const body = data instanceof Uint8Array ? data : encodeUTF8(String(data));
    const checksum = crc32(body);
    const stamp = dosDateTime(modifiedAt || new Date());
    const local = new Uint8Array(30 + nameBytes.length + body.length);
    const localView = new DataView(local.buffer);
    setUint32(localView, 0, 0x04034b50);
    setUint16(localView, 4, 20);
    setUint16(localView, 6, 0x0800);
    setUint16(localView, 8, 0);
    setUint16(localView, 10, stamp.dosTime);
    setUint16(localView, 12, stamp.dosDate);
    setUint32(localView, 14, checksum);
    setUint32(localView, 18, body.length);
    setUint32(localView, 22, body.length);
    setUint16(localView, 26, nameBytes.length);
    setUint16(localView, 28, 0);
    local.set(nameBytes, 30);
    local.set(body, 30 + nameBytes.length);

    const central = new Uint8Array(46 + nameBytes.length);
    const centralView = new DataView(central.buffer);
    setUint32(centralView, 0, 0x02014b50);
    setUint16(centralView, 4, 20);
    setUint16(centralView, 6, 20);
    setUint16(centralView, 8, 0x0800);
    setUint16(centralView, 10, 0);
    setUint16(centralView, 12, stamp.dosTime);
    setUint16(centralView, 14, stamp.dosDate);
    setUint32(centralView, 16, checksum);
    setUint32(centralView, 20, body.length);
    setUint32(centralView, 24, body.length);
    setUint16(centralView, 28, nameBytes.length);
    setUint16(centralView, 30, 0);
    setUint16(centralView, 32, 0);
    setUint16(centralView, 34, 0);
    setUint16(centralView, 36, 0);
    setUint32(centralView, 38, 0);
    setUint32(centralView, 42, offset);
    central.set(nameBytes, 46);

    return {
      local: local,
      central: central,
      size: local.length,
    };
  }

  function buildZip(files) {
    let offset = 0;
    const locals = [];
    const centrals = [];
    files.forEach(function (file) {
      const entry = zipEntry(file.name, file.data, offset, file.modifiedAt);
      locals.push(entry.local);
      centrals.push(entry.central);
      offset += entry.size;
    });

    let centralSize = 0;
    centrals.forEach(function (entry) {
      centralSize += entry.length;
    });

    const end = new Uint8Array(22);
    const endView = new DataView(end.buffer);
    setUint32(endView, 0, 0x06054b50);
    setUint16(endView, 4, 0);
    setUint16(endView, 6, 0);
    setUint16(endView, 8, files.length);
    setUint16(endView, 10, files.length);
    setUint32(endView, 12, centralSize);
    setUint32(endView, 16, offset);
    setUint16(endView, 20, 0);

    return new Blob(locals.concat(centrals, [end]), {
      type: "application/zip",
    });
  }

  function triggerDownload(blob, filename) {
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(function () {
      URL.revokeObjectURL(url);
    }, 1000);
  }

  async function downloadSiteZip() {
    if (!Array.isArray(treeData) || treeData.length === 0) {
      showToast("There are no generated pages to download yet.", "info", 4000);
      return;
    }

    downloadZipButton.disabled = true;
    downloadZipButton.style.opacity = "0.72";
    downloadZipButton.style.cursor = "progress";
    downloadZipButton.querySelector("span").textContent = "Preparing ZIP...";

    try {
      const pages = treeData.slice().sort(function (a, b) {
        if (a.depth !== b.depth) {
          return a.depth - b.depth;
        }
        return String(a.label || "").localeCompare(String(b.label || ""));
      });
      const files = [];

      for (let i = 0; i < pages.length; i++) {
        const node = pages[i];
        const res = await fetch(pageURL(node.id), {
          credentials: "same-origin",
        });
        if (!res.ok) {
          throw new Error("Failed to fetch page " + (node.label || node.id));
        }
        const html = stripBootstrapRuntime(await res.text());
        files.push({
          name: pageFilename(node, i),
          data: encodeUTF8(html),
          modifiedAt: new Date(),
        });
      }

      files.push({
        name: "manifest.json",
        data: encodeUTF8(
          JSON.stringify(
            {
              site_id: cfg.site_id,
              exported_at: new Date().toISOString(),
              page_count: pages.length,
              pages: pages.map(function (node, index) {
                return {
                  id: node.id,
                  parent_id: node.parent_id || "",
                  depth: node.depth,
                  label: node.label || "Page",
                  history: Array.isArray(node.history) ? node.history : [],
                  filename: pageFilename(node, index),
                };
              }),
            },
            null,
            2,
          ),
        ),
        modifiedAt: new Date(),
      });

      triggerDownload(
        buildZip(files),
        safeFilename("site-" + cfg.site_id) + ".zip",
      );
      showToast(
        "Downloaded " + pages.length + " pages as a ZIP archive.",
        "success",
        5000,
      );
    } catch (err) {
      showToast(
        (err && err.message) || "Failed to build the ZIP archive.",
        "error",
      );
    } finally {
      downloadZipButton.disabled = false;
      downloadZipButton.style.opacity = "1";
      downloadZipButton.style.cursor = "pointer";
      downloadZipButton.querySelector("span").textContent = "Download as ZIP";
    }
  }

  function renderTree() {
    treeList.innerHTML = "";

    if (!Array.isArray(treeData) || treeData.length === 0) {
      const empty = document.createElement("div");
      empty.style.cssText =
        "padding:14px 12px;color:rgba(230,237,247,.6);font:13px/1.5 system-ui,sans-serif";
      empty.textContent = "No generated pages yet.";
      treeList.appendChild(empty);
      return;
    }

    const query = treeQuery.trim().toLowerCase();
    const items = treeData
      .slice()
      .sort(function (a, b) {
        if (a.id === currentPageId) {
          return -1;
        }
        if (b.id === currentPageId) {
          return 1;
        }
        return String(a.label || "").localeCompare(String(b.label || ""));
      })
      .filter(function (node) {
        if (!query) {
          return true;
        }
        const haystack = [
          node.label || "",
          historySummary(node.history),
          Array.isArray(node.history) ? node.history.join(" ") : "",
        ]
          .join(" ")
          .toLowerCase();
        return haystack.indexOf(query) !== -1;
      });

    if (!items.length) {
      const emptySearch = document.createElement("div");
      emptySearch.style.cssText =
        "padding:18px 12px;color:rgba(230,237,247,.58);font:13px/1.5 system-ui,sans-serif";
      emptySearch.textContent = "No pages match that search.";
      treeList.appendChild(emptySearch);
      return;
    }

    items.forEach(function (node) {
      const active = node.id === currentPageId;
      const item = document.createElement("button");
      item.type = "button";
      item.setAttribute("role", "listitem");
      item.style.cssText =
        "width:100%;text-align:left;border:1px solid " +
        (active ? "rgba(145,255,190,.28)" : "rgba(255,255,255,.05)") +
        ";border-radius:16px;padding:12px 13px;background:" +
        (active
          ? "linear-gradient(135deg,rgba(49,90,126,.52),rgba(26,48,76,.7))"
          : "rgba(255,255,255,.03)") +
        ";color:#eef5ff;cursor:pointer;display:flex;flex-direction:column;gap:7px";

      const titleRow = document.createElement("div");
      titleRow.style.cssText = "display:flex;align-items:center;gap:8px";

      const icon = document.createElement("span");
      icon.style.cssText = "display:grid;place-items:center;flex:none";
      icon.innerHTML = pageIconSVG(active);
      titleRow.appendChild(icon);

      const title = document.createElement("div");
      title.style.cssText =
        "font:600 13px/1.35 system-ui,sans-serif;min-width:0;flex:1";
      title.textContent = node.label || "Page";
      titleRow.appendChild(title);

      const badge = document.createElement("span");
      badge.style.cssText =
        "flex:none;padding:3px 8px;border-radius:999px;background:rgba(255,255,255,.06);color:rgba(230,237,247,.62);font:700 10px/1 system-ui,sans-serif";
      badge.textContent = active ? "Current" : "Page";
      titleRow.appendChild(badge);

      const meta = document.createElement("div");
      meta.style.cssText =
        "font:12px/1.45 system-ui,sans-serif;color:rgba(230,237,247,.58);padding-left:24px";
      meta.textContent = historySummary(node.history);

      const path = document.createElement("div");
      path.style.cssText =
        "padding-left:24px;color:rgba(145,255,190,.72);font:700 10px/1.2 system-ui,sans-serif;letter-spacing:.08em;text-transform:uppercase";
      path.textContent = node.depth > 0 ? "Depth " + node.depth : "Root";

      item.appendChild(titleRow);
      item.appendChild(meta);
      item.appendChild(path);
      item.addEventListener("click", function () {
        navigateToPage(node.id);
      });
      treeList.appendChild(item);
    });
  }

  function showToast(message, level, durationMs) {
    const palette = {
      info: {
        bg: "linear-gradient(135deg,rgba(44,112,182,.96),rgba(28,57,112,.96))",
        border: "rgba(123,198,255,.5)",
        iconBg: "rgba(255,255,255,.18)",
      },
      success: {
        bg: "linear-gradient(135deg,rgba(25,122,72,.96),rgba(11,74,51,.96))",
        border: "rgba(132,247,188,.45)",
        iconBg: "rgba(255,255,255,.16)",
      },
      error: {
        bg: "linear-gradient(135deg,rgba(160,47,47,.97),rgba(111,20,20,.97))",
        border: "rgba(255,168,168,.42)",
        iconBg: "rgba(255,255,255,.16)",
      },
    };

    const theme = palette[level] || palette.info;
    const toast = document.createElement("div");
    toast.style.cssText =
      "min-width:280px;max-width:420px;padding:14px 16px;border-radius:16px;background:" +
      theme.bg +
      ";color:#fff;font:14px/1.5 system-ui,sans-serif;box-shadow:0 18px 48px rgba(0,0,0,.3);border:1px solid " +
      theme.border +
      ";pointer-events:auto;opacity:0;transform:translateY(8px);transition:opacity .18s ease,transform .18s ease;display:flex;align-items:flex-start;gap:12px";

    const icon = document.createElement("div");
    icon.style.cssText =
      "width:24px;height:24px;border-radius:999px;background:" +
      theme.iconBg +
      ";display:flex;align-items:center;justify-content:center;flex:none";
    icon.innerHTML = iconSVG(level);

    const text = document.createElement("div");
    text.style.cssText = "flex:1;white-space:pre-wrap";
    text.textContent = message;

    toast.appendChild(icon);
    toast.appendChild(text);
    toastWrap.appendChild(toast);

    requestAnimationFrame(function () {
      toast.style.opacity = "1";
      toast.style.transform = "translateY(0)";
    });

    setTimeout(
      function () {
        toast.style.opacity = "0";
        toast.style.transform = "translateY(8px)";
        setTimeout(function () {
          toast.remove();
        }, 220);
      },
      Math.max(Number(durationMs) || 10000, 1200),
    );
  }

  function startProgress(estimateMs) {
    clearInterval(progressTimer);
    const total = Math.max(Number(estimateMs) || 0, 1200);
    const started = Date.now();
    progress.style.opacity = "1";
    spinner.style.opacity = "1";
    progress.style.width = "2%";
    progressTimer = setInterval(function () {
      const elapsed = Date.now() - started;
      const ratio = Math.min(elapsed / total, 0.94);
      progress.style.width = 2 + ratio * 92 + "%";
    }, 120);
  }

  function finishProgress(immediate) {
    clearInterval(progressTimer);
    progressTimer = null;
    progress.style.width = "100%";
    spinner.style.opacity = "0";
    setTimeout(
      function () {
        progress.style.opacity = "0";
        progress.style.width = "0";
      },
      immediate ? 0 : 180,
    );
  }

  function hrefLooksExternal(href) {
    return /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(String(href || "").trim());
  }

  function parseInlineNavigationTarget(value) {
    const source = String(value || "").trim();
    if (!source) {
      return "";
    }

    const patterns = [
      /(?:window|document)?\.?location(?:\.href)?\s*=\s*(['"])(.*?)\1/i,
      /(?:window|document)?\.?location\.(?:assign|replace)\(\s*(['"])(.*?)\1\s*\)/i,
    ];

    for (let i = 0; i < patterns.length; i++) {
      const match = source.match(patterns[i]);
      if (match && match[2]) {
        return match[2].trim();
      }
    }

    return "";
  }

  function navigationTargetFor(el) {
    if (!el) {
      return null;
    }

    if (el.tagName === "A") {
      const href = (el.getAttribute("href") || "").trim();
      return {
        kind: "anchor",
        element: el,
        href: href,
        intent:
          el.getAttribute("aria-label") ||
          el.getAttribute("title") ||
          (el.textContent || "").trim() ||
          href ||
          "",
      };
    }

    const inlineHref = parseInlineNavigationTarget(el.getAttribute("onclick"));
    if (!inlineHref || hrefLooksExternal(inlineHref)) {
      return null;
    }

    return {
      kind: "inline",
      element: el,
      href: inlineHref,
      intent:
        el.getAttribute("aria-label") ||
        el.getAttribute("title") ||
        (el.textContent || "").trim() ||
        inlineHref ||
        "",
    };
  }

  async function navigate(target) {
    if (role !== "owner") {
      showToast(
        "Read-only mode: you are watching someone else control this page.",
        "info",
        5000,
      );
      return;
    }

    startProgress(cfg.default_estimate_ms || 9000);
    const res = await fetch(endpoint("navigate"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        page_id: currentPageId,
        intent: target.intent || "",
        href: target.href || "",
      }),
    });

    if (!res.ok) {
      finishProgress(true);
      showToast((await res.text()) || "Failed to generate the page.", "error");
      return;
    }

    const data = await res.json();
    finishProgress();
    currentPageId = data.page_id;
    history.pushState({ pageId: data.page_id }, "", data.url);
    location.assign(data.url);
  }

  async function navigateToPage(pageId) {
    if (role !== "owner") {
      showToast(
        "Read-only mode: you are watching someone else control this page.",
        "info",
        5000,
      );
      return;
    }

    const res = await fetch(endpoint("route"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        page_id: pageId,
      }),
    });

    if (!res.ok) {
      showToast(
        (await res.text()) || "Failed to change page history.",
        "error",
      );
      return;
    }

    const data = await res.json();
    currentPageId = data.page_id;
    history.pushState({ pageId: data.page_id }, "", data.url);
    location.assign(data.url);
  }

  document.addEventListener(
    "click",
    function (ev) {
      const target = ev.target.closest && ev.target.closest("a,button");
      if (!target) {
        return;
      }

      const navTarget = navigationTargetFor(target);
      if (!navTarget) {
        return;
      }
      if (
        target.dataset.x3External === "true" ||
        target.target === "_blank" ||
        target.hasAttribute("download") ||
        ev.metaKey ||
        ev.ctrlKey ||
        ev.shiftKey ||
        ev.altKey
      ) {
        return;
      }
      ev.preventDefault();
      ev.stopPropagation();
      if (typeof ev.stopImmediatePropagation === "function") {
        ev.stopImmediatePropagation();
      }
      navigate(navTarget);
    },
    true,
  );

  window.addEventListener("popstate", async function (ev) {
    const pageId = (ev.state && ev.state.pageId) || currentPageId;
    if (role !== "owner") {
      return;
    }

    const res = await fetch(endpoint("route"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        page_id: pageId,
      }),
    });

    if (!res.ok) {
      showToast(
        (await res.text()) || "Failed to change page history.",
        "error",
      );
      return;
    }

    const data = await res.json();
    currentPageId = data.page_id;
    if (location.href !== data.url) {
      location.assign(data.url);
    }
  });

  let lastSentTime = 0;
  const throttleInterval = 40;
  let pendingCursorTimeout = null;

  function queueCursorSend(payload) {
    const now = Date.now();
    const timeSinceLastSend = now - lastSentTime;

    if (timeSinceLastSend >= throttleInterval) {
      send(payload);
      lastSentTime = now;
      if (pendingCursorTimeout) {
        clearTimeout(pendingCursorTimeout);
        pendingCursorTimeout = null;
      }
    } else {
      if (pendingCursorTimeout) {
        clearTimeout(pendingCursorTimeout);
      }
      pendingCursorTimeout = setTimeout(function () {
        send(payload);
        lastSentTime = Date.now();
        pendingCursorTimeout = null;
      }, throttleInterval - timeSinceLastSend);
    }
  }

  function getUniqueSelector(el) {
    if (!el || el === document.documentElement || el === document.body) {
      return "";
    }
    if (el.id) {
      return "#" + CSS.escape(el.id);
    }
    const parts = [];
    while (
      el &&
      el.nodeType === Node.ELEMENT_NODE &&
      el !== document.body &&
      el !== document.documentElement
    ) {
      if (el.id) {
        parts.unshift("#" + CSS.escape(el.id));
        break;
      }
      let tagName = el.nodeName.toLowerCase();
      let sibling = el;
      let index = 1;
      while ((sibling = sibling.previousElementSibling)) {
        if (sibling.nodeName.toLowerCase() === tagName) {
          index++;
        }
      }
      parts.unshift(tagName + ":nth-of-type(" + index + ")");
      el = el.parentElement;
    }
    return parts.join(" > ");
  }

  document.addEventListener(
    "mousemove",
    function (ev) {
      if (role !== "owner" || activeViewers <= 1) {
        return;
      }

      const target = ev.target;
      const rect =
        target && target.getBoundingClientRect
          ? target.getBoundingClientRect()
          : null;

      let selector = "";
      let rx = 0;
      let ry = 0;

      if (
        rect &&
        rect.width > 0 &&
        rect.height > 0 &&
        target !== document.documentElement &&
        target !== document.body
      ) {
        try {
          selector = getUniqueSelector(target);
          rx =
            Math.round(((ev.clientX - rect.left) / rect.width) * 10000) / 10000;
          ry =
            Math.round(((ev.clientY - rect.top) / rect.height) * 10000) / 10000;
        } catch (err) {
          selector = "";
        }
      }

      const x_pct =
        Math.round((ev.clientX / window.innerWidth) * 10000) / 10000;
      const y_pct =
        Math.round((ev.clientY / window.innerHeight) * 10000) / 10000;

      const payload = {
        type: "cursor",
        page_id: currentPageId,
        x: Math.round(ev.clientX),
        y: Math.round(ev.clientY),
        x_pct: x_pct,
        y_pct: y_pct,
        selector: selector,
        rx: rx,
        ry: ry,
      };

      queueCursorSend(payload);
    },
    { passive: true },
  );

  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape") {
      toggleTree(false);
    }
  });

  menuButton.addEventListener("click", function () {
    toggleTree();
  });

  treeClose.addEventListener("click", function () {
    toggleTree(false);
  });

  treeSearch.addEventListener("input", function () {
    treeQuery = treeSearch.value || "";
    renderTree();
  });

  downloadZipButton.addEventListener("click", function () {
    downloadSiteZip();
  });

  renderTree();
  setInterval(function () {
    send({ type: "ping", page_id: currentPageId });
    if (Date.now() > cfg.expires_at) {
      location.reload();
    }
  }, 5000);

  history.replaceState({ pageId: currentPageId }, "", location.href);
  connect();
})();
