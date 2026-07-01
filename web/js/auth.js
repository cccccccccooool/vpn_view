(function () {
  async function bindAuth() {
    const form = document.getElementById("login-form");
    const input = document.getElementById("login-secret");
    const error = document.getElementById("login-error");
    const submit = document.getElementById("login-submit");
    if (!form) return;

    form.addEventListener("submit", async event => {
      event.preventDefault();
      error.classList.add("hidden");
      submit.disabled = true;
      submit.textContent = "Signing in...";
      try {
        await window.api.login(input.value);
        input.value = "";
        await window.app.bootstrap();
        window.app.navigate("#/dashboard");
      } catch (err) {
        error.textContent = err.message || "Login failed";
        error.classList.remove("hidden");
      } finally {
        submit.disabled = false;
        submit.textContent = "Sign in";
      }
    });
  }

  window.AuthView = { bindAuth };
})();
