(function () {
  "use strict";

  var ACTION_ICON_ATTR = "data-cw-chatwoot-icon";
  var ACTION_DIVIDER_ATTR = "data-cw-chatwoot-divider";
  var PANEL_ID = "cw-instance-chatwoot-panel";
  var PANEL_FALLBACK_ID = "cw-instance-chatwoot-panel-fallback";
  var PANEL_MARKER_ATTR = "data-cw-chatwoot-root";
  var SETTINGS_QUERY_KEY = "chatwoot";
  var ACTIVE_INSTANCE_ID = "";

  var instancesCache = {
    at: 0,
    rows: [],
    byKey: {},
  };

  function qs(root, selector) {
    return root.querySelector(selector);
  }

  function qsa(root, selector) {
    return root.querySelectorAll(selector);
  }

  function parseInstanceIdFromPath(pathname) {
    var m = (pathname || "").match(/\/instances\/([^\/?#]+)\/settings(?:\/|$)/i);
    if (m) return decodeURIComponent(m[1]);

    var fallback = (pathname || "").match(/\/instances\/([^\/?#]+)(?:\/|$)/i);
    return fallback ? decodeURIComponent(fallback[1]) : "";
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

  function isUUID(value) {
    return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(String(value || ""));
  }

  function normalizeStr(value) {
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

  function indexInstanceRows(rows) {
    var index = {};
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i];
      if (!row) continue;

      var keys = [row.id, row.instanceName, row.profileName];
      for (var k = 0; k < keys.length; k++) {
        var key = normalizeStr(keys[k]);
        if (key) index[key] = row;
      }
    }
    return index;
  }

  async function fetchInstances() {
    var now = Date.now();
    if (instancesCache.rows.length && now - instancesCache.at < 15000) {
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
        var normalized = normalizeInstanceRow(data[i]);
        if (normalized) rows.push(normalized);
      }

      instancesCache = {
        at: now,
        rows: rows,
        byKey: indexInstanceRows(rows),
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

    await fetchInstances();
    var hit = instancesCache.byKey[normalizeStr(raw)];
    if (hit && hit.id) return hit.id;

    return raw;
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

    var card = findInstanceCard(el);
    var fromCard = readInstanceNameFromCard(card);
    if (fromCard) return fromCard;

    var current = parseInstanceIdFromPath(window.location.pathname || "");
    return current || "";
  }

  function isOnInstanceSettingsPage() {
    return /\/manager\/instances\/[^\/?#]+\/settings(?:\/|$)/i.test(window.location.pathname || "");
  }

  function shouldShowChatwootPanel() {
    if (!isOnInstanceSettingsPage()) return false;

    var params = new URLSearchParams(window.location.search || "");
    if (!params.has(SETTINGS_QUERY_KEY)) return false;

    var value = (params.get(SETTINGS_QUERY_KEY) || "").trim().toLowerCase();
    return value === "" || value === "1" || value === "true" || value === "yes";
  }

  function goToInstanceChatwootSettings(instanceId) {
    if (!instanceId) return;

    var params = new URLSearchParams(window.location.search || "");
    params.set(SETTINGS_QUERY_KEY, "1");

    var target =
      "/manager/instances/" +
      encodeURIComponent(instanceId) +
      "/settings" +
      (params.toString() ? "?" + params.toString() : "");

    if (window.location.pathname + window.location.search === target) {
      window.dispatchEvent(new Event("locationchange"));
      return;
    }

    // Force full navigation so the settings page always renders.
    window.location.assign(target);
  }

  function closeChatwootPanelQuery() {
    if (!isOnInstanceSettingsPage()) return;

    var params = new URLSearchParams(window.location.search || "");
    if (!params.has(SETTINGS_QUERY_KEY)) return;

    params.delete(SETTINGS_QUERY_KEY);

    var target = window.location.pathname + (params.toString() ? "?" + params.toString() : "");
    history.pushState({}, "", target);
    window.dispatchEvent(new Event("locationchange"));
  }

  function panelHTML() {
    return (
      '<div class="flex items-center justify-between gap-3 mb-4">' +
      '<div class="flex items-center gap-2">' +
      '<svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" style="color:#1f93ff;">' +
      '<path fill="currentColor" d="M0 12c0 6.629 5.371 12 12 12s12-5.371 12-12S18.629 0 12 0 0 5.371 0 12m17.008 5.29H11.44a5.57 5.57 0 0 1-5.562-5.567A5.57 5.57 0 0 1 11.44 6.16a5.57 5.57 0 0 1 5.567 5.563Z"></path>' +
      '</svg>' +
      '<h2 class="text-lg font-semibold text-foreground">Integração Chatwoot</h2>' +
      '</div>' +
      '<button id="cw-close-page" type="button" class="inline-flex items-center rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground hover:bg-accent">Fechar</button>' +
      '</div>' +
      '<p class="text-sm text-muted-foreground mb-4">Configuração por instância com sincronização bidirecional de mensagens e mídia.</p>' +
      '<div class="mb-4 rounded-md border border-input bg-background px-3 py-2 text-xs text-muted-foreground">' +
      '<strong class="text-foreground">Instância ID:</strong> <span id="cw-instance-id-label">-</span>' +
      '</div>' +
      '<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 mb-4">' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-enabled" type="checkbox" class="rounded border-input w-4 h-4"/> Habilitado</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-signMsg" type="checkbox" class="rounded border-input w-4 h-4"/> Assinar mensagem</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-reopenConversation" type="checkbox" class="rounded border-input w-4 h-4"/> Reabrir conversa</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-conversationPending" type="checkbox" class="rounded border-input w-4 h-4"/> Conversa pendente</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-mergeBrazilContacts" type="checkbox" class="rounded border-input w-4 h-4"/> Mesclar contatos BR</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-importContacts" type="checkbox" class="rounded border-input w-4 h-4"/> Importar contatos</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-importMessages" type="checkbox" class="rounded border-input w-4 h-4"/> Importar mensagens</label>' +
      '</div>' +
      '<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">URL Chatwoot</label><input id="cw-url" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="https://chatwoot.seudominio.com"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Account ID</label><input id="cw-accountId" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="1"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Token API</label><input id="cw-token" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="api_access_token"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Inbox ID (Caixa)</label><input id="cw-inboxId" type="number" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="10"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Inbox Identifier (opcional)</label><input id="cw-inboxIdentifier" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="opcional"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Nome Inbox</label><input id="cw-nameInbox" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="Suporte WhatsApp"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Organização</label><input id="cw-organization" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="Minha Empresa"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Logo (URL)</label><input id="cw-logo" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="https://..."/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Dias limite importação</label><input id="cw-daysLimitImportMessages" type="number" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground" value="3"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Delimitador assinatura</label><input id="cw-signDelimiter" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground" value="\\n"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Webhook Secret (opcional)</label><input id="cw-webhookSecret" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground"/></div>' +
      '</div>' +
      '<div class="mt-4">' +
      '<label class="block text-sm font-medium text-foreground mb-1">Ignore JIDs (um por linha)</label>' +
      '<textarea id="cw-ignoreJids" rows="4" class="w-full rounded-md border border-input bg-background px-3 py-2 text-foreground placeholder:text-muted-foreground" placeholder="5511999998888@s.whatsapp.net"></textarea>' +
      '</div>' +
      '<div class="mt-4 rounded-md border border-input bg-background px-3 py-2">' +
      '<div class="text-sm font-medium text-foreground mb-1">Webhook URL para configurar no Chatwoot</div>' +
      '<code id="cw-webhookUrl" class="block break-all text-xs text-muted-foreground"></code>' +
      '</div>' +
      '<div class="mt-4 flex items-center justify-end gap-2">' +
      '<span id="cw-status" class="text-xs text-muted-foreground mr-auto"></span>' +
      '<button id="cw-load" type="button" class="inline-flex items-center rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground hover:bg-accent">Recarregar</button>' +
      '<button id="cw-save" type="button" class="inline-flex items-center rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground hover:bg-primary/90">Salvar</button>' +
      '</div>'
    );
  }

  function createPanel() {
    var panel = document.createElement("div");
    panel.id = PANEL_ID;
    panel.setAttribute(PANEL_MARKER_ATTR, "1");
    panel.className = "rounded-lg border border-sidebar-border bg-card p-6";
    panel.innerHTML = panelHTML();
    return panel;
  }

  function bindPanel(panel) {
    var closeBtn = qs(panel, "#cw-close-page");
    if (closeBtn) {
      closeBtn.addEventListener("click", function () {
        closeChatwootPanelQuery();
      });
    }

    var loadBtn = qs(panel, "#cw-load");
    if (loadBtn) {
      loadBtn.onclick = function () {
        if (!ACTIVE_INSTANCE_ID) return;
        loadConfig(panel, ACTIVE_INSTANCE_ID);
      };
    }

    var saveBtn = qs(panel, "#cw-save");
    if (saveBtn) {
      saveBtn.onclick = function () {
        if (!ACTIVE_INSTANCE_ID) return;
        saveConfig(panel, ACTIVE_INSTANCE_ID);
      };
    }
  }

  function setStatus(root, text, isError) {
    var node = qs(root, "#cw-status");
    if (!node) return;
    node.textContent = text || "";
    node.style.color = isError ? "#ef4444" : "";
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
      setStatus(root, "API key não encontrada no manager.", true);
      return;
    }

    setStatus(root, "Carregando configuração...", false);
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
      setStatus(root, msg, true);
      return;
    }

    fillForm(root, data.data || {});
    setStatus(root, "Configuração carregada.", false);
  }

  async function saveConfig(root, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setStatus(root, "API key não encontrada no manager.", true);
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
      var msg = (data && data.error) || "Falha ao salvar configuração";
      setStatus(root, msg, true);
      return;
    }

    fillForm(root, (data && data.data) || payload);
    setStatus(root, "Configuração salva com sucesso.", false);
  }

  function findSettingsContentContainer() {
    return (
      document.querySelector("div.max-w-4xl.mx-auto.space-y-6") ||
      document.querySelector('div[class*="max-w-4xl"][class*="mx-auto"]') ||
      document.querySelector('div[class*="max-w-4xl"]') ||
      document.querySelector(".flex-1.overflow-y-auto.p-6 .max-w-4xl") ||
      document.querySelector(".flex-1.overflow-y-auto.p-6") ||
      document.querySelector("main")
    );
  }

  function ensureFallbackContainer() {
    var root = document.getElementById(PANEL_FALLBACK_ID);
    if (root) return root;

    root = document.createElement("div");
    root.id = PANEL_FALLBACK_ID;
    root.style.cssText = [
      "position:fixed",
      "inset:0",
      "z-index:999998",
      "overflow:auto",
      "padding:16px",
      "background:rgba(2,6,23,.88)",
    ].join(";");

    var body = document.body || document.documentElement;
    body.appendChild(root);
    return root;
  }

  function removePanelIfExists() {
    var panel = document.getElementById(PANEL_ID);
    if (panel) panel.remove();

    var fallback = document.getElementById(PANEL_FALLBACK_ID);
    if (fallback) fallback.remove();
  }

  async function ensureSettingsPanel() {
    if (!shouldShowChatwootPanel()) {
      removePanelIfExists();
      return;
    }

    var container = findSettingsContentContainer();
    var useFallback = !container;
    if (!container) container = ensureFallbackContainer();

    var panel = document.getElementById(PANEL_ID);
    if (!panel) {
      panel = createPanel();
      if (useFallback) {
        panel.style.maxWidth = "1200px";
        panel.style.margin = "0 auto";
      }
      if (container.firstChild) {
        container.insertBefore(panel, container.firstChild);
      } else {
        container.appendChild(panel);
      }
      bindPanel(panel);
    }

    var currentPathInstance = parseInstanceIdFromPath(window.location.pathname || "");
    var resolvedInstance = await resolveInstanceIdSmart(currentPathInstance);
    var finalInstance = resolvedInstance || currentPathInstance;
    if (!finalInstance) return;

    var label = qs(panel, "#cw-instance-id-label");
    if (label) label.textContent = finalInstance;

    var current = panel.getAttribute("data-cw-instance-id") || "";
    if (current === finalInstance) return;

    panel.setAttribute("data-cw-instance-id", finalInstance);
    ACTIVE_INSTANCE_ID = finalInstance;

    var webhookNode = qs(panel, "#cw-webhookUrl");
    if (webhookNode) {
      webhookNode.textContent = detectApiBaseURL() + "/chatwoot/webhook/" + encodeURIComponent(finalInstance);
    }

    loadConfig(panel, finalInstance);
  }

  function createIconButton(instanceCandidate, referenceEl) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.setAttribute(ACTION_ICON_ATTR, "1");
    if (instanceCandidate) btn.setAttribute("data-cw-instance-id", instanceCandidate);
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

    btn.onclick = async function (ev) {
      ev.preventDefault();
      ev.stopPropagation();

      var dynamicCandidate =
        btn.getAttribute("data-cw-instance-id") ||
        instanceCandidate ||
        resolveInstanceIdFromElement(btn) ||
        readInstanceNameFromCard(findInstanceCard(btn)) ||
        "";

      if (!dynamicCandidate) return;

      var resolved = await resolveInstanceIdSmart(dynamicCandidate);
      if (!resolved || !isUUID(resolved)) return;

      btn.setAttribute("data-cw-instance-id", resolved);
      goToInstanceChatwootSettings(resolved);
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
    var bubbleButtons = document.querySelectorAll(
      'button[title="Enviar mensagem de texto"], button[title*="mensagem"], button[title*="Mensagem"]'
    );

    if (!bubbleButtons.length) {
      var bubblePaths = document.querySelectorAll('svg path[d*="H7l-4 4V5a2 2"]');
      bubbleButtons = [];
      for (var p = 0; p < bubblePaths.length; p++) {
        var iconButton = bubblePaths[p].closest("button, a");
        if (iconButton) bubbleButtons.push(iconButton);
      }
    }

    for (var i = 0; i < bubbleButtons.length; i++) {
      var bubbleButton = bubbleButtons[i];
      if (!bubbleButton) continue;

      var parent = bubbleButton.parentElement;
      if (!parent) continue;

      var instanceCandidate = resolveInstanceIdFromElement(parent);
      var existing = parent.querySelector("[" + ACTION_ICON_ATTR + "]");
      var existingDivider = parent.querySelector("[" + ACTION_DIVIDER_ATTR + "]");

      if (existing) {
        var existingId = existing.getAttribute("data-cw-instance-id") || "";
        if (!isUUID(existingId) && instanceCandidate) {
          resolveInstanceIdSmart(instanceCandidate).then(function (resolved) {
            if (resolved && isUUID(resolved)) {
              existing.setAttribute("data-cw-instance-id", resolved);
            }
          });
        }

        if (!existingDivider) {
          existingDivider = createActionDivider(bubbleButton);
        }

        if (existing.nextSibling !== existingDivider || existingDivider.nextSibling !== bubbleButton) {
          parent.insertBefore(existing, bubbleButton);
          parent.insertBefore(existingDivider, bubbleButton);
        }
      } else {
        var icon = createIconButton(instanceCandidate, bubbleButton);
        if (instanceCandidate && !isUUID(instanceCandidate)) {
          resolveInstanceIdSmart(instanceCandidate).then(function (resolved) {
            if (resolved && isUUID(resolved) && icon && icon.setAttribute) {
              icon.setAttribute("data-cw-instance-id", resolved);
            }
          });
        }
        var divider = existingDivider || createActionDivider(bubbleButton);
        parent.insertBefore(icon, bubbleButton);
        parent.insertBefore(divider, bubbleButton);
      }
    }
  }

  function ensureFallbackIconNearSettings() {
    var settingsLinks = document.querySelectorAll('a[href*="/manager/instances/"][href*="/settings"]');

    for (var i = 0; i < settingsLinks.length; i++) {
      var link = settingsLinks[i];
      var instanceCandidate = parseInstanceIdFromHref(link.getAttribute("href"));
      if (!instanceCandidate) continue;

      var parent = link.parentElement;
      if (!parent) continue;

      var already = parent.querySelector("[" + ACTION_ICON_ATTR + "]");
      if (already) continue;

      var icon = createIconButton(instanceCandidate, link);
      parent.insertBefore(icon, link);
    }
  }

  function ensureUI() {
    ensureIconLeftOfBlueBubble();
    ensureFallbackIconNearSettings();
    ensureSettingsPanel();
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
  setInterval(ensureUI, 1200);
  ensureUI();
})();
