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
    phonePrivkey: '',
    isPaired: false
  };

  let isLocked = true;
  let enteredPin = '';
  let isConnected = false;
  let pendingAction = null;
  let pollInterval = null;

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
  let reconnectTimer = null;

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

  // Cryptographic & Format Validators
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

  // Load stored credentials from localStorage
  function loadStoredConfig() {
    const raw = localStorage.getItem('pcremote_cfg') || localStorage.getItem('laptopcontrol_cfg');
    if (raw) {
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object') {
          config = { ...config, ...parsed };
        }
      } catch (e) {}
    }

    if (config.deviceName) {
      deviceName.textContent = config.deviceName;
    }
  }

  function ensurePhoneKeys() {
    if (!config.phonePrivkey || !config.phonePubkey) {
      if (typeof NostrTools !== 'undefined' && NostrTools.generateSecretKey) {
        const sk = NostrTools.generateSecretKey();
        config.phonePrivkey = NostrTools.utils.bytesToHex(sk);
        config.phonePubkey = NostrTools.getPublicKey(sk);
        if (config.isPaired) {
          saveConfig();
        }
      }
    }
  }

  function saveConfig() {
    localStorage.setItem('pcremote_cfg', JSON.stringify({
      laptopPubkey: config.laptopPubkey,
      pairingToken: config.pairingToken,
      lanHost: config.lanHost,
      deviceName: config.deviceName,
      phonePubkey: config.phonePubkey,
      phonePrivkey: config.phonePrivkey,
      isPaired: !!config.isPaired
    }));
  }

  function showToast(msg) {
    toastText.textContent = msg;
    toast.classList.add('active');
    setTimeout(() => toast.classList.remove('active'), 3200);
  }

  function setStatus(state, text) {
    if (state === 'online') {
      statusIndicator.className = 'status-indicator';
      statusLabel.textContent = text || 'ONLINE';
      isConnected = true;
    } else {
      statusIndicator.className = 'status-indicator offline';
      statusLabel.textContent = text || 'OFFLINE';
      isConnected = false;
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
      landingDesc.textContent = 'Open this page from the pairing QR code displayed by PC Remote on your Windows PC.';
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
      landingDesc.textContent = 'The pairing link is malformed or invalid. Generate a new pairing QR code from PC Remote on your Windows PC.';
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
      landingDesc.textContent = 'This pairing session has expired for your security. Generate a new pairing QR code from PC Remote on your Windows PC.';
      landingBadge.style.display = 'none';
      btnLandingAction.style.display = 'none';
      setStatus('offline', 'EXPIRED');
    }
  }

  // State Machine Evaluator
  function evaluateInitialState() {
    loadStoredConfig();

    // 1. Inspect URL Fragment
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
      // Validate cryptographic structure strictly before trusting
      if (!isValidPubkey(urlKey) || !isValidPairingToken(urlPair)) {
        showLandingState('invalid');
        return;
      }

      // Valid parameters: set staging config in-memory (NOT marked as paired, not saved yet)
      config.laptopPubkey = urlKey;
      config.pairingToken = urlPair;
      if (urlLan && isValidLanHost(urlLan)) config.lanHost = urlLan;
      if (urlName) config.deviceName = urlName;
      config.isPaired = false;

      if (config.deviceName) {
        deviceName.textContent = config.deviceName;
      }

      // Display pairing PIN view
      landingStateView.style.display = 'none';
      mainDashboard.style.display = 'none';
      actionStateView.style.display = 'none';
      pinView.style.display = 'flex';
      pinTitle.textContent = config.deviceName ? `Pair with ${config.deviceName}` : 'Pair with PC';
      pinSubtitle.textContent = 'Enter your 6-digit Master PIN to pair this phone';

      // Connect to Nostr relay mesh for the pairing handshake
      initRelays();
      return;
    }

    // 3. Case: Authenticated Session (Previously paired device)
    if (config.isPaired && isValidPubkey(config.laptopPubkey)) {
      landingStateView.style.display = 'none';
      actionStateView.style.display = 'none';

      if (isLocked) {
        pinTitle.textContent = 'Enter your 6-digit PIN';
        pinSubtitle.textContent = config.deviceName ? `Authenticate to control ${config.deviceName}` : 'Authenticate to control this PC';
        mainDashboard.style.display = 'none';
        pinView.style.display = 'flex';
      } else {
        pinView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        startTelemetryLoop();
      }

      // Connect to relays
      initRelays();
      return;
    }

    // 4. Case: Direct Public Visit (No pairing context)
    showLandingState('public');
    // Remain 100% idle. NO WebSockets. NO HTTP calls. NO polling.
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
        setStatus('online', 'CONNECTED');
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
        if (!anyConnected) {
          setStatus('offline', 'DISCONNECTED');
        }
        // Only reconnect if we are in an active session or pairing mode
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

    let plaintext = '';
    if (typeof NostrTools !== 'undefined') {
      try {
        const skBytes = NostrTools.utils.hexToBytes(config.phonePrivkey);
        const convKey = NostrTools.nip44.v2.utils.getConversationKey(skBytes, evt.pubkey);
        plaintext = NostrTools.nip44.v2.decrypt(evt.content, convKey);
      } catch (e) {
        try {
          plaintext = NostrTools.nip04.decrypt(config.phonePrivkey, evt.pubkey, evt.content);
        } catch (err) {}
      }
    }

    if (!plaintext) return;

    try {
      const data = JSON.parse(plaintext);
      if (data && data.id && pendingRequests.has(data.id)) {
        const { resolve, timer } = pendingRequests.get(data.id);
        clearTimeout(timer);
        pendingRequests.delete(data.id);
        resolve(data);
      }
    } catch (e) {}
  }

  function sendRelayRequest(action, extraPayload = {}) {
    return new Promise((resolve, reject) => {
      ensurePhoneKeys();

      if (!config.phonePrivkey || !config.laptopPubkey) {
        return reject(new Error('Missing pairing keys. Scan the QR code on your PC.'));
      }

      if (typeof NostrTools === 'undefined') {
        return reject(new Error('Nostr cryptography library not loaded.'));
      }

      const reqId = 'req_' + Math.random().toString(36).substring(2, 10) + Date.now().toString(36);
      const payload = {
        id: reqId,
        action: action,
        timestamp: Math.floor(Date.now() / 1000),
        ...extraPayload
      };

      let ciphertext = '';
      const skBytes = NostrTools.utils.hexToBytes(config.phonePrivkey);
      try {
        const convKey = NostrTools.nip44.v2.utils.getConversationKey(skBytes, config.laptopPubkey);
        ciphertext = NostrTools.nip44.v2.encrypt(JSON.stringify(payload), convKey);
      } catch (e) {
        return reject(new Error('Encryption failed: ' + e.message));
      }

      const template = {
        kind: 4,
        created_at: Math.floor(Date.now() / 1000),
        tags: [['p', config.laptopPubkey]],
        content: ciphertext
      };

      let signedEvt;
      try {
        signedEvt = NostrTools.finalizeEvent(template, skBytes);
      } catch (e) {
        return reject(new Error('Failed to sign event: ' + e.message));
      }

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
          reject(new Error('Request timed out'));
        }
      }, 6500);

      pendingRequests.set(reqId, { resolve, reject, timer });
    });
  }

  // Communication Engine (Direct LAN Probe -> Nostr E2EE Fallback)
  async function sendRequest(action, extraPayload = {}) {
    const canAttemptLan = config.lanHost && (
      window.location.protocol === 'http:' ||
      window.location.host === config.lanHost
    );

    if (canAttemptLan) {
      const endpoint = `http://${config.lanHost}/api/control`;
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 1200);
        const res = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            action: action,
            pin: config.pin,
            token: config.pairingToken,
            phonePubKey: config.phonePubkey,
            timestamp: Math.floor(Date.now() / 1000),
            ...extraPayload
          }),
          signal: controller.signal
        });
        clearTimeout(timeoutId);
        if (res.ok || res.status === 401 || res.status === 403 || res.status === 400) {
          setStatus('online', 'LAN DIRECT');
          return await res.json();
        }
      } catch (e) {}
    }

    return sendRelayRequest(action, extraPayload);
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

  function verifyPin(pinCandidate) {
    config.pin = pinCandidate;

    // Pairing Handshake Flow (When device is not yet authorized)
    if (!config.isPaired) {
      showToast('Pairing with PC...');
      sendRequest('pair', {
        token: config.pairingToken,
        pin: pinCandidate,
        deviceName: getClientDeviceName()
      })
        .then((res) => {
          if (res && res.error) {
            // Check if token expired
            if (res.error.toLowerCase().includes('expired') || res.error.toLowerCase().includes('pairing token')) {
              showLandingState('expired');
              return;
            }
            pinError.textContent = res.error || 'Incorrect Master PIN';
            enteredPin = '';
            updatePinDots();
          } else {
            // Authenticated successfully!
            config.isPaired = true;
            config.pairingToken = '';
            saveConfig();
            isLocked = false;
            pinView.style.display = 'none';
            landingStateView.style.display = 'none';
            mainDashboard.style.display = 'flex';
            if (res.telemetry) updateTelemetryUI(res.telemetry);
            showToast('Paired successfully with ' + (res.deviceName || 'PC'));
            startTelemetryLoop();
          }
        })
        .catch((err) => {
          handlePinError(err);
        });
      return;
    }

    // Already Paired: Local unlock & telemetry update
    sendRequest('telemetry')
      .then((res) => {
        if (res && (res.error || res.status === 'unauthorized')) {
          if (res.error && res.error.includes('not authorized')) {
            config.isPaired = false;
            saveConfig();
            showLandingState('public');
            return;
          }
          pinError.textContent = res.error || 'Connection unauthorized';
          enteredPin = '';
          updatePinDots();
        } else {
          isLocked = false;
          pinView.style.display = 'none';
          landingStateView.style.display = 'none';
          mainDashboard.style.display = 'flex';
          updateTelemetryUI(res);
          startTelemetryLoop();
        }
      })
      .catch((err) => {
        handlePinError(err);
      });
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
      lockDesc.textContent = 'Workstation is currently locked';
      lockRight.innerHTML = '<span class="locked-badge">Locked</span>';
    } else {
      btnLock.disabled = false;
      btnLock.classList.remove('is-locked');
      lockName.textContent = 'Lock';
      lockDesc.textContent = 'Lock screen immediately';
      lockRight.innerHTML = '<svg class="control-arrow" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"/></svg>';
    }
  }

  function startTelemetryLoop() {
    if (pollInterval) clearInterval(pollInterval);
    pollInterval = setInterval(() => {
      if (!isLocked && config.isPaired) {
        sendRequest('telemetry').then((res) => {
          if (res && res.telemetry) updateTelemetryUI(res.telemetry);
          else if (res) updateTelemetryUI(res);
        }).catch(() => {});
      }
    }, 3000);
  }

  // Refresh Button
  btnRefresh.addEventListener('click', () => {
    sendRequest('telemetry')
      .then((data) => {
        if (data && data.telemetry) updateTelemetryUI(data.telemetry);
        else if (data) updateTelemetryUI(data);
        showToast('Telemetry updated');
      })
      .catch((e) => showToast('Failed: ' + e.message));
  });

  // Action Handlers
  btnLock.addEventListener('click', () => {
    btnLock.disabled = true;
    sendRequest('lock')
      .then((res) => {
        if (res && res.error) {
          showToast('Failed: ' + res.error);
          btnLock.disabled = false;
          return;
        }
        showToast('Workstation locked');
        btnLock.classList.add('is-locked');
        lockName.textContent = 'Locked';
        lockDesc.textContent = 'Workstation is currently locked';
        lockRight.innerHTML = '<span class="locked-badge">Locked</span>';
      })
      .catch((e) => {
        showToast('Failed: ' + e.message);
        btnLock.disabled = false;
      });
  });

  function openConfirm(action, title, desc, isDanger) {
    pendingAction = action;
    modalTitle.textContent = title;
    modalDesc.textContent = desc;
    btnModalConfirm.className = 'btn-modal ' + (isDanger ? 'confirm-danger' : 'confirm-primary');
    btnModalConfirm.textContent = isDanger ? 'Shut Down' : (action === 'restart' ? 'Restart' : 'Sleep');
    confirmModal.classList.add('active');
  }

  btnSleep.addEventListener('click', () => {
    openConfirm(
      'sleep',
      'Confirm Sleep',
      'Windows will enter low-power sleep mode. You will need to press the laptop power button or wake it up locally to resume.',
      false
    );
  });

  btnRestart.addEventListener('click', () => {
    openConfirm(
      'restart',
      'Confirm Restart',
      'Windows will close running applications and reboot in 5 seconds. The connection will automatically restore once Windows starts back up.',
      false
    );
  });

  btnShutdown.addEventListener('click', () => {
    openConfirm(
      'shutdown',
      'Confirm Shut Down',
      'Windows will shut down completely in 5 seconds. You will need physical access to turn the laptop on again.',
      true
    );
  });

  btnModalCancel.addEventListener('click', () => {
    confirmModal.classList.remove('active');
    pendingAction = null;
  });

  function showActionState(action) {
    if (pollInterval) clearInterval(pollInterval);

    mainDashboard.style.display = 'none';
    pinView.style.display = 'none';
    landingStateView.style.display = 'none';
    actionStateView.style.display = 'flex';

    if (action === 'sleep') {
      stateIconWrap.className = 'state-icon-wrap is-sleeping';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
      stateTitle.textContent = 'PC is in Sleep Mode';
      stateDesc.textContent = 'Windows entered low-power standby mode. The remote connection has closed. Press the physical power button on the PC to wake it up.';
      stateBadgeDot.className = 'state-badge-dot';
      stateBadgeText.textContent = 'STANDBY • DISCONNECTED';
      btnStateAction.textContent = 'Reconnect when awake';
      btnStateAction.disabled = false;
      btnStateAction.onclick = () => tryReconnect();
    } else if (action === 'restart') {
      stateIconWrap.className = 'state-icon-wrap is-restarting';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>';
      stateTitle.textContent = 'Restarting Windows...';
      stateDesc.textContent = 'Windows is rebooting. Connection will automatically restore once Windows starts back up.';
      stateBadgeDot.className = 'state-badge-dot pulse';
      stateBadgeText.textContent = 'REBOOTING';
      btnStateAction.textContent = 'Auto-reconnecting...';
      btnStateAction.disabled = true;

      startAutoReconnectLoop();
    } else if (action === 'shutdown') {
      stateIconWrap.className = 'state-icon-wrap is-off';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"/><line x1="12" y1="2" x2="12" y2="12"/></svg>';
      stateTitle.textContent = 'PC is Powered Off';
      stateDesc.textContent = 'Windows has powered down completely. Remote control is unavailable until the PC is turned on manually.';
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
          actionStateView.style.display = 'none';
          mainDashboard.style.display = 'flex';
          updateTelemetryUI(tel);
          startTelemetryLoop();
          showToast('Reconnected to Windows');
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
        actionStateView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        updateTelemetryUI(tel);
        startTelemetryLoop();
        showToast('Reconnected');
      } else {
        showToast('PC is still offline');
      }
    }).catch(() => {
      showToast('PC is still offline');
    }).finally(() => {
      btnStateAction.disabled = false;
      btnStateAction.textContent = 'Check connection again';
    });
  }

  btnModalConfirm.addEventListener('click', () => {
    if (pendingAction) {
      const act = pendingAction;
      confirmModal.classList.remove('active');
      sendRequest(act)
        .then((res) => {
          if (res && res.error) {
            showToast('Failed: ' + res.error);
            return;
          }
          showActionState(act);
        })
        .catch((e) => {
          showToast('Failed: ' + (e.message || 'Action failed'));
        });
      pendingAction = null;
    }
  });

  // Evaluate deterministic initial state on application launch
  evaluateInitialState();

})();
