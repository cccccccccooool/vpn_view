(function () {
  function formatBytes(bytes, decimals = 1) {
    if (bytes === null || bytes === undefined || Number.isNaN(Number(bytes))) return "-";
    bytes = Number(bytes);
    if (bytes === 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB", "PB"];
    const idx = Math.min(Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024)), units.length - 1);
    return `${(bytes / Math.pow(1024, idx)).toFixed(decimals).replace(/\.0$/, "")} ${units[idx]}`;
  }

  function formatSpeed(value) {
    return `${formatBytes(value)}/s`;
  }

  function formatDate(value) {
    if (!value) return "Never";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "Invalid";
    return date.toLocaleDateString();
  }

  function escapeHtml(value) {
    return String(value ?? "").replace(/[&<>"']/g, ch => ({
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;"
    }[ch]));
  }

  function text(value) {
    return document.createTextNode(String(value ?? ""));
  }

  function clear(node) {
    if (!node) return;
    while (node.firstChild) node.removeChild(node.firstChild);
  }

  function append(parent, children) {
    const items = Array.isArray(children) ? children : [children];
    items.forEach(child => {
      if (child === null || child === undefined || child === false) return;
      if (Array.isArray(child)) append(parent, child);
      else if (child instanceof Node) parent.appendChild(child);
      else parent.appendChild(text(child));
    });
  }

  function setAttrs(node, attrs = {}) {
    Object.entries(attrs || {}).forEach(([key, value]) => {
      if (value === null || value === undefined || value === false) return;
      if (key === "class" || key === "className") {
        const classValue = Array.isArray(value) ? value.filter(Boolean).join(" ") : String(value);
        if (node.namespaceURI === "http://www.w3.org/2000/svg") node.setAttribute("class", classValue);
        else node.className = classValue;
        return;
      }
      if (key === "dataset" || key === "data") {
        Object.entries(value || {}).forEach(([dataKey, dataValue]) => {
          if (dataValue !== null && dataValue !== undefined) node.dataset[dataKey] = String(dataValue);
        });
        return;
      }
      if (key === "style") {
        Object.entries(value || {}).forEach(([styleKey, styleValue]) => {
          if (styleValue !== null && styleValue !== undefined) node.style[styleKey] = String(styleValue);
        });
        return;
      }
      if (key.startsWith("on") && typeof value === "function") {
        node.addEventListener(key.slice(2).toLowerCase(), value);
        return;
      }
      if (value === true) {
        node.setAttribute(key, "");
        return;
      }
      node.setAttribute(key, String(value));
    });
  }

  function el(tag, attrs = {}, children = []) {
    const node = document.createElement(tag);
    setAttrs(node, attrs);
    append(node, children);
    return node;
  }

  function svgIcon(pathD, attrs = {}) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    setAttrs(svg, { viewBox: "0 0 24 24", "aria-hidden": "true", focusable: "false", ...attrs });
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("d", pathD);
    path.setAttribute("fill", "currentColor");
    svg.appendChild(path);
    return svg;
  }

  function setProgressWidth(node, percent) {
    const value = Number(percent);
    const clamped = Number.isFinite(value) ? Math.max(0, Math.min(100, value)) : 0;
    node.style.width = `${clamped}%`;
  }

  function generateUUID() {
    if (crypto.randomUUID) return crypto.randomUUID();
    return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, c => {
      const r = Math.random() * 16 | 0;
      const v = c === "x" ? r : (r & 0x3) | 0x8;
      return v.toString(16);
    });
  }

  function debounce(fn, delay = 250) {
    let timer;
    return (...args) => {
      clearTimeout(timer);
      timer = setTimeout(() => fn(...args), delay);
    };
  }

  function toast(message, type = "info") {
    const root = document.getElementById("toast-root");
    if (!root) return;
    const item = document.createElement("div");
    item.className = `toast ${type}`;
    item.textContent = message;
    root.appendChild(item);
    setTimeout(() => item.remove(), 3500);
  }

  async function copy(value, label = "Text") {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(value);
        toast(`${label} copied`, "success");
        return;
      } catch (err) {
        console.warn("Clipboard API failed", err);
      }
    }

    try {
      const textArea = document.createElement("textarea");
      textArea.value = value;
      textArea.style.position = "absolute";
      textArea.style.left = "-99999px";
      textArea.style.top = `${window.pageYOffset || document.documentElement.scrollTop}px`;
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      textArea.setSelectionRange(0, 99999);
      const successful = document.execCommand("copy");
      document.body.removeChild(textArea);
      if (!successful) throw new Error("execCommand failed");
      toast(`${label} copied`, "success");
    } catch (err) {
      console.warn("Clipboard fallback failed", err);
      toast("Copy failed in this browser context", "error");
    }
  }

  window.VPNViewUtils = {
    formatBytes,
    formatSpeed,
    formatDate,
    escapeHtml,
    text,
    clear,
    append,
    el,
    svgIcon,
    setProgressWidth,
    generateUUID,
    debounce,
    toast,
    copy
  };
})();
