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
    const el = document.createElement("div");
    el.className = `toast ${type}`;
    el.textContent = message;
    root.appendChild(el);
    setTimeout(() => el.remove(), 3500);
  }

  async function copy(text, label = "Text") {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        toast(`${label} 已复制`, "success");
        return;
      } catch (err) {
        console.warn("Clipboard API failed", err);
      }
    }
    
    // Fallback for HTTP (non-secure context)
    try {
      const textArea = document.createElement("textarea");
      textArea.value = text;
      textArea.style.position = "absolute";
      textArea.style.left = "-99999px";
      textArea.style.top = (window.pageYOffset || document.documentElement.scrollTop) + "px";
      document.body.appendChild(textArea);
      textArea.focus();
      textArea.select();
      textArea.setSelectionRange(0, 99999); // For iOS devices
      const successful = document.execCommand('copy');
      document.body.removeChild(textArea);
      if (successful) {
        toast(`${label} 已复制`, "success");
      } else {
        throw new Error('execCommand failed');
      }
    } catch (err) {
      toast(`复制失败: 请手动复制`, "error");
      prompt("您的浏览器不支持自动复制，请手动复制以下内容：", text);
    }
  }

  window.VPNViewUtils = {
    formatBytes,
    formatSpeed,
    formatDate,
    escapeHtml,
    generateUUID,
    debounce,
    toast,
    copy
  };
})();
