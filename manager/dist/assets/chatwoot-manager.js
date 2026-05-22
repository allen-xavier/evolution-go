(function () {
  "use strict";

  var BUTTON_ID = "cw-instance-settings-btn";
  var MODAL_ID = "cw-instance-settings-modal";

  function parseInstanceId() {
    var m = window.location.pathname.match(/\/manager\/instances\/([^/]+)\/settings/);
    return m ? decodeURIComponent(m[1]) : "";
  }

  function deepFindApiKey(value, seen) {
    if (!value || typeof value !== "object") return "";
    if (seen.has(value)) return "";
    seen.add(value);

    if (typeof value.apiKey === "string" && value.apiKey.trim() !== "") {
      return value.apiKey.trim();
    }

    var keys = Object.keys(value);
    for (var i = 0; i < keys.length; i++) {
      var key = keys[i];
      try {
        var found = deepFindApiKey(value[key], seen);
        if (found) return found;
      } catch (_e) {}
    }
    return "";
  }

  function detectApiKey() {
    try {
      var rawAuth = localStorage.getItem("evolution-auth");
      if (rawAuth) {
        var parsedAuth = JSON.parse(rawAuth);
        var state = parsedAuth.state || parsedAuth;
        if (state && typeof state.apiKey === "string" && state.apiKey.trim() !== "") {
          return state.apiKey.trim();
        }
      }
    } catch (_e) {}

    try {
      for (var i = 0; i < localStorage.length; i++) {
        var key = localStorage.key(i);
        if (!key) continue;
        var raw = localStorage.getItem(key);
        if (!raw) continue;

        try {
          var parsed = JSON.parse(raw);
          var found = deepFindApiKey(parsed, new WeakSet());
          if (found) return found;
        } catch (_e) {}
      }
    } catch (_e) {}
    return "";
  }

  function qs(root, selector) {
    return root.querySelector(selector);
  }

  function detectApiBaseURL() {
    try {
      var rawAuth = localStorage.getItem("evolution-auth");
      if (rawAuth) {
        var parsedAuth = JSON.parse(rawAuth);
        var state = parsedAuth.state || parsedAuth;
        if (state && typeof state.apiUrl === "string" && state.apiUrl.trim() !== "") {
          return state.apiUrl.trim().replace(/\/$/, "");
        }
      }
    } catch (_e) {}
    return window.location.origin.replace(/\/$/, "");
  }

  function createModal() {
    var old = document.getElementById(MODAL_ID);
    if (old) old.remove();

    var modal = document.createElement("div");
    modal.id = MODAL_ID;
    modal.style.cssText = [
      "position:fixed",
      "inset:0",
      "display:none",
      "background:rgba(15,23,42,.55)",
      "z-index:999999",
      "align-items:center",
      "justify-content:center",
      "padding:16px",
      "font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,sans-serif",
    ].join(";");

    modal.innerHTML =
      '<div style="width:min(920px,95vw);max-height:92vh;overflow:auto;background:#fff;border-radius:14px;box-shadow:0 20px 60px rgba(0,0,0,.35);padding:16px 18px;">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">' +
      '<h3 style="margin:0;font-size:18px;color:#0f172a;">Chatwoot da Instância</h3>' +
      '<button id="cw-close" type="button" style="border:none;background:#e2e8f0;color:#0f172a;border-radius:8px;padding:6px 10px;cursor:pointer;">Fechar</button>' +
      "</div>" +
      '<p style="margin:0 0 12px;color:#475569;font-size:13px;">Configuração por instância com sincronização bidirecional de mensagens e mídia.</p>' +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;">' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-enabled" type="checkbox"/> <span>Habilitado</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-signMsg" type="checkbox"/> <span>Assinar mensagem</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-reopenConversation" type="checkbox"/> <span>Reabrir conversa</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-conversationPending" type="checkbox"/> <span>Conversa pendente</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-mergeBrazilContacts" type="checkbox"/> <span>Mesclar contatos BR</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-importContacts" type="checkbox"/> <span>Importar contatos</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-importMessages" type="checkbox"/> <span>Importar mensagens</span></label>' +
      "</div>" +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;margin-top:10px;">' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">URL Chatwoot</div><input id="cw-url" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="https://chatwoot.seudominio.com"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Account ID</div><input id="cw-accountId" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="1"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Token API</div><input id="cw-token" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="api_access_token"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Inbox ID (Caixa)</div><input id="cw-inboxId" type="number" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="10"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Inbox Identifier (opcional)</div><input id="cw-inboxIdentifier" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="opcional"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Nome Inbox</div><input id="cw-nameInbox" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="Suporte WhatsApp"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Organização</div><input id="cw-organization" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="Minha Empresa"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Logo (URL)</div><input id="cw-logo" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="https://..."/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Dias limite importação</div><input id="cw-daysLimitImportMessages" type="number" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" value="3"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Delimitador assinatura</div><input id="cw-signDelimiter" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" value="\\n"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Webhook Secret (opcional)</div><input id="cw-webhookSecret" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">API Key Manager (fallback)</div><input id="cw-apiKey" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="(auto detectado)"/></div>' +
      "</div>" +
      '<div style="margin-top:10px;">' +
      '<div style="font-size:12px;color:#334155;margin-bottom:4px;">Ignore JIDs (um por linha)</div>' +
      '<textarea id="cw-ignoreJids" rows="4" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="5511999998888@s.whatsapp.net"></textarea>' +
      "</div>" +
      '<div style="margin-top:10px;padding:8px;border:1px solid #cbd5e1;border-radius:8px;background:#f8fafc;">' +
      '<div style="font-size:12px;color:#334155;margin-bottom:4px;">Webhook URL para configurar no Chatwoot</div>' +
      '<code id="cw-webhookUrl" style="display:block;word-break:break-all;color:#0f172a;font-size:12px;"></code>' +
      "</div>" +
      '<div style="margin-top:12px;display:flex;gap:8px;align-items:center;justify-content:flex-end;">' +
      '<span id="cw-status" style="font-size:12px;color:#334155;flex:1 1 auto;"></span>' +
      '<button id="cw-load" type="button" style="border:none;background:#e2e8f0;color:#0f172a;border-radius:8px;padding:8px 10px;cursor:pointer;">Recarregar</button>' +
      '<button id="cw-save" type="button" style="border:none;background:#0ea5e9;color:#fff;border-radius:8px;padding:8px 12px;cursor:pointer;">Salvar</button>' +
      "</div>" +
      "</div>";

    document.body.appendChild(modal);
    return modal;
  }

  function getHeaderApiKey(modal) {
    var manual = qs(modal, "#cw-apiKey").value.trim();
    if (manual) return manual;
    return detectApiKey();
  }

  function setStatus(modal, text, isError) {
    var node = qs(modal, "#cw-status");
    node.textContent = text || "";
    node.style.color = isError ? "#dc2626" : "#0f766e";
  }

  function fillForm(modal, data) {
    data = data || {};
    qs(modal, "#cw-enabled").checked = !!data.enabled;
    qs(modal, "#cw-signMsg").checked = !!data.signMsg;
    qs(modal, "#cw-reopenConversation").checked = !!data.reopenConversation;
    qs(modal, "#cw-conversationPending").checked = !!data.conversationPending;
    qs(modal, "#cw-mergeBrazilContacts").checked = !!data.mergeBrazilContacts;
    qs(modal, "#cw-importContacts").checked = !!data.importContacts;
    qs(modal, "#cw-importMessages").checked = !!data.importMessages;

    qs(modal, "#cw-url").value = data.url || "";
    qs(modal, "#cw-accountId").value = data.accountId || "";
    qs(modal, "#cw-token").value = data.token || "";
    qs(modal, "#cw-inboxId").value = data.inboxId || "";
    qs(modal, "#cw-inboxIdentifier").value = data.inboxIdentifier || "";
    qs(modal, "#cw-nameInbox").value = data.nameInbox || "";
    qs(modal, "#cw-organization").value = data.organization || "";
    qs(modal, "#cw-logo").value = data.logo || "";
    qs(modal, "#cw-daysLimitImportMessages").value = data.daysLimitImportMessages || 3;
    qs(modal, "#cw-signDelimiter").value = data.signDelimiter || "\\n";
    qs(modal, "#cw-webhookSecret").value = data.webhookSecret || "";
    qs(modal, "#cw-ignoreJids").value = Array.isArray(data.ignoreJids) ? data.ignoreJids.join("\n") : "";
    qs(modal, "#cw-apiKey").value = detectApiKey();
  }

  function readForm(modal) {
    return {
      enabled: qs(modal, "#cw-enabled").checked,
      signMsg: qs(modal, "#cw-signMsg").checked,
      reopenConversation: qs(modal, "#cw-reopenConversation").checked,
      conversationPending: qs(modal, "#cw-conversationPending").checked,
      mergeBrazilContacts: qs(modal, "#cw-mergeBrazilContacts").checked,
      importContacts: qs(modal, "#cw-importContacts").checked,
      importMessages: qs(modal, "#cw-importMessages").checked,
      url: qs(modal, "#cw-url").value.trim(),
      accountId: qs(modal, "#cw-accountId").value.trim(),
      token: qs(modal, "#cw-token").value.trim(),
      inboxId: Number(qs(modal, "#cw-inboxId").value || 0),
      inboxIdentifier: qs(modal, "#cw-inboxIdentifier").value.trim(),
      nameInbox: qs(modal, "#cw-nameInbox").value.trim(),
      organization: qs(modal, "#cw-organization").value.trim(),
      logo: qs(modal, "#cw-logo").value.trim(),
      daysLimitImportMessages: Number(qs(modal, "#cw-daysLimitImportMessages").value || 3),
      signDelimiter: qs(modal, "#cw-signDelimiter").value || "\\n",
      webhookSecret: qs(modal, "#cw-webhookSecret").value.trim(),
      ignoreJids: qs(modal, "#cw-ignoreJids")
        .value.split("\n")
        .map(function (v) {
          return v.trim();
        })
        .filter(Boolean),
    };
  }

  async function loadConfig(modal, instanceId) {
    var apiKey = getHeaderApiKey(modal);
    if (!apiKey) {
      setStatus(modal, "API key não encontrada. Cole no campo API Key Manager.", true);
      return;
    }

    setStatus(modal, "Carregando configuração...", false);
    var resp = await fetch("/chatwoot/find/" + encodeURIComponent(instanceId), {
      method: "GET",
      headers: { apikey: apiKey },
    });

    var data = {};
    try {
      data = await resp.json();
    } catch (_e) {}

    if (!resp.ok) {
      var msg = (data && data.error) || "Falha ao carregar configuração";
      setStatus(modal, msg, true);
      return;
    }

    fillForm(modal, data.data || {});
    setStatus(modal, "Configuração carregada.", false);
  }

  async function saveConfig(modal, instanceId) {
    var apiKey = getHeaderApiKey(modal);
    if (!apiKey) {
      setStatus(modal, "API key não encontrada. Cole no campo API Key Manager.", true);
      return;
    }

    var payload = readForm(modal);
    setStatus(modal, "Salvando...", false);

    var resp = await fetch("/chatwoot/set/" + encodeURIComponent(instanceId), {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        apikey: apiKey,
      },
      body: JSON.stringify(payload),
    });

    var data = {};
    try {
      data = await resp.json();
    } catch (_e) {}

    if (!resp.ok) {
      var msg = (data && data.error) || "Falha ao salvar configuração";
      setStatus(modal, msg, true);
      return;
    }

    fillForm(modal, (data && data.data) || payload);
    setStatus(modal, "Configuração salva com sucesso.", false);
  }

  function ensureButton() {
    var instanceId = parseInstanceId();
    var btn = document.getElementById(BUTTON_ID);
    var modal = document.getElementById(MODAL_ID);

    if (!instanceId) {
      if (btn) btn.remove();
      if (modal) modal.style.display = "none";
      return;
    }

    if (!btn) {
      btn = document.createElement("button");
      btn.id = BUTTON_ID;
      btn.type = "button";
      btn.textContent = "Chatwoot";
      btn.style.cssText = [
        "position:fixed",
        "right:20px",
        "bottom:20px",
        "z-index:999998",
        "border:none",
        "border-radius:999px",
        "background:#0ea5e9",
        "color:#fff",
        "padding:10px 14px",
        "font-weight:600",
        "cursor:pointer",
        "box-shadow:0 8px 24px rgba(2,132,199,.35)",
      ].join(";");
      document.body.appendChild(btn);
    }

    if (!modal) {
      modal = createModal();
      qs(modal, "#cw-close").addEventListener("click", function () {
        modal.style.display = "none";
      });
      modal.addEventListener("click", function (ev) {
        if (ev.target === modal) modal.style.display = "none";
      });
    }

    btn.onclick = function () {
      var baseURL = detectApiBaseURL();
      var webhookNode = qs(modal, "#cw-webhookUrl");
      if (webhookNode) {
        webhookNode.textContent = baseURL + "/chatwoot/webhook/" + encodeURIComponent(instanceId);
      }
      modal.style.display = "flex";
      loadConfig(modal, instanceId);
    };

    qs(modal, "#cw-load").onclick = function () {
      loadConfig(modal, instanceId);
    };

    qs(modal, "#cw-save").onclick = function () {
      saveConfig(modal, instanceId);
    };
  }

  function installRouteObserver() {
    var pushState = history.pushState;
    var replaceState = history.replaceState;

    history.pushState = function () {
      pushState.apply(history, arguments);
      window.dispatchEvent(new Event("locationchange"));
    };
    history.replaceState = function () {
      replaceState.apply(history, arguments);
      window.dispatchEvent(new Event("locationchange"));
    };

    window.addEventListener("popstate", function () {
      window.dispatchEvent(new Event("locationchange"));
    });
    window.addEventListener("locationchange", ensureButton);
  }

  installRouteObserver();
  ensureButton();
})();
