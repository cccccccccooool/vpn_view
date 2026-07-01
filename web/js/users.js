(function () {
  const u = () => window.VPNViewUtils;
  let users = [];
  let timer = null;
  let search = "";
  let bound = false;
  let lastMobile = null;

  const ICONS = {
    edit: "M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34a.9959.9959 0 0 0-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z",
    disable: "M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm5 11H7v-2h10v2z",
    enable: "M8 5v14l11-7z",
    link: "M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z",
    delete: "M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z",
    upload: "M4 12l1.41 1.41L11 7.83V20h2V7.83l5.58 5.59L20 12l-8-8-8 8z",
    download: "M20 12l-1.41-1.41L13 16.17V4h-2v12.17l-5.58-5.59L4 12l8 8 8-8z"
  };

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

  function availableCores() {
    const cores = (window.app.cores || []).filter(core => core.enabled && core.status !== "disabled");
    if (cores.length) return cores;
    return [{
      id: window.app.defaultCore || "",
      type: "default",
      enabled: true,
      status: "ready",
      credential_fields: window.app.credentialFields || [],
      capabilities: window.app.capabilities || {}
    }];
  }

  function coreForID(id) {
    const cores = availableCores();
    return cores.find(core => core.id === id) || cores.find(core => core.id === window.app.defaultCore) || cores[0];
  }

  function fieldsForCore(coreID) {
    const core = coreForID(coreID);
    return core?.credential_fields || window.app.credentialFields || [];
  }

  function capsForCore(coreID) {
    const core = coreForID(coreID);
    return core?.capabilities || window.app.capabilities || {};
  }

  function render() {
    const caps = window.app.capabilities || {};
    document.getElementById("btn-create-user")?.classList.toggle("hidden", !caps.add_user);
    const q = search.toLowerCase();
    const filtered = users.filter(user => !q ||
      (user.name || "").toLowerCase().includes(q) ||
      (user.id || "").toLowerCase().includes(q) ||
      (user.core_id || "").toLowerCase().includes(q) ||
      (user.adapter_type || "").toLowerCase().includes(q)
    );
    if (isMobile()) renderMobile(filtered, caps);
    else renderDesktop(filtered, caps);
  }

  function renderDesktop(filtered, caps) {
    const head = document.getElementById("users-head");
    const body = document.getElementById("users-body");
    if (!head || !body) return;
    document.querySelector(".mobile-user-list")?.remove();

    const cols = ["名称", "ID", "核心"];
    if (caps.user_speed) cols.push("速率");
    cols.push("流量", "配额", "限制", "过期时间", "状态", "操作");
    u().clear(head);
    u().clear(body);
    head.appendChild(u().el("tr", {}, cols.map(col => u().el("th", {}, col))));

    if (!filtered.length) {
      body.appendChild(u().el("tr", {}, [
        u().el("td", { colspan: "99", class: "empty-state" }, "未找到用户。")
      ]));
      return;
    }
    filtered.forEach(user => body.appendChild(userRow(user, caps)));
  }

  function userRow(user, caps) {
    const total = (user.upload || 0) + (user.download || 0);
    const quotaPct = user.quota > 0 ? Math.min(100, total / user.quota * 100) : 0;
    const row = u().el("tr");
    row.appendChild(cell("名称", "", user.name || user.id));
    row.appendChild(cell("ID", "mono faint", user.id));
    row.appendChild(cell("核心", "", coreBadge(user)));
    if (caps.user_speed) row.appendChild(cell("速率", "mono", speedContent(user)));
    row.appendChild(cell("流量", "mono", trafficContent(user, caps)));
    row.appendChild(cell("配额", "", quotaContent(total, user.quota, quotaPct, caps)));
    row.appendChild(cell("限制", "", limitContent(user, caps)));
    row.appendChild(cell("过期时间", "faint", u().formatDate(user.expire_at)));
    row.appendChild(cell("状态", "", statusBadge(user.enabled, "已启用", "已禁用")));
    row.appendChild(cell("操作", "", u().el("div", { class: "actions" }, desktopActions(user, caps))));
    return row;
  }

  function cell(label, className, children) {
    const attrs = { "data-label": label };
    if (className) attrs.class = className;
    return u().el("td", attrs, children);
  }

  function coreBadge(user) {
    const coreID = user.core_id || window.app.defaultCore || "default";
    const type = user.adapter_type || coreForID(coreID)?.type || "core";
    return u().el("div", { class: "limit-info" }, [
      u().el("span", { class: "badge" }, coreID),
      u().el("span", { class: "faint mono" }, type)
    ]);
  }

  function speedContent(user) {
    return limitInfo([
      limitRow("upload", u().formatSpeed(user.speed_up)),
      limitRow("download", u().formatSpeed(user.speed_down))
    ]);
  }

  function trafficContent(user, caps) {
    if (!caps.query_traffic) return u().el("span", { class: "faint" }, "不支持");
    return limitInfo([
      limitRow("upload", u().formatBytes(user.upload)),
      limitRow("download", u().formatBytes(user.download))
    ]);
  }

  function quotaContent(total, quota, quotaPct, caps) {
    if (!caps.query_traffic) return u().el("span", { class: "faint" }, "未启用");
    if (!(quota > 0)) return u().el("span", { class: "faint" }, "无限制");
    return [progressBar(quotaPct), u().el("div", { class: "faint mono" }, `${u().formatBytes(total)} / ${u().formatBytes(quota)}`)];
  }

  function limitContent(user, caps) {
    const upLimit = user.speed_limit_up ? u().formatSpeed(user.speed_limit_up) : "无限制";
    const downLimit = user.speed_limit_down ? u().formatSpeed(user.speed_limit_down) : "无限制";
    return [
      limitInfo([limitRow("upload", upLimit), limitRow("download", downLimit)]),
      u().el("div", { class: "badge micro" }, caps.speed_limit ? "原生支持" : "软件监控")
    ];
  }

  function limitInfo(children) {
    return u().el("div", { class: "limit-info" }, children);
  }

  function limitRow(kind, value) {
    return u().el("div", { class: "limit-row" }, [u().svgIcon(ICONS[kind]), ` ${value}`]);
  }

  function progressBar(percent) {
    const span = u().el("span");
    u().setProgressWidth(span, percent);
    return u().el("div", { class: "progress" }, span);
  }

  function statusBadge(enabled, okText, badText) {
    return u().el("span", { class: `badge ${enabled ? "ok" : "bad"}` }, enabled ? okText : badText);
  }

  function desktopActions(user, caps) {
    const items = [actionButton("edit", user.id, "编辑", "btn ghost icon", "edit")];
    if (caps.disable_user || caps.enable_user) {
      items.push(actionButton("toggle", user.id, user.enabled ? "禁用" : "启用", "btn ghost icon", user.enabled ? "disable" : "enable"));
    }
    if (caps.subscription) items.push(actionButton("sub", user.id, "复制订阅链接", "btn ghost icon", "link"));
    if (caps.remove_user) items.push(actionButton("delete", user.id, "删除", "btn danger icon", "delete"));
    return items;
  }

  function actionButton(action, id, title, className, iconName, label = "") {
    return u().el("button", {
      class: className,
      title,
      type: "button",
      data: { action, id },
      onclick: onAction
    }, [u().svgIcon(ICONS[iconName]), label ? ` ${label}` : null]);
  }

  function renderMobile(filtered, caps) {
    const tablePanel = document.querySelector(".table-panel");
    if (!tablePanel) return;
    let mobileList = tablePanel.querySelector(".mobile-user-list");
    if (!mobileList) {
      mobileList = u().el("div", { class: "mobile-user-list" });
      tablePanel.appendChild(mobileList);
    }
    const expandedIds = new Set();
    mobileList.querySelectorAll(".m-user-card.expanded").forEach(el => {
      if (el.dataset.uid) expandedIds.add(el.dataset.uid);
    });
    u().clear(mobileList);
    if (!filtered.length) {
      mobileList.appendChild(u().el("div", { class: "empty-state" }, "未找到用户。"));
      return;
    }
    filtered.forEach(user => mobileList.appendChild(mobileCard(user, caps, expandedIds.has(user.id))));
  }

  function mobileCard(user, caps, expanded) {
    const initial = (user.name || user.id || "?").charAt(0).toUpperCase();
    const creds = user.credentials || {};
    const uuid = creds.uuid || creds.password || creds.ss_password || user.id || "";
    const uuidDisplay = uuid.length > 24 ? `${uuid.slice(0, 12)}...${uuid.slice(-6)}` : uuid;
    const total = (user.upload || 0) + (user.download || 0);
    const quotaPct = user.quota > 0 ? Math.min(100, total / user.quota * 100) : 0;

    const card = u().el("div", { class: `m-user-card${expanded ? " expanded" : ""}`, data: { uid: user.id } });
    const head = u().el("div", { class: "m-user-head" }, [
      u().el("div", { class: "m-user-avatar" }, initial),
      u().el("div", { class: "m-user-info" }, [
        u().el("div", { class: "m-user-name" }, [user.name || user.id, " ", statusBadge(user.enabled, "启用", "禁用")]),
        u().el("div", { class: "m-user-uuid" }, `${uuidDisplay} · ${user.core_id || window.app.defaultCore || "default"}`)
      ]),
      chevronIcon()
    ]);
    head.addEventListener("click", () => card.classList.toggle("expanded"));
    const body = u().el("div", { class: "m-user-body" }, [
      u().el("div", { class: "m-user-stats" }, mobileStats(user, caps)),
      caps.query_traffic && user.quota > 0 ? mobileQuota(total, user.quota, quotaPct) : null,
      u().el("div", { class: "m-user-actions" }, mobileActions(user, caps))
    ]);
    card.append(head, body);
    return card;
  }

  function mobileStats(user, caps) {
    const stats = [mobileStat("核心", `${user.core_id || window.app.defaultCore || "default"} / ${user.adapter_type || "core"}`)];
    if (caps.query_traffic) {
      stats.push(mobileStat("上传", u().formatBytes(user.upload)));
      stats.push(mobileStat("下载", u().formatBytes(user.download)));
    }
    if (caps.user_speed) {
      stats.push(mobileStat("上传速率", u().formatSpeed(user.speed_up)));
      stats.push(mobileStat("下载速率", u().formatSpeed(user.speed_down)));
    }
    stats.push(mobileStat("过期时间", u().formatDate(user.expire_at)));
    stats.push(mobileStat("限速", `${user.speed_limit_up ? u().formatSpeed(user.speed_limit_up) : "无限"} / ${user.speed_limit_down ? u().formatSpeed(user.speed_limit_down) : "无限"}`));
    return stats;
  }

  function mobileStat(label, value) {
    return u().el("div", { class: "m-user-stat" }, [
      u().el("div", { class: "m-user-stat-label" }, label),
      u().el("div", { class: "m-user-stat-value" }, value)
    ]);
  }

  function mobileQuota(total, quota, quotaPct) {
    return u().el("div", { class: "m-quota-bar" }, [
      progressBar(quotaPct),
      u().el("div", { class: "m-quota-text" }, `${u().formatBytes(total)} / ${u().formatBytes(quota)}`)
    ]);
  }

  function mobileActions(user, caps) {
    const items = [actionButton("edit", user.id, "编辑", "m-action-btn", "edit", "编辑")];
    if (caps.disable_user || caps.enable_user) {
      items.push(actionButton("toggle", user.id, user.enabled ? "禁用" : "启用", "m-action-btn", user.enabled ? "disable" : "enable", user.enabled ? "禁用" : "启用"));
    }
    if (caps.subscription) items.push(actionButton("sub", user.id, "订阅", "m-action-btn", "link", "订阅"));
    if (caps.remove_user) items.push(actionButton("delete", user.id, "删除", "m-action-btn danger", "delete", "删除"));
    return items;
  }

  function chevronIcon() {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "m-user-chevron");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "2");
    svg.setAttribute("aria-hidden", "true");
    const polyline = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
    polyline.setAttribute("points", "6 9 12 15 18 9");
    svg.appendChild(polyline);
    return svg;
  }

  async function onAction(event) {
    event.stopPropagation();
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
      const confirmed = await openConfirm({
        title: "删除用户",
        message: `此操作会删除用户 ${user.name || user.id}，请输入用户 ID 确认。`,
        confirmText: user.id,
        submitLabel: "删除",
        danger: true
      });
      if (!confirmed) return;
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
    const selectedCore = window.app.defaultCore || availableCores()[0]?.id || "";
    openModal("创建用户", formNode(selectedCore), async () => {
      const coreID = value("core");
      const fields = fieldsForCore(coreID);
      const caps = capsForCore(coreID);
      const payload = collectForm(fields, caps);
      await window.api.createUser(payload);
      u().toast("用户已创建", "success");
      await refresh(true);
    });
    wireCoreSelector();
    wireDependencies(fieldsForCore(selectedCore));
    wireAutoGenerate(fieldsForCore(selectedCore));
  }

  function openEdit(user) {
    const caps = window.app.capabilities || {};
    openModal("编辑用户", editNode(user, caps), async () => {
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

  function formNode(selectedCore) {
    const caps = capsForCore(selectedCore);
    return u().el("div", { class: "form-grid", id: "user-form-grid" }, [
      formSection("基础信息", [
        coreSelect(selectedCore),
        u().el("label", { class: "field full" }, [
          u().el("span", {}, "用户 ID"),
          u().el("input", { id: "field-id", placeholder: "留空则自动生成 UUID 或随机 ID" })
        ]),
        u().el("label", { class: "field" }, [
          u().el("span", {}, "名称"),
          u().el("input", { id: "field-name", required: true })
        ])
      ]),
      credentialsSection(selectedCore),
      formSection("配额与限速", [quotaNodes(), limitNodes(caps), expireField("留空表示永不过期。")])
    ]);
  }

  function coreSelect(selectedCore) {
    const cores = availableCores();
    return u().el("label", { class: "field full" }, [
      u().el("span", {}, "归属核心"),
      u().el("select", { id: "field-core" }, cores.map(core => u().el("option", {
        value: core.id,
        selected: core.id === selectedCore
      }, `${core.id || "default"} · ${core.type || "core"}`))),
      u().el("span", { class: "hint" }, "用户会写入所选核心；旧后端未返回 cores 时自动使用默认核心。")
    ]);
  }

  function credentialsSection(coreID) {
    const fields = fieldsForCore(coreID);
    return formSection("连接凭据", [
      u().el("div", { class: "form-section-note" }, "按所选核心显示对应字段。"),
      u().el("div", { id: "credential-fields", class: "form-grid nested-fields" },
        fields.length ? fields.map(fieldNode) : [u().el("div", { class: "hint" }, "当前核心不需要额外凭据。")]
      )
    ]);
  }

  function editNode(user, caps) {
    return u().el("div", { class: "form-grid" }, [
      formSection("基础信息", [
        u().el("label", { class: "field full" }, [
          u().el("span", {}, "名称"),
          u().el("input", { id: "field-name", value: user.name || "" })
        ]),
        u().el("div", { class: "field full" }, [
          u().el("span", {}, "归属核心"),
          coreBadge(user)
        ])
      ]),
      formSection("配额与限速", [
        quotaNodes(user.quota),
        limitNodes(caps, user),
        expireField("清空表示永不过期。", user.expire_at ? String(user.expire_at).slice(0, 10) : "")
      ])
    ]);
  }

  function formSection(title, children) {
    return u().el("div", { class: "form-section full" }, [
      u().el("div", { class: "form-section-title" }, title),
      children
    ]);
  }

  function fieldNode(field) {
    const attrs = { class: "field credential-field", id: `field-container-${field.key}` };
    if (field.depends_on_key) {
      attrs["data-depends-key"] = field.depends_on_key;
      attrs["data-depends-val"] = field.depends_on_val;
    }
    if (field.type === "select") {
      return u().el("label", attrs, [
        u().el("span", {}, field.label),
        u().el("select", { id: `cred-${field.key}`, required: Boolean(field.required) },
          (field.options || []).map(opt => u().el("option", { value: opt, selected: opt === field.default }, opt || "(无)"))
        )
      ]);
    }
    const inputChildren = [u().el("input", { id: `cred-${field.key}`, value: field.default || "", required: Boolean(field.required) })];
    if (field.auto_generate) {
      inputChildren.push(u().el("button", { class: "btn ghost", type: "button", data: { generate: field.key } }, "生成"));
    }
    return u().el("label", attrs, [
      u().el("span", {}, field.label),
      u().el("div", { class: "input-with-action" }, inputChildren)
    ]);
  }

  function quotaNodes(quota = 0) {
    return [
      u().el("label", { class: "field" }, [
        u().el("span", {}, "流量配额"),
        u().el("input", { id: "field-quota", type: "number", min: "0", step: "any", value: quota ? quota / 1073741824 : "", placeholder: "0 = 无限制" })
      ]),
      u().el("label", { class: "field" }, [
        u().el("span", {}, "配额单位"),
        u().el("select", { id: "field-quota_unit" }, [
          u().el("option", { value: "gb" }, "GB"),
          u().el("option", { value: "tb" }, "TB")
        ])
      ])
    ];
  }

  function limitNodes(caps, user = {}) {
    const hint = caps.speed_limit ? "底层核心提供原生限速。" : "当前核心暂无原生限速；由系统监控在软件层执行。";
    return [
      u().el("label", { class: "field" }, [
        u().el("span", {}, "上传限速"),
        u().el("input", { id: "field-limit_up", type: "number", min: "0", step: "any", value: user.speed_limit_up ? user.speed_limit_up / 1048576 : "", placeholder: "0 = 无限制" })
      ]),
      u().el("label", { class: "field" }, [
        u().el("span", {}, "下载限速"),
        u().el("input", { id: "field-limit_down", type: "number", min: "0", step: "any", value: user.speed_limit_down ? user.speed_limit_down / 1048576 : "", placeholder: "0 = 无限制" })
      ]),
      u().el("label", { class: "field" }, [
        u().el("span", {}, "上传限速单位"),
        speedUnitSelect("field-limit_unit_up"),
        u().el("span", { class: "hint" }, hint)
      ]),
      u().el("label", { class: "field" }, [
        u().el("span", {}, "下载限速单位"),
        speedUnitSelect("field-limit_unit_down")
      ])
    ];
  }

  function speedUnitSelect(id) {
    return u().el("select", { id }, [
      u().el("option", { value: "mbs" }, "MB/s"),
      u().el("option", { value: "kbs" }, "KB/s")
    ]);
  }

  function expireField(hint, value = "") {
    return u().el("label", { class: "field" }, [
      u().el("span", {}, "过期时间"),
      u().el("input", { id: "field-expire", type: "date", value }),
      u().el("span", { class: "hint" }, hint)
    ]);
  }

  function collectForm(fields) {
    const credentials = {};
    fields.forEach(field => {
      const container = document.getElementById(`field-container-${field.key}`);
      if (container && container.classList.contains("hidden")) return;
      const raw = document.getElementById(`cred-${field.key}`)?.value?.trim() || "";
      if (field.required && !raw) throw new Error(`${field.label} 为必填项`);
      if (raw) credentials[field.key] = raw;
    });
    return {
      id: value("id"),
      name: value("name"),
      core_id: value("core"),
      credentials,
      quota: toBytes(value("quota"), value("quota_unit")),
      speed_limit_up: toBPS(value("limit_up"), value("limit_unit_up")),
      speed_limit_down: toBPS(value("limit_down"), value("limit_unit_down")),
      expire_at: dateValue("expire")
    };
  }

  function openModal(title, body, onSubmit) {
    const root = document.getElementById("modal-root");
    if (!root) return;
    u().clear(root);
    const bodyNode = u().el("div", { class: "modal-body" });
    u().append(bodyNode, body);
    const modal = u().el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": "modal-title" }, [
      u().el("div", { class: "modal-header" }, [
        u().el("h3", { id: "modal-title" }, title),
        u().el("button", { class: "btn ghost icon", type: "button", "aria-label": "关闭", onclick: hideModal }, "x")
      ]),
      bodyNode,
      u().el("div", { class: "modal-footer" }, [
        u().el("button", { class: "btn ghost", type: "button", onclick: hideModal }, "取消"),
        u().el("button", { class: "btn primary", type: "button", onclick: submit }, "保存")
      ])
    ]);
    root.appendChild(modal);
    root.classList.remove("hidden");
    document.body.classList.add("modal-open");
    root.onclick = onBackdropClick;

    async function submit() {
      try {
        await onSubmit();
        hideModal();
      } catch (err) {
        u().toast(err.message, "error");
      }
    }
  }

  function openConfirm({ title, message, confirmText, submitLabel = "确认", danger = false }) {
    const root = document.getElementById("modal-root");
    if (!root) return Promise.resolve(false);
    return new Promise(resolve => {
      u().clear(root);
      const input = confirmText ? u().el("input", { type: "text", autocomplete: "off" }) : null;
      const submit = u().el("button", { class: danger ? "btn danger" : "btn primary", type: "button", disabled: Boolean(confirmText) }, submitLabel);
      if (input) input.addEventListener("input", () => { submit.disabled = input.value !== confirmText; });
      const finish = result => {
        hideModal();
        resolve(result);
      };
      submit.addEventListener("click", () => finish(true));
      const bodyChildren = [u().el("p", { class: "hint" }, message)];
      if (input) {
        bodyChildren.push(u().el("label", { class: "field full" }, [
          u().el("span", {}, `输入 ${confirmText} 确认`),
          input
        ]));
      }
      const modal = u().el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": "modal-title" }, [
        u().el("div", { class: "modal-header" }, [
          u().el("h3", { id: "modal-title" }, title),
          u().el("button", { class: "btn ghost icon", type: "button", "aria-label": "关闭", onclick: () => finish(false) }, "x")
        ]),
        u().el("div", { class: "modal-body" }, bodyChildren),
        u().el("div", { class: "modal-footer" }, [
          u().el("button", { class: "btn ghost", type: "button", onclick: () => finish(false) }, "取消"),
          submit
        ])
      ]);
      root.appendChild(modal);
      root.classList.remove("hidden");
      document.body.classList.add("modal-open");
      root.onclick = event => {
        if (event.target === root) finish(false);
      };
      input?.focus();
    });
  }

  function onBackdropClick(event) {
    if (event.target === event.currentTarget) hideModal();
  }

  function hideModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;
    root.classList.add("hidden");
    document.body.classList.remove("modal-open");
    root.onclick = null;
    u().clear(root);
  }

  function wireCoreSelector() {
    const select = document.getElementById("field-core");
    const credentials = document.getElementById("credential-fields");
    if (!select || !credentials) return;
    select.addEventListener("change", () => {
      const fields = fieldsForCore(select.value);
      u().clear(credentials);
      u().append(credentials, fields.length ? fields.map(fieldNode) : [u().el("div", { class: "hint" }, "当前核心不需要额外凭据。")]);
      wireAutoGenerate(fields);
      wireDependencies(fields);
    });
  }

  function wireAutoGenerate(fields) {
    fields.forEach(field => {
      if (!field.auto_generate) return;
      const button = Array.from(document.querySelectorAll("[data-generate]")).find(el => el.dataset.generate === field.key);
      button?.addEventListener("click", () => {
        document.getElementById(`cred-${field.key}`).value = u().generateUUID();
      });
    });
  }

  function wireDependencies(fields) {
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
        const input = document.getElementById(`cred-${field.key}`);
        if (!input) return;
        if (isMatch && field.required) input.setAttribute("required", "");
        if (!isMatch) {
          input.removeAttribute("required");
          if (input.tagName === "SELECT") input.selectedIndex = 0;
          else input.value = "";
        }
      });
    };
    depSources.forEach(key => document.getElementById(`cred-${key}`)?.addEventListener("change", updateVisibility));
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

  function start() {
    stop();
    if (!bound) {
      document.getElementById("btn-create-user")?.addEventListener("click", openCreate);
      document.getElementById("btn-refresh-users")?.addEventListener("click", () => refresh(false));
      document.getElementById("users-search")?.addEventListener("input", u().debounce(event => {
        search = event.target.value;
        render();
      }, 150));
      window.addEventListener("resize", onResize);
      bound = true;
    }
    refresh();
    timer = setInterval(() => refresh(true), 5000);
  }

  function onResize() {
    const mobile = isMobile();
    if (lastMobile !== null && mobile !== lastMobile) {
      document.querySelector(".mobile-user-list")?.remove();
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
