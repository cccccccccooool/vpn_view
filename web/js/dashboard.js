(function () {
  const u = () => window.VPNViewUtils;
  let timer = null;
  let bound = false;
  let userList = [];
  let expandedUser = null; // 当前展开详情的用户 ID

  function resolveUserName(userId) {
    if (!userId) return "未知用户";
    const user = userList.find(u => u.id === userId);
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
      statsRoot.innerHTML = cards.join("");

      await renderConnections();
    } catch (err) {
      statsRoot.innerHTML = `<div class="unsupported">仪表盘加载失败: ${u().escapeHtml(err.message)}</div>`;
    }
  }

  /**
   * 将原始连接数组按 user_id 聚合，返回用户级别摘要数组
   */
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
      // 追踪最近一条连接的开始时间
      if (!entry.latestStart || (conn.start && conn.start > entry.latestStart)) {
        entry.latestStart = conn.start;
      }
    }
    // 按连接数降序排列
    return Object.values(map).sort((a, b) => b.connCount - a.connCount);
  }

  /**
   * 提取连接的目标域名/IP，用于展示"访问了哪个网站"
   */
  function extractHost(destination) {
    if (!destination) return "-";
    // 去掉端口号，保留域名/IP
    const parts = destination.split(":");
    return parts[0] || destination;
  }

  /**
   * 渲染按用户聚合后的连接面板
   */
  function renderUserConnectionList(userGroups, root, caps) {
    if (!userGroups.length) {
      root.innerHTML = `<div class="empty-state">暂无活跃连接。</div>`;
      return;
    }

    let html = `<div class="user-connections-list">`;

    for (const group of userGroups) {
      const isExpanded = expandedUser === group.userId;
      const totalTraffic = group.totalUpload + group.totalDownload;
      const chevron = isExpanded ? "▼" : "▶";

      html += `
        <div class="user-conn-group ${isExpanded ? "expanded" : ""}">
          <div class="user-conn-header" data-toggle-user="${u().escapeHtml(group.userId)}">
            <div class="user-conn-chevron">${chevron}</div>
            <div class="user-conn-info">
              <div class="user-conn-name">${u().escapeHtml(group.name)}</div>
              <div class="user-conn-meta faint">${group.connCount} 个连接 · 最近活跃 ${u().formatDate(group.latestStart)}</div>
            </div>
            <div class="user-conn-stats">
              <div class="user-conn-traffic mono">
                <span class="upload-indicator">↑</span>${u().formatBytes(group.totalUpload)}
                <span class="download-indicator">↓</span>${u().formatBytes(group.totalDownload)}
              </div>
              <div class="faint">合计 ${u().formatBytes(totalTraffic)}</div>
            </div>
            ${caps.disable_user ? `
            <div class="user-conn-actions" style="margin-left: 16px;">
              <button class="btn danger small" data-disable-user="${u().escapeHtml(group.userId)}" title="禁用并强行切断该用户的所有连接" style="padding: 4px 8px; font-size: 12px; border-radius: 6px; border: none; white-space: nowrap;">✕ 禁用用户</button>
            </div>` : ""}
          </div>`;

      // 展开时渲染详细连接列表
      if (isExpanded) {
        // 按目标域名聚合展示
        const siteMap = {};
        for (const conn of group.connections) {
          const host = extractHost(conn.destination);
          if (!siteMap[host]) {
            siteMap[host] = { host, count: 0, upload: 0, download: 0, connections: [] };
          }
          siteMap[host].count++;
          siteMap[host].upload += (conn.upload || 0);
          siteMap[host].download += (conn.download || 0);
          siteMap[host].connections.push(conn);
        }
        const sites = Object.values(siteMap).sort((a, b) => (b.upload + b.download) - (a.upload + a.download));

        html += `<div class="user-conn-details">
          <table class="conn-detail-table">
            <thead><tr>
              <th>访问目标</th>
              <th>连接数</th>
              <th>上传</th>
              <th>下载</th>
            </tr></thead>
            <tbody>`;

        for (const site of sites) {
          html += `<tr>
            <td class="mono">${u().escapeHtml(site.host)}</td>
            <td class="mono">${site.count}</td>
            <td class="mono"><span class="upload-indicator">↑</span>${u().formatBytes(site.upload)}</td>
            <td class="mono"><span class="download-indicator">↓</span>${u().formatBytes(site.download)}</td>
          </tr>`;
        }

        html += `</tbody></table></div>`;
      }

      html += `</div>`;
    }

    html += `</div>`;
    root.innerHTML = html;

    // 绑定用户行点击展开/折叠
    root.querySelectorAll("[data-toggle-user]").forEach(el => {
      el.addEventListener("click", () => {
        const uid = el.dataset.toggleUser;
        expandedUser = (expandedUser === uid) ? null : uid;
        renderUserConnectionList(userGroups, root, caps);
      });
    });

    // 绑定禁用用户按钮
    root.querySelectorAll("[data-disable-user]").forEach(btn => {
      btn.addEventListener("click", async (e) => {
        e.stopPropagation(); // 阻止触发父元素的展开折叠事件
        const uid = btn.dataset.disableUser;
        const userName = resolveUserName(uid);
        if (!confirm(`确定要禁用并切断用户 ${userName} 的所有活跃连接吗？`)) return;
        try {
          await window.api.updateUser(uid, { enabled: false });
          u().toast(`用户 ${userName} 已被禁用并切断连接`, "success");
          await renderConnections();
        } catch (err) {
          u().toast(err.message, "error");
        }
      });
    });
  }

  async function renderConnections() {
    const caps = window.app.capabilities;
    const panel = document.getElementById("connections-panel");
    const root = document.getElementById("connections-content");
    if (!panel || !root) return;

    if (!caps.active_conns) {
      root.innerHTML = `<div class="unsupported">当前适配器不支持获取活跃连接列表。</div>`;
      return;
    }

    try {
      const [connPayload, usersPayload] = await Promise.all([
        window.api.connections(),
        window.api.users().catch(() => ({ users: [] }))
      ]);
      userList = usersPayload.users || [];
      const conns = connPayload.connections || [];
      const userGroups = aggregateByUser(conns);
      renderUserConnectionList(userGroups, root, caps);
    } catch (err) {
      root.innerHTML = `<div class="unsupported">连接加载失败: ${u().escapeHtml(err.message)}</div>`;
    }
  }

  function card(label, value, sub) {
    return `<div class="stat-card">
      <div class="label">${u().escapeHtml(label)}</div>
      <div class="value">${u().escapeHtml(value)}</div>
      <div class="sub">${u().escapeHtml(sub)}</div>
    </div>`;
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
