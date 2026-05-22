(function () {
  "use strict";

  var ACTION_ICON_ATTR = "data-cw-chatwoot-icon";
  var MODAL_ID = "cw-instance-chatwoot-modal";
  var ACTIVE_INSTANCE_ID = "";

  function qs(root, selector) {
    return root.querySelector(selector);
  }

  function parseInstanceIdFromPath(pathname) {
    var m = (pathname || "").match(/\/instances\/([^\/?#]+)(?:\/settings)?(?:\/|$)/i);
    return m ? decodeURIComponent(m[1]) : "";
  }

  function parseInstanceIdFromHref(href) {
    if (!href) return "";
    try {
      var url = new URL(href, window.location.origin);
      return parseInstanceIdFromPath(url.pathname || "");
    } catch (_e) {
      return parseInstanceIdFromPath(String(href));
    }
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
          if (parsed && typeof parsed === "object") {
            if (typeof parsed.apiKey === "string" && parsed.apiKey.trim() !== "") {
              return parsed.apiKey.trim();
            }
            if (parsed.state && typeof parsed.state.apiKey === "string" && parsed.state.apiKey.trim() !== "") {
              return parsed.state.apiKey.trim();
            }
          }
        } catch (_e) {}
      }
    } catch (_e) {}

    return "";
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

  function resolveInstanceIdFromElement(el) {
    var node = el;
    for (var i = 0; i < 8 && node; i++) {
      if (node.getAttribute) {
        var explicit = node.getAttribute("data-cw-instance-id") || node.getAttribute("data-instance-id");
        if (explicit) return explicit;
      }

      if (node.querySelector) {
        var link = node.querySelector('a[href*="/manager/instances/"][href*="/settings"]');
        if (link) {
          var fromHref = parseInstanceIdFromHref(link.getAttribute("href"));
          if (fromHref) return fromHref;
        }
      }

      node = node.parentElement;
    }

    var current = parseInstanceIdFromPath(window.location.pathname || "");
    return current || "";
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
      '<h3 style="margin:0;font-size:18px;color:#0f172a;">Chatwoot da Instancia</h3>' +
      '<button id="cw-close" type="button" style="border:none;background:#e2e8f0;color:#0f172a;border-radius:8px;padding:6px 10px;cursor:pointer;">Fechar</button>' +
      '</div>' +
      '<p style="margin:0 0 12px;color:#475569;font-size:13px;">Configuracao por instancia com sincronizacao bidirecional de mensagens e midia.</p>' +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;">' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-enabled" type="checkbox"/> <span>Habilitado</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-signMsg" type="checkbox"/> <span>Assinar mensagem</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-reopenConversation" type="checkbox"/> <span>Reabrir conversa</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-conversationPending" type="checkbox"/> <span>Conversa pendente</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-mergeBrazilContacts" type="checkbox"/> <span>Mesclar contatos BR</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-importContacts" type="checkbox"/> <span>Importar contatos</span></label>' +
      '<label style="display:flex;gap:8px;align-items:center;"><input id="cw-importMessages" type="checkbox"/> <span>Importar mensagens</span></label>' +
      '</div>' +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;margin-top:10px;">' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">URL Chatwoot</div><input id="cw-url" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="https://chatwoot.seudominio.com"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Account ID</div><input id="cw-accountId" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="1"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Token API</div><input id="cw-token" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="api_access_token"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Inbox ID (Caixa)</div><input id="cw-inboxId" type="number" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="10"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Inbox Identifier (opcional)</div><input id="cw-inboxIdentifier" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="opcional"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Nome Inbox</div><input id="cw-nameInbox" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="Suporte WhatsApp"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Organizacao</div><input id="cw-organization" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="Minha Empresa"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Logo (URL)</div><input id="cw-logo" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="https://..."/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Dias limite importacao</div><input id="cw-daysLimitImportMessages" type="number" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" value="3"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Delimitador assinatura</div><input id="cw-signDelimiter" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" value="\\n"/></div>' +
      '<div><div style="font-size:12px;color:#334155;margin-bottom:4px;">Webhook Secret (opcional)</div><input id="cw-webhookSecret" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px"/></div>' +
      '</div>' +
      '<div style="margin-top:10px;">' +
      '<div style="font-size:12px;color:#334155;margin-bottom:4px;">Ignore JIDs (um por linha)</div>' +
      '<textarea id="cw-ignoreJids" rows="4" style="width:100%;padding:8px;border:1px solid #cbd5e1;border-radius:8px" placeholder="5511999998888@s.whatsapp.net"></textarea>' +
      '</div>' +
      '<div style="margin-top:10px;padding:8px;border:1px solid #cbd5e1;border-radius:8px;background:#f8fafc;">' +
      '<div style="font-size:12px;color:#334155;margin-bottom:4px;">Webhook URL para configurar no Chatwoot</div>' +
      '<code id="cw-webhookUrl" style="display:block;word-break:break-all;color:#0f172a;font-size:12px;"></code>' +
      '</div>' +
      '<div style="margin-top:12px;display:flex;gap:8px;align-items:center;justify-content:flex-end;">' +
      '<span id="cw-status" style="font-size:12px;color:#334155;flex:1 1 auto;"></span>' +
      '<button id="cw-load" type="button" style="border:none;background:#e2e8f0;color:#0f172a;border-radius:8px;padding:8px 10px;cursor:pointer;">Recarregar</button>' +
      '<button id="cw-save" type="button" style="border:none;background:#0ea5e9;color:#fff;border-radius:8px;padding:8px 12px;cursor:pointer;">Salvar</button>' +
      '</div>' +
      '</div>';

    document.body.appendChild(modal);
    return modal;
  }

  function ensureModal() {
    var modal = document.getElementById(MODAL_ID);
    if (modal) return modal;

    modal = createModal();

    qs(modal, "#cw-close").addEventListener("click", function () {
      modal.style.display = "none";
    });

    modal.addEventListener("click", function (ev) {
      if (ev.target === modal) modal.style.display = "none";
    });

    qs(modal, "#cw-load").onclick = function () {
      if (!ACTIVE_INSTANCE_ID) return;
      loadConfig(modal, ACTIVE_INSTANCE_ID);
    };

    qs(modal, "#cw-save").onclick = function () {
      if (!ACTIVE_INSTANCE_ID) return;
      saveConfig(modal, ACTIVE_INSTANCE_ID);
    };

    return modal;
  }

  function setStatus(modal, text, isError) {
    var node = qs(modal, "#cw-status");
    if (!node) return;
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
    var apiKey = detectApiKey();
    if (!apiKey) {
      setStatus(modal, "API key nao encontrada no manager.", true);
      return;
    }

    setStatus(modal, "Carregando configuracao...", false);
    var resp = await fetch("/chatwoot/find/" + encodeURIComponent(instanceId), {
      method: "GET",
      headers: { apikey: apiKey },
    });

    var data = {};
    try {
      data = await resp.json();
    } catch (_e) {}

    if (!resp.ok) {
      var msg = (data && data.error) || "Falha ao carregar configuracao";
      setStatus(modal, msg, true);
      return;
    }

    fillForm(modal, data.data || {});
    setStatus(modal, "Configuracao carregada.", false);
  }

  async function saveConfig(modal, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setStatus(modal, "API key nao encontrada no manager.", true);
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
      var msg = (data && data.error) || "Falha ao salvar configuracao";
      setStatus(modal, msg, true);
      return;
    }

    fillForm(modal, (data && data.data) || payload);
    setStatus(modal, "Configuracao salva com sucesso.", false);
  }

  function openModalForInstance(instanceId) {
    if (!instanceId) return;

    ACTIVE_INSTANCE_ID = instanceId;
    var modal = ensureModal();

    var baseURL = detectApiBaseURL();
    var webhookNode = qs(modal, "#cw-webhookUrl");
    if (webhookNode) {
      webhookNode.textContent = baseURL + "/chatwoot/webhook/" + encodeURIComponent(instanceId);
    }

    modal.style.display = "flex";
    loadConfig(modal, instanceId);
  }

  function createIconButton(instanceId, referenceEl) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.setAttribute(ACTION_ICON_ATTR, "1");
    btn.setAttribute("data-cw-instance-id", instanceId);
    btn.setAttribute("title", "Chatwoot");
    btn.setAttribute("aria-label", "Chatwoot");

    if (referenceEl && referenceEl.className) {
      btn.className = referenceEl.className;
    } else {
      btn.style.cssText = [
        "display:inline-flex",
        "align-items:center",
        "justify-content:center",
        "width:36px",
        "height:36px",
        "border-radius:8px",
        "border:1px solid rgba(148,163,184,.30)",
        "background:transparent",
        "cursor:pointer",
      ].join(";");
    }

    btn.style.marginRight = "6px";
    btn.style.color = "#22c55e";
    btn.innerHTML =
      '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">' +
      '<path d="M5 16.2V5.8A2.8 2.8 0 0 1 7.8 3h8.4A2.8 2.8 0 0 1 19 5.8v6.4A2.8 2.8 0 0 1 16.2 15H9l-4 3.6v-2.4z" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"></path>' +
      '<circle cx="10" cy="9" r="1" fill="currentColor"></circle>' +
      '<circle cx="13" cy="9" r="1" fill="currentColor"></circle>' +
      '<circle cx="16" cy="9" r="1" fill="currentColor"></circle>' +
      "</svg>";

    btn.onclick = function (ev) {
      ev.preventDefault();
      ev.stopPropagation();
      openModalForInstance(instanceId);
    };

    return btn;
  }

  function ensureIconLeftOfBlueBubble() {
    var bubblePaths = document.querySelectorAll('svg path[d*="H7l-4 4V5a2 2"]');

    for (var i = 0; i < bubblePaths.length; i++) {
      var path = bubblePaths[i];
      var bubbleButton = path.closest("button, a");
      if (!bubbleButton) continue;

      var parent = bubbleButton.parentElement;
      if (!parent) continue;

      var instanceId = resolveInstanceIdFromElement(parent);
      if (!instanceId) continue;

      var existing = parent.querySelector("[" + ACTION_ICON_ATTR + '][data-cw-instance-id="' + instanceId + '"]');
      if (existing) {
        if (existing.nextSibling !== bubbleButton) {
          parent.insertBefore(existing, bubbleButton);
        }
        continue;
      }

      var icon = createIconButton(instanceId, bubbleButton);
      parent.insertBefore(icon, bubbleButton);
    }
  }

  function ensureFallbackIconNearSettings() {
    var settingsLinks = document.querySelectorAll('a[href*="/manager/instances/"][href*="/settings"]');

    for (var i = 0; i < settingsLinks.length; i++) {
      var link = settingsLinks[i];
      var instanceId = parseInstanceIdFromHref(link.getAttribute("href"));
      if (!instanceId) continue;

      var parent = link.parentElement;
      if (!parent) continue;

      var already = parent.querySelector("[" + ACTION_ICON_ATTR + '][data-cw-instance-id="' + instanceId + '"]');
      if (already) continue;

      var icon = createIconButton(instanceId, link);
      parent.insertBefore(icon, link);
    }
  }

  function ensureUI() {
    ensureModal();
    ensureIconLeftOfBlueBubble();
    ensureFallbackIconNearSettings();
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

    window.addEventListener("locationchange", ensureUI);
  }

  function installDOMObserver() {
    if (!window.MutationObserver) return;

    var observer = new MutationObserver(function () {
      ensureUI();
    });

    observer.observe(document.body, { childList: true, subtree: true });
  }

  installRouteObserver();
  installDOMObserver();
  setInterval(ensureUI, 1500);
  ensureUI();
})();
