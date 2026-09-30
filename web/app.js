// PC Remote - Professional Client Engine
(() => {
  'use strict';

  // State
  let config = {
    laptopPubkey: '',
    pairingToken: '',
    lanHost: '',
    pin: '',
    phonePubkey: '',
    phonePrivkey: ''
  };

  let isLocked = true;
  let enteredPin = '';
  let isConnected = false;
  let pendingAction = null;
  let pollInterval = null;

  // DOM Elements
  const pinView = document.getElementById('pinView');
  const mainDashboard = document.getElementById('mainDashboard');
  const pinError = document.getElementById('pinError');
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

  // Register Service Worker for offline PWA capability
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('sw.js').catch(() => {});
    });
  }

  // Load or initialize stored credentials
  function loadStoredConfig() {
    const raw = localStorage.getItem('pcremote_cfg') || localStorage.getItem('laptopcontrol_cfg');
    if (raw) {
      try {
        const parsed = JSON.parse(raw);
        config = { ...config, ...parsed };
      } catch (e) {}
    }

    // Parse URL Fragment if arriving from QR code
    const hash = window.location.hash.substring(1);
    if (hash) {
      const params = new URLSearchParams(hash);
      if (params.get('key')) config.laptopPubkey = params.get('key');
      if (params.get('pair')) config.pairingToken = params.get('pair');
      if (params.get('lan')) config.lanHost = params.get('lan');
      if (params.get('name')) config.deviceName = params.get('name');

      saveConfig();
      // Clean fragment from address bar so it's not saved in history
      window.history.replaceState(null, '', window.location.pathname);
    }

    if (config.deviceName) {
      deviceName.textContent = config.deviceName;
    }
  }

  function saveConfig() {
    localStorage.setItem('pcremote_cfg', JSON.stringify({
      laptopPubkey: config.laptopPubkey,
      pairingToken: config.pairingToken,
      lanHost: config.lanHost,
      deviceName: config.deviceName,
      phonePubkey: config.phonePubkey,
      phonePrivkey: config.phonePrivkey
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
    sendRequest('telemetry')
      .then((res) => {
        if (res && res.error) {
          pinError.textContent = res.error;
          enteredPin = '';
          updatePinDots();
        } else {
          isLocked = false;
          pinView.style.display = 'none';
          mainDashboard.style.display = 'flex';
          updateTelemetryUI(res);
          startTelemetryLoop();
        }
      })
      .catch((err) => {
        pinError.textContent = err.message || 'Connection failed';
        enteredPin = '';
        updatePinDots();
      });
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

  // Communication Engine (Dual Path: Direct LAN -> Decentralized Relay Fallback)
  async function sendRequest(action, extraPayload = {}) {
    const payload = {
      action: action,
      pin: config.pin,
      token: config.pairingToken,
      timestamp: Math.floor(Date.now() / 1000),
      ...extraPayload
    };

    // 1. Try local LAN direct HTTP
    if (config.lanHost || window.location.origin.includes('http')) {
      const endpoint = config.lanHost ? `http://${config.lanHost}/api/control` : '/api/control';
      try {
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 1800);
        const res = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
          signal: controller.signal
        });
        clearTimeout(timeoutId);
        if (res.ok || res.status === 401 || res.status === 403 || res.status === 400) {
          setStatus('online', 'ONLINE');
          return await res.json();
        }
      } catch (e) {}
    }

    // 2. Fallback to Decentralized Relay over WebSocket
    return sendRelayRequest(payload);
  }

  // Decentralized Relay Pool
  const RELAYS = [
    'wss://relay.damus.io',
    'wss://nos.lol',
    'wss://relay.primal.net'
  ];

  let relaySockets = [];

  function initRelays() {
    RELAYS.forEach((url) => {
      try {
        const ws = new WebSocket(url);
        ws.onopen = () => {
          setStatus('online', 'ONLINE');
          relaySockets.push(ws);
        };
        ws.onclose = () => {
          relaySockets = relaySockets.filter(s => s !== ws);
          if (relaySockets.length === 0) setStatus('offline', 'OFFLINE');
        };
      } catch (e) {}
    });
  }

  function sendRelayRequest(payload) {
    return new Promise((resolve, reject) => {
      if (relaySockets.length === 0) {
        return reject(new Error('No active relay connection'));
      }

      const reqId = 'req_' + Math.random().toString(36).substring(2, 10);
      payload.id = reqId;

      const ws = relaySockets[0];
      const messageHandler = (event) => {
        try {
          const data = JSON.parse(event.data);
          if (data && data.id === reqId) {
            ws.removeEventListener('message', messageHandler);
            resolve(data);
          }
        } catch (e) {}
      };

      ws.addEventListener('message', messageHandler);
      setTimeout(() => {
        ws.removeEventListener('message', messageHandler);
        reject(new Error('Request timed out'));
      }, 5000);

      ws.send(JSON.stringify(payload));
    });
  }

  // Update UI Telemetry
  function updateTelemetryUI(data) {
    if (!data) return;

    if (data.deviceName) deviceName.textContent = data.deviceName;

    // Network
    if (data.networkName) networkName.textContent = data.networkName;
    if (data.networkType) networkType.textContent = data.networkType;

    // Battery Box UI
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

    // CPU
    if (data.cpu !== undefined) {
      cpuValue.textContent = `${data.cpu}%`;
      cpuMeter.style.width = `${Math.min(100, Math.max(0, data.cpu))}%`;
      cpuMeter.className = 'meter-fill ' + (data.cpu >= 80 ? 'red' : (data.cpu >= 50 ? 'amber' : ''));
    }

    // RAM
    if (data.ramPercent !== undefined) {
      ramValue.textContent = `${data.ramPercent}%`;
      ramMeter.style.width = `${Math.min(100, Math.max(0, data.ramPercent))}%`;
      ramGb.textContent = `${data.ramUsedGb || 0} / ${data.ramTotalGb || 0} GB`;
    }

    // Uptime
    if (data.uptime) {
      uptimeValue.textContent = data.uptime;
    }

    // Lock State Management
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
      if (!isLocked) {
        sendRequest('telemetry').then(updateTelemetryUI).catch(() => {});
      }
    }, 3000);
  }

  // Refresh Button
  btnRefresh.addEventListener('click', () => {
    sendRequest('telemetry')
      .then((data) => {
        updateTelemetryUI(data);
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
    actionStateView.style.display = 'flex';

    if (action === 'sleep') {
      stateIconWrap.className = 'state-icon-wrap is-sleeping';
      stateIconWrap.innerHTML = '<svg viewBox="0 0 24 24"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>';
      stateTitle.textContent = 'Laptop is in Sleep Mode';
      stateDesc.textContent = 'Windows entered low-power standby mode. The remote connection has closed. Press the physical power button on the laptop to wake it up.';
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
      stateTitle.textContent = 'Laptop is Powered Off';
      stateDesc.textContent = 'Windows has powered down completely. Remote control is unavailable until the laptop is turned on manually.';
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
        if (data && !data.error) {
          clearInterval(reconnectTimer);
          reconnectTimer = null;
          actionStateView.style.display = 'none';
          mainDashboard.style.display = 'flex';
          updateTelemetryUI(data);
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
      if (data && !data.error) {
        actionStateView.style.display = 'none';
        mainDashboard.style.display = 'flex';
        updateTelemetryUI(data);
        startTelemetryLoop();
        showToast('Reconnected');
      } else {
        showToast('Laptop is still offline');
      }
    }).catch(() => {
      showToast('Laptop is still offline');
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

  // Initialization
  loadStoredConfig();
  initRelays();

  if (isLocked) {
    mainDashboard.style.display = 'none';
    pinView.style.display = 'flex';
  }

})();
