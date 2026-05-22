(function () {
  "use strict";

  var ACTION_ICON_ATTR = "data-cw-chatwoot-icon";
  var CARD_ID = "cw-instance-chatwoot-card";
  var ANCHOR = "chatwoot-config";

  function qs(root, selector) {
    return root.querySelector(selector);
  }

  function parseInstanceIdFromPath(pathname) {
    var m = (pathname || "").match(/\/instances\/([^\/?#]+)(?:\/settings)?(?:\/|$)/i);
    return m ? decodeURIComponent(m[1]) : "";
  }

  function parseInstanceId() {
    return parseInstanceIdFromPath(window.location.pathname || "");
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

  function isSettingsPage() {
    return /\/manager\/instances\/[^\/?#]+\/settings(?:\/|$)/i.test(window.location.pathname || "");
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
            if (parsed.apiKey && typeof parsed.apiKey === "string") return parsed.apiKey;
            if (parsed.state && typeof parsed.state.apiKey === "string") return parsed.state.apiKey;
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

  function removeLegacyFloatingUI() {
    var oldButton = document.getElementById("cw-instance-settings-btn");
    if (oldButton) oldButton.remove();

    var oldModal = document.getElementById("cw-instance-settings-modal");
    if (oldModal) oldModal.remove();
  }

  function createChatwootIcon(instanceId, referenceEl) {
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
        "border:1px solid rgba(148,163,184,.3)",
        "background:transparent",
        "cursor:pointer",
      ].join(";");
    }

    btn.style.marginLeft = "6px";
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
      window.location.href = "/manager/instances/" + encodeURIComponent(instanceId) + "/settings#" + ANCHOR;
    };

    return btn;
  }

  function ensureInlineInstanceIcons() {
    var settingsLinks = document.querySelectorAll('a[href*="/manager/instances/"][href*="/settings"]');

    for (var i = 0; i < settingsLinks.length; i++) {
      var link = settingsLinks[i];
      var instanceId = parseInstanceIdFromHref(link.getAttribute("href"));
      if (!instanceId) continue;

      var container = link.parentElement;
      if (!container) continue;

      if (container.querySelector("[" + ACTION_ICON_ATTR + '][data-cw-instance-id="' + instanceId + '"]')) {
        continue;
      }

      var icon = createChatwootIcon(instanceId, link);
      var actions = container.querySelectorAll("button, a");
      if (actions.length > 0 && actions[0].parentElement === container) {
        container.insertBefore(icon, actions[0].nextSibling);
      } else {
        container.appendChild(icon);
      }
    }
  }

  function setCardStatus(card, text, isError) {
    var node = qs(card, "[data-cw-status]");
    if (!node) return;
    node.textContent = text || "";
    node.style.color = isError ? "#dc2626" : "#16a34a";
  }

  function fillCardForm(card, data) {
    data = data || {};
    qs(card, "#cw-enabled").checked = !!data.enabled;
    qs(card, "#cw-signMsg").checked = !!data.signMsg;
    qs(card, "#cw-reopenConversation").checked = !!data.reopenConversation;
    qs(card, "#cw-conversationPending").checked = !!data.conversationPending;
    qs(card, "#cw-mergeBrazilContacts").checked = !!data.mergeBrazilContacts;
    qs(card, "#cw-importContacts").checked = !!data.importContacts;
    qs(card, "#cw-importMessages").checked = !!data.importMessages;

    qs(card, "#cw-url").value = data.url || "";
    qs(card, "#cw-accountId").value = data.accountId || "";
    qs(card, "#cw-token").value = data.token || "";
    qs(card, "#cw-inboxId").value = data.inboxId || "";
    qs(card, "#cw-inboxIdentifier").value = data.inboxIdentifier || "";
    qs(card, "#cw-nameInbox").value = data.nameInbox || "";
    qs(card, "#cw-organization").value = data.organization || "";
    qs(card, "#cw-logo").value = data.logo || "";
    qs(card, "#cw-daysLimitImportMessages").value = data.daysLimitImportMessages || 3;
    qs(card, "#cw-signDelimiter").value = data.signDelimiter || "\\n";
    qs(card, "#cw-webhookSecret").value = data.webhookSecret || "";
    qs(card, "#cw-ignoreJids").value = Array.isArray(data.ignoreJids) ? data.ignoreJids.join("\n") : "";
  }

  function readCardForm(card) {
    return {
      enabled: qs(card, "#cw-enabled").checked,
      signMsg: qs(card, "#cw-signMsg").checked,
      reopenConversation: qs(card, "#cw-reopenConversation").checked,
      conversationPending: qs(card, "#cw-conversationPending").checked,
      mergeBrazilContacts: qs(card, "#cw-mergeBrazilContacts").checked,
      importContacts: qs(card, "#cw-importContacts").checked,
      importMessages: qs(card, "#cw-importMessages").checked,
      url: qs(card, "#cw-url").value.trim(),
      accountId: qs(card, "#cw-accountId").value.trim(),
      token: qs(card, "#cw-token").value.trim(),
      inboxId: Number(qs(card, "#cw-inboxId").value || 0),
      inboxIdentifier: qs(card, "#cw-inboxIdentifier").value.trim(),
      nameInbox: qs(card, "#cw-nameInbox").value.trim(),
      organization: qs(card, "#cw-organization").value.trim(),
      logo: qs(card, "#cw-logo").value.trim(),
      daysLimitImportMessages: Number(qs(card, "#cw-daysLimitImportMessages").value || 3),
      signDelimiter: qs(card, "#cw-signDelimiter").value || "\\n",
      webhookSecret: qs(card, "#cw-webhookSecret").value.trim(),
      ignoreJids: qs(card, "#cw-ignoreJids")
        .value.split("\n")
        .map(function (v) {
          return v.trim();
        })
        .filter(Boolean),
    };
  }

  function getSettingsContainer() {
    var byClass = document.querySelector(".max-w-4xl.mx-auto.space-y-6");
    if (byClass) return byClass;

    var cards = document.querySelectorAll("div.rounded-lg.border");
    for (var i = 0; i < cards.length; i++) {
      var title = cards[i].querySelector("h2");
      if (!title) continue;
      var text = (title.textContent || "").toLowerCase();
      if (text.indexOf("webhook") >= 0 || text.indexOf("avanc") >= 0 || text.indexOf("avancad") >= 0) {
        return cards[i].parentElement;
      }
    }

    return null;
  }

  async function loadChatwootConfig(card, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setCardStatus(card, "API key do manager nao encontrada.", true);
      return;
    }

    setCardStatus(card, "Carregando configuracao...", false);

    var resp = await fetch("/chatwoot/find/" + encodeURIComponent(instanceId), {
      method: "GET",
      headers: { apikey: apiKey },
    });

    var data = {};
    try {
      data = await resp.json();
    } catch (_e) {}

    if (!resp.ok) {
      setCardStatus(card, (data && data.error) || "Falha ao carregar configuracao.", true);
      return;
    }

    fillCardForm(card, (data && data.data) || {});
    setCardStatus(card, "Configuracao carregada.", false);
  }

  async function saveChatwootConfig(card, instanceId) {
    var apiKey = detectApiKey();
    if (!apiKey) {
      setCardStatus(card, "API key do manager nao encontrada.", true);
      return;
    }

    var payload = readCardForm(card);
    setCardStatus(card, "Salvando configuracao...", false);

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
      setCardStatus(card, (data && data.error) || "Falha ao salvar configuracao.", true);
      return;
    }

    fillCardForm(card, (data && data.data) || payload);
    setCardStatus(card, "Configuracao salva com sucesso.", false);
  }

  function createSettingsCard(instanceId) {
    var card = document.createElement("div");
    card.id = CARD_ID;
    card.setAttribute("data-cw-instance-id", instanceId);
    card.className = "rounded-lg border border-sidebar-border bg-card p-6";

    var baseURL = detectApiBaseURL();

    card.innerHTML =
      '<h2 class="text-lg font-semibold text-foreground mb-4" id="' + ANCHOR + '">Configuracoes do Chatwoot</h2>' +
      '<p class="text-sm text-muted-foreground mb-4">Configuracao por instancia. Cada instancia pode usar inbox diferente no Chatwoot.</p>' +
      '<div class="space-y-4">' +
      '<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-enabled" type="checkbox" class="rounded border-input"/> Habilitado</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-signMsg" type="checkbox" class="rounded border-input"/> Assinar mensagem</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-reopenConversation" type="checkbox" class="rounded border-input"/> Reabrir conversa</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-conversationPending" type="checkbox" class="rounded border-input"/> Conversa pendente</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-mergeBrazilContacts" type="checkbox" class="rounded border-input"/> Mesclar contatos BR</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-importContacts" type="checkbox" class="rounded border-input"/> Importar contatos</label>' +
      '<label class="flex items-center gap-2 text-sm text-foreground"><input id="cw-importMessages" type="checkbox" class="rounded border-input"/> Importar mensagens</label>' +
      '</div>' +
      '<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">URL Chatwoot</label><input id="cw-url" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="https://chatwoot.seudominio.com"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Account ID</label><input id="cw-accountId" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="1"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Token API</label><input id="cw-token" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="api_access_token"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Inbox ID (caixa)</label><input id="cw-inboxId" type="number" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="10"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Inbox Identifier</label><input id="cw-inboxIdentifier" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="opcional"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Nome Inbox</label><input id="cw-nameInbox" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="Suporte WhatsApp"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Organizacao</label><input id="cw-organization" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="Minha Empresa"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Logo (URL)</label><input id="cw-logo" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="https://..."/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Dias limite importacao</label><input id="cw-daysLimitImportMessages" type="number" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" value="3"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Delimitador assinatura</label><input id="cw-signDelimiter" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" value="\\n"/></div>' +
      '<div><label class="block text-sm font-medium text-foreground mb-1">Webhook Secret</label><input id="cw-webhookSecret" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="opcional"/></div>' +
      '</div>' +
      '<div>' +
      '<label class="block text-sm font-medium text-foreground mb-1">Ignore JIDs (um por linha)</label>' +
      '<textarea id="cw-ignoreJids" rows="4" class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" placeholder="5511999998888@s.whatsapp.net"></textarea>' +
      '</div>' +
      '<div class="rounded-md border border-input bg-background/40 p-3">' +
      '<div class="text-sm font-medium text-foreground mb-1">Webhook URL</div>' +
      '<code class="text-xs break-all text-muted-foreground">' + baseURL + '/chatwoot/webhook/' + encodeURIComponent(instanceId) + '</code>' +
      '</div>' +
      '<div class="flex items-center justify-end gap-2">' +
      '<span data-cw-status class="text-sm text-muted-foreground flex-1"></span>' +
      '<button type="button" id="cw-load" class="rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground">Recarregar</button>' +
      '<button type="button" id="cw-save" class="rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground">Salvar Chatwoot</button>' +
      '</div>' +
      '</div>';

    qs(card, "#cw-load").onclick = function () {
      loadChatwootConfig(card, instanceId);
    };

    qs(card, "#cw-save").onclick = function () {
      saveChatwootConfig(card, instanceId);
    };

    return card;
  }

  function ensureSettingsCard() {
    var current = document.getElementById(CARD_ID);

    if (!isSettingsPage()) {
      if (current) current.remove();
      return;
    }

    var instanceId = parseInstanceId();
    if (!instanceId) return;

    var container = getSettingsContainer();
    if (!container) return;

    if (current) {
      if (current.getAttribute("data-cw-instance-id") === instanceId) {
        return;
      }
      current.remove();
    }

    var card = createSettingsCard(instanceId);
    container.appendChild(card);
    loadChatwootConfig(card, instanceId);

    if ((window.location.hash || "").replace(/^#/, "") === ANCHOR) {
      setTimeout(function () {
        card.scrollIntoView({ behavior: "smooth", block: "start" });
      }, 150);
    }
  }

  function ensureUI() {
    removeLegacyFloatingUI();
    ensureInlineInstanceIcons();
    ensureSettingsCard();
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

    window.addEventListener("hashchange", ensureUI);
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
