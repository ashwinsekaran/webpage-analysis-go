(function () {
  const form = document.getElementById("analysis-form");
  const input = document.getElementById("url");
  const button = document.getElementById("analyze-btn");
  const clientError = document.getElementById("client-error");

  if (!form || !input || !button || !clientError) {
    return;
  }

  form.addEventListener("submit", function (event) {
    const value = input.value.trim();

    try {
      const parsed = new URL(value);
      if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
        throw new Error("Only http:// and https:// URLs are supported.");
      }
      clientError.classList.add("is-hidden");
      clientError.textContent = "";
      button.classList.add("is-loading");
      button.disabled = true;
    } catch (error) {
      event.preventDefault();
      clientError.textContent = error.message || "Please enter a valid URL.";
      clientError.classList.remove("is-hidden");
      button.classList.remove("is-loading");
      button.disabled = false;
    }
  });
})();
