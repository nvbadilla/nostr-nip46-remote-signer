(function () {
  var importForm = document.getElementById("import-form");
  var secretInput = document.getElementById("secret-input");
  var generateButton = document.getElementById("generate-key");
  var relayInput = document.getElementById("relay-input");
  var startButton = document.getElementById("start-signer");
  var stopButton = document.getElementById("stop-signer");
  var copyButton = document.getElementById("copy-bunker");
  var bunkerInput = document.getElementById("bunker-url");
  var pubkey = document.getElementById("pubkey");
  var npub = document.getElementById("npub");
  var statePill = document.getElementById("state-pill");
  var stateDetail = document.getElementById("state-detail");
  var feedback = document.getElementById("feedback");
  var currentBunkerURL = "";

  function request(path, options) {
    return fetch(path, options || {})
      .then(function (res) {
        return res.json().catch(function () {
          return {};
        }).then(function (body) {
          if (!res.ok) {
            throw new Error(body.error || ("HTTP " + res.status));
          }
          return body;
        });
      });
  }

  function setFeedback(message, isError) {
    feedback.textContent = message || "";
    feedback.className = "feedback" + (message ? (isError ? " error" : " ok") : "");
  }

  function setBusy(isBusy) {
    startButton.disabled = isBusy;
    stopButton.disabled = isBusy;
    generateButton.disabled = isBusy;
  }

  function renderStatus(status) {
    var identity = status.identity || {};
    var state = status.state || "stopped";

    pubkey.textContent = identity.loaded ? identity.public_key : "Not loaded";
    npub.textContent = identity.loaded ? identity.npub : "Not loaded";

    relayInput.value = status.relay || relayInput.value || "";
    bunkerInput.value = currentBunkerURL;

    statePill.textContent = state;
    statePill.className = "pill " + state;

    if (state === "error") {
      stateDetail.textContent = status.last_error || "Signer error";
      return;
    }

    if (state === "stopped") {
      stateDetail.textContent = "Signer is stopped.";
    } else if (state === "waiting") {
      stateDetail.textContent = "Waiting for a client to connect.";
    } else if (state === "connected") {
      stateDetail.textContent = "Client connected.";
    } else if (state === "signed") {
      stateDetail.textContent = "At least one event signed in this session.";
    } else {
      stateDetail.textContent = "Status: " + state;
    }
  }

  function refreshStatus() {
    return request("/api/signer/status", { method: "GET" })
      .then(function (status) {
        renderStatus(status);
      })
      .catch(function (err) {
        setFeedback(err.message, true);
      });
  }

  importForm.addEventListener("submit", function (event) {
    event.preventDefault();

    var secret = secretInput.value;
    secretInput.value = "";

    setBusy(true);
    setFeedback("", false);

    request("/api/identity/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ secret: secret })
    })
      .then(function (status) {
        renderStatus(status);
        setFeedback("Identity imported.", false);
      })
      .catch(function (err) {
        setFeedback(err.message, true);
      })
      .finally(function () {
        setBusy(false);
      });
  });

  generateButton.addEventListener("click", function () {
    setBusy(true);
    setFeedback("", false);

    request("/api/identity/generate", { method: "POST" })
      .then(function (status) {
        renderStatus(status);
        setFeedback("New identity generated.", false);
      })
      .catch(function (err) {
        setFeedback(err.message, true);
      })
      .finally(function () {
        setBusy(false);
      });
  });

  startButton.addEventListener("click", function () {
    setBusy(true);
    setFeedback("", false);

    request("/api/signer/start", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relay: relayInput.value })
    })
      .then(function (response) {
        currentBunkerURL = response.bunker_url || "";
        renderStatus(response);
        setFeedback("Signer started.", false);
      })
      .catch(function (err) {
        setFeedback(err.message, true);
      })
      .finally(function () {
        setBusy(false);
      });
  });

  stopButton.addEventListener("click", function () {
    setBusy(true);
    setFeedback("", false);

    request("/api/signer/stop", { method: "POST" })
      .then(function (status) {
        currentBunkerURL = "";
        renderStatus(status);
        setFeedback("Signer stopped.", false);
      })
      .catch(function (err) {
        setFeedback(err.message, true);
      })
      .finally(function () {
        setBusy(false);
      });
  });

  copyButton.addEventListener("click", function () {
    if (!bunkerInput.value) {
      setFeedback("No bunker URL available.", true);
      return;
    }

    navigator.clipboard.writeText(bunkerInput.value)
      .then(function () {
        setFeedback("Bunker URL copied.", false);
      })
      .catch(function () {
        setFeedback("Unable to copy bunker URL.", true);
      });
  });

  refreshStatus();
  setInterval(refreshStatus, 3000);
})();
