"use strict";
const $ = (id) => document.getElementById(id);
const descriptions = {
  support: "Investigate GitHub issues and draft responses.",
  devops: "Investigate service health and logs.",
  release: "Inspect releases and configured workflows.",
  monitor: "Check service responsiveness and health changes.",
  alert: "Inspect incidents and notification delivery.",
};
let services = [],
  health = {},
  selected = "",
  busy = false;
const chats = new Map();
function element(tag, text, cls) {
  const el = document.createElement(tag);
  if (text !== undefined) el.textContent = text;
  if (cls) el.className = cls;
  return el;
}
async function api(path, options) {
  const response = await fetch(path, options);
  const raw = await response.text();
  let data;
  try {
    data = JSON.parse(raw);
  } catch {
    data = { error: raw };
  }
  if (!response.ok)
    throw new Error(data.error || `Request failed (${response.status})`);
  return data;
}
function conversation(name) {
  if (!chats.has(name))
    chats.set(name, { session: crypto.randomUUID(), messages: [] });
  return chats.get(name);
}
function controls() {
  $("prompt").disabled = !selected || busy;
  $("send").disabled = !selected || busy;
  $("new-chat").disabled = !selected || busy;
  $("send").textContent = busy ? "Working…" : "Send";
}
function renderChat() {
  const box = $("messages");
  box.replaceChildren();
  $("chat-name").textContent = selected || "Choose an agent";
  $("chat-description").textContent = selected
    ? descriptions[selected] || "An agent discovered through Go Micro."
    : "Agents appear when they register with Go Micro.";
  if (!selected) {
    box.append(
      element(
        "p",
        services.some((s) => s.agent)
          ? "Choose an agent to start a conversation."
          : "No agents are available. Start your services with micro run, then refresh.",
        "empty",
      ),
    );
    controls();
    return;
  }
  const chat = conversation(selected);
  if (!chat.messages.length)
    box.append(element("p", `Start a conversation with ${selected}.`, "empty"));
  for (const msg of chat.messages) {
    const node = element(
      "article",
      undefined,
      "message" + (msg.error ? " error" : ""),
    );
    node.append(element("h3", msg.role), element("div", msg.text, "text"));
    for (const tool of msg.tools || []) {
      const details = element("details");
      details.append(element("summary", tool.name));
      details.append(
        element(
          "pre",
          `Input\n${tool.input || "{}"}\n\nResult\n${tool.result || "(no result)"}`,
        ),
      );
      node.append(details);
    }
    if (msg.run) node.append(element("small", `Run ${msg.run}`));
    box.append(node);
  }
  box.scrollTop = box.scrollHeight;
  controls();
}
function renderAgents() {
  const names = [
    ...new Set(services.filter((s) => s.agent).map((s) => s.name)),
  ];
  if (selected && !names.includes(selected)) selected = "";
  $("agent-count").textContent = names.length;
  $("agent-list").replaceChildren();
  for (const name of names) {
    const button = element("button");
    button.type = "button";
    button.setAttribute("aria-pressed", String(name === selected));
    const dot = element("span", undefined, `dot ${health[name]?.status || ""}`);
    dot.title = health[name]?.status || "Health not available";
    dot.setAttribute("aria-hidden", "true");
    button.append(dot, element("span", name));
    button.onclick = () => {
      selected = name;
      renderAgents();
      renderChat();
    };
    $("agent-list").append(button);
  }
  if (!names.length)
    $("agent-list").append(element("p", "No agents discovered.", "muted"));
}
function renderServices() {
  const box = $("service-list");
  box.replaceChildren();
  if (!services.length)
    box.append(
      element(
        "p",
        "No services discovered. Check that Studio and your services use the same registry.",
        "empty",
      ),
    );
  for (const svc of services) {
    const card = element("article", undefined, "service"),
      row = element("div", undefined, "row");
    row.append(
      element("h2", svc.name),
      element(
        "span",
        `${svc.instances} instance${svc.instances === 1 ? "" : "s"} · ${health[svc.name]?.status || "health unavailable"}`,
        "badge",
      ),
    );
    card.append(row);
    card.append(
      element(
        "small",
        `Version ${svc.version || "unspecified"}${svc.agent ? " · Agent" : ""}`,
      ),
    );
    if (health[svc.name]?.error)
      card.append(element("p", health[svc.name].error, "muted"));
    for (const ep of svc.endpoints || []) {
      const detail = element("details");
      detail.append(element("summary", ep.name));
      detail.append(
        element(
          "pre",
          JSON.stringify(
            { request: ep.request, response: ep.response },
            null,
            2,
          ),
        ),
      );
      card.append(detail);
    }
    box.append(card);
  }
}
function renderAlerts(data) {
  const box = $("alert-list");
  box.replaceChildren();
  const incidents = data.incidents || [],
    notices = data.notifications || [];
  box.append(
    element(
      "p",
      `${incidents.length} open incident${incidents.length === 1 ? "" : "s"} · Delivery: ${data.destination}`,
      "muted",
    ),
  );
  for (const incident of incidents) {
    const card = element("article", undefined, "alert");
    card.append(
      element("h2", incident.service),
      element("p", "Open incident"),
      element("small", `Opened ${incident.opened_at}`),
    );
    box.append(card);
  }
  for (const notice of notices) {
    const card = element("article", undefined, "alert");
    const row = element("div", undefined, "row");
    row.append(
      element("h2", notice.service),
      element("span", notice.status, "badge"),
    );
    card.append(row, element("p", notice.text));
    card.append(
      element(
        "small",
        `${notice.created_at} · ${notice.delivered_at ? "Delivered " + notice.delivered_at : "Delivery pending"} · ${notice.attempts} attempt(s)`,
      ),
    );
    if (notice.error) card.append(element("p", notice.error, "muted"));
    box.append(card);
  }
  if (!incidents.length && !notices.length)
    box.append(element("p", "No incidents recorded.", "empty"));
}
let refreshing = false;
async function refresh() {
  if (refreshing) return;
  refreshing = true;
  $("refresh").disabled = true;
  const [inventory, status, alerts] = await Promise.allSettled([
    api("/api/services"),
    api("/api/health"),
    api("/api/alerts"),
  ]);
  const warnings = [];
  if (inventory.status === "fulfilled") services = inventory.value;
  else warnings.push("Discovery unavailable: " + inventory.reason.message);
  health = {};
  if (status.status === "fulfilled")
    for (const s of status.value.services || []) health[s.service] = s;
  else warnings.push("Monitor unavailable: " + status.reason.message);
  const previous = selected;
  renderAgents();
  renderServices();
  if (previous !== selected || !selected || !$("messages").children.length)
    renderChat();
  if (alerts.status === "fulfilled") renderAlerts(alerts.value);
  else {
    $("alert-list").replaceChildren(
      element("p", "Alert unavailable: " + alerts.reason.message, "muted"),
    );
  }
  $("notice").textContent = warnings.join(" ");
  $("notice").hidden = !warnings.length;
  $("refresh").disabled = false;
  refreshing = false;
}
document.querySelectorAll("nav button").forEach(
  (button) =>
    (button.onclick = () => {
      document.querySelectorAll("nav button").forEach((b) => {
        b.removeAttribute("aria-current");
      });
      button.setAttribute("aria-current", "page");
      document
        .querySelectorAll(".view")
        .forEach(
          (section) => (section.hidden = section.id !== button.dataset.view),
        );
    }),
);
$("refresh").onclick = refresh;
$("new-chat").onclick = () => {
  chats.delete(selected);
  renderChat();
  $("prompt").focus();
};
$("chat-form").onsubmit = async (event) => {
  event.preventDefault();
  const message = $("prompt").value.trim();
  if (!selected || busy || !message) return;
  const name = selected,
    chat = conversation(name);
  chat.messages.push({ role: "You", text: message });
  $("prompt").value = "";
  busy = true;
  renderChat();
  try {
    const response = await api("/api/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ agent: name, message, session: chat.session }),
    });
    chat.messages.push({
      role: name,
      text: response.reply || "(No text returned)",
      tools: response.tool_calls,
      run: response.run_id,
    });
  } catch (error) {
    chat.messages.push({
      role: name,
      text:
        error.message +
        "\nThe request was not retried. Check the agent before repeating an action.",
      error: true,
    });
  } finally {
    busy = false;
    renderChat();
    if (selected === name) $("prompt").focus();
  }
};
$("prompt").addEventListener("keydown", (event) => {
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    $("chat-form").requestSubmit();
  }
});
renderChat();
refresh();
setInterval(() => {
  if (!document.hidden) refresh();
}, 30000);
