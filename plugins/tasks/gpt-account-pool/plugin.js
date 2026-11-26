// Provider credentials stay in New API. This plugin starts no external service.
const CLIENT_ID = "app_EMoamEEZ73f0CkXaXp7hrann";
const REDIRECT_URI = "http://localhost:1455/auth/callback";
const AUTH_ROOT = "https://auth.openai.com";
const CLAIM = "https://api.openai.com/auth";

export const meta = {
  apiVersion: 1,
  key: "gpt-account-pool",
  name: { en: "GPT account pool", zh: "GPT 账号池" },
  version: "1.0.0",
  author: { name: "ShiKi" },
  description: { en: "ChatGPT account login, shared pools and account quota", zh: "ChatGPT 账号登录、共享号池与账号额度" },
  baseUrl: "https://chatgpt.com",
  allowedHosts: ["auth.openai.com", "chatgpt.com"],
  accountPool: true,
  relay: [
    { incoming_path: "/v1/responses", upstream_path: "/backend-api/codex/responses", converter: "plugin" },
    { incoming_path: "/v1/chat/completions", upstream_path: "/backend-api/codex/responses", converter: "openai_chat_completions_to_openai_responses+plugin" },
    { incoming_path: "/v1/messages", upstream_path: "/backend-api/codex/responses", converter: "claude_messages_to_openai_responses+plugin" },
  ],
};

function claims(token) {
  try {
    return JSON.parse(utils.base64URLDecode(String(token || "").split(".")[1]));
  } catch (e) {
    return {};
  }
}

function category(plan) {
  const normalized = String(plan || "").toLowerCase();
  if (normalized === "pro" || normalized === "prolite" || normalized === "pro_lite") return "pro";
  if (normalized === "plus") return "plus";
  return "unknown";
}

function parseUnixSeconds(value) {
  if (value === null || value === undefined || value === "") return 0;
  if (typeof value === "number") {
    if (!Number.isFinite(value) || value <= 0) return 0;
    return value > 1e12 ? Math.floor(value / 1000) : Math.floor(value);
  }
  const text = String(value).trim();
  if (!text) return 0;
  if (/^\d+(\.\d+)?$/.test(text)) {
    const num = Number(text);
    if (!Number.isFinite(num) || num <= 0) return 0;
    return num > 1e12 ? Math.floor(num / 1000) : Math.floor(num);
  }
  const parsed = Date.parse(text);
  if (!Number.isFinite(parsed) || parsed <= 0) return 0;
  return Math.floor(parsed / 1000);
}

function credentialResult(source, previous) {
  // CPA files and Codex CLI auth.json use these two representations.
  const input = source.tokens || source;
  const access = String(input.access_token || "").trim();
  const refreshToken = String(input.refresh_token || (previous && previous.refresh_token) || "").trim();
  if (!access || access.length > 32768 || refreshToken.length > 16384) throw new Error("Invalid account credential");
  const rawIdToken = String(input.id_token || source.id_token || (previous && previous.id_token) || "").trim();
  const idToken = rawIdToken.length <= 16384 ? rawIdToken : "";
  const payload = claims(access);
  const idPayload = claims(idToken);
  const auth = payload[CLAIM] || idPayload[CLAIM] || {};
  const idAuth = idPayload[CLAIM] || {};
  const accountId = String(auth.chatgpt_account_id || idAuth.chatgpt_account_id || input.account_id || source.account_id || (previous && previous.account_id) || "").trim();
  if (!accountId || accountId.length > 256 || /[\r\n]/.test(accountId)) throw new Error("Account ID is required");
  const email = String(source.email || input.email || payload.email || idPayload.email || (previous && previous.email) || accountId).slice(0, 256);
  // JWT claims are display hints. The usage endpoint confirms the plan later.
  const plan = String(auth.chatgpt_plan_type || idAuth.chatgpt_plan_type || (previous && previous.plan) || "unknown");
  const subscriptionExpiresAt = parseUnixSeconds(
    idAuth.chatgpt_subscription_active_until ||
      auth.chatgpt_subscription_active_until ||
      source.chatgpt_subscription_active_until ||
      source.subscription_active_until ||
      (source.subscription && source.subscription.active_until) ||
      (previous && previous.subscription_expires_at)
  );
  const subscriptionStartAt = parseUnixSeconds(
    idAuth.chatgpt_subscription_active_start ||
      auth.chatgpt_subscription_active_start ||
      source.chatgpt_subscription_active_start ||
      source.subscription_active_start ||
      (source.subscription && source.subscription.active_start) ||
      (previous && previous.subscription_start_at)
  );
  let expiresAt = Number(payload.exp || 0);
  if (input.expires_in) expiresAt = utils.unixNow() + Number(input.expires_in);
  if (!expiresAt) expiresAt = Number(input.expires_at || source.expires_at || input.expiresAt || source.expiresAt || 0);
  if (!expiresAt && source.expired) expiresAt = parseUnixSeconds(source.expired);
  if (!Number.isSafeInteger(expiresAt) || expiresAt < 0) throw new Error("Invalid credential expiry");
  const data = Object.assign({}, previous || {}, {
    access_token: access,
    refresh_token: refreshToken,
    account_id: accountId,
    email,
    plan,
    plan_verified: !!(previous && previous.plan_verified),
    subscription_expires_at: subscriptionExpiresAt,
    subscription_start_at: subscriptionStartAt,
  });
  if (idToken) data.id_token = idToken;
  return {
    credential: data,
    headers: { Authorization: "Bearer " + access, "ChatGPT-Account-Id": accountId },
    expiresAt,
    accountId,
    label: email,
  };
}

function tokenRequest(fields) {
  const body = Object.keys(fields).map(key => encodeURIComponent(key) + "=" + encodeURIComponent(fields[key])).join("&");
  const response = utils.fetch({ url: AUTH_ROOT + "/oauth/token", method: "POST", headers: { "Content-Type": "application/x-www-form-urlencoded" }, body });
  let result = {};
  try { result = (typeof response.body === "string" ? JSON.parse(response.body) : response.body); } catch (e) { /* Keep errors free of vendor data. */ }
  return { status: response.status, data: result };
}

export function login(ctx, input) {
  if (!input) {
    const params = {
      client_id: CLIENT_ID, response_type: "code", redirect_uri: REDIRECT_URI,
      scope: "openid profile email offline_access", prompt: "login", state: ctx.login.state,
      nonce: ctx.login.nonce, code_challenge: ctx.login.codeChallenge,
      code_challenge_method: "S256", id_token_add_organizations: "true", codex_cli_simplified_flow: "true",
    };
    return {
      type: "paste",
      url: AUTH_ROOT + "/oauth/authorize?" + Object.keys(params).map(key => encodeURIComponent(key) + "=" + encodeURIComponent(params[key])).join("&"),
      hint: { en: "After signing in, paste the complete localhost callback address here.", zh: "登录完成后，将浏览器中的完整 localhost 回调地址粘贴到这里。" },
    };
  }
  if (!input.callback || !input.callback.code || input.callback.state !== ctx.login.state) throw new Error("Invalid authorization callback");
  const response = tokenRequest({ client_id: CLIENT_ID, grant_type: "authorization_code", code: input.callback.code, redirect_uri: REDIRECT_URI, code_verifier: ctx.login.codeVerifier });
  if (response.status !== 200) throw new Error("Authorization exchange failed");
  return Object.assign({ type: "done" }, credentialResult(response.data));
}

export function importCredential(ctx, input) {
  if (!input || typeof input !== "object" || Array.isArray(input)) throw new Error("Expected an account JSON object");
  if (input.type && input.type !== "codex") throw new Error("Unsupported account provider");
  return credentialResult(input);
}

export function refresh(ctx) {
  const current = ctx.credential;
  if (!current.refresh_token) return { relogin: true };
  const response = tokenRequest({ client_id: CLIENT_ID, grant_type: "refresh_token", refresh_token: current.refresh_token, scope: "openid profile email" });
  const error = response.data.error;
  const code = typeof error === "object" && error ? error.code : error;
  if (response.status === 401 || (response.status === 400 && ["invalid_grant", "refresh_token_invalidated", "refresh_token_reused", "refresh_token_expired"].indexOf(code) >= 0)) return { relogin: true };
  if (response.status !== 200) throw new Error("Temporary account refresh failure");
  return credentialResult(response.data, current);
}

export function signRequest(ctx) {
  const data = ctx.credential;
  if (!data || !data.access_token || !data.account_id) throw new Error("Account login is required");
  return { headers: { Authorization: "Bearer " + data.access_token, "ChatGPT-Account-Id": data.account_id, "OpenAI-Beta": "responses=experimental", originator: "codex_cli_rs" } };
}

export function convertRequest(ctx, request) {
  const body = request.body;
  if (!body || typeof body !== "object") throw new Error("Expected a Responses request");
  body.model = request.model;
  body.store = false;
  body.stream = true;
  if (body.instructions === undefined || body.instructions === null) body.instructions = "";
  // The backend requires streaming. The host reconstructs synchronous replies.
  // Keep the client's messages, tools, instructions and reasoning effort intact.
  for (const key of ["max_output_tokens", "temperature", "top_p", "frequency_penalty", "presence_penalty", "stream_options"]) delete body[key];
  return { body, stream: true, state: { clientStream: request.stream } };
}

export function convertResponse(ctx, response) {
  return { body: response.body };
}

export function convertStreamEvent(ctx, input) {
  if (input.done) return { events: [] };
  const raw = String(input.data || "").trim();
  if (!raw || raw === "[DONE]") return {};
  let event;
  try { event = JSON.parse(raw); } catch (e) { return { error: { status: 502, code: "invalid_stream", message: "Invalid upstream response event" } }; }
  if (event.type === "error" || event.type === "response.failed") {
    const error = event.error || (event.response && event.response.error) || {};
    return { error: { status: 502, code: String(error.code || "upstream_error"), message: String(error.message || "Upstream generation failed").slice(0, 256) } };
  }
  if (!ctx.state.clientStream) {
    if (event.type === "response.completed" && event.response) return { body: event.response };
    return {};
  }
  return { events: [{ event: input.event, data: event }] };
}

export function credentialRejected(ctx, response) {
  if (response.status === 401) return true;
  if (response.status !== 403) return false;
  try {
    const body = (typeof response.body === "string" ? JSON.parse(response.body) : response.body);
    const error = body.error || body;
    return ["account_deactivated", "account_banned", "token_invalidated"].indexOf(error.code) >= 0;
  } catch (e) { return false; }
}

function quotaWindows(body) {
  const windows = [];
  for (const pair of [["rate_limit", "Account"], ["rate_limits", "Account"], ["code_review_rate_limit", "Code review"]]) {
    const rate = body[pair[0]];
    if (!rate) continue;
    for (const name of ["primary_window", "secondary_window", "primary", "secondary"]) {
      const window = rate[name];
      if (!window || !Number.isFinite(Number(window.used_percent))) continue;
      const seconds = Number(window.limit_window_seconds || Number(window.window_duration_mins || 0) * 60);
      const standardWindow = pair[0] === "rate_limit" || pair[0] === "rate_limits";
      const label = standardWindow && seconds === 18000 ? "5h" : standardWindow && seconds === 604800 ? "7day" : pair[1] + " " + (seconds ? String(seconds / 3600) + "h" : name);
      windows.push({ name: { en: label, zh: label }, used: Math.max(0, Math.min(100, Number(window.used_percent))), limit: 100, unit: "%", resetAt: Number(window.reset_at || window.resets_at || 0) });
    }
  }
  const subscription = body.subscription;
  if (subscription) {
    for (const pair of [["window", "Current window"], ["weekly", "7day"]]) {
      const window = subscription[pair[0]];
      if (!window || !Number.isFinite(Number(window.used_percent))) continue;
      windows.push({ name: { en: pair[1], zh: pair[1] }, used: Math.max(0, Math.min(100, Number(window.used_percent))), limit: 100, unit: "%", resetAt: Number(window.resets_at || window.reset_at || 0) });
    }
  }
  return windows;
}

function parseJSONBody(raw) {
  if (raw && typeof raw === "object") return raw;
  try { return JSON.parse(String(raw || "")); } catch (e) { return null; }
}

function fetchSubscriptionMeta(baseUrl, data) {
  try {
    const response = utils.fetch({
      url: baseUrl + "/backend-api/subscriptions?account_id=" + encodeURIComponent(data.account_id),
      method: "GET",
      headers: {
        Authorization: "Bearer " + data.access_token,
        "ChatGPT-Account-Id": data.account_id,
        Accept: "application/json",
        originator: "codex_cli_rs",
      },
    });
    if (response.status < 200 || response.status >= 300) return null;
    const body = parseJSONBody(response.body);
    if (!body || typeof body !== "object") return null;
    const entitlement = body.entitlement && typeof body.entitlement === "object" ? body.entitlement : {};
    const lastSub = body.last_active_subscription && typeof body.last_active_subscription === "object" ? body.last_active_subscription : {};
    const expiresAt = parseUnixSeconds(body.active_until || body.activeUntil || entitlement.expires_at || entitlement.renews_at);
    const startAt = parseUnixSeconds(body.active_start || body.activeStart);
    const plan = String(entitlement.subscription_plan || "").slice(0, 128);
    const out = {
      subscription_expires_at: expiresAt,
      subscription_start_at: startAt,
      subscription_plan: plan,
    };
    if (typeof lastSub.will_renew === "boolean") out.will_renew = lastSub.will_renew;
    return out;
  } catch (e) {
    return null;
  }
}

function parseResetCreditPayload(body, usageSummary) {
  const summary = usageSummary && typeof usageSummary === "object" ? usageSummary : {};
  const list = body && Array.isArray(body.credits) ? body.credits : body && Array.isArray(body.data) ? body.data : [];
  const credits = [];
  for (const raw of list) {
    if (!raw || typeof raw !== "object") continue;
    const status = String(raw.status || "").toLowerCase();
    if (status && status !== "available") continue;
    credits.push({
      id: String(raw.id || raw.credit_id || "").slice(0, 128),
      title: String(raw.title || "").slice(0, 160),
      status: status || "available",
      reset_type: String(raw.reset_type || raw.resetType || "").slice(0, 64),
      granted_at: parseUnixSeconds(raw.granted_at || raw.grantedAt),
      expires_at: parseUnixSeconds(raw.expires_at || raw.expiresAt),
      supported_by_plan: raw.is_supported_by_plan !== false,
    });
    if (credits.length >= 16) break;
  }
  credits.sort((a, b) => {
    if (!a.expires_at && !b.expires_at) return 0;
    if (!a.expires_at) return 1;
    if (!b.expires_at) return -1;
    return a.expires_at - b.expires_at;
  });
  const rawAvailable = body && Number.isFinite(Number(body.available_count)) ? Number(body.available_count) : null;
  const usageAvailable = Number.isFinite(Number(summary.available_count)) ? Number(summary.available_count) : null;
  const availableCount = Math.max(0, Math.floor(rawAvailable !== null ? rawAvailable : credits.length > 0 ? credits.length : usageAvailable !== null ? usageAvailable : 0));
  const usageApplicable = Number.isFinite(Number(summary.applicable_available_count)) ? Number(summary.applicable_available_count) : null;
  const rawApplicable = body && Number.isFinite(Number(body.applicable_available_count)) ? Number(body.applicable_available_count) : null;
  const applicableCount = Math.max(0, Math.floor(usageApplicable !== null ? usageApplicable : rawApplicable !== null ? rawApplicable : availableCount));
  return {
    available_count: availableCount,
    applicable_available_count: applicableCount,
    credits,
    error: "",
  };
}

function fetchResetCredits(baseUrl, data, usageSummary) {
  try {
    const response = utils.fetch({
      url: baseUrl + "/backend-api/wham/rate-limit-reset-credits",
      method: "GET",
      headers: {
        Authorization: "Bearer " + data.access_token,
        "ChatGPT-Account-Id": data.account_id,
        Accept: "application/json",
        "OpenAI-Beta": "codex-1",
        originator: "Codex Desktop",
      },
    });
    if (response.status >= 200 && response.status < 300) {
      const body = parseJSONBody(response.body);
      if (body && typeof body === "object") {
        return parseResetCreditPayload(body, usageSummary);
      }
    }
    const fallback = parseResetCreditPayload(null, usageSummary);
    fallback.error = "HTTP " + String(response.status);
    return fallback;
  } catch (e) {
    const fallback = parseResetCreditPayload(null, usageSummary);
    fallback.error = "reset_credits_unavailable";
    return fallback;
  }
}

export function scheduled(ctx) {
  const data = ctx.credential;
  const baseUrl = ctx.baseUrl.replace(/\/$/, "");
  const response = utils.fetch({ url: baseUrl + "/backend-api/wham/usage", method: "GET", headers: { Authorization: "Bearer " + data.access_token, "ChatGPT-Account-Id": data.account_id, originator: "codex_cli_rs" } });
  if (response.status === 401) {
    const next = Object.assign({}, data, { quota_checked_at: utils.unixNow(), quota_error: "login_required" });
    return { next: 300, credential: next, keyStatus: "auto_disabled", notify: { en: "Account login is required", zh: "账号需要重新登录" } };
  }
  if (response.status === 403) {
    let code = "";
    try {
      const body = parseJSONBody(response.body);
      const error = body && (body.error || body);
      code = String((error && (error.code || error.type)) || "");
    } catch (e) { /* Do not expose vendor response bodies. */ }
    if (["account_deactivated", "account_banned", "token_invalidated"].indexOf(code) >= 0) {
      const next = Object.assign({}, data, { quota_checked_at: utils.unixNow(), quota_error: "account_banned" });
      return { next: 300, credential: next, keyStatus: "auto_disabled", notify: { en: "Account is unavailable", zh: "账号不可用" } };
    }
    throw new Error("Account quota query was forbidden");
  }
  if (response.status !== 200) throw new Error("Account quota query failed");
  const body = parseJSONBody(response.body);
  if (!body || typeof body !== "object") throw new Error("Invalid account quota response");
  const sub = fetchSubscriptionMeta(baseUrl, data);
  const usageResetSummary = body.rate_limit_reset_credits || body.rateLimitResetCredits || null;
  const resetCredits = fetchResetCredits(baseUrl, data, usageResetSummary);
  const next = Object.assign({}, data, {
    plan: String(body.plan_type || data.plan),
    plan_verified: !!body.plan_type,
    windows: quotaWindows(body),
    reset_credits: resetCredits,
    quota_checked_at: utils.unixNow(),
    quota_error: "",
  });
  if (sub) {
    if (sub.subscription_expires_at) next.subscription_expires_at = sub.subscription_expires_at;
    if (sub.subscription_start_at) next.subscription_start_at = sub.subscription_start_at;
    if (sub.subscription_plan) next.subscription_plan = sub.subscription_plan;
    if (typeof sub.will_renew === "boolean") next.will_renew = sub.will_renew;
  }
  const result = { next: 300, credential: next };
  if (next.windows.length) result.balance = { windows: next.windows };
  return result;
}

export function resetQuota(ctx) {
  const data = ctx.credential;
  if (!data || !data.access_token || !data.account_id) throw new Error("Account login is required");
  const baseUrl = ctx.baseUrl.replace(/\/$/, "");
  const response = utils.fetch({
    url: baseUrl + "/backend-api/wham/rate-limit-reset-credits/consume",
    method: "POST",
    headers: {
      Authorization: "Bearer " + data.access_token,
      "ChatGPT-Account-Id": data.account_id,
      "Content-Type": "application/json",
      Accept: "application/json",
      "OpenAI-Beta": "codex-1",
      originator: "Codex Desktop",
    },
    body: JSON.stringify({ redeem_request_id: utils.uuid() }),
  });
  const body = parseJSONBody(response.body);
  if (response.status < 200 || response.status >= 300) {
    const detail = body && (body.detail || body.message || (body.error && body.error.message));
    const suffix = typeof detail === "string" && detail.trim() ? ": " + detail.trim().slice(0, 200) : "";
    throw new Error("Reset card request failed (HTTP " + String(response.status) + ")" + suffix);
  }
  const code = body && typeof body === "object" ? String(body.code || body.status || "").trim() : "";
  if (code !== "reset" && code !== "already_redeemed" && code !== "success") {
    throw new Error("Reset card redemption was not confirmed");
  }
  const message = body && typeof body.message === "string" ? body.message.trim().slice(0, 256) : "";
  return { code, message };
}

export function describeAccount(ctx) {
  const data = ctx.credential;
  const profile = {
    plan: data.plan || "unknown",
    category: category(data.plan),
    plan_verified: !!data.plan_verified,
    subscription_expires_at: Number(data.subscription_expires_at || 0),
    subscription_start_at: Number(data.subscription_start_at || 0),
    subscription_plan: String(data.subscription_plan || ""),
    supports_reset_credits: true,
    windows: data.windows || [],
    quota_checked_at: data.quota_checked_at || 0,
    quota_error: data.quota_error || "",
  };
  if (typeof data.will_renew === "boolean") profile.will_renew = data.will_renew;
  if (data.reset_credits && typeof data.reset_credits === "object") profile.reset_credits = data.reset_credits;
  return profile;
}
