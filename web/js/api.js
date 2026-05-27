(function () {
  class API {
    constructor() {
      this.base = window.location.origin;
    }

    get token() {
      return localStorage.getItem("vpnview_token");
    }

    set token(value) {
      if (value) localStorage.setItem("vpnview_token", value);
      else localStorage.removeItem("vpnview_token");
    }

    async request(method, path, body, options = {}) {
      const headers = {};
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (this.token && !options.noAuth) headers.Authorization = `Bearer ${this.token}`;

      const res = await fetch(this.base + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body)
      });

      const contentType = res.headers.get("content-type") || "";
      const payload = contentType.includes("application/json") ? await res.json() : await res.text();
      if (!res.ok) {
        if (res.status === 401) this.token = null;
        const msg = typeof payload === "string" ? payload : (payload.error || `HTTP ${res.status}`);
        throw new Error(msg);
      }
      return payload;
    }

    login(secret) {
      return this.request("POST", "/api/auth/login", { secret }, { noAuth: true });
    }
    capabilities() {
      return this.request("GET", "/api/capabilities");
    }
    stats() {
      return this.request("GET", "/api/stats/global");
    }
    connections() {
      return this.request("GET", "/api/stats/connections");
    }
    killConnection(id) {
      return this.request("DELETE", `/api/stats/connections/${encodeURIComponent(id)}`);
    }
    users() {
      return this.request("GET", "/api/users");
    }
    createUser(payload) {
      return this.request("POST", "/api/users", payload);
    }
    updateUser(id, payload) {
      return this.request("PATCH", `/api/users/${encodeURIComponent(id)}`, payload);
    }
    deleteUser(id) {
      return this.request("DELETE", `/api/users/${encodeURIComponent(id)}`);
    }
    subscriptionURL(id) {
      return `${this.base}/api/sub/${encodeURIComponent(id)}`;
    }
  }

  window.api = new API();
})();
