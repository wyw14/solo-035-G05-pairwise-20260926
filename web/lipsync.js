const content = document.getElementById("content");
const stamp = document.getElementById("stamp");

const STATUS_TEXT = {
  aligned: "有效测量",
  pending: "缺测",
  jitter: "抖动超限",
  stale: "非当前代次"
};

function fmt(value, digits = 1) {
  return Number.isFinite(value) ? value.toFixed(digits) : "—";
}

function row(label, value) {
  return `<dt>${label}</dt><dd>${value ?? "—"}</dd>`;
}

function card(profile) {
  const status = profile.status || "pending";
  const badge = `<span class="tag tag-${status}">${STATUS_TEXT[status] || status}</span>`;
  const samples = (profile.samples || [])
    .map(sample => `<li>#${sample.index} · ${fmt(sample.offset_ms)} ms</li>`)
    .join("");
  return `
  <article class="item item-${status}">
    <h2>${profile.event_id} ${badge}</h2>
    <p class="reason">${profile.reason || "—"}</p>
    <dl>
      ${row("信号源", profile.source)}
      ${row("信号源代次", profile.source_generation)}
      ${row("节目版本", profile.revision)}
      ${row("实测偏移", `${fmt(profile.offset_ms)} ms`)}
      ${row("稳定区间", `${fmt(profile.min_offset_ms)} ~ ${fmt(profile.max_offset_ms)} ms`)}
      ${row("抖动 / 容差", `${fmt(profile.jitter_ms)} / ${fmt(profile.tolerance_ms)} ms`)}
      ${row("配对样本", profile.sample_count)}
      ${row("视频标记", profile.marker_count)}
      ${row("音频脉冲", profile.pulse_count)}
      ${row("未配对", profile.missing_pairs)}
    </dl>
    ${samples ? `<ul class="samples">${samples}</ul>` : ""}
  </article>`;
}

async function refresh() {
  try {
    const response = await fetch("/api/lip-sync", { headers: { Accept: "application/json" } });
    const payload = await response.json();
    const profiles = payload.lip_sync || [];
    content.innerHTML = profiles.length
      ? profiles.map(card).join("")
      : `<p class="empty">当前没有任何音画同步实测记录</p>`;
    stamp.textContent = `状态 ${response.status} · ${new Date().toLocaleTimeString()}`;
  } catch (error) {
    content.innerHTML = `<p class="empty">服务连接失败 ${error.message}</p>`;
    stamp.textContent = "连接已中断";
  }
}

refresh();
setInterval(refresh, 5000);
