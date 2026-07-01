(function () {
  const u = () => window.VPNViewUtils;
  let timer = null;
  let userList = [];
  let expandedUser = null;

  function resolveUserName(userId) {
    if (!userId) return "未知用户";
    const user = userList.find(item => item.id === userId);
    return user ? (user.name || user.id) : userId;
  }

  async function refresh() {
    const caps = window.app.capabilities;
    const statsRoot = document.getElementById("dashboard-stats");
    if (!statsRoot) return;

    try {
      const stats = await window.api.stats();
      const cards = [];
      if (caps.realtime_speed) {
        cards.push(card("上传速率", u().formatSpeed(stats.speed_up), "原生实时获取"));
        cards.push(card("下载速率", u().formatSpeed(stats.speed_down), "原生实时获取"));
      } else {
        cards.push(card("上传速率", u().formatSpeed(stats.speed_up), "基于流量轮询估算"));
        cards.push(card("下载速率", u().formatSpeed(stats.speed_down), "基于流量轮询估算"));
      }
      cards.push(card("总上传流量", u().formatBytes(stats.total_upload), "持久化存储"));
      cards.push(card("总下载流量", u().formatBytes(stats.total_download), "持久化存储"));
      cards.push(card("生效用户数", String(stats.active_users || 0), "数据库记录"));
      cards.push(card("活跃连接数", String(stats.active_connections || 0), caps.active_conns ? "适配器原生上报" : "当前适配器不支持"));
      if (stats.last_poll_error) {
        cards.push(card("流量轮询", "异常", stats.last_poll_error));
      }
      u().clear(statsRoot);
      cards.forEach(node => statsRoot.appendChild(node));

      await renderConnections();
    } catch (err) {
      renderMessage(statsRoot, "unsupported", `仪表盘加载失败: ${err.message}`);
    }
  }

  function aggregateByUser(conns) {
    const map = {};
    for (const conn of conns) {
      const uid = conn.user_id || "__unknown__";
      if (!map[uid]) {
        map[uid] = {
          userId: uid,
          name: resolveUserName(conn.user_id),
          connCount: 0,
          totalUpload: 0,
          totalDownload: 0,
          connections: [],
          latestStart: null
        };
      }
      const entry = map[uid];
      entry.connCount++;
      entry.totalUpload += (conn.upload || 0);
      entry.totalDownload += (conn.download || 0);
      entry.connections.push(conn);
      if (!entry.latestStart || (conn.start && conn.start > entry.latestStart)) {
        entry.latestStart = conn.start;
      }
    }
    return Object.values(map).sort((a, b) => b.connCount - a.connCount);
  }

  function extractHost(destination) {
    if (!destination) return "-";
    const parts = destination.split(":");
    return parts[0] || destination;
  }

  function renderUserConnectionList(userGroups, root, caps) {
    u().clear(root);
    if (!userGroups.length) {
      root.appendChild(u().el("div", { class: "empty-state" }, "暂无活跃连接。"));
      return;
    }

    const list = u().el("div", { class: "user-connections-list" });
    userGroups.forEach(group => list.appendChild(connectionGroup(group, caps, userGroups, root)));
    root.appendChild(list);
  }

  function connectionGroup(group, caps, userGroups, root) {
    const isExpanded = expandedUser === group.userId;
    const totalTraffic = group.totalUpload + group.totalDownload;
    const wrapper = u().el("div", { class: ["user-conn-group", isExpanded && "expanded"] });

    const header = u().el("div", { class: "user-conn-header" }, [
      u().el("div", { class: "user-conn-chevron" }, isExpanded ? "▾" : "▸"),
      u().el("div", { class: "user-conn-info" }, [
        u().el("div", { class: "user-conn-name" }, group.name),
        u().el("div", { class: "user-conn-meta faint" }, `${group.connCount} 个连接 · 最近活跃 ${u().formatDate(group.latestStart)}`)
      ]),
      u().el("div", { class: "user-conn-stats" }, [
        u().el("div", { class: "user-conn-traffic mono" }, [
          u().el("span", { class: "upload-indicator" }, "↑"),
          u().formatBytes(group.totalUpload),
          " ",
          u().el("span", { class: "download-indicator" }, "↓"),
          u().formatBytes(group.totalDownload)
        ]),
        u().el("div", { class: "faint" }, `合计 ${u().formatBytes(totalTraffic)}`)
      ]),
      caps.disable_user ? disableUserButton(group.userId) : null
    ]);

    header.addEventListener("click", () => {
      expandedUser = expandedUser === group.userId ? null : group.userId;
      renderUserConnectionList(userGroups, root, caps);
    });

    wrapper.appendChild(header);
    if (isExpanded) {
      wrapper.appendChild(connectionDetails(group.connections));
    }
    return wrapper;
  }

  function disableUserButton(userId) {
    const button = u().el("button", {
      class: "btn danger small user-conn-disable",
      type: "button",
      title: "禁用并强行切断该用户的所有连接"
    }, "✕ 禁用用户");
    button.addEventListener("click", async event => {
      event.stopPropagation();
      const userName = resolveUserName(userId);
      const confirmed = await openConfirm({
        title: "禁用用户",
        message: `确定要禁用并切断用户 ${userName} 的所有活跃连接吗？`,
        submitLabel: "禁用",
        danger: true
      });
      if (!confirmed) return;
      try {
        await window.api.updateUser(userId, { enabled: false });
        u().toast(`用户 ${userName} 已被禁用并切断连接`, "success");
        await renderConnections();
      } catch (err) {
        u().toast(err.message, "error");
      }
    });
    return u().el("div", { class: "user-conn-actions" }, button);
  }

  function connectionDetails(connections) {
    const siteMap = {};
    for (const conn of connections) {
      const host = extractHost(conn.destination);
      if (!siteMap[host]) {
        siteMap[host] = { host, count: 0, upload: 0, download: 0 };
      }
      siteMap[host].count++;
      siteMap[host].upload += (conn.upload || 0);
      siteMap[host].download += (conn.download || 0);
    }
    const sites = Object.values(siteMap).sort((a, b) => (b.upload + b.download) - (a.upload + a.download));
    const tbody = u().el("tbody");
    sites.forEach(site => {
      tbody.appendChild(u().el("tr", {}, [
        u().el("td", { class: "mono" }, site.host),
        u().el("td", { class: "mono" }, String(site.count)),
        u().el("td", { class: "mono" }, [
          u().el("span", { class: "upload-indicator" }, "↑"),
          u().formatBytes(site.upload)
        ]),
        u().el("td", { class: "mono" }, [
          u().el("span", { class: "download-indicator" }, "↓"),
          u().formatBytes(site.download)
        ])
      ]));
    });

    return u().el("div", { class: "user-conn-details" }, [
      u().el("table", { class: "conn-detail-table" }, [
        u().el("thead", {}, u().el("tr", {}, [
          u().el("th", {}, "访问目标"),
          u().el("th", {}, "连接数"),
          u().el("th", {}, "上传"),
          u().el("th", {}, "下载")
        ])),
        tbody
      ])
    ]);
  }

  async function renderConnections() {
    const caps = window.app.capabilities;
    const root = document.getElementById("connections-content");
    if (!root) return;

    if (!caps.active_conns) {
      renderMessage(root, "unsupported", "当前适配器不支持获取活跃连接列表。");
      return;
    }

    try {
      const [connPayload, usersPayload] = await Promise.all([
        window.api.connections(),
        window.api.users().catch(() => ({ users: [] }))
      ]);
      userList = usersPayload.users || [];
      const conns = connPayload.connections || [];
      renderUserConnectionList(aggregateByUser(conns), root, caps);
    } catch (err) {
      renderMessage(root, "unsupported", `连接加载失败: ${err.message}`);
    }
  }

  function card(label, value, sub) {
    return u().el("div", { class: "stat-card" }, [
      u().el("div", { class: "label" }, label),
      u().el("div", { class: "value" }, value),
      u().el("div", { class: "sub" }, sub)
    ]);
  }

  function renderMessage(root, className, message) {
    u().clear(root);
    root.appendChild(u().el("div", { class: className }, message));
  }

  function openConfirm({ title, message, submitLabel = "确认", danger = false }) {
    const root = document.getElementById("modal-root");
    if (!root) return Promise.resolve(false);
    return new Promise(resolve => {
      const finish = result => {
        hideModal();
        resolve(result);
      };
      u().clear(root);
      const modal = u().el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": "modal-title" }, [
        u().el("div", { class: "modal-header" }, [
          u().el("h3", { id: "modal-title" }, title),
          u().el("button", { class: "btn ghost icon", type: "button", "aria-label": "关闭", onclick: () => finish(false) }, "×")
        ]),
        u().el("div", { class: "modal-body" }, [
          u().el("p", { class: "hint" }, message)
        ]),
        u().el("div", { class: "modal-footer" }, [
          u().el("button", { class: "btn ghost", type: "button", onclick: () => finish(false) }, "取消"),
          u().el("button", { class: danger ? "btn danger" : "btn primary", type: "button", onclick: () => finish(true) }, submitLabel)
        ])
      ]);
      root.appendChild(modal);
      root.classList.remove("hidden");
      document.body.classList.add("modal-open");
      root.onclick = event => {
        if (event.target === root) finish(false);
      };
    });
  }

  function hideModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;
    root.classList.add("hidden");
    document.body.classList.remove("modal-open");
    root.onclick = null;
    u().clear(root);
  }

  function start() {
    stop();
    expandedUser = null;
    refresh();
    timer = setInterval(refresh, 4000);
  }

  function stop() {
    if (timer) clearInterval(timer);
    timer = null;
  }

  window.DashboardView = { start, stop, refresh };
})();
