(function () {
  const u = () => window.VPNViewUtils;
  let users = [];
  let timer = null;
  let search = "";
  let bound = false;

  async function refresh(silent = false) {
    try {
      const payload = await window.api.users();
      users = payload.users || [];
      render();
    } catch (err) {
      if (!silent) u().toast(`用户加载失败: ${err.message}`, "error");
    }
  }

  function isMobile() {
    return window.innerWidth <= 768;
  }

  function render() {
    const caps = window.app.capabilities;
    const createBtn = document.getElementById("btn-create-user");
    if (createBtn) createBtn.classList.toggle("hidden", !caps.add_user);

    const filtered = users.filter(user => {
      const q = search.toLowerCase();
      return !q || (user.name || "").toLowerCase().includes(q) || (user.id || "").toLowerCase().includes(q);
    });

    if (isMobile()) {
      renderMobile(filtered, caps);
    } else {
      renderDesktop(filtered, caps);
    }
  }

  /* ============================================================
     DESKTOP: classic table rendering (unchanged logic)
     ============================================================ */
  function renderDesktop(filtered, caps) {
    const head = document.getElementById("users-head");
    const body = document.getElementById("users-body");
    if (!head || !body) return;

    // Remove mobile list if it exists
    const mobileList = document.querySelector(".mobile-user-list");
    if (mobileList) mobileList.remove();

    const cols = ["名称", "ID"];
    if (caps.user_speed) cols.push("速率");
    cols.push("流量", "配额", "限速", "过期时间", "状态", "操作");
    head.innerHTML = `<tr>${cols.map(col => `<th>${col}</th>`).join("")}</tr>`;

    if (!filtered.length) {
      body.innerHTML = `<tr><td colspan="99" class="empty-state">未找到用户。</td></tr>`;
      return;
    }
    body.innerHTML = filtered.map(user => row(user, caps)).join("");
    body.querySelectorAll("[data-action]").forEach(btn => btn.addEventListener("click", onAction));
  }

  function row(user, caps) {
    const total = (user.upload || 0) + (user.download || 0);
    const quotaPct = user.quota > 0 ? Math.min(100, total / user.quota * 100) : 0;
    const status = user.enabled
      ? `<span class="badge ok">已启用</span>`
      : `<span class="badge bad">已禁用</span>`;
    const traffic = caps.query_traffic
      ? `<div class="limit-info">
          <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z"/></svg> ${u().formatBytes(user.upload)}</div>
          <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M20 12l-1.41-1.41L13 16.17V4h-2v12.17l-5.58-5.59L4 12l8 8 8-8z"/></svg> ${u().formatBytes(user.download)}</div>
         </div>`
      : `<span class="faint">不支持</span>`;
    const quota = caps.query_traffic
      ? user.quota > 0
        ? `<div class="progress"><span style="width:${quotaPct}%"></span></div><div class="faint mono" style="font-size:12px;">${u().formatBytes(total)} / ${u().formatBytes(user.quota)}</div>`
        : `<span class="faint">无限制</span>`
      : `<span class="faint">已禁用</span>`;
    const speed = caps.user_speed
      ? `<td class="mono" data-label="速率">
          <div class="limit-info">
            <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z"/></svg> ${u().formatSpeed(user.speed_up)}</div>
            <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M20 12l-1.41-1.41L13 16.17V4h-2v12.17l-5.58-5.59L4 12l8 8 8-8z"/></svg> ${u().formatSpeed(user.speed_down)}</div>
          </div>
         </td>`
      : "";
    const limitHint = caps.speed_limit ? "原生支持" : "软件监控";
    const upLimit = user.speed_limit_up ? u().formatSpeed(user.speed_limit_up) : "无限制";
    const downLimit = user.speed_limit_down ? u().formatSpeed(user.speed_limit_down) : "无限制";
    const limit = `<div class="limit-info mono">
        <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z"/></svg> ${upLimit}</div>
        <div class="limit-row"><svg viewBox="0 0 24 24"><path d="M20 12l-1.41-1.41L13 16.17V4h-2v12.17l-5.58-5.59L4 12l8 8 8-8z"/></svg> ${downLimit}</div>
      </div>
      <div class="badge micro">${limitHint}</div>`;

    return `<tr>
      <td data-label="名称" style="font-weight:500;">${u().escapeHtml(user.name || user.id)}</td>
      <td data-label="ID" class="mono faint">${u().escapeHtml(user.id)}</td>
      ${speed}
      <td data-label="流量" class="mono">${traffic}</td>
      <td data-label="配额">${quota}</td>
      <td data-label="限速">${limit}</td>
      <td data-label="过期时间" class="faint">${u().escapeHtml(u().formatDate(user.expire_at))}</td>
      <td data-label="状态">${status}</td>
      <td data-label="操作"><div class="actions">${desktopActions(user, caps)}</div></td>
    </tr>`;
  }

  function desktopActions(user, caps) {
    const iconEdit = `<svg viewBox="0 0 24 24"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34a.9959.9959 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"/></svg>`;
    const iconDisable = `<svg viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm5 11H7v-2h10v2z"/></svg>`;
    const iconEnable = `<svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>`;
    const iconLink = `<svg viewBox="0 0 24 24"><path d="M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z"/></svg>`;
    const iconDelete = `<svg viewBox="0 0 24 24"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>`;
    
    const items = [
      `<button class="btn ghost icon" title="编辑" data-action="edit" data-id="${u().escapeHtml(user.id)}">${iconEdit}</button>`
    ];
    if (caps.disable_user || caps.enable_user) {
      const icon = user.enabled ? iconDisable : iconEnable;
      const title = user.enabled ? "禁用" : "启用";
      items.push(`<button class="btn ghost icon" title="${title}" data-action="toggle" data-id="${u().escapeHtml(user.id)}">${icon}</button>`);
    }
    if (caps.subscription) {
      items.push(`<button class="btn ghost icon" title="复制订阅链接" data-action="sub" data-id="${u().escapeHtml(user.id)}">${iconLink}</button>`);
    }
    if (caps.remove_user) {
      items.push(`<button class="btn danger icon" title="删除" data-action="delete" data-id="${u().escapeHtml(user.id)}">${iconDelete}</button>`);
    }
    return items.join("");
  }

  /* ============================================================
     MOBILE: compact card with tap-to-expand in-place actions
     ============================================================ */
  function renderMobile(filtered, caps) {
    const tablePanel = document.querySelector(".table-panel");
    if (!tablePanel) return;

    // Reuse or create mobile list container (sibling to table-scroll)
    let mobileList = tablePanel.querySelector(".mobile-user-list");
    if (!mobileList) {
      mobileList = document.createElement("div");
      mobileList.className = "mobile-user-list";
      tablePanel.appendChild(mobileList);
    }

    if (!filtered.length) {
      mobileList.innerHTML = `<div class="empty-state">未找到用户。</div>`;
      return;
    }

    // Preserve expanded state
    const expandedIds = new Set();
    mobileList.querySelectorAll(".m-user-card.expanded").forEach(el => {
      if (el.dataset.uid) expandedIds.add(el.dataset.uid);
    });

    mobileList.innerHTML = filtered.map(user => mobileCard(user, caps, expandedIds.has(user.id))).join("");

    // Bind card head tap => toggle expand
    mobileList.querySelectorAll(".m-user-head").forEach(head => {
      head.addEventListener("click", () => {
        const card = head.closest(".m-user-card");
        card.classList.toggle("expanded");
      });
    });

    // Bind action buttons
    mobileList.querySelectorAll("[data-action]").forEach(btn => btn.addEventListener("click", onAction));
  }

  function mobileCard(user, caps, expanded) {
    const esc = u().escapeHtml;
    const initial = (user.name || user.id || "?").charAt(0).toUpperCase();
    const statusBadge = user.enabled
      ? `<span class="badge ok" style="font-size:11px;padding:2px 8px;">启用</span>`
      : `<span class="badge bad" style="font-size:11px;padding:2px 8px;">禁用</span>`;

    // Credential UUID display
    const creds = user.credentials || {};
    const uuid = creds.uuid || creds.password || creds.ss_password || user.id || "";
    const uuidDisplay = uuid.length > 24 ? uuid.slice(0, 12) + "…" + uuid.slice(-6) : uuid;

    // Stats
    const total = (user.upload || 0) + (user.download || 0);
    const quotaPct = user.quota > 0 ? Math.min(100, total / user.quota * 100) : 0;

    // Stat cells
    let statsHTML = "";
    if (caps.query_traffic) {
      statsHTML += `
        <div class="m-user-stat"><div class="m-user-stat-label">上传</div><div class="m-user-stat-value">${u().formatBytes(user.upload)}</div></div>
        <div class="m-user-stat"><div class="m-user-stat-label">下载</div><div class="m-user-stat-value">${u().formatBytes(user.download)}</div></div>`;
    }
    if (caps.user_speed) {
      statsHTML += `
        <div class="m-user-stat"><div class="m-user-stat-label">上传速率</div><div class="m-user-stat-value">${u().formatSpeed(user.speed_up)}</div></div>
        <div class="m-user-stat"><div class="m-user-stat-label">下载速率</div><div class="m-user-stat-value">${u().formatSpeed(user.speed_down)}</div></div>`;
    }
    statsHTML += `
      <div class="m-user-stat"><div class="m-user-stat-label">过期时间</div><div class="m-user-stat-value">${esc(u().formatDate(user.expire_at))}</div></div>
      <div class="m-user-stat"><div class="m-user-stat-label">限速</div><div class="m-user-stat-value">${user.speed_limit_up ? u().formatSpeed(user.speed_limit_up) : "无限"} / ${user.speed_limit_down ? u().formatSpeed(user.speed_limit_down) : "无限"}</div></div>`;

    // Quota bar
    let quotaHTML = "";
    if (caps.query_traffic && user.quota > 0) {
      quotaHTML = `<div class="m-quota-bar">
        <div class="progress"><span style="width:${quotaPct}%"></span></div>
        <div class="m-quota-text">${u().formatBytes(total)} / ${u().formatBytes(user.quota)}</div>
      </div>`;
    }

    // Action buttons (in-place, not a popup)
    const iconEdit = `<svg viewBox="0 0 24 24"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34a.9959.9959 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"/></svg>`;
    const iconDisable = `<svg viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm5 11H7v-2h10v2z"/></svg>`;
    const iconEnable = `<svg viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>`;
    const iconLink = `<svg viewBox="0 0 24 24"><path d="M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z"/></svg>`;
    const iconDelete = `<svg viewBox="0 0 24 24"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>`;

    let actionsHTML = `<button class="m-action-btn" data-action="edit" data-id="${esc(user.id)}">${iconEdit} 编辑</button>`;
    if (caps.disable_user || caps.enable_user) {
      const icon = user.enabled ? iconDisable : iconEnable;
      const label = user.enabled ? "禁用" : "启用";
      actionsHTML += `<button class="m-action-btn" data-action="toggle" data-id="${esc(user.id)}">${icon} ${label}</button>`;
    }
    if (caps.subscription) {
      actionsHTML += `<button class="m-action-btn" data-action="sub" data-id="${esc(user.id)}">${iconLink} 订阅</button>`;
    }
    if (caps.remove_user) {
      actionsHTML += `<button class="m-action-btn danger" data-action="delete" data-id="${esc(user.id)}">${iconDelete} 删除</button>`;
    }

    const chevronSVG = `<svg class="m-user-chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>`;

    return `<div class="m-user-card${expanded ? ' expanded' : ''}" data-uid="${esc(user.id)}">
      <div class="m-user-head">
        <div class="m-user-avatar">${esc(initial)}</div>
        <div class="m-user-info">
          <div class="m-user-name">${esc(user.name || user.id)} ${statusBadge}</div>
          <div class="m-user-uuid">${esc(uuidDisplay)}</div>
        </div>
        ${chevronSVG}
      </div>
      <div class="m-user-body">
        <div class="m-user-stats">${statsHTML}</div>
        ${quotaHTML}
        <div class="m-user-actions">${actionsHTML}</div>
      </div>
    </div>`;
  }

  async function onAction(event) {
    const id = event.currentTarget.dataset.id;
    const action = event.currentTarget.dataset.action;
    const user = users.find(item => item.id === id);
    if (!user) return;
    if (action === "edit") return openEdit(user);
    if (action === "sub") return u().copy(window.api.subscriptionURL(id), "订阅链接");
    if (action === "toggle") {
      try {
        await window.api.updateUser(id, { enabled: !user.enabled });
        u().toast("用户状态已更新", "success");
        await refresh(true);
      } catch (err) {
        u().toast(err.message, "error");
      }
      return;
    }
    if (action === "delete") {
      if (!confirm(`确定删除用户 ${user.name || user.id} 吗？`)) return;
      try {
        await window.api.deleteUser(id);
        u().toast("用户已删除", "success");
        await refresh(true);
      } catch (err) {
        u().toast(err.message, "error");
      }
    }
  }

  function openCreate() {
    const caps = window.app.capabilities;
    const fields = window.app.credentialFields || [];
    openModal("创建用户", formHTML({ fields, caps }), async () => {
      const payload = collectForm(fields, caps);
      await window.api.createUser(payload);
      u().toast("用户已创建", "success");
      await refresh(true);
    });
    wireAutoGenerate(fields);
    wireDependencies(fields);
  }

  function openEdit(user) {
    const caps = window.app.capabilities;
    openModal("编辑用户", editHTML(user, caps), async () => {
      const payload = {
        name: value("name"),
        quota: toBytes(value("quota"), value("quota_unit")),
        speed_limit_up: toBPS(value("limit_up"), value("limit_unit_up")),
        speed_limit_down: toBPS(value("limit_down"), value("limit_unit_down")),
        expire_at: dateValue("expire")
      };
      await window.api.updateUser(user.id, payload);
      u().toast("用户已更新", "success");
      await refresh(true);
    });
  }

  function formHTML({ fields, caps }) {
    return `<div class="form-grid">
      <div class="form-section full">
        <div class="form-section-title">基础信息</div>
        <label class="field full"><span>用户 ID</span><input id="field-id" placeholder="留空则自动生成 UUID 或随机 ID"></label>
        <label class="field"><span>名称</span><input id="field-name" required></label>
      </div>
      <div class="form-section full">
        <div class="form-section-title">连接凭据</div>
        <div class="form-section-note">按所选协议显示对应字段。</div>
        ${fields.map(fieldHTML).join("") || `<div class="hint">当前适配器不需要额外凭据。</div>`}
      </div>
      <div class="form-section full">
        <div class="form-section-title">配额与限速</div>
        ${quotaHTML()}
        ${limitHTML(caps)}
        <label class="field"><span>过期时间</span><input id="field-expire" type="date"><span class="hint">留空表示永不过期。</span></label>
      </div>
    </div>`;
  }

  function editHTML(user, caps) {
    return `<div class="form-grid">
      <div class="form-section full">
        <div class="form-section-title">基础信息</div>
        <label class="field full"><span>名称</span><input id="field-name" value="${u().escapeHtml(user.name || "")}"></label>
      </div>
      <div class="form-section full">
        <div class="form-section-title">配额与限速</div>
        ${quotaHTML(user.quota)}
        ${limitHTML(caps, user)}
        <label class="field"><span>过期时间</span><input id="field-expire" type="date" value="${user.expire_at ? String(user.expire_at).slice(0, 10) : ""}"><span class="hint">清空表示永不过期。</span></label>
      </div>
    </div>`;
  }

  function fieldHTML(field) {
    const required = field.required ? " required" : "";
    const value = field.default || "";
    const depAttrs = field.depends_on_key 
      ? ` data-depends-key="${u().escapeHtml(field.depends_on_key)}" data-depends-val="${u().escapeHtml(field.depends_on_val)}"` 
      : "";
    if (field.type === "select") {
      return `<label class="field credential-field" id="field-container-${u().escapeHtml(field.key)}"${depAttrs}>
        <span>${u().escapeHtml(field.label)}</span>
        <select id="cred-${u().escapeHtml(field.key)}"${required}>
          ${(field.options || []).map(opt => `<option value="${u().escapeHtml(opt)}"${opt === field.default ? ' selected' : ''}>${u().escapeHtml(opt || "(无)")}</option>`).join("")}
        </select>
      </label>`;
    }
    return `<label class="field credential-field" id="field-container-${u().escapeHtml(field.key)}"${depAttrs}>
      <span>${u().escapeHtml(field.label)}</span>
      <div class="input-with-action">
        <input id="cred-${u().escapeHtml(field.key)}" value="${u().escapeHtml(value)}"${required}>
        ${field.auto_generate ? `<button class="btn ghost" type="button" data-generate="${u().escapeHtml(field.key)}">生成</button>` : ""}
      </div>
    </label>`;
  }

  function quotaHTML(quota = 0) {
    return `<label class="field"><span>流量配额</span><input id="field-quota" type="number" min="0" step="any" value="${quota ? quota / 1073741824 : ""}" placeholder="0 = 无限制"></label>
      <label class="field"><span>配额单位</span><select id="field-quota_unit"><option value="gb">GB</option><option value="tb">TB</option></select></label>`;
  }

  function limitHTML(caps, user = {}) {
    const hint = caps.speed_limit ? "底层适配器提供原生限速。" : "适配器暂无原生限速；由系统监控在软件层面执行限速。";
    return `<label class="field"><span>上传限速</span><input id="field-limit_up" type="number" min="0" step="any" value="${user.speed_limit_up ? user.speed_limit_up / 1048576 : ""}" placeholder="0 = 无限制"></label>
      <label class="field"><span>下载限速</span><input id="field-limit_down" type="number" min="0" step="any" value="${user.speed_limit_down ? user.speed_limit_down / 1048576 : ""}" placeholder="0 = 无限制"></label>
      <label class="field"><span>上传限速单位</span><select id="field-limit_unit_up"><option value="mbs">MB/s</option><option value="kbs">KB/s</option></select><span class="hint">${hint}</span></label>
      <label class="field"><span>下载限速单位</span><select id="field-limit_unit_down"><option value="mbs">MB/s</option><option value="kbs">KB/s</option></select></label>`;
  }

  function collectForm(fields, caps) {
    const credentials = {};
    fields.forEach(field => {
      const container = document.getElementById(`field-container-${field.key}`);
      if (container && container.classList.contains("hidden")) {
        return; // 隐藏字段跳过收集和校验
      }
      const raw = document.getElementById(`cred-${field.key}`)?.value?.trim() || "";
      if (field.required && !raw) throw new Error(`${field.label} 为必填项`);
      if (raw) credentials[field.key] = raw;
    });
    return {
      id: value("id"),
      name: value("name"),
      credentials,
      quota: toBytes(value("quota"), value("quota_unit")),
      speed_limit_up: toBPS(value("limit_up"), value("limit_unit_up")),
      speed_limit_down: toBPS(value("limit_down"), value("limit_unit_down")),
      expire_at: dateValue("expire")
    };
  }

  function openModal(title, body, onSubmit) {
    const root = document.getElementById("modal-root");
    root.innerHTML = `<div class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title">
      <div class="modal-header"><h3 id="modal-title">${u().escapeHtml(title)}</h3><button class="btn ghost icon" data-close aria-label="关闭">×</button></div>
      <div class="modal-body">${body}</div>
      <div class="modal-footer"><button class="btn ghost" data-close>取消</button><button class="btn primary" data-submit>保存</button></div>
    </div>`;
    root.classList.remove("hidden");
    document.body.classList.add("modal-open");
    root.querySelectorAll("[data-close]").forEach(btn => btn.addEventListener("click", closeModal));
    root.addEventListener("click", event => {
      if (event.target === root) closeModal();
    }, { once: true });
    root.querySelector("[data-submit]").addEventListener("click", async () => {
      try {
        await onSubmit();
        closeModal();
      } catch (err) {
        u().toast(err.message, "error");
      }
    });
  }

  function closeModal() {
    document.getElementById("modal-root").classList.add("hidden");
    document.body.classList.remove("modal-open");
  }

  function wireAutoGenerate(fields) {
    fields.forEach(field => {
      if (!field.auto_generate) return;
      document.querySelector(`[data-generate="${field.key}"]`)?.addEventListener("click", () => {
        document.getElementById(`cred-${field.key}`).value = u().generateUUID();
      });
    });
  }

  function wireDependencies(fields) {
    // 收集所有作为依赖源的字段 Key (例如 "protocol")
    const depSources = new Set(fields.filter(f => f.depends_on_key).map(f => f.depends_on_key));
    
    const updateVisibility = () => {
      fields.forEach(field => {
        if (!field.depends_on_key) return;
        const container = document.getElementById(`field-container-${field.key}`);
        const sourceInput = document.getElementById(`cred-${field.depends_on_key}`);
        if (!container || !sourceInput) return;

        const allowed = field.depends_on_val ? field.depends_on_val.split(",").map(item => item.trim()) : [];
        const isMatch = allowed.includes(sourceInput.value);
        container.classList.toggle("hidden", !isMatch);
        container.dataset.dependencyActive = isMatch ? "true" : "false";

        const input = document.getElementById(`cred-${field.key}`);
        if (input) {
          if (isMatch) {
            if (field.required) input.setAttribute("required", "");
          } else {
            input.removeAttribute("required");
            // 当隐藏时重置其值，避免残留垃圾数据
            if (input.tagName === "SELECT") {
              input.selectedIndex = 0;
            } else {
              input.value = "";
            }
          }
        }
      });
    };

    // 绑定 change 事件到各个依赖源
    depSources.forEach(key => {
      const el = document.getElementById(`cred-${key}`);
      el?.addEventListener("change", updateVisibility);
    });

    // 首次载入初始化一次显隐状态
    updateVisibility();
  }

  function value(id) {
    return document.getElementById(`field-${id}`)?.value?.trim() || "";
  }
  function dateValue(id) {
    const raw = value(id);
    return raw ? new Date(`${raw}T00:00:00`).toISOString() : null;
  }
  function toBytes(raw, unit) {
    const n = Number(raw);
    if (!n || n < 0) return 0;
    return Math.round(n * (unit === "tb" ? 1099511627776 : 1073741824));
  }
  function toBPS(raw, unit) {
    const n = Number(raw);
    if (!n || n < 0) return 0;
    return Math.round(n * (unit === "kbs" ? 1024 : 1048576));
  }
  function shortID(id) {
    if (!id) return "-";
    return id.length > 18 ? `${id.slice(0, 10)}...${id.slice(-4)}` : id;
  }

  function start() {
    stop();
    if (!bound) {
      document.getElementById("btn-create-user")?.addEventListener("click", openCreate);
      document.getElementById("btn-refresh-users")?.addEventListener("click", () => refresh(false));
      document.getElementById("users-search")?.addEventListener("input", u().debounce(event => {
        search = event.target.value;
        render();
      }, 150));
      // Re-render on viewport resize to switch between mobile cards and desktop table
      window.addEventListener("resize", onResize);
      bound = true;
    }
    refresh();
    timer = setInterval(() => refresh(true), 5000);
  }

  let lastMobile = null;
  function onResize() {
    const mobile = isMobile();
    if (lastMobile !== null && mobile !== lastMobile) {
      // Viewport crossed breakpoint — force full re-render
      const mobileList = document.querySelector(".mobile-user-list");
      if (mobileList) mobileList.remove();
      render();
    }
    lastMobile = mobile;
  }

  function stop() {
    if (timer) clearInterval(timer);
    timer = null;
  }

  window.UsersView = { start, stop, refresh };
})();
