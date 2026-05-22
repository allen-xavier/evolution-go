(function () {
  "use strict";

  var ACTION_ICON_ATTR = "data-cw-chatwoot-icon";
  var ACTION_DIVIDER_ATTR = "data-cw-chatwoot-divider";
  var MODAL_ID = "cw-instance-chatwoot-modal";
  var CACHE_TTL_MS = 15000;

  var ACTIVE_INSTANCE_ID = "";
  var instancesCache = {
    at: 0,
    rows: [],
    byKey: {},
  };

  function qs(root, selector) {
    return root.querySelector(selector);
  }

  function isUUID(value) {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(String(value || ""));
  }

  function norm(value) {
    return String(value || "").trim().toLowerCase();
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

  function normalizeInstanceRow(raw) {
    if (!raw || typeof raw !== "object") return null;

    var id = raw.id || raw.instanceId || raw.instanceID || "";
    var instanceName = raw.instanceName || raw.name || raw.instance || "";
    var profileName = raw.profileName || raw.profile_name || "";

    return {
      id: String(id || "").trim(),
      instanceName: String(instanceName || "").trim(),
      profileName: String(profileName || "").trim(),
    };
  }

  function indexRows(rows) {
    var index = {};
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      if (!row) continue;

      var keys = [row.id, row.instanceName, row.profileName];
      for (var k = 0; k < keys.length; k++) {
        var key = norm(keys[k]);
        if (key) index[key] = row;
      }
    }
    return index;
  }

  async function fetchInstances(force) {
    var now = Date.now();
    if (!force && instancesCache.rows.length && now - instancesCache.at < CACHE_TTL_MS) {
      return instancesCache.rows;
    }

    var apiKey = detectApiKey();
    if (!apiKey) return instancesCache.rows || [];

    try {
      var resp = await fetch("/instance/all", {
        method: "GET",
        headers: { apikey: apiKey },
      });

      var body = {};
      try {
        body = await resp.json();
      } catch (_e) {}

      if (!resp.ok) return instancesCache.rows || [];

      var data = Array.isArray(body) ? body : Array.isArray(body.data) ? body.data : [];
      var rows = [];
      for (var i = 0; i < data.length; i++) {
        var item = normalizeInstanceRow(data[i]);
        if (item) rows.push(item);
      }

      instancesCache = {
        at: now,
        rows: rows,
        byKey: indexRows(rows),
      };

      return rows;
    } catch (_e) {
      return instancesCache.rows || [];
    }
  }

  async function resolveInstanceIdSmart(candidate) {
    var raw = String(candidate || "").trim();
    if (!raw) return "";
    if (isUUID(raw)) return raw;

    await fetchInstances(false);
    var hit = instancesCache.byKey[norm(raw)];
    if (hit && hit.id) return hit.id;

    await fetchInstances(true);
    hit = instancesCache.byKey[norm(raw)];
    if (hit && hit.id) return hit.id;

    return "";
  }

  function findInstanceCard(el) {
    if (!el || !el.closest) return null;
    return (
      el.closest('[class*="group"][class*="relative"][class*="overflow-hidden"]') ||
      el.closest('[class*="group"][class*="relative"]') ||
      el.closest('[class*="overflow-hidden"]')
    );
  }

  function readInstanceNameFromCard(card) {
    if (!card || !card.querySelector) return "";

    var subtitle = card.querySelector("h3 + p");
    if (subtitle) {
      var text = (subtitle.textContent || "").trim();
      if (text) return text;
    }

    var heading = card.querySelector("h3");
    if (heading) {
      var fallback = (heading.textContent || "").trim();
      if (fallback) return fallback;
    }

    return "";
  }

  function resolveCandidateFromElement(el) {
    var node = el;
    for (var i = 0; i < 8 && node; i++) {
      if (node.getAttribute) {
        var explicit =
          node.getAttribute("data-cw-instance-id") ||
          node.getAttribute("data-cw-instance-candidate") ||
          node.getAttribute("data-instance-id");
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

    var card = findInstanceCard(el);
    var fromCard = readInstanceNameFromCard(card);
    if (fromCard) return fromCard;

    var fromPath = parseInstanceIdFromPath(window.location.pathname || "");
    return fromPath || "";
  }

  function setStatus(root, text, isError) {
    var node = qs(root, "#cw-status");
    if (!node) return;
    node.textContent = text || "";
    node.style.color = isError ? "#ef4444" : "#94a3b8";
  }

  function fillForm(root, data) {
    data = data || {};
    qs(root, "#cw-enabled").checked = !!data.enabled;
    qs(root, "#cw-signMsg").checked = !!data.signMsg;
    qs(root, "#cw-reopenConversation").checked = !!data.reopenConversation;
    qs(root, "#cw-conversationPending").checked = !!data.conversationPending;
    qs(root, "#cw-mergeBrazilContacts").checked = !!data.mergeBrazilContacts;
    qs(root, "#cw-importContacts").checked = !!data.importContacts;
    qs(root, "#cw-importMessages").checked = !!data.importMessages;

    qs(root, "#cw-url").value = data.url || "";
    qs(root, "#cw-accountId").value = data.accountId || "";
    qs(root, "#cw-token").value = data.token || "";
    qs(root, "#cw-inboxId").value = data.inboxId || "";
    qs(root, "#cw-inboxIdentifier").value = data.inboxIdentifier || "";
    qs(root, "#cw-nameInbox").value = data.nameInbox || "";
    qs(root, "#cw-organization").value = data.organization || "";
    qs(root, "#cw-logo").value = data.logo || "";
    qs(root, "#cw-daysLimitImportMessages").value = data.daysLimitImportMessages || 3;
    qs(root, "#cw-signDelimiter").value = data.signDelimiter || "\\n";
    qs(root, "#cw-webhookSecret").value = data.webhookSecret || "";
    qs(root, "#cw-ignoreJids").value = Array.isArray(data.ignoreJids) ? data.ignoreJids.join("\n") : "";
  }

  function readForm(root) {
    return {
      enabled: qs(root, "#cw-enabled").checked,
      signMsg: qs(root, "#cw-signMsg").checked,
      reopenConversation: qs(root, "#cw-reopenConversation").checked,
      conversationPending: qs(root, "#cw-conversationPending").checked,
      mergeBrazilContacts: qs(root, "#cw-mergeBrazilContacts").checked,
      importContacts: qs(root, "#cw-importContacts").checked,
      importMessages: qs(root, "#cw-importMessages").checked,
      url: qs(root, "#cw-url").value.trim(),
      accountId: qs(root, "#cw-accountId").value.trim(),
      token: qs(root, "#cw-token").value.trim(),
      inboxId: Number(qs(root, "#cw-inboxId").value || 0),
      inboxIdentifier: qs(root, "#cw-inboxIdentifier").value.trim(),
      nameInbox: qs(root, "#cw-nameInbox").value.trim(),
      organization: qs(root, "#cw-organization").value.trim(),
      logo: qs(root, "#cw-logo").value.trim(),
      daysLimitImportMessages: Number(qs(root, "#cw-daysLimitImportMessages").value || 3),
      signDelimiter: qs(root, "#cw-signDelimiter").value || "\\n",
      webhookSecret: qs(root, "#cw-webhookSecret").value.trim(),
      ignoreJids: qs(root, "#cw-ignoreJids")
        .value.split("\n")
        .map(function (v) {
          return v.trim();
        })
        .filter(Boolean),
    };
  }

  async function loadConfig(root, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setStatus(root, "API key nao encontrada no manager.", true);
      return;
    }

    setStatus(root, "Carregando configuracao...", false);
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
      setStatus(root, msg, true);
      return;
    }

    fillForm(root, data.data || {});
    setStatus(root, "Configuracao carregada.", false);
  }

  async function saveConfig(root, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setStatus(root, "API key nao encontrada no manager.", true);
      return;
    }

    var payload = readForm(root);
    setStatus(root, "Salvando...", false);

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
      setStatus(root, msg, true);
      return;
    }

    fillForm(root, (data && data.data) || payload);
    setStatus(root, "Configuracao salva com sucesso.", false);
  }

  function ensureModal() {
    var modal = document.getElementById(MODAL_ID);
    if (modal) return modal;

    modal = document.createElement("div");
    modal.id = MODAL_ID;
    modal.style.cssText = [
      "position:fixed",
      "inset:0",
      "display:none",
      "align-items:center",
      "justify-content:center",
      "padding:16px",
      "background:rgba(2,6,23,.85)",
      "z-index:999999",
      "overflow:auto",
      "font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto,Ubuntu,sans-serif",
    ].join(";");

    modal.innerHTML =
      '<div style="width:min(980px,96vw);max-height:94vh;overflow:auto;background:#0b1220;border:1px solid rgba(148,163,184,.22);border-radius:14px;box-shadow:0 20px 60px rgba(0,0,0,.5);padding:16px;">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;gap:8px;margin-bottom:10px;">' +
      '<div style="display:flex;align-items:center;gap:8px;">' +
      '<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" style="color:#1f93ff;">' +
      '<path fill="currentColor" d="M0 12c0 6.629 5.371 12 12 12s12-5.371 12-12S18.629 0 12 0 0 5.371 0 12m17.008 5.29H11.44a5.57 5.57 0 0 1-5.562-5.567A5.57 5.57 0 0 1 11.44 6.16a5.57 5.57 0 0 1 5.567 5.563Z"></path>' +
      '</svg>' +
      '<h3 style="margin:0;font-size:18px;color:#e2e8f0;">Chatwoot da Instancia</h3>' +
      '</div>' +
      '<button id="cw-close" type="button" style="border:1px solid rgba(148,163,184,.3);background:#111827;color:#e2e8f0;border-radius:8px;padding:6px 10px;cursor:pointer;">Fechar</button>' +
      '</div>' +
      '<p style="margin:0 0 10px;color:#94a3b8;font-size:13px;">Configuracao por instancia com sincronizacao bidirecional de mensagens e midia.</p>' +
      '<div style="margin:0 0 10px;padding:8px;border:1px solid rgba(148,163,184,.25);border-radius:8px;background:#0f172a;color:#94a3b8;font-size:12px;">Instancia ID: <strong id="cw-instance-id" style="color:#e2e8f0;"></strong></div>' +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;margin-bottom:10px;">' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-enabled" type="checkbox"/> Habilitado</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-signMsg" type="checkbox"/> Assinar mensagem</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-reopenConversation" type="checkbox"/> Reabrir conversa</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-conversationPending" type="checkbox"/> Conversa pendente</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-mergeBrazilContacts" type="checkbox"/> Mesclar contatos BR</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-importContacts" type="checkbox"/> Importar contatos</label>' +
      '<label style="display:flex;gap:8px;align-items:center;color:#e2e8f0;font-size:14px;"><input id="cw-importMessages" type="checkbox"/> Importar mensagens</label>' +
      '</div>' +
      '<div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:10px;">' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">URL Chatwoot</div><input id="cw-url" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" placeholder="https://chatwoot.seudominio.com"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Account ID</div><input id="cw-accountId" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" placeholder="1"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Token API</div><input id="cw-token" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" placeholder="api_access_token"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Inbox ID (Caixa)</div><input id="cw-inboxId" type="number" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" placeholder="10"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Inbox Identifier (opcional)</div><input id="cw-inboxIdentifier" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Nome Inbox</div><input id="cw-nameInbox" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Organizacao</div><input id="cw-organization" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Logo (URL)</div><input id="cw-logo" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Dias limite importacao</div><input id="cw-daysLimitImportMessages" type="number" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" value="3"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Delimitador assinatura</div><input id="cw-signDelimiter" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;" value="\\n"/></div>' +
      '<div><div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Webhook Secret (opcional)</div><input id="cw-webhookSecret" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"/></div>' +
      '</div>' +
      '<div style="margin-top:10px;">' +
      '<div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Ignore JIDs (um por linha)</div>' +
      '<textarea id="cw-ignoreJids" rows="4" style="width:100%;padding:8px;border:1px solid rgba(148,163,184,.35);background:#0f172a;color:#e2e8f0;border-radius:8px;"></textarea>' +
      '</div>' +
      '<div style="margin-top:10px;padding:8px;border:1px solid rgba(148,163,184,.25);border-radius:8px;background:#0f172a;">' +
      '<div style="font-size:12px;color:#94a3b8;margin-bottom:4px;">Webhook URL para configurar no Chatwoot</div>' +
      '<code id="cw-webhookUrl" style="display:block;word-break:break-all;color:#e2e8f0;font-size:12px;"></code>' +
      '</div>' +
      '<div style="margin-top:12px;display:flex;gap:8px;align-items:center;justify-content:flex-end;">' +
      '<span id="cw-status" style="font-size:12px;color:#94a3b8;flex:1 1 auto;"></span>' +
      '<button id="cw-load" type="button" style="border:1px solid rgba(148,163,184,.35);background:#111827;color:#e2e8f0;border-radius:8px;padding:8px 10px;cursor:pointer;">Recarregar</button>' +
      '<button id="cw-save" type="button" style="border:none;background:#0284c7;color:#fff;border-radius:8px;padding:8px 12px;cursor:pointer;">Salvar</button>' +
      '</div>' +
      '</div>';

    document.body.appendChild(modal);

    qs(modal, "#cw-close").onclick = function () {
      modal.style.display = "none";
    };

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

  function openModalForInstance(instanceId) {
    if (!instanceId || !isUUID(instanceId)) return;

    var modal = ensureModal();
    ACTIVE_INSTANCE_ID = instanceId;

    var idNode = qs(modal, "#cw-instance-id");
    if (idNode) idNode.textContent = instanceId;

    var webhookNode = qs(modal, "#cw-webhookUrl");
    if (webhookNode) {
      webhookNode.textContent = detectApiBaseURL() + "/chatwoot/webhook/" + encodeURIComponent(instanceId);
    }

    modal.style.display = "flex";
    loadConfig(modal, instanceId);
  }

  async function openModalFromCandidate(candidate) {
    var resolved = await resolveInstanceIdSmart(candidate);
    if (!resolved || !isUUID(resolved)) return;
    openModalForInstance(resolved);
  }

  function createIconButton(instanceCandidate, referenceEl) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.setAttribute(ACTION_ICON_ATTR, "1");
    if (instanceCandidate) btn.setAttribute("data-cw-instance-candidate", instanceCandidate);
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
    btn.style.color = "#1f93ff";
    btn.innerHTML =
      '<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">' +
      '<path fill="currentColor" d="M0 12c0 6.629 5.371 12 12 12s12-5.371 12-12S18.629 0 12 0 0 5.371 0 12m17.008 5.29H11.44a5.57 5.57 0 0 1-5.562-5.567A5.57 5.57 0 0 1 11.44 6.16a5.57 5.57 0 0 1 5.567 5.563Z"></path>' +
      '</svg>';

    btn.onclick = function (ev) {
      ev.preventDefault();
      ev.stopPropagation();

      var candidate =
        btn.getAttribute("data-cw-instance-id") ||
        btn.getAttribute("data-cw-instance-candidate") ||
        resolveCandidateFromElement(btn) ||
        "";

      if (!candidate) return;
      openModalFromCandidate(candidate);
    };

    return btn;
  }

  function createActionDivider(referenceEl) {
    var divider = document.createElement("div");
    divider.setAttribute(ACTION_DIVIDER_ATTR, "1");

    var refDivider = referenceEl ? referenceEl.previousElementSibling : null;
    var refClass = refDivider && refDivider.className ? String(refDivider.className) : "";
    if (refClass && (refClass.indexOf("w-px") >= 0 || refClass.indexOf("bg-sidebar-border") >= 0)) {
      divider.className = refClass;
    } else {
      divider.className = "w-px bg-sidebar-border";
      divider.style.width = "1px";
      divider.style.background = "rgba(148,163,184,.35)";
    }

    return divider;
  }

  function ensureIconLeftOfBlueBubble() {
    var anchors = [];
    var seen = new Set();

    var bubbleButtons = document.querySelectorAll(
      'button[title="Enviar mensagem de texto"], button[title*="mensagem"], button[title*="Mensagem"]'
    );
    for (var bb = 0; bb < bubbleButtons.length; bb++) {
      anchors.push(bubbleButtons[bb]);
    }

    var bubblePaths = document.querySelectorAll('svg path[d*="H7l-4 4V5a2 2"]');
    for (var bp = 0; bp < bubblePaths.length; bp++) {
      var fromPath = bubblePaths[bp].closest("button, a");
      if (fromPath) anchors.push(fromPath);
    }

    var settingsPaths = document.querySelectorAll('svg path[d*="M12.22 2h-.44a2 2"]');
    for (var sp = 0; sp < settingsPaths.length; sp++) {
      var fromSettings = settingsPaths[sp].closest("button, a");
      if (fromSettings) anchors.push(fromSettings);
    }

    for (var i = 0; i < anchors.length; i++) {
      var anchor = anchors[i];
      if (!anchor || !anchor.parentElement) continue;

      var parent = anchor.parentElement;
      if (seen.has(parent)) continue;
      seen.add(parent);

      var bubbleButton = parent.querySelector(
        'button[title="Enviar mensagem de texto"], button[title*="mensagem"], button[title*="Mensagem"]'
      );
      if (!bubbleButton) {
        var bubblePathInParent = parent.querySelector('svg path[d*="H7l-4 4V5a2 2"]');
        if (bubblePathInParent && bubblePathInParent.closest) {
          bubbleButton = bubblePathInParent.closest("button, a");
        }
      }

      var targetButton = bubbleButton || anchor;
      if (!targetButton) continue;

      var instanceCandidate = resolveCandidateFromElement(parent);
      var existing = parent.querySelector("[" + ACTION_ICON_ATTR + "]");
      var existingDivider = parent.querySelector("[" + ACTION_DIVIDER_ATTR + "]");

      if (existing) {
        if (instanceCandidate && !existing.getAttribute("data-cw-instance-candidate")) {
          existing.setAttribute("data-cw-instance-candidate", instanceCandidate);
        }

        if (instanceCandidate && !existing.getAttribute("data-cw-instance-id")) {
          (function (btnRef, candidateRef) {
            resolveInstanceIdSmart(candidateRef).then(function (resolved) {
              if (resolved && isUUID(resolved) && btnRef && btnRef.isConnected) {
                btnRef.setAttribute("data-cw-instance-id", resolved);
              }
            });
          })(existing, instanceCandidate);
        }

        if (!existingDivider) {
          existingDivider = createActionDivider(targetButton);
        }

        if (
          existing.parentElement !== parent ||
          existing.nextElementSibling !== existingDivider ||
          existingDivider.nextElementSibling !== targetButton
        ) {
          parent.insertBefore(existing, targetButton);
          parent.insertBefore(existingDivider, targetButton);
        }
        continue;
      }

      var icon = createIconButton(instanceCandidate, targetButton);
      if (instanceCandidate && isUUID(instanceCandidate)) {
        icon.setAttribute("data-cw-instance-id", instanceCandidate);
      } else if (instanceCandidate) {
        (function (btnRef, candidateRef) {
          resolveInstanceIdSmart(candidateRef).then(function (resolved) {
            if (resolved && isUUID(resolved) && btnRef && btnRef.isConnected) {
              btnRef.setAttribute("data-cw-instance-id", resolved);
            }
          });
        })(icon, instanceCandidate);
      }

      var divider = existingDivider || createActionDivider(targetButton);
      parent.insertBefore(icon, targetButton);
      parent.insertBefore(divider, targetButton);
    }
  }

  function ensureFallbackIconNearSettings() {
    var settingsLinks = document.querySelectorAll('a[href*="/manager/instances/"][href*="/settings"]');

    for (var i = 0; i < settingsLinks.length; i++) {
      var link = settingsLinks[i];
      var candidate = parseInstanceIdFromHref(link.getAttribute("href"));
      if (!candidate) continue;

      var parent = link.parentElement;
      if (!parent) continue;

      var existing = parent.querySelector("[" + ACTION_ICON_ATTR + "]");
      if (existing) continue;

      var icon = createIconButton(candidate, link);
      if (isUUID(candidate)) icon.setAttribute("data-cw-instance-id", candidate);
      parent.insertBefore(icon, link);
    }
  }

  function safeEnsureUI() {
    try {
      ensureIconLeftOfBlueBubble();
      ensureFallbackIconNearSettings();
    } catch (_e) {}
  }

  var ensureScheduled = false;
  function scheduleEnsureUI() {
    if (ensureScheduled) return;
    ensureScheduled = true;
    setTimeout(function () {
      ensureScheduled = false;
      safeEnsureUI();
    }, 40);
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

    window.addEventListener("locationchange", scheduleEnsureUI);
  }

  function installDOMObserver() {
    if (!window.MutationObserver || !document.body) return;

    var observer = new MutationObserver(function () {
      scheduleEnsureUI();
    });

    observer.observe(document.body, { childList: true, subtree: true });
  }

  function start() {
    ensureModal();
    installRouteObserver();
    installDOMObserver();
    setInterval(scheduleEnsureUI, 1200);
    scheduleEnsureUI();
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", start, { once: true });
  } else {
    start();
  }
})();
