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

      if (window.api.token) {
        try {
          await this.bootstrap();
          this.navigate(location.hash || "#/dashboard");
        } catch {
          this.logout();
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
      document.querySelectorAll(".adapter-label-text").forEach(el => {
        el.textContent = `${supported} capabilities`;
      });
    },

    navigate(hash) {
      if (!window.api.token) {
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

    logout() {
      window.api.token = null;
      this.showLogin();
    }
  };

  window.app = app;
  document.addEventListener("DOMContentLoaded", () => app.init());
})();
