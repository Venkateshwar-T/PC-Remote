// PC Remote - Production Nostr E2EE Client Engine
(() => {
  'use strict';

  // State
  let config = {
    laptopPubkey: '',
    pairingToken: '',
    lanHost: '',
    deviceName: '',
    phonePubkey: '',
    phonePrivkey: '', // Kept in volatile memory only while unlocked
    isPaired: false
  };

  let isLocked = true;
  let enteredPin = '';
  let isConnected = false;
  let isPcOnline = false;
  let consecutiveFailures = 0;
  let lastSeenTime = null;
  let lastTelemetryData = null;
  let pendingAction = null;
  let pollInterval = null;
  let activeTransport = 'CONNECTED';

  // DOM Elements - Views
  const landingStateView = document.getElementById('landingStateView');
  const landingIconWrap = document.getElementById('landingIconWrap');
  const landingTitle = document.getElementById('landingTitle');
  const landingDesc = document.getElementById('landingDesc');
  const landingBadge = document.getElementById('landingBadge');
  const landingBadgeText = document.getElementById('landingBadgeText');
  const btnLandingAction = document.getElementById('btnLandingAction');

  const pinView = document.getElementById('pinView');
  const pinTitle = document.getElementById('pinTitle');
  const pinSubtitle = document.getElementById('pinSubtitle');
  const pinError = document.getElementById('pinError');

  const mainDashboard = document.getElementById('mainDashboard');
  const deviceName = document.getElementById('deviceName');
  const statusIndicator = document.getElementById('statusIndicator');
  const statusLabel = document.getElementById('statusLabel');
  const statusSub = document.getElementById('statusSub');
  const btnRefresh = document.getElementById('btnRefresh');

  // Network & Battery UI
  const networkName = document.getElementById('networkName');
  const networkType = document.getElementById('networkType');
  const batteryFill = document.getElementById('batteryFill');
  const batteryPercentText = document.getElementById('batteryPercentText');
  const batteryLightning = document.getElementById('batteryLightning');

  // Metrics
  const cpuValue = document.getElementById('cpuValue');
  const cpuMeter = document.getElementById('cpuMeter');
  const ramValue = document.getElementById('ramValue');
  const ramMeter = document.getElementById('ramMeter');
  const ramGb = document.getElementById('ramGb');
  const uptimeValue = document.getElementById('uptimeValue');

  // Controls
  const btnLock = document.getElementById('btnLock');
  const lockName = document.getElementById('lockName');
  const lockDesc = document.getElementById('lockDesc');
  const lockRight = document.getElementById('lockRight');
  const btnSleep = document.getElementById('btnSleep');
  const btnRestart = document.getElementById('btnRestart');
  const btnShutdown = document.getElementById('btnShutdown');

  // Modal & Toast
  const confirmModal = document.getElementById('confirmModal');
  const modalTitle = document.getElementById('modalTitle');
  const modalDesc = document.getElementById('modalDesc');
  const btnModalCancel = document.getElementById('btnModalCancel');
  const btnModalConfirm = document.getElementById('btnModalConfirm');
  const toast = document.getElementById('toast');
  const toastText = document.getElementById('toastText');

  // Action State Screen (Sleep / Restart / Shutdown)
  const actionStateView = document.getElementById('actionStateView');
  const stateIconWrap = document.getElementById('stateIconWrap');
  const stateTitle = document.getElementById('stateTitle');
  const stateDesc = document.getElementById('stateDesc');
  const stateBadgeDot = document.getElementById('stateBadgeDot');
  const stateBadgeText = document.getElementById('stateBadgeText');
  const btnStateAction = document.getElementById('btnStateAction');
  const btnStateDismiss = document.getElementById('btnStateDismiss');
  let reconnectTimer = null;

  // Remote Desktop View & Controls
  const btnOpenRemoteDesktop = document.getElementById('btnOpenRemoteDesktop');
  const remoteConsentModal = document.getElementById('remoteConsentModal');
  const btnCancelRemoteConsent = document.getElementById('btnCancelRemoteConsent');
  const btnConfirmRemoteConsent = document.getElementById('btnConfirmRemoteConsent');

  const remoteDesktopView = document.getElementById('remoteDesktopView');
  const remoteHeader = document.getElementById('remoteHeader');
  const btnRemoteBack = document.getElementById('btnRemoteBack');
  const btnToggleOrientation = document.getElementById('btnToggleOrientation');
  const remoteTransportBadge = document.getElementById('remoteTransportBadge');
  const remoteTransportLabel = document.getElementById('remoteTransportLabel');
  const remoteBatteryText = document.getElementById('remoteBatteryText');

  const remoteScreenContainer = document.getElementById('remoteScreenContainer');
  const remoteVideo = document.getElementById('remoteVideo');
  const remoteConnectingOverlay = document.getElementById('remoteConnectingOverlay');
  const remoteConnectingText = document.getElementById('remoteConnectingText');
  const remoteTouchpadArea = document.getElementById('remoteTouchpadArea');

  const btnFloatingQuick = document.getElementById('btnFloatingQuick');
  const quickActionsSheet = document.getElementById('quickActionsSheet');
  const btnQuickLock = document.getElementById('btnQuickLock');
  const btnQuickSleep = document.getElementById('btnQuickSleep');
  const btnQuickRestart = document.getElementById('btnQuickRestart');
  const btnQuickShutdown = document.getElementById('btnQuickShutdown');

  let remoteSessionId = null;
  let remoteAuthChallenge = null;
  let remoteRTC = null;
  let remoteControlDC = null;
  let remoteInputDC = null;
  let remotePingTimer = null;
  let remoteConnectTimer = null;
  let isRemoteDesktopActive = false;
  const REMOTE_CONNECT_TIMEOUT_MS = 20000;

  // Multi-Relay Pool (Public Nostr Relays)
  const RELAYS = [
    'wss://relay.damus.io',
    'wss://nos.lol',
    'wss://relay.primal.net'
  ];

  const relaySockets = new Map(); // url -> WebSocket
  const pendingRequests = new Map(); // reqId -> { resolve, reject, timer }

  // Register Service Worker for offline PWA capability
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('sw.js').catch(() => {});
    });
  }

  // --- Cryptographic & Format Validators ---
  function isValidPubkey(key) {
    return typeof key === 'string' && /^[0-9a-fA-F]{64}$/.test(key);
  }

  function isValidPairingToken(tok) {
    return typeof tok === 'string' && /^[0-9a-fA-F]{32}$/.test(tok);
  }

  function isValidLanHost(host) {
    return typeof host === 'string' && /^([0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]{1,5}$/.test(host);
  }

  function getClientDeviceName() {
    const ua = navigator.userAgent || '';
    if (/iPhone/i.test(ua)) return 'iPhone Remote';
    if (/iPad/i.test(ua)) return 'iPad Remote';
    if (/Android/i.test(ua)) return 'Android Remote';
    if (/Macintosh/i.test(ua)) return 'Mac Remote';
    if (/Windows/i.test(ua)) return 'Windows Remote';
    return 'Mobile Remote';
  }

  function hexToBytes(hex) {
    const bytes = new Uint8Array(hex.length / 2);
    for (let i = 0; i < bytes.length; i++) {
      bytes[i] = parseInt(hex.substr(i * 2, 2), 16);
    }
    return bytes;
  }

  function bytesToHex(bytes) {
    return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
  }

  // --- IndexedDB & Web Crypto Vault ---
  const VAULT_DB_NAME = 'PCRemoteVaultDB';
  const VAULT_STORE = 'vault';

  function openVaultDB() {
    return new Promise((resolve, reject) => {
      if (!window.indexedDB) {
        return reject(new Error('IndexedDB not supported'));
      }
      const req = indexedDB.open(VAULT_DB_NAME, 1);
      req.onupgradeneeded = (e) => {
        const db = e.target.result;
        if (!db.objectStoreNames.contains(VAULT_STORE)) {
          db.createObjectStore(VAULT_STORE);
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error);
    });
  }

  async function saveVaultAuth(vaultData) {
    try {
      const db = await openVaultDB();
      await new Promise((resolve, reject) => {
        const tx = db.transaction(VAULT_STORE, 'readwrite');
        tx.objectStore(VAULT_STORE).put(vaultData, 'auth');
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
      });
    } catch (e) {
      // Transparent fallback to localStorage if IndexedDB is unavailable
      localStorage.setItem('pcremote_vault_auth', JSON.stringify(vaultData));
    }
  }

  async function loadVaultAuth() {
    try {
      const db = await openVaultDB();
      const result = await new Promise((resolve, reject) => {
        const tx = db.transaction(VAULT_STORE, 'readonly');
        const req = tx.objectStore(VAULT_STORE).get('auth');
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
      if (result) return result;
    } catch (e) {}

    // Check localStorage fallback
    const raw = localStorage.getItem('pcremote_vault_auth');
    if (raw) {
      try {
        return JSON.parse(raw);
      } catch (e) {}
    }

    // Check legacy unencrypted config for seamless migration
    const legacyRaw = localStorage.getItem('pcremote_cfg') || localStorage.getItem('laptopcontrol_cfg');
    if (legacyRaw) {
      try {
        const legacy = JSON.parse(legacyRaw);
        if (legacy && legacy.isPaired && legacy.phonePrivkey) {
          return {
            legacy: true,
            phonePrivkey: legacy.phonePrivkey,
            phonePubkey: legacy.phonePubkey,
            laptopPubkey: legacy.laptopPubkey,
            deviceName: legacy.deviceName,
            lanHost: legacy.lanHost,
            isPaired: true
          };
        }
      } catch (e) {}
    }
    return null;
  }

  // Web Crypto Key Derivation & AES-GCM
  async function deriveAesKey(pin, saltBytes) {
    const enc = new TextEncoder();
    const subtle = window.crypto.subtle;
    const pinKey = await subtle.importKey(
      'raw',
      enc.encode(pin),
      { name: 'PBKDF2' },
      false,
      ['deriveKey']
    );
    return await subtle.deriveKey(
      {
        name: 'PBKDF2',
        salt: saltBytes,
        iterations: 100000,
        hash: 'SHA-256'
      },
      pinKey,
      { name: 'AES-GCM', length: 256 },
      false,
      ['encrypt', 'decrypt']
    );
  }

  async function encryptPrivkeyWithPin(pin, privkeyHex) {
    const saltBytes = window.crypto.getRandomValues(new Uint8Array(16));
    const ivBytes = window.crypto.getRandomValues(new Uint8Array(12));
    const aesKey = await deriveAesKey(pin, saltBytes);
    const enc = new TextEncoder();
    const ctBuffer = await window.crypto.subtle.encrypt(
      { name: 'AES-GCM', iv: ivBytes },
      aesKey,
      enc.encode(privkeyHex)
    );
    return {
      saltHex: bytesToHex(saltBytes),
      ivHex: bytesToHex(ivBytes),
      ciphertextHex: bytesToHex(new Uint8Array(ctBuffer))
    };
  }

  async function decryptPrivkeyWithPin(pin, vaultData) {
    if (!vaultData || !vaultData.saltHex || !vaultData.ivHex || !vaultData.ciphertextHex) {
      throw new Error('Vault missing or incomplete');
    }
    const saltBytes = hexToBytes(vaultData.saltHex);
    const ivBytes = hexToBytes(vaultData.ivHex);
    const ctBytes = hexToBytes(vaultData.ciphertextHex);
    const aesKey = await deriveAesKey(pin, saltBytes);
    const decryptedBuffer = await window.crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: ivBytes },
      aesKey,
      ctBytes
    );
    return new TextDecoder().decode(decryptedBuffer);
  }

  function ensurePhoneKeys() {
    if (config.isPaired) {
      // When paired, phonePubkey is established and phonePrivkey is only loaded via local PIN unlock
      return;
    }
    if (!config.phonePrivkey || !config.phonePubkey) {
      if (typeof NostrTools !== 'undefined' && NostrTools.generateSecretKey) {
        const sk = NostrTools.generateSecretKey();
        config.phonePrivkey = NostrTools.utils.bytesToHex(sk);
        config.phonePubkey = NostrTools.getPublicKey(sk);
      }
    }
  }

  function showToast(msg) {
    toastText.textContent = msg;
    toast.classList.add('active');
    setTimeout(() => toast.classList.remove('active'), 3200);
  }

  function getStorage() {
    try {
      if (typeof window !== 'undefined' && window.sessionStorage) return window.sessionStorage;
    } catch (e) {}
    try {
      if (typeof window !== 'undefined' && window.localStorage) return window.localStorage;
    } catch (e) {}
    return null;
  }

  function saveActionState(actState) {
    const s = getStorage();
    if (s && actState) {
      try { s.setItem('pcremote_action_state', JSON.stringify(actState)); } catch (e) {}
    }
  }

  function loadActionState() {
    const s = getStorage();
    if (s) {
      try {
        const raw = s.getItem('pcremote_action_state');
        if (raw) return JSON.parse(raw);
      } catch (e) {}
    }
    return null;
  }

  function clearActionState() {
    const s = getStorage();
    if (s) {
      try { s.removeItem('pcremote_action_state'); } catch (e) {}
    }
  }

  function saveLastTelemetry(data) {
    const s = getStorage();
    if (s && data) {
      try {
        s.setItem('pcremote_last_telemetry', JSON.stringify({
          data: data,
          lastSeen: Date.now()
        }));
      } catch (e) {}
    }
  }

  function loadLastTelemetry() {
    const s = getStorage();
    if (s) {
      try {
        const raw = s.getItem('pcremote_last_telemetry');
        if (raw) return JSON.parse(raw);
      } catch (e) {}
    }
    return null;
  }

  function formatTime(timestamp) {
    if (!timestamp) return '';
    try {
      return new Date(timestamp).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    } catch (e) {
      return '';
    }
  }

  function setControlsEnabled(enabled) {
    if (btnLock) {
      if (lastTelemetryData && lastTelemetryData.isLocked) {
        btnLock.disabled = true;
      } else {
        btnLock.disabled = !enabled;
      }
    }
    if (btnSleep) btnSleep.disabled = !enabled;
    if (btnRestart) btnRestart.disabled = !enabled;
    if (btnShutdown) btnShutdown.disabled = !enabled;
  }

  function setStatus(state, text) {
    if (state === 'online') {
      statusIndicator.className = 'status-indicator';
      statusLabel.textContent = text || 'CONNECTED';
      isPcOnline = true;
      isConnected = true;
      setControlsEnabled(true);
      if (statusSub) statusSub.style.display = 'none';
    } else if (state === 'connecting') {
      statusIndicator.className = 'status-indicator connecting';
      statusLabel.textContent = text || 'CONNECTING...';
      isPcOnline = false;
      isConnected = false;
      setControlsEnabled(false);
      if (statusSub) {
        if (lastSeenTime) {
          statusSub.textContent = 'Last online at ' + formatTime(lastSeenTime);
          statusSub.style.display = 'block';
        } else {
          statusSub.style.display = 'none';
        }
      }
    } else {
      statusIndicator.className = 'status-indicator offline';
      statusLabel.textContent = text || 'OFFLINE';
      isPcOnline = false;
      isConnected = false;
      setControlsEnabled(false);
      if (statusSub) {
        if (lastSeenTime) {
          statusSub.textContent = 'Last online at ' + formatTime(lastSeenTime);
          statusSub.style.display = 'block';
        } else {
          statusSub.style.display = 'none';
        }
      }
    }
  }

  function restoreCachedTelemetry() {
    const cached = loadLastTelemetry();
    if (cached) {
      if (cached.lastSeen) {
        lastSeenTime = cached.lastSeen;
        if (statusSub && !isPcOnline) {
          statusSub.textContent = 'Last online at ' + formatTime(lastSeenTime);
          statusSub.style.display = 'block';
        }
      }
      if (cached.data) {
        lastTelemetryData = cached.data;
        if (cached.data.battery !== undefined) {
          const pct = Math.min(100, Math.max(0, cached.data.battery));
          if (batteryPercentText) batteryPercentText.textContent = `${pct}%`;
          if (batteryFill) {
            batteryFill.style.width = `${pct}%`;
            if (pct <= 20) batteryFill.className = 'battery-fill red';
            else if (pct <= 40) batteryFill.className = 'battery-fill amber';
            else batteryFill.className = 'battery-fill';
          }
        }
        if (cached.data.deviceName && deviceName) deviceName.textContent = cached.data.deviceName;
      }
    }
  }

  // Deterministic Landing Views
  function showLandingState(type) {
    mainDashboard.style.display = 'none';
    pinView.style.display = 'none';
    actionStateView.style.display = 'none';
    landingStateView.style.display = 'flex';

    if (type === 'public') {
      landingIconWrap.className = 'state-icon-wrap';
      landingIconWrap.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
          <rect x="2" y="3" width="20" height="14" rx="2"/>
          <line x1="8" y1="21" x2="16" y2="21"/>
          <line x1="12" y1="17" x2="12" y2="21"/>
        </svg>`;
      landingTitle.textContent = 'No Device Paired';
      landingDesc.textContent = 'Open this page from the pairing QR code displayed by PC Remote on your PC.';
      landingBadge.style.display = 'none';
      btnLandingAction.style.display = 'none';
      setStatus('offline', 'IDLE');
    } else if (type === 'invalid') {
      landingIconWrap.className = 'state-icon-wrap is-off';
      landingIconWrap.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
          <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
          <line x1="12" y1="9" x2="12" y2="13"/>
          <line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>`;
      landingTitle.textContent = 'Invalid Pairing Link';
      landingDesc.textContent = 'The pairing link is malformed or invalid. Generate a new pairing QR code from PC Remote on your PC.';
      landingBadge.style.display = 'none';
      btnLandingAction.style.display = 'none';
      setStatus('offline', 'INVALID');
    } else if (type === 'expired') {
      landingIconWrap.className = 'state-icon-wrap is-sleeping';
      landingIconWrap.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="10"/>
          <polyline points="12 6 12 12 16 14"/>
        </svg>`;
      landingTitle.textContent = 'Pairing Link Expired';
      landingDesc.textContent = 'This pairing session has expired for your security. Generate a new pairing QR code from PC Remote on your PC.';
      landingBadge.style.display = 'none';
      btnLandingAction.style.display = 'none';
      setStatus('offline', 'EXPIRED');
    }
  }

  // State Machine Evaluator
  async function evaluateInitialState() {
    if (remoteDesktopView) remoteDesktopView.style.display = 'none';
    if (remoteConsentModal) remoteConsentModal.style.display = 'none';

    // 1. Inspect URL Fragment first
    const hash = window.location.hash.substring(1);
    let urlPair = null;
    let urlKey = null;
    let urlLan = null;
    let urlName = null;
    let hasUrlParams = false;

    if (hash) {
      try {
        const params = new URLSearchParams(hash);
        urlPair = params.get('pair');
        urlKey = params.get('key');
        urlLan = params.get('lan');
        urlName = params.get('name');
        if (urlPair !== null || urlKey !== null || urlLan !== null || urlName !== null) {
          hasUrlParams = true;
        }
      } catch (e) {}
      // Immediately clean fragment from visible address bar
      window.history.replaceState(null, '', window.location.pathname);
    }

    // 2. Case: Arrived with URL pairing parameters (QR Code scan)
    if (hasUrlParams) {
      if (!isValidPubkey(urlKey) || !isValidPairingToken(urlPair)) {
        showLandingState('invalid');
        return;
      }

      config.laptopPubkey = urlKey;
      config.pairingToken = urlPair;
      if (urlLan && isValidLanHost(urlLan)) config.lanHost = urlLan;
      if (urlName) config.deviceName = urlName;
      config.isPaired = false;

      if (config.deviceName) {
        deviceName.textContent = config.deviceName;
      }

      landingStateView.style.display = 'none';
      mainDashboard.style.display = 'none';
      actionStateView.style.display = 'none';
      pinView.style.display = 'flex';
      pinTitle.textContent = config.deviceName ? `Pair with ${config.deviceName}` : 'Pair with PC';
      pinSubtitle.textContent = 'Enter your 6-digit Master PIN to pair this phone';

      initRelays();
      return;
    }

    // 3. Check stored vault for authenticated session
    const vault = await loadVaultAuth();
    if (vault && vault.isPaired && isValidPubkey(vault.laptopPubkey)) {
      config.laptopPubkey = vault.laptopPubkey || '';
      config.phonePubkey = vault.phonePubkey || '';
      config.deviceName = vault.deviceName || '';
      config.lanHost = vault.lanHost || '';
      config.isPaired = true;
      config.phonePrivkey = ''; // Explicitly lock key until local PIN is validated

      if (config.deviceName) {
        deviceName.textContent = config.deviceName;
      }

      landingStateView.style.display = 'none';
      actionStateView.style.display = 'none';

      if (isLocked) {
        pinTitle.textContent = 'Enter your 6-digit PIN';
        pinSubtitle.textContent = config.deviceName ? `Authenticate to control ${config.deviceName}` : 'Authenticate to control this PC';
        mainDashboard.style.display = 'none';
        pinView.style.display = 'flex';
      } else {
        pinView.style.display = 'none';
        const savedAction = loadActionState();
        if (savedAction && savedAction.action && (Date.now() - (savedAction.timestamp || 0) < 30 * 60 * 1000)) {
          showActionState(savedAction.action);
        } else {
          mainDashboard.style.display = 'flex';
          setStatus('connecting', 'CONNECTING...');
          restoreCachedTelemetry();
          startTelemetryLoop();
        }
      }

      initRelays();
      return;
    }

    // 4. Case: Direct Public Visit (No pairing context)
    showLandingState('public');
  }

  // Nostr Relay Mesh Management
  function initRelays() {
    ensurePhoneKeys();

    RELAYS.forEach((url) => {
      connectRelay(url);
    });
  }

  function connectRelay(url) {
    if (relaySockets.has(url)) {
      const existing = relaySockets.get(url);
      if (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING) {
        return;
      }
    }

    try {
      const ws = new WebSocket(url);
      relaySockets.set(url, ws);

      ws.onopen = () => {
        if (!isPcOnline) {
          setStatus('connecting', 'CONNECTING...');
        }
        if (config.phonePubkey) {
          const subId = 'sub_' + Math.random().toString(36).substring(2, 8);
          const filter = {
            kinds: [4],
            '#p': [config.phonePubkey],
            since: Math.floor(Date.now() / 1000) - 60
          };
          ws.send(JSON.stringify(['REQ', subId, filter]));
        }
      };

      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          if (!Array.isArray(msg)) return;
          const [type, subId, evt] = msg;
          if (type === 'EVENT' && evt && evt.kind === 4) {
            handleIncomingNostrEvent(evt);
          }
        } catch (e) {}
      };

      ws.onclose = () => {
        relaySockets.delete(url);
        let anyConnected = false;
        for (const sock of relaySockets.values()) {
          if (sock.readyState === WebSocket.OPEN) {
            anyConnected = true;
            break;
          }
        }
        if (!anyConnected && !isPcOnline) {
          setStatus('offline', 'DISCONNECTED');
        }
        if (config.isPaired || config.pairingToken) {
          setTimeout(() => connectRelay(url), 4000);
        }
      };

      ws.onerror = () => {
        ws.close();
      };
    } catch (e) {}
  }

  function handleIncomingNostrEvent(evt) {
    if (!evt || !evt.pubkey || !evt.content) return;

    if (config.laptopPubkey && evt.pubkey !== config.laptopPubkey) {
      return;
    }

    if (typeof NostrTools !== 'undefined' && NostrTools.verifyEvent) {
      if (!NostrTools.verifyEvent(evt)) {
        return;
      }
    }

    // Strict NIP-44 v2 decryption (no legacy NIP-04)
    let plaintext = '';
    if (typeof NostrTools !== 'undefined' && config.phonePrivkey) {
      try {
        const skBytes = NostrTools.utils.hexToBytes(config.phonePrivkey);
        const convKey = NostrTools.nip44.v2.utils.getConversationKey(skBytes, evt.pubkey);
        plaintext = NostrTools.nip44.v2.decrypt(evt.content, convKey);
      } catch (e) {}
    }

    if (!plaintext) return;

    try {
      const data = JSON.parse(plaintext);
      if (data && data.signal && isRemoteDesktopActive) {
        handleIncomingRemoteSignal(data.signal);
      }
      if (data && data.id && pendingRequests.has(data.id)) {
        const { resolve, timer } = pendingRequests.get(data.id);
        clearTimeout(timer);
        pendingRequests.delete(data.id);
        isPcOnline = true;
        consecutiveFailures = 0;
        lastSeenTime = Date.now();
        activeTransport = 'CONNECTED';
        setStatus('online', 'CONNECTED');
        resolve(data);
      }
    } catch (e) {}
  }

  // Create unified cryptographic Nostr envelope shared across LAN and Relay
  function createCommandEnvelope(action, extraPayload = {}) {
    ensurePhoneKeys();

    if (!config.phonePrivkey || !config.laptopPubkey) {
      throw new Error('Missing pairing keys. Scan the QR code on your PC.');
    }

    if (typeof NostrTools === 'undefined') {
      throw new Error('Nostr cryptography library not loaded.');
    }

    const reqId = 'req_' + Math.random().toString(36).substring(2, 10) + Date.now().toString(36);
    const payload = {
      id: reqId,
      action: action,
      timestamp: Math.floor(Date.now() / 1000),
      ...extraPayload
    };

    const skBytes = NostrTools.utils.hexToBytes(config.phonePrivkey);
    const convKey = NostrTools.nip44.v2.utils.getConversationKey(skBytes, config.laptopPubkey);
    const ciphertext = NostrTools.nip44.v2.encrypt(JSON.stringify(payload), convKey);

    const template = {
      kind: 4,
      created_at: Math.floor(Date.now() / 1000),
      tags: [['p', config.laptopPubkey]],
      content: ciphertext
    };

    const signedEvt = NostrTools.finalizeEvent(template, skBytes);
    return { reqId, signedEvt, convKey };
  }

  // Unified Request Dispatcher (Direct LAN HTTP -> Nostr E2EE Relay Fallback)
  async function sendRequest(action, extraPayload = {}) {
    let envelope;
    try {
      envelope = createCommandEnvelope(action, extraPayload);
    } catch (err) {
      return Promise.reject(err);
    }
    const { reqId, signedEvt, convKey } = envelope;

    const isRemoteReq = (action === 'remote_request');
    const lanTimeoutMs = isRemoteReq ? 15000 : 1400;
    const relayTimeoutMs = isRemoteReq ? 15000 : 6500;
    const sendStartTime = Date.now();

    if (isRemoteReq) {
      console.log(`[RemoteReq Timing] [Req: ${reqId}] remote_request dispatched at ${new Date().toISOString()} (LAN timeout: ${lanTimeoutMs}ms, Relay timeout: ${relayTimeoutMs}ms)`);
    }

    const canAttemptLan = !!config.lanHost;

    if (canAttemptLan) {
      const endpoint = `http://${config.lanHost}/api/control`;
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), lanTimeoutMs);

        // POST ONLY the signed Nostr envelope! (Zero plaintext PIN!)
        const res = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(signedEvt),
          signal: controller.signal
        });
        clearTimeout(timeoutId);

        if (res.ok) {
          const respEvt = await res.json();
          // Cryptographically verify laptop signature and decrypt response with NIP-44 v2
          if (respEvt && respEvt.content && respEvt.pubkey === config.laptopPubkey) {
            if (typeof NostrTools !== 'undefined' && NostrTools.verifyEvent(respEvt)) {
              const plain = NostrTools.nip44.v2.decrypt(respEvt.content, convKey);
              const data = JSON.parse(plain);
              if (data && data.id === reqId) {
                isPcOnline = true;
                consecutiveFailures = 0;
                lastSeenTime = Date.now();
                activeTransport = 'LOCAL';
                setStatus('online', 'LOCAL');
                if (isRemoteReq) {
                  console.log(`[RemoteReq Timing] [Req: ${reqId}] remote_request succeeded via LAN in ${Date.now() - sendStartTime}ms`);
                }
                return data;
              }
            }
          } else if (respEvt && (respEvt.error || respEvt.status)) {
            if (isRemoteReq) {
              console.log(`[RemoteReq Timing] [Req: ${reqId}] remote_request error via LAN in ${Date.now() - sendStartTime}ms:`, respEvt);
            }
            return respEvt;
          }
        } else if (res.status === 401 || res.status === 400 || res.status === 403) {
          const errData = await res.json();
          if (isRemoteReq) {
            console.log(`[RemoteReq Timing] [Req: ${reqId}] remote_request HTTP ${res.status} in ${Date.now() - sendStartTime}ms:`, errData);
          }
          return errData;
        }
      } catch (e) {
        if (isRemoteReq) {
          console.log(`[RemoteReq Timing] [Req: ${reqId}] LAN attempt ended after ${Date.now() - sendStartTime}ms (${e.name === 'AbortError' ? 'timeout' : e.message}), falling back to Nostr relays`);
        }
        // Fall through to Nostr relay pool
      }
    }

    // Fallback: publish cryptographic envelope to Nostr relays
    return new Promise((resolve, reject) => {
      const rawMsg = JSON.stringify(['EVENT', signedEvt]);
      let activeCount = 0;
      for (const [url, ws] of relaySockets.entries()) {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(rawMsg);
          activeCount++;
        }
      }

      if (activeCount === 0) {
        initRelays();
        return reject(new Error('Connecting to Nostr network... Please retry in a moment.'));
      }

      const timer = setTimeout(() => {
        if (pendingRequests.has(reqId)) {
          pendingRequests.delete(reqId);
          if (isRemoteReq) {
            console.warn(`[RemoteReq Timing] [Req: ${reqId}] remote_request timed out on relays after ${Date.now() - sendStartTime}ms (limit: ${relayTimeoutMs}ms)`);
          }
          reject(new Error('Request timed out'));
        }
      }, relayTimeoutMs);

      pendingRequests.set(reqId, {
        resolve: (data) => {
          if (isRemoteReq) {
            console.log(`[RemoteReq Timing] [Req: ${reqId}] remote_request resolved via relay in ${Date.now() - sendStartTime}ms`);
          }
          resolve(data);
        },
        reject,
        timer
      });
    });
  }

  // PIN Keypad Management
  function updatePinDots() {
    for (let i = 0; i < 6; i++) {
      const dot = document.getElementById(`dot${i}`);
      if (dot) {
        if (i < enteredPin.length) dot.classList.add('filled');
        else dot.classList.remove('filled');
      }
    }
  }

  function handleKeypadPress(key) {
    if (enteredPin.length < 6) {
      enteredPin += key;
      updatePinDots();
      pinError.textContent = '';
      if (enteredPin.length === 6) {
        verifyPin(enteredPin);
      }
    }
  }

  async function verifyPin(pinCandidate) {
    // 1. Initial Pairing Handshake (device not yet paired)
    if (!config.isPaired) {
      showToast('Pairing with PC...');
      try {
        const res = await sendRequest('pair', {
          token: config.pairingToken,
          pin: pinCandidate,
          deviceName: getClientDeviceName()
        });

        if (res && res.error) {
          if (res.error.toLowerCase().includes('expired') || res.error.toLowerCase().includes('pairing token')) {
            showLandingState('expired');
            return;
          }
          pinError.textContent = res.error || 'Incorrect Master PIN';
          enteredPin = '';
          updatePinDots();
          return;
        }

        // Successfully paired with laptop!
        // Encrypt phonePrivkey with Web Crypto AES-GCM key derived from Master PIN
        const encData = await encryptPrivkeyWithPin(pinCandidate, config.phonePrivkey);
        await saveVaultAuth({
          saltHex: encData.saltHex,
          ivHex: encData.ivHex,
          ciphertextHex: encData.ciphertextHex,
          phonePubkey: config.phonePubkey,
          laptopPubkey: config.laptopPubkey,
          deviceName: config.deviceName,
          lanHost: config.lanHost,
          isPaired: true
        });

        // Purge any legacy plaintext keys from local storage
        localStorage.removeItem('pcremote_cfg');
        localStorage.removeItem('laptopcontrol_cfg');

        config.isPaired = true;
        config.pairingToken = '';
        isLocked = false;
        pinView.style.display = 'none';
        landingStateView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        if (res && res.telemetry) updateTelemetryUI(res.telemetry);
        showToast('Paired successfully with ' + (res.deviceName || 'PC'));
        startTelemetryLoop();
      } catch (err) {
        handlePinError(err);
      }
      return;
    }

    // 2. Already Paired: Local Phone Session Unlock (Web Crypto PBKDF2/AES-GCM verifier)
    try {
      const vault = await loadVaultAuth();
      if (!vault) {
        config.isPaired = false;
        showLandingState('public');
        return;
      }

      // Handle legacy migration if unencrypted key was in storage
      if (vault.legacy && vault.phonePrivkey) {
        config.phonePrivkey = vault.phonePrivkey;
        const encData = await encryptPrivkeyWithPin(pinCandidate, config.phonePrivkey);
        await saveVaultAuth({
          saltHex: encData.saltHex,
          ivHex: encData.ivHex,
          ciphertextHex: encData.ciphertextHex,
          phonePubkey: config.phonePubkey,
          laptopPubkey: config.laptopPubkey,
          deviceName: config.deviceName,
          lanHost: config.lanHost,
          isPaired: true
        });
        localStorage.removeItem('pcremote_cfg');
        localStorage.removeItem('laptopcontrol_cfg');
      } else {
        // Attempt to decrypt stored private key with candidate PIN
        // If PIN is wrong, AES-GCM authentication tag check fails and throws error!
        const decryptedPrivkey = await decryptPrivkeyWithPin(pinCandidate, vault);
        config.phonePrivkey = decryptedPrivkey;
      }

      // Unlock session
      isLocked = false;
      enteredPin = '';
      updatePinDots();
      pinView.style.display = 'none';
      landingStateView.style.display = 'none';

      const savedAction = loadActionState();
      if (savedAction && savedAction.action && (Date.now() - (savedAction.timestamp || 0) < 30 * 60 * 1000)) {
        showActionState(savedAction.action);
      } else {
        clearActionState();
        mainDashboard.style.display = 'flex';
        setStatus('connecting', 'CONNECTING...');
        restoreCachedTelemetry();
        startTelemetryLoop();

        // Refresh telemetry using phone's cryptographic identity (zero PIN transmitted!)
        sendRequest('telemetry').then((data) => {
          const tel = (data && data.telemetry) ? data.telemetry : data;
          if (tel && !data.error) {
            consecutiveFailures = 0;
            isPcOnline = true;
            lastSeenTime = Date.now();
            setStatus('online', activeTransport || 'CONNECTED');
            updateTelemetryUI(tel);
          }
        }).catch(() => {
          consecutiveFailures++;
          setStatus('offline', 'OFFLINE');
        });
      }

    } catch (err) {
      // AES-GCM MAC verification failed -> Incorrect PIN
      pinError.textContent = 'Incorrect PIN';
      enteredPin = '';
      updatePinDots();
    }
  }

  function handlePinError(err) {
    let msg = err.message || 'Connection failed';
    if (window.location.protocol === 'https:' && config.lanHost) {
      pinError.innerHTML = `${msg} — <a href="http://${config.lanHost}/" style="color:#22c55e;text-decoration:underline;">Switch to Direct LAN</a>`;
    } else {
      pinError.textContent = msg;
    }
    enteredPin = '';
    updatePinDots();
  }

  document.querySelectorAll('.keypad-btn[data-key]').forEach(btn => {
    btn.addEventListener('click', () => handleKeypadPress(btn.getAttribute('data-key')));
  });

  document.getElementById('btnClear')?.addEventListener('click', () => {
    enteredPin = '';
    pinError.textContent = '';
    updatePinDots();
  });

  document.getElementById('btnDel')?.addEventListener('click', () => {
    if (enteredPin.length > 0) {
      enteredPin = enteredPin.slice(0, -1);
      pinError.textContent = '';
      updatePinDots();
    }
  });

  // Update UI Telemetry
  function updateTelemetryUI(data) {
    if (!data) return;

    lastTelemetryData = data;
    saveLastTelemetry(data);
    lastSeenTime = Date.now();

    if (data.deviceName) deviceName.textContent = data.deviceName;

    if (data.networkName) networkName.textContent = data.networkName;
    if (data.networkType) networkType.textContent = data.networkType;

    if (data.battery !== undefined) {
      const pct = Math.min(100, Math.max(0, data.battery));
      batteryPercentText.textContent = `${pct}%`;
      batteryFill.style.width = `${pct}%`;

      if (pct <= 20) {
        batteryFill.className = 'battery-fill red';
      } else if (pct <= 40) {
        batteryFill.className = 'battery-fill amber';
      } else {
        batteryFill.className = 'battery-fill';
      }

      if (data.charging) {
        batteryLightning.style.display = 'block';
      } else {
        batteryLightning.style.display = 'none';
      }
    }

    if (data.cpu !== undefined) {
      cpuValue.textContent = `${data.cpu}%`;
      cpuMeter.style.width = `${Math.min(100, Math.max(0, data.cpu))}%`;
      cpuMeter.className = 'meter-fill ' + (data.cpu >= 80 ? 'red' : (data.cpu >= 50 ? 'amber' : ''));
    }

    if (data.ramPercent !== undefined) {
      ramValue.textContent = `${data.ramPercent}%`;
      ramMeter.style.width = `${Math.min(100, Math.max(0, data.ramPercent))}%`;
      ramGb.textContent = `${data.ramUsedGb || 0} / ${data.ramTotalGb || 0} GB`;
    }

    if (data.uptime) {
      uptimeValue.textContent = data.uptime;
    }

    if (data.isLocked) {
      btnLock.disabled = true;
      btnLock.classList.add('is-locked');
      lockName.textContent = 'Locked';
      lockDesc.textContent = 'PC is currently locked';
      lockRight.innerHTML = '<span class="locked-badge">Locked</span>';
    } else {
      btnLock.disabled = !isPcOnline;
      btnLock.classList.remove('is-locked');
      lockName.textContent = 'Lock';
      lockDesc.textContent = 'Lock PC immediately';
      lockRight.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="9 18 15 12 9 6"/>
        </svg>`;
    }
  }

  // Telemetry Polling Loop
  function startTelemetryLoop() {
    if (pollInterval) clearInterval(pollInterval);
    pollInterval = setInterval(() => {
      if (!isLocked && config.isPaired) {
        sendRequest('telemetry')
          .then((data) => {
            const tel = (data && data.telemetry) ? data.telemetry : data;
            if (tel && !data.error) {
              consecutiveFailures = 0;
              isPcOnline = true;
              lastSeenTime = Date.now();
              setStatus('online', activeTransport || 'CONNECTED');
              updateTelemetryUI(tel);
            }
          })
          .catch(() => {
            consecutiveFailures++;
            if (consecutiveFailures >= 2) {
              setStatus('offline', 'OFFLINE');
              cpuValue.textContent = '--%';
              cpuMeter.style.width = '0%';
              ramValue.textContent = '--%';
              ramMeter.style.width = '0%';
              uptimeValue.textContent = '--';
              networkName.textContent = 'Disconnected';
              networkType.textContent = 'OFFLINE';
            }
          });
      }
    }, 4000);
  }

  // PC Lock Action
  btnLock.addEventListener('click', () => {
    if (!isPcOnline) {
      showToast('PC is offline');
      return;
    }
    btnLock.disabled = true;
    sendRequest('lock')
      .then((res) => {
        showToast('PC locked');
        btnLock.classList.add('is-locked');
        lockName.textContent = 'Locked';
        lockDesc.textContent = 'PC is currently locked';
        lockRight.innerHTML = '<span class="locked-badge">Locked</span>';
      })
      .catch((e) => {
        showToast('Lock failed: ' + (e.message || 'Error'));
      })
      .finally(() => {
        btnLock.disabled = !isPcOnline;
      });
  });

  if (btnRefresh) {
    btnRefresh.addEventListener('click', () => {
      showToast('Checking PC status...');
      sendRequest('telemetry')
        .then((data) => {
          const tel = (data && data.telemetry) ? data.telemetry : data;
          if (tel && !data.error) {
            consecutiveFailures = 0;
            isPcOnline = true;
            lastSeenTime = Date.now();
            setStatus('online', activeTransport || 'CONNECTED');
            updateTelemetryUI(tel);
            showToast('PC is online');
          }
        })
        .catch(() => {
          consecutiveFailures++;
          setStatus('offline', 'OFFLINE');
          showToast('PC is offline');
        });
    });
  }

  function openConfirm(action, title, desc, isDanger) {
    pendingAction = action;
    modalTitle.textContent = title;
    modalDesc.textContent = desc;
    btnModalConfirm.className = 'btn-modal ' + (isDanger ? 'confirm-danger' : 'confirm-primary');
    btnModalConfirm.textContent = isDanger ? 'Shut Down' : (action === 'restart' ? 'Restart' : 'Sleep');
    confirmModal.classList.add('active');
  }

  btnSleep.addEventListener('click', () => {
    if (!isPcOnline) {
      showToast('PC is offline');
      return;
    }
    openConfirm(
      'sleep',
      'Confirm Sleep',
      'The PC will enter low-power sleep mode. You will need to press the PC power button or wake it up locally to resume.',
      false
    );
  });

  btnRestart.addEventListener('click', () => {
    if (!isPcOnline) {
      showToast('PC is offline');
      return;
    }
    openConfirm(
      'restart',
      'Confirm Restart',
      'The PC will close running applications and reboot in 5 seconds. The connection will automatically restore once the PC starts back up.',
      false
    );
  });

  btnShutdown.addEventListener('click', () => {
    if (!isPcOnline) {
      showToast('PC is offline');
      return;
    }
    openConfirm(
      'shutdown',
      'Confirm Shut Down',
      'The PC will shut down completely in 5 seconds. You will need physical access to turn the PC on again.',
      true
    );
  });

  btnModalCancel.addEventListener('click', () => {
    confirmModal.classList.remove('active');
    pendingAction = null;
  });

  function showActionState(action) {
    if (pollInterval) clearInterval(pollInterval);
    saveActionState({ action: action, timestamp: Date.now() });

    mainDashboard.style.display = 'none';
    pinView.style.display = 'none';
    landingStateView.style.display = 'none';
    actionStateView.style.display = 'flex';

    if (btnStateDismiss) {
      btnStateDismiss.style.display = 'inline-block';
      btnStateDismiss.onclick = () => {
        actionStateView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        setStatus('offline', 'OFFLINE');
        restoreCachedTelemetry();
        startTelemetryLoop();
      };
    }

    if (action === 'sleep') {
      stateIconWrap.className = 'state-icon-wrap is-sleeping';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
      stateTitle.textContent = 'PC is in Sleep Mode';
      stateDesc.textContent = 'The PC entered low-power standby mode. The remote connection has closed. Press the physical power button on the PC to wake it up.';
      stateBadgeDot.className = 'state-badge-dot';
      stateBadgeText.textContent = 'STANDBY • DISCONNECTED';
      btnStateAction.textContent = 'Reconnect when awake';
      btnStateAction.disabled = false;
      btnStateAction.onclick = () => tryReconnect();
    } else if (action === 'restart') {
      stateIconWrap.className = 'state-icon-wrap is-restarting';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>';
      stateTitle.textContent = 'Restarting PC...';
      stateDesc.textContent = 'The PC is rebooting. Connection will automatically restore once the PC starts back up.';
      stateBadgeDot.className = 'state-badge-dot pulse';
      stateBadgeText.textContent = 'REBOOTING';
      btnStateAction.textContent = 'Auto-reconnecting...';
      btnStateAction.disabled = true;

      startAutoReconnectLoop();
    } else if (action === 'shutdown') {
      stateIconWrap.className = 'state-icon-wrap is-off';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>';
      stateTitle.textContent = 'PC is Powered Off';
      stateDesc.textContent = 'The PC has powered down completely. Remote control is unavailable until the PC is turned on manually.';
      stateBadgeDot.className = 'state-badge-dot';
      stateBadgeText.textContent = 'POWERED OFF';
      btnStateAction.textContent = 'Check if turned on';
      btnStateAction.disabled = false;
      btnStateAction.onclick = () => tryReconnect();
    }
  }

  function startAutoReconnectLoop() {
    let attempts = 0;
    if (reconnectTimer) clearInterval(reconnectTimer);
    reconnectTimer = setInterval(() => {
      attempts++;
      sendRequest('telemetry').then((data) => {
        const tel = (data && data.telemetry) ? data.telemetry : data;
        if (tel && !data.error) {
          clearInterval(reconnectTimer);
          reconnectTimer = null;
          clearActionState();
          isPcOnline = true;
          consecutiveFailures = 0;
          lastSeenTime = Date.now();
          actionStateView.style.display = 'none';
          mainDashboard.style.display = 'flex';
          updateTelemetryUI(tel);
          startTelemetryLoop();
          showToast('Reconnected to PC');
        }
      }).catch(() => {
        btnStateAction.textContent = `Auto-reconnecting (${attempts * 3}s)...`;
      });
    }, 3000);
  }

  function tryReconnect() {
    btnStateAction.textContent = 'Checking connection...';
    btnStateAction.disabled = true;
    sendRequest('telemetry').then((data) => {
      const tel = (data && data.telemetry) ? data.telemetry : data;
      if (tel && !data.error) {
        clearActionState();
        isPcOnline = true;
        consecutiveFailures = 0;
        lastSeenTime = Date.now();
        actionStateView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        updateTelemetryUI(tel);
        startTelemetryLoop();
        showToast('Reconnected to PC');
      } else {
        showToast('PC is still offline');
      }
    }).catch(() => {
      showToast('PC is still offline');
    }).finally(() => {
      btnStateAction.disabled = false;
      btnStateAction.textContent = 'Check if turned on';
    });
  }

  btnModalConfirm.addEventListener('click', () => {
    if (pendingAction) {
      const act = pendingAction;
      confirmModal.classList.remove('active');
      saveActionState({ action: act, timestamp: Date.now() });
      showActionState(act);
      sendRequest(act)
        .then((res) => {
          if (res && res.error) {
            showToast('Failed: ' + res.error);
            clearActionState();
            return;
          }
        })
        .catch((e) => {
          showToast('Failed: ' + (e.message || 'Action failed'));
          clearActionState();
        });
      pendingAction = null;
    }
  });

  // ==================================================
  // Remote Desktop Client Logic
  // ==================================================
  const GESTURE_CONFIG = {
    TAP_MAX_DURATION_MS: 250,
    LONG_PRESS_DURATION_MS: 500,
    MOVE_TOLERANCE_PX: 8,
    POINTER_SENSITIVITY: 1.25,
    SCROLL_SENSITIVITY: 1.5,
    TWO_FINGER_TAP_MAX_DURATION_MS: 300,
  };

  let pendingDX = 0;
  let pendingDY = 0;
  let rAFScheduled = false;

  let latestAbsX = null;
  let latestAbsY = null;
  let rAFAbsScheduled = false;

  function sendControlMessage(msg) {
    if (remoteControlDC && remoteControlDC.readyState === 'open') {
      try {
        remoteControlDC.send(JSON.stringify(msg));
      } catch (err) {}
    }
  }

  function sendInputMessage(msg) {
    if (remoteInputDC && remoteInputDC.readyState === 'open') {
      try {
        remoteInputDC.send(JSON.stringify(msg));
      } catch (err) {}
    } else if (remoteControlDC && remoteControlDC.readyState === 'open') {
      try {
        remoteControlDC.send(JSON.stringify(msg));
      } catch (err) {}
    }
  }

  function queueRelativeMove(dx, dy) {
    pendingDX += dx;
    pendingDY += dy;
    if (!rAFScheduled) {
      rAFScheduled = true;
      requestAnimationFrame(() => {
        rAFScheduled = false;
        if (Math.abs(pendingDX) > 0.01 || Math.abs(pendingDY) > 0.01) {
          sendInputMessage({
            type: 'mouse_move',
            mode: 'relative',
            dx: Math.round(pendingDX),
            dy: Math.round(pendingDY)
          });
          pendingDX = 0;
          pendingDY = 0;
        }
      });
    }
  }

  function queueAbsoluteMove(x, y) {
    latestAbsX = x;
    latestAbsY = y;
    if (!rAFAbsScheduled) {
      rAFAbsScheduled = true;
      requestAnimationFrame(() => {
        rAFAbsScheduled = false;
        if (latestAbsX !== null && latestAbsY !== null) {
          sendInputMessage({
            type: 'mouse_move',
            mode: 'absolute',
            x: Math.round(latestAbsX * 10000) / 10000,
            y: Math.round(latestAbsY * 10000) / 10000
          });
          latestAbsX = null;
          latestAbsY = null;
        }
      });
    }
  }

  function sendInBandAuth() {
    if (!remoteControlDC || remoteControlDC.readyState !== 'open' || !remoteAuthChallenge) {
      return;
    }
    try {
      const created_at = Math.floor(Date.now() / 1000);
      const evtTemplate = {
        kind: 28000,
        created_at: created_at,
        tags: [],
        content: remoteAuthChallenge
      };
      const skBytes = NostrTools.utils.hexToBytes(config.phonePrivkey);
      const signedEvt = NostrTools.finalizeEvent(evtTemplate, skBytes);
      remoteControlDC.send(JSON.stringify({
        type: 'auth',
        token: remoteAuthChallenge,
        created_at: created_at,
        sig: signedEvt.sig
      }));
    } catch (err) {
      showToast('Auth error: ' + (err.message || 'Signature failed'));
    }
  }

  function startRemotePingLoop() {
    if (remotePingTimer) clearInterval(remotePingTimer);
    remotePingTimer = setInterval(() => {
      if (remoteControlDC && remoteControlDC.readyState === 'open') {
        remoteControlDC.send(JSON.stringify({ type: 'ping' }));
      }
    }, 3000);
  }

  async function handleIncomingRemoteSignal(signal) {
    if (!remoteRTC || !isRemoteDesktopActive || !signal) return;
    try {
      if (signal.type === 'answer' && signal.sdp) {
        if (remoteRTC.signalingState === 'have-local-offer') {
          await remoteRTC.setRemoteDescription(new RTCSessionDescription({
            type: 'answer',
            sdp: signal.sdp
          }));
        }
      } else if ((signal.type === 'ice-candidate' || signal.type === 'candidate') && signal.candidate) {
        // Host (Go) sends candidates as a nested object {candidate, sdpMid, sdpMLineIndex};
        // tolerate a flat string form too for backward compatibility.
        const c = signal.candidate;
        const isObj = (typeof c === 'object' && c !== null);
        const candidateStr = isObj ? c.candidate : c;
        const sdpMid = isObj ? c.sdpMid : signal.sdpMid;
        const sdpMLineIndex = isObj ? c.sdpMLineIndex : signal.sdpMLineIndex;
        if (candidateStr) {
          await remoteRTC.addIceCandidate(new RTCIceCandidate({
            candidate: candidateStr,
            sdpMid: sdpMid,
            sdpMLineIndex: sdpMLineIndex
          }));
        }
      } else if (signal.type === 'error') {
        const detail = signal.error || signal.message || 'Remote desktop failed on the host';
        showToast('Remote desktop error: ' + detail);
        stopRemoteDesktopSession();
      } else if (signal.type === 'session-close' || signal.type === 'close') {
        showToast('Remote desktop ended by host');
        stopRemoteDesktopSession();
      }
    } catch (err) {}
  }

  async function sendRemoteSignal(sig) {
    const res = await sendRequest('remote_signal', { signal: sig });
    if (res && res.signal) {
      await handleIncomingRemoteSignal(res.signal);
    }
    return res;
  }

  function clearRemoteConnectTimer() {
    if (remoteConnectTimer) {
      clearTimeout(remoteConnectTimer);
      remoteConnectTimer = null;
    }
  }

  // Fails the session with a visible error if the host never completes the
  // WebRTC handshake, instead of leaving the user on an endless black screen.
  function startRemoteConnectTimer() {
    clearRemoteConnectTimer();
    remoteConnectTimer = setTimeout(() => {
      if (!isRemoteDesktopActive) return;
      showToast('Connection timed out. The host did not start streaming.');
      stopRemoteDesktopSession();
    }, REMOTE_CONNECT_TIMEOUT_MS);
  }

  async function startRemoteDesktopSession() {
    if (!isPcOnline) {
      showToast('PC is offline');
      return;
    }
    if (!config.phonePrivkey) {
      showToast('Device not unlocked');
      return;
    }

    try {
      isRemoteDesktopActive = true;
      mainDashboard.style.display = 'none';
      if (remoteConsentModal) remoteConsentModal.style.display = 'none';
      remoteDesktopView.style.display = 'flex';
      remoteConnectingOverlay.style.display = 'flex';
      remoteConnectingText.textContent = 'Requesting desktop session...';
      remoteTransportBadge.className = 'network-badge-dot';
      remoteTransportLabel.textContent = 'CONNECTING';

      // Keep battery gauge synced
      if (remoteBatteryText && batteryPercentText) {
        remoteBatteryText.textContent = batteryPercentText.textContent;
      }

      // Step 1: Request session from service
      const reqStart = Date.now();
      const res = await sendRequest('remote_request');
      console.log(`[RemoteReq Timing] sendRequest('remote_request') completed in ${Date.now() - reqStart}ms, response status: ${res && res.status}`);
      if (!res || res.error || (res.status !== 'ready' && res.status !== 'accepted')) {
        const errMsg = (res && (res.error || res.message)) ? (res.error || res.message) : 'Session rejected by host';
        throw new Error(errMsg);
      }

      remoteSessionId = res.sessionId;
      remoteAuthChallenge = res.authChallenge;
      remoteConnectingText.textContent = 'Establishing secure WebRTC connection...';
      startRemoteConnectTimer();

      // Step 2: Configure WebRTC PeerConnection
      remoteRTC = new RTCPeerConnection({
        iceServers: [
          { urls: 'stun:stun.l.google.com:19302' }
        ]
      });

      remoteRTC.addTransceiver('video', { direction: 'recvonly' });

      // Create DataChannels
      remoteControlDC = remoteRTC.createDataChannel('control', { ordered: true });
      remoteInputDC = remoteRTC.createDataChannel('input', { ordered: false, maxRetransmits: 0 });

      remoteControlDC.onopen = () => {
        if (remoteAuthChallenge) {
          sendInBandAuth();
        }
      };

      remoteControlDC.onmessage = (e) => {
        try {
          const msg = JSON.parse(e.data);
          if (msg.type === 'auth_success') {
            clearRemoteConnectTimer();
            remoteConnectingOverlay.style.display = 'none';
            remoteTransportBadge.className = 'network-badge-dot pulse';
            remoteTransportLabel.textContent = activeTransport || 'CONNECTED';
            startRemotePingLoop();
          } else if (msg.type === 'auth_failed') {
            showToast('Remote desktop authentication failed');
            stopRemoteDesktopSession();
          } else if (msg.type === 'auth_challenge') {
            remoteAuthChallenge = msg.token;
            sendInBandAuth();
          } else if (msg.type === 'transport_state') {
            if (msg.transport) {
              remoteTransportLabel.textContent = msg.transport;
            }
          }
        } catch (err) {}
      };

      remoteRTC.ontrack = (event) => {
        if (event.streams && event.streams[0]) {
          remoteVideo.srcObject = event.streams[0];
        } else {
          remoteVideo.srcObject = new MediaStream([event.track]);
        }
        remoteVideo.play().catch(() => {});
      };

      remoteRTC.onicecandidate = (event) => {
        if (event.candidate && remoteSessionId) {
          sendRemoteSignal({
            sessionId: remoteSessionId,
            type: 'ice-candidate',
            candidate: {
              candidate: event.candidate.candidate,
              sdpMid: event.candidate.sdpMid,
              sdpMLineIndex: event.candidate.sdpMLineIndex
            }
          }).catch(() => {});
        }
      };

      remoteRTC.onconnectionstatechange = () => {
        if (remoteRTC.connectionState === 'connected') {
          remoteTransportBadge.className = 'network-badge-dot pulse';
        } else if (remoteRTC.connectionState === 'failed' || remoteRTC.connectionState === 'closed') {
          if (isRemoteDesktopActive) {
            showToast('Remote desktop connection closed');
            stopRemoteDesktopSession();
          }
        }
        // Note: 'disconnected' is intentionally NOT treated as fatal. It is
        // frequently transient on mobile (network switch / brief loss) and
        // WebRTC can recover on its own; only 'failed'/'closed' end the session.
      };

      // Step 3: Create offer and send signal
      const offer = await remoteRTC.createOffer();
      await remoteRTC.setLocalDescription(offer);

      const sigRes = await sendRemoteSignal({
        sessionId: remoteSessionId,
        type: 'offer',
        sdp: offer.sdp
      });
      if (sigRes && sigRes.signal) {
        await handleIncomingRemoteSignal(sigRes.signal);
      }
    } catch (err) {
      showToast('Failed to start remote desktop: ' + (err.message || 'Error'));
      stopRemoteDesktopSession();
    }
  }

  function stopRemoteDesktopSession() {
    if (!isRemoteDesktopActive) return;
    isRemoteDesktopActive = false;
    clearRemoteConnectTimer();

    if (remotePingTimer) {
      clearInterval(remotePingTimer);
      remotePingTimer = null;
    }

    if (remoteSessionId) {
      sendRemoteSignal({
        sessionId: remoteSessionId,
        type: 'session-close'
      }).catch(() => {});
    }

    if (remoteControlDC) {
      try { remoteControlDC.close(); } catch (e) {}
      remoteControlDC = null;
    }
    if (remoteInputDC) {
      try { remoteInputDC.close(); } catch (e) {}
      remoteInputDC = null;
    }
    if (remoteRTC) {
      try { remoteRTC.close(); } catch (e) {}
      remoteRTC = null;
    }
    if (remoteVideo && remoteVideo.srcObject) {
      try {
        const stream = remoteVideo.srcObject;
        if (stream.getTracks) {
          stream.getTracks().forEach(t => t.stop());
        }
      } catch (e) {}
      remoteVideo.srcObject = null;
    }

    remoteSessionId = null;
    remoteAuthChallenge = null;

    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    }
    if (remoteDesktopView) {
      remoteDesktopView.classList.remove('is-landscape');
      remoteDesktopView.style.display = 'none';
    }
    if (quickActionsSheet) {
      quickActionsSheet.classList.remove('active');
    }
    if (remoteConnectingOverlay) {
      remoteConnectingOverlay.style.display = 'none';
    }
    if (mainDashboard) {
      mainDashboard.style.display = 'flex';
    }
  }

  // --- Coordinate Mapping for Letterboxed Video in Landscape ---
  function getNormalizedVideoCoordinates(clientX, clientY) {
    if (!remoteVideo) return null;
    const rect = remoteVideo.getBoundingClientRect();
    if (!rect || rect.width <= 0 || rect.height <= 0) return null;

    const videoAspect = (remoteVideo.videoWidth && remoteVideo.videoHeight)
      ? (remoteVideo.videoWidth / remoteVideo.videoHeight)
      : (16 / 9);
    const elemAspect = rect.width / rect.height;

    let renderW = rect.width;
    let renderH = rect.height;
    let offsetX = 0;
    let offsetY = 0;

    if (elemAspect > videoAspect) {
      // Pillarboxed (letterbox left and right)
      renderW = rect.height * videoAspect;
      offsetX = (rect.width - renderW) / 2;
    } else {
      // Letterboxed (letterbox top and bottom)
      renderH = rect.width / videoAspect;
      offsetY = (rect.height - renderH) / 2;
    }

    const relX = clientX - rect.left - offsetX;
    const relY = clientY - rect.top - offsetY;

    if (relX < 0 || relX > renderW || relY < 0 || relY > renderH) {
      return null; // Touch was inside letterbox bar
    }

    const normX = Math.max(0.0, Math.min(1.0, relX / renderW));
    const normY = Math.max(0.0, Math.min(1.0, relY / renderH));
    return { x: normX, y: normY };
  }

  function isLandscapeMode() {
    return window.innerWidth > window.innerHeight || (remoteDesktopView && remoteDesktopView.classList.contains('is-landscape'));
  }

  // --- Portrait Gesture Recognizer on #remoteTouchpadArea ---
  if (remoteTouchpadArea) {
    let tStartTime = 0;
    let tStartX = 0;
    let tStartY = 0;
    let tLastX = 0;
    let tLastY = 0;
    let tIsDragging = false;
    let tLongPressTimer = null;
    let tLongPressTriggered = false;

    let twoFingerStart = 0;
    let twoFingerStartY = 0;
    let twoFingerStartX = 0;
    let twoFingerMoved = false;

    remoteTouchpadArea.addEventListener('touchstart', (e) => {
      e.preventDefault();
      if (e.touches.length === 1) {
        tStartTime = Date.now();
        tStartX = e.touches[0].clientX;
        tStartY = e.touches[0].clientY;
        tLastX = tStartX;
        tLastY = tStartY;
        tIsDragging = false;
        tLongPressTriggered = false;

        if (tLongPressTimer) clearTimeout(tLongPressTimer);
        tLongPressTimer = setTimeout(() => {
          if (!tIsDragging) {
            tLongPressTriggered = true;
            sendControlMessage({ type: 'mouse_click', button: 'right' });
            if (navigator.vibrate) navigator.vibrate(40);
          }
        }, GESTURE_CONFIG.LONG_PRESS_DURATION_MS);
      } else if (e.touches.length === 2) {
        if (tLongPressTimer) clearTimeout(tLongPressTimer);
        twoFingerStart = Date.now();
        twoFingerMoved = false;
        twoFingerStartX = (e.touches[0].clientX + e.touches[1].clientX) / 2;
        twoFingerStartY = (e.touches[0].clientY + e.touches[1].clientY) / 2;
      }
    }, { passive: false });

    remoteTouchpadArea.addEventListener('touchmove', (e) => {
      e.preventDefault();
      if (e.touches.length === 1) {
        const curX = e.touches[0].clientX;
        const curY = e.touches[0].clientY;
        const dist = Math.hypot(curX - tStartX, curY - tStartY);

        if (dist > GESTURE_CONFIG.MOVE_TOLERANCE_PX) {
          if (tLongPressTimer) {
            clearTimeout(tLongPressTimer);
            tLongPressTimer = null;
          }
          tIsDragging = true;
        }

        if (tIsDragging) {
          const dx = (curX - tLastX) * GESTURE_CONFIG.POINTER_SENSITIVITY;
          const dy = (curY - tLastY) * GESTURE_CONFIG.POINTER_SENSITIVITY;
          tLastX = curX;
          tLastY = curY;
          queueRelativeMove(dx, dy);
        }
      } else if (e.touches.length === 2) {
        if (tLongPressTimer) clearTimeout(tLongPressTimer);
        const midX = (e.touches[0].clientX + e.touches[1].clientX) / 2;
        const midY = (e.touches[0].clientY + e.touches[1].clientY) / 2;
        const deltaX = midX - twoFingerStartX;
        const deltaY = midY - twoFingerStartY;

        if (Math.hypot(deltaX, deltaY) > 4) {
          twoFingerMoved = true;
          // Invert deltaY for natural scroll
          sendInputMessage({
            type: 'scroll',
            dy: -deltaY * GESTURE_CONFIG.SCROLL_SENSITIVITY,
            dx: -deltaX * GESTURE_CONFIG.SCROLL_SENSITIVITY
          });
          twoFingerStartX = midX;
          twoFingerStartY = midY;
        }
      }
    }, { passive: false });

    remoteTouchpadArea.addEventListener('touchend', (e) => {
      e.preventDefault();
      if (tLongPressTimer) {
        clearTimeout(tLongPressTimer);
        tLongPressTimer = null;
      }

      if (e.touches.length === 0) {
        const elapsed = Date.now() - tStartTime;
        if (!tIsDragging && !tLongPressTriggered && elapsed <= GESTURE_CONFIG.TAP_MAX_DURATION_MS) {
          sendControlMessage({ type: 'mouse_click', button: 'left' });
        }
        if (twoFingerStart > 0 && !twoFingerMoved) {
          const tfElapsed = Date.now() - twoFingerStart;
          if (tfElapsed <= GESTURE_CONFIG.TWO_FINGER_TAP_MAX_DURATION_MS) {
            sendControlMessage({ type: 'mouse_click', button: 'middle' });
          }
        }
        twoFingerStart = 0;
        twoFingerMoved = false;
        tIsDragging = false;
        tLongPressTriggered = false;
      }
    }, { passive: false });

    remoteTouchpadArea.addEventListener('touchcancel', () => {
      if (tLongPressTimer) {
        clearTimeout(tLongPressTimer);
        tLongPressTimer = null;
      }
      tIsDragging = false;
      tLongPressTriggered = false;
      twoFingerStart = 0;
      twoFingerMoved = false;
    });
  }

  // --- Landscape Direct-Screen Touch Recognizer on #remoteVideo ---
  if (remoteVideo) {
    let vStartTime = 0;
    let vStartX = 0;
    let vStartY = 0;
    let vIsDragging = false;
    let vLongPressTimer = null;
    let vLongPressTriggered = false;

    let vTwoFingerStart = 0;
    let vTwoFingerStartY = 0;
    let vTwoFingerStartX = 0;
    let vTwoFingerMoved = false;

    remoteVideo.addEventListener('touchstart', (e) => {
      if (!isLandscapeMode()) return; // Screen preview is display-only in portrait
      e.preventDefault();

      if (e.touches.length === 1) {
        const coords = getNormalizedVideoCoordinates(e.touches[0].clientX, e.touches[0].clientY);
        if (!coords) return;

        vStartTime = Date.now();
        vStartX = e.touches[0].clientX;
        vStartY = e.touches[0].clientY;
        vIsDragging = false;
        vLongPressTriggered = false;

        queueAbsoluteMove(coords.x, coords.y);

        if (vLongPressTimer) clearTimeout(vLongPressTimer);
        vLongPressTimer = setTimeout(() => {
          if (!vIsDragging) {
            vLongPressTriggered = true;
            sendControlMessage({ type: 'mouse_click', button: 'right' });
            if (navigator.vibrate) navigator.vibrate(40);
          }
        }, GESTURE_CONFIG.LONG_PRESS_DURATION_MS);
      } else if (e.touches.length === 2) {
        if (vLongPressTimer) clearTimeout(vLongPressTimer);
        vTwoFingerStart = Date.now();
        vTwoFingerMoved = false;
        vTwoFingerStartX = (e.touches[0].clientX + e.touches[1].clientX) / 2;
        vTwoFingerStartY = (e.touches[0].clientY + e.touches[1].clientY) / 2;
      }
    }, { passive: false });

    remoteVideo.addEventListener('touchmove', (e) => {
      if (!isLandscapeMode()) return;
      e.preventDefault();

      if (e.touches.length === 1) {
        const curX = e.touches[0].clientX;
        const curY = e.touches[0].clientY;
        const dist = Math.hypot(curX - vStartX, curY - vStartY);

        if (dist > GESTURE_CONFIG.MOVE_TOLERANCE_PX) {
          if (vLongPressTimer) {
            clearTimeout(vLongPressTimer);
            vLongPressTimer = null;
          }
          vIsDragging = true;
        }

        if (vIsDragging) {
          const coords = getNormalizedVideoCoordinates(curX, curY);
          if (coords) {
            queueAbsoluteMove(coords.x, coords.y);
          }
        }
      } else if (e.touches.length === 2) {
        if (vLongPressTimer) clearTimeout(vLongPressTimer);
        const midX = (e.touches[0].clientX + e.touches[1].clientX) / 2;
        const midY = (e.touches[0].clientY + e.touches[1].clientY) / 2;
        const deltaX = midX - vTwoFingerStartX;
        const deltaY = midY - vTwoFingerStartY;

        if (Math.hypot(deltaX, deltaY) > 4) {
          vTwoFingerMoved = true;
          sendInputMessage({
            type: 'scroll',
            dy: -deltaY * GESTURE_CONFIG.SCROLL_SENSITIVITY,
            dx: -deltaX * GESTURE_CONFIG.SCROLL_SENSITIVITY
          });
          vTwoFingerStartX = midX;
          vTwoFingerStartY = midY;
        }
      }
    }, { passive: false });

    remoteVideo.addEventListener('touchend', (e) => {
      if (!isLandscapeMode()) return;
      e.preventDefault();

      if (vLongPressTimer) {
        clearTimeout(vLongPressTimer);
        vLongPressTimer = null;
      }

      if (e.touches.length === 0) {
        const elapsed = Date.now() - vStartTime;
        if (!vIsDragging && !vLongPressTriggered && elapsed <= GESTURE_CONFIG.TAP_MAX_DURATION_MS) {
          sendControlMessage({ type: 'mouse_click', button: 'left' });
        }
        if (vTwoFingerStart > 0 && !vTwoFingerMoved) {
          const tfElapsed = Date.now() - vTwoFingerStart;
          if (tfElapsed <= GESTURE_CONFIG.TWO_FINGER_TAP_MAX_DURATION_MS) {
            sendControlMessage({ type: 'mouse_click', button: 'middle' });
          }
        }
        vTwoFingerStart = 0;
        vTwoFingerMoved = false;
        vIsDragging = false;
        vLongPressTriggered = false;
      }
    }, { passive: false });

    remoteVideo.addEventListener('touchcancel', () => {
      if (vLongPressTimer) {
        clearTimeout(vLongPressTimer);
        vLongPressTimer = null;
      }
      vIsDragging = false;
      vLongPressTriggered = false;
      vTwoFingerStart = 0;
      vTwoFingerMoved = false;
    });
  }

  // --- Remote UI Event Listeners ---
  if (btnOpenRemoteDesktop) {
    btnOpenRemoteDesktop.addEventListener('click', () => {
      if (!isPcOnline) {
        showToast('PC is offline');
        return;
      }
      if (remoteConsentModal) {
        remoteConsentModal.style.display = 'flex';
      } else {
        startRemoteDesktopSession();
      }
    });
  }

  if (btnCancelRemoteConsent) {
    btnCancelRemoteConsent.addEventListener('click', () => {
      if (remoteConsentModal) {
        remoteConsentModal.style.display = 'none';
      }
    });
  }

  if (btnConfirmRemoteConsent) {
    btnConfirmRemoteConsent.addEventListener('click', () => {
      if (remoteConsentModal) {
        remoteConsentModal.style.display = 'none';
      }
      startRemoteDesktopSession();
    });
  }

  if (btnRemoteBack) {
    btnRemoteBack.addEventListener('click', () => {
      stopRemoteDesktopSession();
    });
  }

  if (btnToggleOrientation) {
    btnToggleOrientation.addEventListener('click', async () => {
      if (!remoteDesktopView) return;
      const willBeLandscape = !remoteDesktopView.classList.contains('is-landscape');
      remoteDesktopView.classList.toggle('is-landscape');

      try {
        if (willBeLandscape) {
          if (document.documentElement.requestFullscreen) {
            await document.documentElement.requestFullscreen().catch(() => {});
          }
          if (screen.orientation && screen.orientation.lock) {
            await screen.orientation.lock('landscape').catch(() => {});
          }
        } else {
          if (document.fullscreenElement) {
            await document.exitFullscreen().catch(() => {});
          }
          if (screen.orientation && screen.orientation.unlock) {
            screen.orientation.unlock();
          }
        }
      } catch (err) {}
    });
  }

  // --- Quick Actions Floating Button & Actions ---
  if (btnFloatingQuick) {
    btnFloatingQuick.addEventListener('click', (e) => {
      e.stopPropagation();
      if (quickActionsSheet) {
        quickActionsSheet.classList.toggle('active');
      }
    });
  }

  if (document.addEventListener) {
    document.addEventListener('click', (e) => {
      if (quickActionsSheet && quickActionsSheet.classList && quickActionsSheet.classList.contains('active')) {
        if (quickActionsSheet.contains && !quickActionsSheet.contains(e.target) && e.target !== btnFloatingQuick) {
          quickActionsSheet.classList.remove('active');
        }
      }
    });
  }

  if (btnQuickLock) {
    btnQuickLock.addEventListener('click', () => {
      if (quickActionsSheet) quickActionsSheet.classList.remove('active');
      btnLock.click();
    });
  }

  if (btnQuickSleep) {
    btnQuickSleep.addEventListener('click', () => {
      if (quickActionsSheet) quickActionsSheet.classList.remove('active');
      btnSleep.click();
    });
  }

  if (btnQuickRestart) {
    btnQuickRestart.addEventListener('click', () => {
      if (quickActionsSheet) quickActionsSheet.classList.remove('active');
      btnRestart.click();
    });
  }

  if (btnQuickShutdown) {
    btnQuickShutdown.addEventListener('click', () => {
      if (quickActionsSheet) quickActionsSheet.classList.remove('active');
      btnShutdown.click();
    });
  }

  window.__remoteDesktop = {
    start: startRemoteDesktopSession,
    stop: stopRemoteDesktopSession,
    handleIncomingRemoteSignal: handleIncomingRemoteSignal,
    getNormalizedVideoCoordinates: getNormalizedVideoCoordinates,
    GESTURE_CONFIG: GESTURE_CONFIG,
    sendControlMessage: sendControlMessage,
    sendInputMessage: sendInputMessage,
  };

  // Evaluate deterministic initial state on application launch
  window.__initPromise = evaluateInitialState();
  window.__verifyPin = verifyPin;
  window.__config = config;

})();
