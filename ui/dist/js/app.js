/* ========================================================
   NEXDNS — CORE APPLICATION CONTROLLER
   ======================================================== */

// Global State
const State = {
  currentPage: 'dashboard',
  theme: 'midnight-purple',
  mode: 'system',
  providers: [],
  runningApps: [],
  appRules: [],
  chartDashboard: null,
  chartTraffic: null,
};

let nativeThemeTimer = null;
let themeSaveTimer = null;
let themeSaveRevision = 0;
let savedTheme = 'midnight-purple';
let modeSwitchBusy = false;
let queuedMode = null;
let modeBeingApplied = null;

const DEFAULT_THEME = 'midnight-purple';
const THEME_IDS = new Set([
  'midnight-purple',
  'cyber-neon',
  'obsidian-black',
  'arctic-blue',
  'emerald-pro',
  'crimson-elite',
  'light-professional',
  'graphite-aurora',
]);

function iconMarkup(name, className = 'icon') {
  return `<svg class="${className}" aria-hidden="true" focusable="false"><use href="#icon-${name}"></use></svg>`;
}

// Safe Go backend caller bridge
async function callGo(fnName, ...args) {
  if (typeof window[fnName] === 'function') {
    try {
      const res = await window[fnName](...args);
      return res;
    } catch (err) {
      console.error(`Error calling Go ${fnName}:`, err);
      throw err;
    }
  } else {
    console.warn(`Go function ${fnName} not available in dev mode`);
    return null;
  }
}

// Toast notification helper
function showToast(message, type = 'info') {
  const container = document.getElementById('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.setAttribute('role', type === 'error' ? 'alert' : 'status');
  toast.setAttribute('aria-live', type === 'error' ? 'assertive' : 'polite');
  const iconName = type === 'success' ? 'check' : type === 'error' ? 'x' : 'info';
  toast.innerHTML = `
    <span class="toast-icon">
      ${iconMarkup(iconName)}
    </span>
    <span class="toast-text">${message}</span>
  `;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateX(100%)';
    toast.style.transition = 'all 0.3s ease';
    setTimeout(() => toast.remove(), 300);
  }, 3500);
}

// Page title map
const PageTitles = {
  dashboard: { title: 'Ana Kontrol Paneli', sub: 'Gerçek zamanlı ağ durumu ve DNS yönetimi' },
  dns: { title: 'DNS Yöneticisi', sub: 'Sistem geneli ve özel DNS sağlayıcıları' },
  apps: { title: 'Uygulama Yöneticisi', sub: 'İşlem bazlı özel DNS yönlendirme kuralları' },
  traffic: { title: 'Trafik İzleyici', sub: 'Canlı indirme, yükleme ve toplam veri kullanımı' },
  discord: { title: 'Discord Bağlantı Testi', sub: 'Discord API, Gateway, CDN ve ses sunucusu tanılaması' },
  network: { title: 'Ağ Bilgileri', sub: 'Aktif adaptör, IP yapılandırması ve donanım detayları' },
  diagnostics: { title: 'Sistem Tanılama', sub: '8 aşamalı derin ağ ve internet sağlık kontrolü' },
  settings: { title: 'Uygulama Ayarları', sub: 'Görünüm temaları, yedekleme ve sistem tercihleri' },
};

// Navigate between pages
function navigateTo(pageId) {
  State.currentPage = pageId;

  // Update navigation items
  document.querySelectorAll('.nav-item').forEach(el => {
    el.classList.toggle('active', el.dataset.page === pageId);
  });

  // Update page visibility
  document.querySelectorAll('.page').forEach(el => {
    el.classList.toggle('active', el.id === `page-${pageId}`);
  });

  // Update topbar title
  const meta = PageTitles[pageId] || { title: 'NEXDNS', sub: 'Advanced Network & DNS Manager' };
  document.getElementById('topbar-title').textContent = meta.title;
  document.getElementById('topbar-sub').textContent = meta.sub;

  // Trigger page-specific data loads
  if (pageId === 'dns') loadDNSPage();
  if (pageId === 'apps') loadAppsPage();
  if (pageId === 'traffic') loadTrafficPage();
  if (pageId === 'discord') loadDiscordPage();
  if (pageId === 'network') loadNetworkPage();
  if (pageId === 'diagnostics') loadDiagnosticsPage();
  if (pageId === 'settings') loadSettingsPage();
}

// ----------------------------------------------------
// INITIALIZATION
// ----------------------------------------------------
window.addEventListener('DOMContentLoaded', async () => {
  // Setup navigation clicks
  document.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', () => {
      const page = item.dataset.page;
      if (page) navigateTo(page);
    });
  });

  document.querySelectorAll('.theme-card').forEach(card => {
    card.addEventListener('keydown', event => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        selectTheme(card.dataset.theme);
      }
    });
  });

  // Setup mode switch buttons
  document.getElementById('mode-system-btn')?.addEventListener('click', () => switchMode('system'));
  document.getElementById('mode-app-btn')?.addEventListener('click', () => switchMode('app'));
  document.getElementById('mode-switch-input')?.addEventListener('change', event => {
    switchMode(event.currentTarget.checked ? 'app' : 'system');
  });

  // Setup charts
  State.chartDashboard = new TrafficChart('chart-dashboard');
  State.chartTraffic = new TrafficChart('chart-traffic-full');

  // Load Settings & Theme
  try {
    const cfg = await callGo('GetSettings');
    savedTheme = cfg && THEME_IDS.has(cfg.theme) ? cfg.theme : DEFAULT_THEME;
    if (cfg && (cfg.mode === 'system' || cfg.mode === 'app')) {
      State.mode = cfg.mode;
      updateModeSwitch(State.mode);
    }
    applyTheme(savedTheme);
  } catch (e) {}

  // Initial load
  await updateDashboard();

  // Start periodic dashboard polling loop (every 1s)
  setInterval(updateDashboard, 1000);
});

// ----------------------------------------------------
// DASHBOARD UPDATER
// ----------------------------------------------------
async function updateDashboard() {
  try {
    const s = await callGo('GetDashboardState');
    if (!s) return;

    const reportedMode = s.activeMode || 'system';
    if (!modeSwitchBusy) State.mode = reportedMode;
    updateModeSwitch(modeSwitchBusy ? (queuedMode || modeBeingApplied || State.mode) : reportedMode);

    // Topbar connection status
    const statusDot = document.getElementById('global-status-dot');
    const statusText = document.getElementById('global-status-text');
    if (statusDot && statusText) {
      if (s.dnsStatus === 'online') {
        statusDot.className = 'status-dot';
        statusText.textContent = 'DNS Yanıt Veriyor';
      } else if (s.dnsStatus === 'checking') {
        statusDot.className = 'status-dot pending';
        statusText.textContent = 'DNS Kontrol Ediliyor';
      } else {
        statusDot.className = 'status-dot error';
        statusText.textContent = 'DNS Yanıt Vermiyor';
      }
    }

    // Dashboard metric cards
    setText('dash-provider', s.activeProvider || 'Bilinmiyor');
    setText('dash-adapter', s.activeAdapterName || '-');
    setText('dash-ipv4', s.activeIPv4 || '-');

    // Latency
    const latElem = document.getElementById('dash-latency');
    if (latElem) {
      if (s.dnsLatencyMs >= 0) {
        latElem.textContent = `${s.dnsLatencyMs} ms`;
        latElem.style.color = s.dnsLatencyMs < 40 ? 'var(--success)' : s.dnsLatencyMs < 100 ? 'var(--warning)' : 'var(--error)';
      } else {
        latElem.textContent = 'Ölçülemedi';
        latElem.style.color = 'var(--text-muted)';
      }
    }

    // Real speeds
    if (s.traffic) {
      setText('dash-speed-down', s.traffic.downloadFormatted || '0 KB/s');
      setText('dash-speed-up', s.traffic.uploadFormatted || '0 KB/s');
      setText('dash-total-down', s.traffic.totalDownFormatted || '0 B');
      setText('dash-total-up', s.traffic.totalUpFormatted || '0 B');

      // Update traffic page metrics if active
      setText('traffic-speed-down', s.traffic.downloadFormatted || '0 KB/s');
      setText('traffic-speed-up', s.traffic.uploadFormatted || '0 KB/s');
      setText('traffic-total-down', s.traffic.totalDownFormatted || '0 B');
      setText('traffic-total-up', s.traffic.totalUpFormatted || '0 B');

      // Feed charts
      if (State.chartDashboard) State.chartDashboard.addData(s.traffic.downloadBps, s.traffic.uploadBps);
      if (State.chartTraffic) State.chartTraffic.addData(s.traffic.downloadBps, s.traffic.uploadBps);
    }

    // Backup restore button status
    const restoreBtn = document.getElementById('btn-restore-backup');
    if (restoreBtn) {
      restoreBtn.disabled = !s.hasBackup;
    }
  } catch (err) {
    console.error('updateDashboard error:', err);
  }
}

function setText(id, text) {
  const el = document.getElementById(id);
  if (el) el.textContent = text;
}

// ----------------------------------------------------
// MODE SWITCHING
// ----------------------------------------------------
function updateModeSwitch(mode) {
  const isAppMode = mode === 'app';
  const toggle = document.getElementById('mode-switch-input');
  if (toggle) toggle.checked = isAppMode;
  document.getElementById('mode-system-btn')?.classList.toggle('active', !isAppMode);
  document.getElementById('mode-app-btn')?.classList.toggle('active', isAppMode);
}

async function switchMode(mode) {
  if (mode !== 'system' && mode !== 'app') return;
  if (!modeSwitchBusy && mode === State.mode) {
    updateModeSwitch(mode);
    return;
  }
  queuedMode = mode;
  updateModeSwitch(mode);
  if (modeSwitchBusy) return;

  modeSwitchBusy = true;
  let succeeded = false;
  let failed = false;
  try {
    while (queuedMode !== null) {
      const nextMode = queuedMode;
      queuedMode = null;
      modeBeingApplied = nextMode;
      try {
        await callGo('SwitchMode', nextMode);
        State.mode = nextMode;
        succeeded = true;
      } catch (err) {
        failed = true;
        queuedMode = null;
        updateModeSwitch(State.mode);
        showToast('Mod değiştirilemedi: ' + err, 'error');
        break;
      }
    }
    if (succeeded && !failed) {
      showToast(`Çalışma Modu Değiştirildi: ${State.mode === 'system' ? 'Tüm Sistem DNS' : 'Seçili Uygulamalar DNS'}`, 'info');
      await updateDashboard();
      if (State.currentPage === 'dns') loadDNSPage();
      if (State.currentPage === 'apps') loadAppsPage();
    }
  } catch (err) {
    showToast('Mod değiştirilemedi: ' + err, 'error');
  } finally {
    modeBeingApplied = null;
    modeSwitchBusy = false;
    if (queuedMode !== null && queuedMode !== State.mode) {
      const nextMode = queuedMode;
      queuedMode = null;
      switchMode(nextMode);
    } else {
      queuedMode = null;
      updateModeSwitch(State.mode);
    }
  }
}

// ----------------------------------------------------
// DNS MANAGER
// ----------------------------------------------------
async function loadDNSPage() {
  const container = document.getElementById('providers-grid');
  if (!container) return;

  try {
    const providers = await callGo('GetProviders');
    if (!providers) return;
    State.providers = providers;
    renderProviderCards(providers);
  } catch (err) {
    console.error('loadDNSPage error:', err);
  }
}

function renderProviderCards(providers) {
  const container = document.getElementById('providers-grid');
  if (!container) return;

  const currentProviderId = document.getElementById('dash-provider')?.dataset?.id || '';

  container.innerHTML = providers.map(p => {
    let latencyBadge = '';
    if (p.latencyMs >= 0) {
      const cls = p.latencyMs < 40 ? 'good' : p.latencyMs < 100 ? 'slow' : 'offline';
      latencyBadge = `<span class="latency-pill ${cls}">${p.latencyMs} ms</span>`;
    } else {
      latencyBadge = `<span class="latency-pill offline">Test Edilmedi</span>`;
    }

    return `
      <div class="card provider-card" id="card-prov-${p.id}">
        <div class="provider-header">
          <div>
            <div class="provider-name">${p.name}</div>
            <div class="provider-tagline">${p.tagline}</div>
          </div>
          ${latencyBadge}
        </div>
        <div class="provider-ips">
          <div><span>IPv4 Birincil:</span> <strong>${p.ipv4Primary}</strong></div>
          <div><span>IPv4 İkincil:</span> <strong>${p.ipv4Secondary || '-'}</strong></div>
          ${p.ipv6Primary ? `<div><span>IPv6 Desteği:</span> <strong style="font-size: 11px;">${p.ipv6Primary}</strong></div>` : ''}
        </div>
        <div style="display: flex; gap: 8px; margin-top: 10px;">
          <button class="btn btn-primary btn-sm" style="flex: 1;" onclick="applyProvider('${p.id}')">
            ${iconMarkup('check', 'icon icon-sm')} Uygula
          </button>
          <button class="btn btn-secondary btn-sm" title="Ping Testi Yap" onclick="testSingleProviderLatency('${p.id}')">
            ${iconMarkup('zap', 'icon icon-sm')} Test
          </button>
        </div>
      </div>
    `;
  }).join('');
}

async function testSingleProviderLatency(id) {
  const card = document.getElementById('card-prov-' + id);
  const btn = card ? card.querySelector('button[title="Ping Testi Yap"]') : null;
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = iconMarkup('loader', 'icon icon-sm icon-spin');
  }

  try {
    const p = await callGo('MeasureProvider', id);
    if (!p) return;
    const idx = State.providers.findIndex(x => x.id === id);
    if (idx >= 0) State.providers[idx] = p;
    renderProviderCards(State.providers);
    if (p.latencyMs >= 0) {
      showToast(`${p.name}: ${p.latencyMs} ms`, 'success');
    } else {
      showToast(`${p.name}: ulaşılamadı`, 'error');
    }
  } catch (err) {
    showToast('Ping testi yapılamadı: ' + err, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `${iconMarkup('zap', 'icon icon-sm')} Test`;
    }
  }
}

async function testAllLatencies() {
  const btn = document.getElementById('btn-test-all-dns');
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `${iconMarkup('loader', 'icon icon-sm icon-spin')} Ölçülüyor...`;
  }

  try {
    const updated = await callGo('MeasureProviders');
    if (updated) {
      State.providers = updated;
      renderProviderCards(updated);
      showToast('DNS ping süreleri başarıyla ölçüldü.', 'success');
    }
  } catch (err) {
    showToast('Ölçüm sırasında hata: ' + err, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `${iconMarkup('zap', 'icon icon-sm')} Tüm Pingleri Test Et`;
    }
  }
}

async function applyProvider(id) {
  try {
    await callGo('ApplyProvider', id);
    showToast('DNS sağlayıcısı başarıyla uygulandı ve önbellek temizlendi.', 'success');
    await updateDashboard();
    loadDNSPage();
  } catch (err) {
    showToast('DNS uygulanamadı (Yönetici izni gerekebilir): ' + err, 'error');
  }
}

async function resetToDHCP() {
  if (!confirm('DNS ayarlarınızı otomatik DHCP moduna sıfırlamak istiyor musunuz?')) return;
  try {
    await callGo('ResetToDHCP');
    showToast('Ağ adaptörü otomatik DHCP DNS moduna alındı.', 'success');
    await updateDashboard();
    loadDNSPage();
  } catch (err) {
    showToast('DHCP sıfırlanamadı: ' + err, 'error');
  }
}

async function restoreLatestBackup() {
  try {
    await callGo('RestoreBackup');
    showToast('Önceki DNS ayarlarınız başarıyla geri yüklendi.', 'success');
    await updateDashboard();
    loadDNSPage();
  } catch (err) {
    showToast('Yedek geri yüklenemedi: ' + err, 'error');
  }
}

async function flushDNS() {
  try {
    await callGo('FlushDNS');
    showToast('Windows DNS önbelleği başarıyla temizlendi (Flush DNS).', 'success');
  } catch (err) {
    showToast('DNS önbelleği temizlenemedi: ' + err, 'error');
  }
}

// ----------------------------------------------------
// APPLICATION MANAGER
// ----------------------------------------------------
async function loadAppsPage() {
  await Promise.all([loadAppRules(), loadRunningApps()]);
}

async function loadAppRules() {
  const container = document.getElementById('app-rules-tbody');
  if (!container) return;

  try {
    const rules = await callGo('GetAppRules');
    State.appRules = rules || [];

    if (!rules || rules.length === 0) {
      container.innerHTML = `<tr><td colspan="5" style="text-align: center; color: var(--text-muted); padding: 24px;">Tanımlı uygulama kuralı bulunmuyor. Aşağıdan çalışan uygulama ekleyebilirsiniz.</td></tr>`;
      return;
    }

    container.innerHTML = rules.map(r => `
      <tr>
        <td><strong>${r.name}</strong></td>
        <td><code>${r.processName}</code></td>
        <td><span class="latency-pill good">${r.providerName || r.dnsPrimary}</span></td>
        <td>
          <label class="switch">
            <input type="checkbox" ${r.enabled ? 'checked' : ''} onchange="toggleRule('${r.id}', this.checked)">
            <span class="slider"></span>
          </label>
        </td>
        <td>
          <button class="btn btn-outline-danger btn-sm" onclick="deleteRule('${r.id}')">Sil</button>
        </td>
      </tr>
    `).join('');
  } catch (err) {
    console.error('loadAppRules error:', err);
  }
}

async function loadRunningApps() {
  const container = document.getElementById('running-apps-tbody');
  if (!container) return;

  try {
    const apps = await callGo('GetRunningApps');
    State.runningApps = apps || [];
    renderRunningAppsTable(State.runningApps);
  } catch (err) {
    console.error('loadRunningApps error:', err);
  }
}

function filterRunningApps(keyword) {
  const kw = keyword.toLowerCase().trim();
  const filtered = State.runningApps.filter(a =>
    a.name.toLowerCase().includes(kw) || a.displayName.toLowerCase().includes(kw)
  );
  renderRunningAppsTable(filtered);
}

function renderRunningAppsTable(apps) {
  const container = document.getElementById('running-apps-tbody');
  if (!container) return;

  if (apps.length === 0) {
    container.innerHTML = `<tr><td colspan="4" style="text-align: center; color: var(--text-muted); padding: 20px;">Uygulama bulunamadı.</td></tr>`;
    return;
  }

  container.innerHTML = apps.slice(0, 50).map(a => `
    <tr>
      <td><strong>${a.displayName}</strong></td>
      <td><code>${a.name}</code></td>
      <td><span style="font-size: 11.5px; color: var(--text-muted);">${a.path}</span></td>
      <td>
        <button class="btn btn-primary btn-sm" onclick="quickAddAppRule('${a.name}', '${a.displayName.replace(/'/g, "\\'")}', '${a.path.replace(/\\/g, "\\\\")}')">
          + Kural Ekle
        </button>
      </td>
    </tr>
  `).join('');
}

async function pickManualExe() {
  try {
    const path = await callGo('OpenFilePicker');
    if (!path) return;
    const parts = path.split('\\');
    const exeName = parts[parts.length - 1];
    quickAddAppRule(exeName, exeName.replace('.exe', ''), path);
  } catch (err) {
    showToast('Dosya seçilemedi: ' + err, 'error');
  }
}

function quickAddAppRule(processName, displayName, fullPath) {
  document.getElementById('modal-app-name').value = displayName;
  document.getElementById('modal-app-proc').value = processName;
  document.getElementById('modal-app-path').value = fullPath;
  document.getElementById('modal-add-app').classList.add('active');
}

async function submitAppRuleModal() {
  const name = document.getElementById('modal-app-name').value.trim();
  const processName = document.getElementById('modal-app-proc').value.trim();
  const path = document.getElementById('modal-app-path').value.trim();
  const provSelect = document.getElementById('modal-app-dns');
  const provId = provSelect.value;
  const provName = provSelect.options[provSelect.selectedIndex].text;

  let dnsIP = '1.1.1.1';
  if (provId === 'google') dnsIP = '8.8.8.8';
  if (provId === 'quad9') dnsIP = '9.9.9.9';
  if (provId === 'adguard') dnsIP = '94.140.14.14';
  if (provId === 'opendns') dnsIP = '208.67.222.222';

  const rule = {
    name: name || processName,
    processName: processName,
    exePath: path,
    providerId: provId,
    providerName: provName,
    dnsPrimary: dnsIP,
    enabled: true
  };

  try {
    await callGo('AddAppRule', JSON.stringify(rule));
    closeModal('modal-add-app');
    showToast(`'${rule.name}' için DNS kuralı eklendi.`, 'success');
    loadAppRules();
  } catch (err) {
    showToast('Kural eklenemedi: ' + err, 'error');
  }
}

async function toggleRule(id, enabled) {
  try {
    await callGo('ToggleAppRule', id, enabled);
    showToast(`Kural ${enabled ? 'aktif edildi' : 'devre dışı bırakıldı'}.`, 'info');
  } catch (err) {
    showToast('Kural durumu değiştirilemedi: ' + err, 'error');
  }
}

async function deleteRule(id) {
  try {
    await callGo('DeleteAppRule', id);
    showToast('Kural silindi.', 'info');
    loadAppRules();
  } catch (err) {
    showToast('Kural silinemedi: ' + err, 'error');
  }
}

// ----------------------------------------------------
// TRAFFIC MONITOR
// ----------------------------------------------------
async function loadTrafficPage() {
  // Live traffic values are already fed by the 1s updateDashboard() loop, so
  // this page transition only needs to refresh the chart canvas dimensions.
  if (State.chartTraffic) State.chartTraffic.resize();
}

// ----------------------------------------------------
// DISCORD CHECK
// ----------------------------------------------------
async function loadDiscordPage() {
  // Can wait for user click or run initial check
}

async function runDiscordDiagnostics() {
  const btn = document.getElementById('btn-run-discord');
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `${iconMarkup('loader', 'icon icon-sm icon-spin')} Discord Servisleri Test Ediliyor...`;
  }

  const container = document.getElementById('discord-results-container');
  if (container) {
    container.innerHTML = `<div style="text-align: center; padding: 40px; color: var(--text-muted);">
      <div style="margin-bottom: 8px;">${iconMarkup('loader', 'icon icon-lg icon-spin')}</div>
      Discord Web, API, CDN, Soket ve DNS testleri yürütülüyor...
    </div>`;
  }

  try {
    const res = await callGo('RunDiscordDiagnostics');
    if (!res) return;

    // Overall banner
    const banner = document.getElementById('discord-overall-banner');
    if (banner) {
      if (res.overallStatus === 'success') {
        banner.className = 'alert-box info';
        banner.innerHTML = `<span class="alert-icon">${iconMarkup('check')}</span><strong>Discord Servisleri Tamamen Erişilebilir:</strong> Tüm kritik Discord bağlantı noktaları ve ses soketleri yanıt veriyor.`;
      } else if (res.overallStatus === 'partial') {
        banner.className = 'alert-box warning';
        banner.innerHTML = `<span class="alert-icon">${iconMarkup('alert')}</span><strong>Kısmi Erişim Kısıtlaması:</strong> Bazı Discord servisleri yanıt verirken diğerlerinde gecikme veya engelleme tespit edildi.`;
      } else {
        banner.className = 'alert-box error';
        banner.innerHTML = `<span class="alert-icon">${iconMarkup('x')}</span><strong>Discord Bağlantısı Başarısız:</strong> Discord sunucularına ulaşılamıyor. DNS veya ISP filtrelemesi mevcut olabilir.`;
      }
    }

    if (container && res.items) {
      container.innerHTML = res.items.map(it => {
        const cls = it.status === 'success' ? 'good' : it.status === 'warning' ? 'slow' : 'offline';
        const latText = it.latencyMs >= 0 ? `${it.latencyMs} ms` : '-';
        return `
          <div class="card" style="margin-bottom: 12px; display: flex; align-items: center; justify-content: space-between; gap: 16px;">
            <div style="flex: 1;">
              <div style="display: flex; align-items: center; gap: 10px;">
                <strong style="font-size: 14.5px;">${it.name}</strong>
                <span class="latency-pill ${cls}">${it.statusText}</span>
              </div>
              <div style="font-size: 12.5px; color: var(--text-muted); margin-top: 4px;">${it.description}</div>
              <div style="font-size: 11px; color: var(--text-dim); margin-top: 2px;">Hedef: <code>${it.target}</code> ${it.details ? `• ${it.details}` : ''}</div>
            </div>
            <div style="text-align: right; min-width: 80px;">
              <div style="font-size: 16px; font-weight: 700; color: var(--text-main);">${latText}</div>
              <div style="font-size: 11px; color: var(--text-dim);">Gecikme</div>
            </div>
          </div>
        `;
      }).join('');
    }
  } catch (err) {
    showToast('Discord testi tamamlanamadı: ' + err, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `${iconMarkup('zap', 'icon icon-sm')} Testleri Yeniden Başlat`;
    }
  }
}

// ----------------------------------------------------
// NETWORK INFORMATION
// ----------------------------------------------------
async function loadNetworkPage() {
  const container = document.getElementById('adapters-list-container');
  if (!container) return;

  try {
    const adapters = await callGo('GetAdapters');
    if (!adapters || adapters.length === 0) {
      container.innerHTML = `<div style="padding: 20px; color: var(--text-muted);">Aktif adaptör bilgisi bulunamadı.</div>`;
      return;
    }

    container.innerHTML = adapters.map(a => `
      <div class="card" style="margin-bottom: 16px;">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; border-bottom: 1px solid var(--border); padding-bottom: 8px;">
          <div>
            <strong style="font-size: 15px;">${a.name}</strong>
            <span style="font-size: 12px; color: var(--text-muted); margin-left: 8px;">(${a.description || a.type})</span>
          </div>
          <span class="latency-pill ${a.status === 'Up' ? 'good' : 'offline'}">${a.status === 'Up' ? 'Aktif (Up)' : 'Kapalı (Down)'}</span>
        </div>
        <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; font-size: 13px;">
          <div><span style="color: var(--text-dim);">IPv4:</span> <strong>${a.ipv4 || 'Atanmamış'}</strong></div>
          <div><span style="color: var(--text-dim);">Ağ Geçidi:</span> <strong>${a.gateway || 'Yok'}</strong></div>
          <div><span style="color: var(--text-dim);">MAC Adresi:</span> <code>${a.macAddress || '-'}</code></div>
          <div><span style="color: var(--text-dim);">Bağlantı Türü:</span> <strong>${a.type}</strong></div>
          <div><span style="color: var(--text-dim);">Bağlantı Hızı:</span> <strong>${a.linkSpeed || 'Bilinmiyor'}</strong></div>
          <div><span style="color: var(--text-dim);">Kayıtlı DNS:</span> <strong>${(a.dnsServers && a.dnsServers.length > 0) ? a.dnsServers.join(', ') : 'Otomatik (DHCP)'}</strong></div>
        </div>
      </div>
    `).join('');
  } catch (err) {
    console.error('loadNetworkPage error:', err);
  }
}

// ----------------------------------------------------
// DIAGNOSTICS
// ----------------------------------------------------
async function loadDiagnosticsPage() {
  // ready for test
}

async function runAllDiagnostics() {
  const btn = document.getElementById('btn-run-diag');
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `${iconMarkup('loader', 'icon icon-sm icon-spin')} Tanılama Yürütülüyor...`;
  }

  const container = document.getElementById('diagnostics-list-container');
  if (container) {
    container.innerHTML = `<div style="text-align: center; padding: 40px; color: var(--text-muted);">
      <div style="margin-bottom: 8px;">${iconMarkup('search', 'icon icon-lg')}</div>
      8 Aşamalı Ağ ve İnternet Tanılaması Çalışıyor...
    </div>`;
  }

  try {
    const res = await callGo('RunNetworkDiagnostics');
    if (!res) return;

    // Score meter
    const scoreElem = document.getElementById('diag-score-value');
    if (scoreElem) {
      scoreElem.textContent = `%${res.score}`;
      scoreElem.style.color = res.score >= 80 ? 'var(--success)' : res.score >= 50 ? 'var(--warning)' : 'var(--error)';
    }

    if (container && res.items) {
      container.innerHTML = res.items.map(it => {
        const cls = it.status === 'success' ? 'good' : it.status === 'warning' ? 'slow' : 'offline';
        return `
          <div class="card" style="margin-bottom: 12px;">
            <div style="display: flex; align-items: center; justify-content: space-between;">
              <div style="display: flex; align-items: center; gap: 10px;">
                <strong style="font-size: 14px;">${it.title}</strong>
                <span class="latency-pill ${cls}">${it.statusText}</span>
              </div>
              ${it.latencyMs > 0 ? `<span style="font-size: 12px; color: var(--text-dim);">${it.latencyMs} ms</span>` : ''}
            </div>
            <div style="font-size: 12.5px; color: var(--text-muted); margin-top: 6px;">${it.description}</div>
            ${it.solution ? `<div style="margin-top: 6px; font-size: 12px; color: var(--warning); background-color: var(--bg-surface); padding: 6px 10px; border-radius: 4px;"><span class="alert-icon">${iconMarkup('lightbulb')}</span>Çözüm: ${it.solution}</div>` : ''}
          </div>
        `;
      }).join('');
    }
  } catch (err) {
    showToast('Tanılama yürütülemedi: ' + err, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `${iconMarkup('zap', 'icon icon-sm')} Tanılamayı Tekrar Çalıştır`;
    }
  }
}

// ----------------------------------------------------
// SETTINGS & THEMES
// ----------------------------------------------------
async function loadSettingsPage() {
  document.querySelectorAll('.theme-card').forEach(card => {
    card.classList.toggle('active', card.dataset.theme === State.theme);
    card.setAttribute('aria-pressed', String(card.dataset.theme === State.theme));
  });
  const autoStartToggle = document.getElementById('auto-start-toggle');
  if (autoStartToggle) {
    try {
      const cfg = await callGo('GetSettings');
      autoStartToggle.checked = Boolean(cfg && cfg.autoStart);
    } catch (err) {
      showToast(`Başlangıç ayarı okunamadı: ${err.message || err}`, 'error');
    }
  }
}

async function toggleAutoStart(enabled) {
  const toggle = document.getElementById('auto-start-toggle');
  try {
    const cfg = await callGo('GetSettings');
    if (!cfg || typeof cfg !== 'object') throw new Error('Mevcut ayarlar okunamadı.');
    cfg.autoStart = enabled;
    await callGo('SaveSettings', JSON.stringify(cfg));
    showToast(enabled ? 'Windows başlangıcında otomatik çalıştırma açıldı.' : 'Windows başlangıcında otomatik çalıştırma kapatıldı.', 'success');
  } catch (err) {
    if (toggle) toggle.checked = !enabled;
    showToast(`Başlangıç ayarı kaydedilemedi: ${err.message || err}`, 'error');
  }
}

function selectTheme(themeId) {
  if (!THEME_IDS.has(themeId)) return;
  applyTheme(themeId);
  const revision = ++themeSaveRevision;
  clearTimeout(themeSaveTimer);
  themeSaveTimer = setTimeout(() => persistSelectedTheme(themeId, revision), 160);
}

async function persistSelectedTheme(themeId, revision) {
  try {
    if (typeof window.GetSettings !== 'function' || typeof window.SaveSettings !== 'function') {
      throw new Error('Tema ayarlarını kaydetme servisi kullanılamıyor.');
    }
    const cfg = await callGo('GetSettings');
    if (!cfg || typeof cfg !== 'object') throw new Error('Mevcut ayarlar okunamadı.');
    cfg.theme = themeId;
    await callGo('SaveSettings', JSON.stringify(cfg));
    savedTheme = themeId;
    if (revision === themeSaveRevision) showToast(`Tema seçildi ve kaydedildi: ${themeId}`, 'success');
  } catch (err) {
    if (revision === themeSaveRevision) {
      applyTheme(savedTheme);
      showToast(`Tema kaydedilemedi: ${err.message || err}`, 'error');
    }
  }
}

function applyTheme(themeId) {
  if (!THEME_IDS.has(themeId)) themeId = DEFAULT_THEME;
  State.theme = themeId;
  document.body.setAttribute('data-theme', themeId);
  scheduleNativeThemeUpdate();
  document.querySelectorAll('.theme-card').forEach(card => {
    card.classList.toggle('active', card.dataset.theme === themeId);
    card.setAttribute('aria-pressed', String(card.dataset.theme === themeId));
  });
}

function scheduleNativeThemeUpdate() {
  clearTimeout(nativeThemeTimer);
  nativeThemeTimer = setTimeout(() => {
    if (typeof window.SetWindowTheme === 'function') {
      const style = getComputedStyle(document.body);
      const background = style.getPropertyValue('--bg-surface').trim();
      const foreground = style.getPropertyValue('--text-main').trim();
      const border = style.getPropertyValue('--border').trim();
      const primary = style.getPropertyValue('--primary').trim();
      const accent = style.getPropertyValue('--accent').trim();
      callGo('SetWindowTheme', background, foreground, border, primary, accent)
        .catch(err => console.warn('Window theme update failed:', err));
    }
    if (State.chartDashboard) State.chartDashboard.draw();
    if (State.chartTraffic) State.chartTraffic.draw();
  }, 80);
}

// Modal helper
function openModal(id) {
  document.getElementById(id)?.classList.add('active');
}
function closeModal(id) {
  document.getElementById(id)?.classList.remove('active');
}
