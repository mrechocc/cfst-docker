const ids = ["intervalHours", "threads", "packets", "downloads", "downloadSeconds", "latencyLimitMs", "lossLimit", "failureThreshold"];
let hasLoadedConfig = false;

const $ = (id) => document.getElementById(id);

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>'"]/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[char]));
}

function formatTime(value) {
  if (!value) return "-";
  const time = new Date(value);
  return Number.isNaN(time.getTime()) ? value : time.toLocaleString("zh-CN", { hour12: false });
}

function fixed(value, digits = 2) {
  return Number(value || 0).toFixed(digits);
}

function showMessage(message, isError = false) {
  const target = $("message");
  target.textContent = message;
  target.classList.toggle("error", isError);
}

async function request(url, options = {}) {
  const response = await fetch(url, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || "请求失败");
  return data;
}

function setConfig(config) {
  if (hasLoadedConfig && document.activeElement?.matches("input")) return;
  ids.forEach((id) => { $(id).value = config[id]; });
  $("autoPrune").checked = config.autoPrune;
  hasLoadedConfig = true;
}

function resultRows(results) {
  if (!results.length) return '<tr><td colspan="5" class="empty">尚无测试结果</td></tr>';
  return results.map((row) => `<tr>
    <td>${escapeHTML(row.ip)}</td><td>${fixed(row.latency)} ms</td><td>${fixed(row.loss * 100)}%</td>
    <td>${fixed(row.speed)} MB/s</td><td>${escapeHTML(row.colo || "N/A")}</td>
  </tr>`).join("");
}

function candidateRows(candidates) {
  if (!candidates.length) return '<tr><td colspan="6" class="empty">候选库将在首次全量测速完成后建立</td></tr>';
  return candidates.map((row) => `<tr>
    <td>${escapeHTML(row.ip)}</td><td>${fixed(row.latency)} ms</td><td>${fixed(row.speed)} MB/s</td>
    <td>${escapeHTML(row.colo || "N/A")}</td><td>${row.consecutiveFailures}</td><td>${formatTime(row.lastSuccessAt)}</td>
  </tr>`).join("");
}

function historyRows(runs) {
  if (!runs.length) return '<tr><td colspan="5" class="empty">暂无记录</td></tr>';
  return runs.map((run) => `<tr>
    <td>${formatTime(run.finishedAt)}</td><td>${escapeHTML(run.source)}</td><td>${run.reachable}/${run.tested}</td>
    <td>${run.speedVerified}</td><td>${run.pruned}${run.error ? ` · ${escapeHTML(run.error)}` : ""}</td>
  </tr>`).join("");
}

function render(state) {
  setConfig(state.config);
  const badge = $("statusBadge");
  badge.textContent = state.running ? "测速运行中" : "等待运行";
  badge.className = `status-badge${state.running ? " running" : state.lastError ? " error" : ""}`;
  $("runButton").disabled = state.running;
  $("runButton").textContent = state.running ? "测速进行中" : "立即测速";
  $("candidateCount").textContent = state.candidateCount;
  $("nextRun").textContent = formatTime(state.nextRunAt);
  $("runSource").textContent = state.running ? state.currentSource : `每 ${state.config.intervalHours} 小时自动运行`;
  const best = state.results.find((result) => result.speed > 0);
  $("bestIP").textContent = best ? best.ip : "暂无可验证速度";
  $("bestDetail").textContent = best ? `${fixed(best.speed)} MB/s · ${fixed(best.latency)} ms · ${best.colo || "N/A"}` : "请先执行一次测速";
  $("resultsBody").innerHTML = resultRows(state.results);
  $("candidatesBody").innerHTML = candidateRows(state.candidates);
  $("historyBody").innerHTML = historyRows(state.runs);
  $("logOutput").textContent = state.lastLog || "暂无日志";
  if (state.lastError) showMessage(`最近任务异常：${state.lastError}`, true);
}

async function refresh() {
  try {
    render(await request("/api/state"));
  } catch (error) {
    showMessage(error.message, true);
  }
}

$("configForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const config = Object.fromEntries(ids.map((id) => [id, Number($(id).value)]));
  config.autoPrune = $("autoPrune").checked;
  try {
    await request("/api/config", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(config) });
    showMessage("设置已保存；下一次自动运行时间已重新计算。");
    await refresh();
  } catch (error) {
    showMessage(error.message, true);
  }
});

$("runButton").addEventListener("click", async () => {
  try {
    await request("/api/run", { method: "POST" });
    showMessage("测速任务已启动，页面会自动刷新状态。");
    await refresh();
  } catch (error) {
    showMessage(error.message, true);
  }
});

$("resetLibrary").addEventListener("click", async () => {
  if (!window.confirm("清空后，下次将重新扫描上游 IP 段。是否继续？")) return;
  try {
    const data = await request("/api/library/reset", { method: "POST" });
    showMessage(data.message);
    await refresh();
  } catch (error) {
    showMessage(error.message, true);
  }
});

refresh();
window.setInterval(refresh, 5000);
