(function () {
  const app = {
    capabilities: {},
    credentialFields: [],
    capsPayload: null,

    async init() {
      await window.AuthView.bindAuth();
      document.getElementById("btn-logout")?.addEventListener("click", () => this.logout());
      document.getElementById("btn-logout-mobile")?.addEventListener("click", () => this.logout());
      window.addEventListener("hashchange", () => this.navigate(location.hash));

      if (window.api.hasSession()) {
        try {
          await this.bootstrap();
          this.navigate(location.hash || "#/dashboard");
        } catch {
          await this.logout();
        }
      } else {
        this.showLogin();
      }
    },

    async bootstrap() {
      this.capsPayload = await window.api.capabilities();
      this.capabilities = this.capsPayload.capabilities || {};
      this.credentialFields = this.capsPayload.credential_fields || [];
      this.applyCapabilities();
    },

    applyCapabilities() {
      const supported = Object.entries(this.capabilities).filter(([, ok]) => ok).length;
      const coreLabel = this.capsPayload?.default_core ? `${this.capsPayload.default_core} · ${supported} capabilities` : `${supported} capabilities`;
      document.querySelectorAll(".adapter-label-text").forEach(el => {
        el.textContent = coreLabel;
      });
    },

    navigate(hash) {
      if (!window.api.hasSession()) {
        this.showLogin();
        return;
      }
      hash = hash || "#/dashboard";
      document.getElementById("login-view").classList.add("hidden");
      document.getElementById("main-view").classList.remove("hidden");
      document.querySelectorAll(".page").forEach(el => el.classList.add("hidden"));
      document.querySelectorAll(".nav-link").forEach(el => el.classList.remove("active"));

      window.DashboardView.stop();
      window.UsersView.stop();

      if (hash === "#/users") {
        document.getElementById("users-view").classList.remove("hidden");
        document.querySelectorAll(".nav-users").forEach(el => el.classList.add("active"));
        window.UsersView.start();
      } else {
        document.getElementById("dashboard-view").classList.remove("hidden");
        document.querySelectorAll(".nav-dashboard").forEach(el => el.classList.add("active"));
        window.DashboardView.start();
      }
    },

    showLogin() {
      window.DashboardView.stop();
      window.UsersView.stop();
      document.getElementById("main-view").classList.add("hidden");
      document.getElementById("login-view").classList.remove("hidden");
      if (location.hash !== "#/login") location.hash = "#/login";
    },

    async logout() {
      await window.api.logout();
      this.showLogin();
    }
  };

  window.app = app;
  document.addEventListener("DOMContentLoaded", () => app.init());
})();
