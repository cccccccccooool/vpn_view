(function () {
  const AUTH_MARKER = "vpnview_authenticated";
  const LEGACY_TOKEN = "vpnview_token";

  class API {
    constructor() {
      this.base = window.location.origin;
    }

    get token() {
      return localStorage.getItem(LEGACY_TOKEN);
    }

    set token(value) {
      if (value) localStorage.setItem(LEGACY_TOKEN, value);
      else localStorage.removeItem(LEGACY_TOKEN);
    }

    hasSession() {
      return Boolean(this.token || sessionStorage.getItem(AUTH_MARKER) || this.csrfToken());
    }

    markAuthenticated() {
      sessionStorage.setItem(AUTH_MARKER, "1");
    }

    clearSession() {
      sessionStorage.removeItem(AUTH_MARKER);
      this.token = null;
    }

    csrfToken() {
      const match = document.cookie.match(/(?:^|;\s*)vpnview_csrf=([^;]+)/);
      return match ? decodeURIComponent(match[1]) : "";
    }

    async request(method, path, body, options = {}) {
      const headers = {};
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (this.token && !options.noAuth) headers.Authorization = `Bearer ${this.token}`;
      if (!options.noAuth && !["GET", "HEAD", "OPTIONS"].includes(method)) {
        const csrf = this.csrfToken();
        if (csrf) headers["X-CSRF-Token"] = csrf;
      }

      const res = await fetch(this.base + path, {
        method,
        credentials: "same-origin",
        headers,
        body: body === undefined ? undefined : JSON.stringify(body)
      });

      const contentType = res.headers.get("content-type") || "";
      const payload = contentType.includes("application/json") ? await res.json() : await res.text();
      if (!res.ok) {
        if (res.status === 401) this.clearSession();
        const msg = typeof payload === "string" ? payload : (payload.error?.message || payload.error || `HTTP ${res.status}`);
        throw new Error(msg);
      }
      return payload;
    }

    async login(secret) {
      const payload = await this.request("POST", "/api/auth/login", { secret }, { noAuth: true });
      this.markAuthenticated();
      return payload;
    }

    async logout() {
      try {
        await this.request("POST", "/api/auth/logout");
      } finally {
        this.clearSession();
      }
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
