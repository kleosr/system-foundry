package streaming

import "net/http"

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Systems Foundry</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
    body { margin: 0 auto; max-width: 960px; padding: 16px; }
    main { display: grid; gap: 16px; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); }
    section { border: 1px solid #8884; border-radius: 8px; padding: 12px; min-height: 240px; }
    pre { white-space: pre-wrap; word-break: break-word; margin: 0; }
    form { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
    #status { font-size: 0.9em; opacity: 0.8; }
  </style>
</head>
<body>
  <h1>Systems Foundry</h1>
  <form id="login">
    <input name="email" type="email" placeholder="owner@acme.test" required autocomplete="username">
    <input name="password" type="password" placeholder="password" required autocomplete="current-password">
    <button type="submit">Sign in</button>
    <span id="status">signed out</span>
  </form>
  <main>
    <section><h2>Model tokens</h2><p id="ttft">TTFT: waiting</p><pre id="tokens"></pre></section>
    <section><h2>Data changes</h2><pre id="changes"></pre></section>
  </main>
  <script>
    const statusLine = document.querySelector("#status");
    const tokens = document.querySelector("#tokens");
    const changes = document.querySelector("#changes");
    const ttft = document.querySelector("#ttft");
    let source = null;
    let firstTokenAt = null;
    let completionStartedAt = null;
    function connect() {
      if (source) { source.close(); }
      source = new EventSource("/stream");
      source.onopen = function () { statusLine.textContent = "streaming"; };
      source.onerror = function () { statusLine.textContent = "reconnecting"; };
      source.addEventListener("token", function (ev) {
        if (completionStartedAt === null) {
          completionStartedAt = performance.now();
          tokens.textContent = "";
        }
        if (firstTokenAt === null) {
          firstTokenAt = performance.now();
          ttft.textContent = "First token received";
        }
        tokens.textContent += ev.data;
      });
      source.addEventListener("change", function (ev) {
        const change = JSON.parse(ev.data);
        if (change.type === "completion") {
          ttft.textContent = "Done: " + change.model + ", " + change.tokens + " tokens" + (change.cacheHit ? ", cache hit" : "");
          firstTokenAt = null;
          completionStartedAt = null;
        }
        changes.textContent = new Date().toLocaleTimeString() + " " + ev.data + "\n" + changes.textContent;
      });
    }
    document.querySelector("#login").addEventListener("submit", async function (ev) {
      ev.preventDefault();
      const form = new FormData(ev.target);
      const response = await fetch("/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: form.get("email"), password: form.get("password") })
      });
      if (!response.ok) {
        statusLine.textContent = "sign-in failed";
        return;
      }
      statusLine.textContent = "signed in";
      connect();
    });
    connect();
  </script>
</body>
</html>
`

func Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}
