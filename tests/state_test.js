// Automated Test Suite for PC Remote PWA Entry Point & State Machine
const fs = require('fs');
const assert = require('assert');

console.log('========================================================');
console.log('  Running PC Remote PWA State Machine & Security Suite');
console.log('========================================================\n');

let onWebSocketMessageSent = null;

// Mock DOM & Browser Environment
function createMockEnvironment(initialHash = '', initialStorage = {}) {
  const storage = { ...initialStorage };
  const wsCreated = [];
  const fetchCalls = [];

  class MockWebSocket {
    static get CONNECTING() { return 0; }
    static get OPEN() { return 1; }
    static get CLOSING() { return 2; }
    static get CLOSED() { return 3; }

    constructor(url) {
      this.url = url;
      this.readyState = 0; // CONNECTING
      this.sent = [];
      wsCreated.push(this);
      setTimeout(() => {
        this.readyState = 1; // OPEN
        if (this.onopen) this.onopen();
      }, 5);
    }
    send(data) {
      this.sent.push(data);
      if (onWebSocketMessageSent) {
        onWebSocketMessageSent(this, data);
      }
    }
    emitMessage(data) {
      if (this.onmessage) {
        this.onmessage({ data: typeof data === 'string' ? data : JSON.stringify(data) });
      }
    }
    close() {
      this.readyState = 3; // CLOSED
      if (this.onclose) this.onclose();
    }
  }

  const elements = {};
  function getElement(id) {
    if (!elements[id]) {
      elements[id] = {
        id: id,
        style: { display: 'none' },
        classList: {
          classes: new Set(),
          add(c) { this.classes.add(c); },
          remove(c) { this.classes.delete(c); },
          contains(c) { return this.classes.has(c); }
        },
        textContent: '',
        innerHTML: '',
        disabled: false,
        listeners: {},
        addEventListener(event, fn) {
          if (!this.listeners[event]) this.listeners[event] = [];
          this.listeners[event].push(fn);
        },
        click() {
          if (this.listeners['click']) {
            for (const fn of this.listeners['click']) fn();
          }
        }
      };
    }
    return elements[id];
  }

  // Prepopulate standard elements
  const elementIDs = [
    'landingStateView', 'landingIconWrap', 'landingTitle', 'landingDesc',
    'landingBadge', 'landingBadgeText', 'btnLandingAction', 'pinView',
    'pinTitle', 'pinSubtitle', 'pinError', 'mainDashboard', 'deviceName',
    'statusIndicator', 'statusLabel', 'statusSub', 'btnRefresh', 'networkName',
    'networkType', 'batteryFill', 'batteryPercentText', 'batteryLightning',
    'cpuValue', 'cpuMeter', 'ramValue', 'ramMeter', 'ramGb', 'uptimeValue',
    'btnLock', 'lockName', 'lockDesc', 'lockRight', 'btnSleep', 'btnRestart',
    'btnShutdown', 'confirmModal', 'modalTitle', 'modalDesc', 'btnModalCancel',
    'btnModalConfirm', 'toast', 'toastText', 'actionStateView', 'stateIconWrap',
    'stateTitle', 'stateDesc', 'stateBadgeDot', 'stateBadgeText', 'btnStateAction',
    'btnStateDismiss'
  ];
  for (let i = 0; i < 6; i++) elementIDs.push('dot' + i);
  elementIDs.forEach(id => getElement(id));

  let currentHash = initialHash;
  let currentPathname = '/';

  const mockWindow = {
    location: {
      hash: currentHash,
      pathname: currentPathname,
      protocol: 'https:',
      host: 'pc-remote-45t.pages.dev'
    },
    history: {
      replaceState(state, title, url) {
        mockWindow.location.pathname = url;
        mockWindow.location.hash = '';
      }
    },
    localStorage: {
      getItem(k) { return storage[k] !== undefined ? storage[k] : null; },
      setItem(k, v) { storage[k] = String(v); },
      removeItem(k) { delete storage[k]; },
      clear() { for (let k in storage) delete storage[k]; }
    },
    sessionStorage: {
      getItem(k) { return storage[k] !== undefined ? storage[k] : null; },
      setItem(k, v) { storage[k] = String(v); },
      removeItem(k) { delete storage[k]; },
      clear() { for (let k in storage) delete storage[k]; }
    },
    WebSocket: MockWebSocket,
    fetch: async (url, opts) => {
      fetchCalls.push({ url, opts });
      return { ok: true, json: async () => ({ status: 'ok' }) };
    },
    document: {
      getElementById: (id) => getElement(id),
      querySelectorAll: () => []
    },
    navigator: {
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)',
      serviceWorker: { register: async () => {} }
    },
    crypto: globalThis.crypto,
    addEventListener: () => {},
    removeEventListener: () => {},
    setTimeout: setTimeout,
    clearTimeout: clearTimeout,
    setInterval: () => 123,
    clearInterval: () => {}
  };

  return { mockWindow, elements, wsCreated, fetchCalls, storage };
}

// Load bundle and app script
const nostrBundleCode = fs.readFileSync('web/nostr.bundle.js', 'utf8')
  .replace('var NostrTools=', 'globalThis.NostrTools=');
const appJsCode = fs.readFileSync('web/app.js', 'utf8');

async function runInMock(initialHash = '', initialStorage = {}) {
  const env = createMockEnvironment(initialHash, initialStorage);
  const sandbox = {
    window: env.mockWindow,
    document: env.mockWindow.document,
    localStorage: env.mockWindow.localStorage,
    sessionStorage: env.mockWindow.sessionStorage,
    navigator: env.mockWindow.navigator,
    crypto: globalThis.crypto,
    WebSocket: env.mockWindow.WebSocket,
    fetch: (...args) => env.mockWindow.fetch(...args),
    setTimeout: setTimeout,
    clearTimeout: clearTimeout,
    setInterval: () => 123,
    clearInterval: () => {},
    console: console,
    ArrayBuffer: ArrayBuffer,
    Uint8Array: Uint8Array,
    DataView: DataView,
    TextEncoder: TextEncoder,
    TextDecoder: TextDecoder,
    URLSearchParams: URLSearchParams
  };

  // Evaluate nostr tools
  const nostrFn = new Function(...Object.keys(sandbox), nostrBundleCode);
  nostrFn(...Object.values(sandbox));
  sandbox.NostrTools = globalThis.NostrTools;

  // Evaluate app
  const appFn = new Function(...Object.keys(sandbox), appJsCode);
  appFn(...Object.values(sandbox));

  // Await initialization promise
  if (sandbox.window.__initPromise) {
    await sandbox.window.__initPromise;
  }

  return env;
}

async function main() {
  // --- TEST A: Direct Public Visit (No pairing data) ---
  console.log('Testing Case A: Direct Public Visit (No pairing data)...');
  {
    const env = await runInMock('', {});
    assert.strictEqual(env.elements.landingStateView.style.display, 'flex', 'Landing view must be displayed');
    assert.strictEqual(env.elements.landingTitle.textContent, 'No Device Paired', 'Title must be No Device Paired');
    assert.strictEqual(env.elements.pinView.style.display, 'none', 'PIN view must be hidden');
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard must be hidden');
    assert.strictEqual(env.wsCreated.length, 0, 'ZERO WebSockets must be created on public visit');
    assert.strictEqual(env.fetchCalls.length, 0, 'ZERO HTTP fetch calls must be made on public visit');
    console.log('  -> PASS: Landing view shown, 0 WebSockets, 0 HTTP calls, completely idle.\n');
  }

  // --- TEST B: Random Fake pair + key in URL ---
  console.log('Testing Case B: Malformed / Fake pairing URL fragment...');
  {
    const fakeHash = '#pair=badtoken123&key=badkey456';
    const env = await runInMock(fakeHash, {});
    assert.strictEqual(env.elements.landingStateView.style.display, 'flex', 'Landing view must be displayed');
    assert.strictEqual(env.elements.landingTitle.textContent, 'Invalid Pairing Link', 'Title must be Invalid Pairing Link');
    assert.strictEqual(env.elements.pinView.style.display, 'none', 'PIN view must be hidden');
    assert.strictEqual(env.wsCreated.length, 0, 'ZERO WebSockets must be created for invalid link');
    assert.strictEqual(env.storage.pcremote_vault_auth, undefined, 'Vault must NOT be polluted with invalid config');
    console.log('  -> PASS: Invalid link detected, no network calls, storage unpolluted.\n');
  }

  // --- TEST C: Valid Cryptographic Format in QR Fragment ---
  console.log('Testing Case C: Valid Cryptographic QR Fragment...');
  {
    const validKey = '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47';
    const validPair = '11223344556677889900aabbccddeeff'; // 32 hex chars
    const validHash = `#pair=${validPair}&key=${validKey}&name=Office-PC`;
    const env = await runInMock(validHash, {});

    assert.strictEqual(env.elements.landingStateView.style.display, 'none', 'Landing view must be hidden for valid pairing');
    assert.strictEqual(env.elements.pinView.style.display, 'flex', 'PIN view must be shown for pairing');
    assert.strictEqual(env.elements.pinTitle.textContent, 'Pair with Office-PC', 'Title must prompt pairing for device');
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard must NOT be shown before pairing');
    assert(env.wsCreated.length > 0, 'WebSockets must connect to prepare for pairing handshake');

    // Storage must remain untouched before pairing completes
    assert.strictEqual(env.storage.pcremote_vault_auth, undefined, 'Device must NOT write to vault before handshake completes');
    console.log('  -> PASS: Pairing keypad shown with device context, storage remains untouched.\n');
  }

  // --- TEST D: Authenticated Session from Storage (PIN unlock view shown) ---
  console.log('Testing Case D: Existing Authenticated Session in Storage...');
  {
    const validKey = '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47';
    const initialStorage = {
      pcremote_vault_auth: JSON.stringify({
        laptopPubkey: validKey,
        phonePubkey: '2222222222222222222222222222222222222222222222222222222222222222',
        saltHex: '00112233445566778899aabbccddeeff',
        ivHex: '00112233445566778899aabb',
        ciphertextHex: 'deadbeefcafe',
        deviceName: 'Home-Laptop',
        isPaired: true
      })
    };
    const env = await runInMock('', initialStorage);
    assert.strictEqual(env.elements.landingStateView.style.display, 'none', 'Landing view must be hidden');
    assert.strictEqual(env.elements.pinView.style.display, 'flex', 'PIN unlock view must be shown for session unlock');
    assert.strictEqual(env.elements.pinTitle.textContent, 'Enter your 6-digit PIN', 'Title must be unlock PIN');
    assert(env.wsCreated.length > 0, 'WebSockets must connect for authenticated session');
    console.log('  -> PASS: Authenticated session recognized, PIN unlock view shown.\n');
  }

  // --- TEST E: Clear Storage and Revisit ---
  console.log('Testing Case E: Clear Storage and Revisit Public URL...');
  {
    const env = await runInMock('', {});
    assert.strictEqual(env.elements.landingStateView.style.display, 'flex', 'Reverts cleanly to No Device Paired');
    assert.strictEqual(env.wsCreated.length, 0, 'Zero network calls');
    console.log('  -> PASS: Cleanly returns to unauthenticated idle state.\n');
  }

  // --- TEST F: PWA Cache Shell Bypassing Check ---
  console.log('Testing Case F: Raw Cached Shell without Storage...');
  {
    const env = await runInMock('', {});
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Cached shell MUST NOT show dashboard');
    assert.strictEqual(env.elements.landingStateView.style.display, 'flex', 'Cached shell MUST default to No Device Paired');
    console.log('  -> PASS: PWA cache cannot bypass authentication.\n');
  }

  // --- TEST G: Handshake with Expired Pairing Token ---
  console.log('Testing Case G: Handshake with Expired Pairing Token...');
  {
    const validKey = '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47';
    const validPair = '11223344556677889900aabbccddeeff';
    const validHash = `#pair=${validPair}&key=${validKey}&name=Office-PC&lan=192.168.1.50:8765`;
    const env = await runInMock(validHash, {});

    env.mockWindow.fetch = async () => ({
      ok: true,
      json: async () => ({ status: 'unauthorized', error: 'Invalid or expired pairing token. Scan QR code again.' })
    });

    await env.mockWindow.__verifyPin('123456');

    assert.strictEqual(env.elements.landingStateView.style.display, 'flex', 'Expired landing state must be shown');
    assert.strictEqual(env.elements.landingTitle.textContent, 'Pairing Link Expired', 'Title must state Pairing Link Expired');
    assert.strictEqual(env.elements.pinView.style.display, 'none', 'PIN view must be hidden');
    console.log('  -> PASS: Expired token handshake correctly transitions to expired state.\n');
  }

  // --- TEST H: Cryptographic Local PIN Verification (Wrong PIN Rejected) ---
  console.log('Testing Case H: Cryptographic Local PIN Verification (Wrong PIN Rejected)...');
  {
    // Generate valid encrypted vault with Master PIN "123456"
    const validKey = '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47';
    const phonePrivkey = '1111111111111111111111111111111111111111111111111111111111111111';

    // Helper to encrypt with Web Crypto
    const enc = new TextEncoder();
    const saltBytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
    const ivBytes = globalThis.crypto.getRandomValues(new Uint8Array(12));
    const pinKey = await globalThis.crypto.subtle.importKey('raw', enc.encode('123456'), { name: 'PBKDF2' }, false, ['deriveKey']);
    const aesKey = await globalThis.crypto.subtle.deriveKey(
      { name: 'PBKDF2', salt: saltBytes, iterations: 100000, hash: 'SHA-256' },
      pinKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']
    );
    const ctBuffer = await globalThis.crypto.subtle.encrypt({ name: 'AES-GCM', iv: ivBytes }, aesKey, enc.encode(phonePrivkey));

    const initialStorage = {
      pcremote_vault_auth: JSON.stringify({
        laptopPubkey: validKey,
        phonePubkey: '2222222222222222222222222222222222222222222222222222222222222222',
        saltHex: Array.from(saltBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
        ivHex: Array.from(ivBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
        ciphertextHex: Array.from(new Uint8Array(ctBuffer)).map(b => b.toString(16).padStart(2, '0')).join(''),
        deviceName: 'Home-Laptop',
        isPaired: true
      })
    };

    const env = await runInMock('', initialStorage);

    // Attempt unlock with INCORRECT PIN "999999"
    await env.mockWindow.__verifyPin('999999');

    // Must NOT unlock! Dashboard must stay hidden, error displayed
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard MUST stay hidden on wrong PIN');
    assert.strictEqual(env.elements.pinError.textContent, 'Incorrect PIN', 'Must display Incorrect PIN error');
    assert.strictEqual(env.mockWindow.__config.phonePrivkey, '', 'phonePrivkey MUST remain empty in memory');
    console.log('  -> PASS: Incorrect PIN rejected cryptographically by AES-GCM MAC check.\n');

    // Attempt unlock with CORRECT PIN "123456"
    await env.mockWindow.__verifyPin('123456');

    // Must unlock successfully!
    assert.strictEqual(env.elements.mainDashboard.style.display, 'flex', 'Dashboard must be visible on correct PIN');
    assert.strictEqual(env.mockWindow.__config.phonePrivkey, phonePrivkey, 'phonePrivkey must be decrypted into memory');
    console.log('  -> PASS: Correct PIN unlocks session and decrypts phonePrivkey.\n');
  }

  // --- TEST I: Tampered isPaired Cannot Bypass Authentication ---
  console.log('Testing Case I: Tampered isPaired in Storage Cannot Bypass Authentication...');
  {
    // Attacker manually sets isPaired: true with fake/garbage data in storage
    const tamperedStorage = {
      pcremote_vault_auth: JSON.stringify({
        laptopPubkey: '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47',
        isPaired: true,
        ciphertextHex: '0000000000000000000000000000'
      })
    };
    const env = await runInMock('', tamperedStorage);

    // Dashboard must NOT be visible!
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard must NOT open for tampered session');
    assert.strictEqual(env.elements.pinView.style.display, 'flex', 'PIN view must still block access');

    // Any PIN entered fails decryption
    await env.mockWindow.__verifyPin('123456');
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard still blocked');
    console.log('  -> PASS: Tampering with isPaired=true cannot bypass cryptographic unlock.\n');
  }

  // --- TEST J: Action State Persistence Across Page Refresh ---
  console.log('Testing Case J: Action State Persistence Across Page Refresh...');
  {
    const validKey = '81f424aec50fd8eee2e5bae94a719a44ec2ea11eff350d3ed8f955e23732de47';
    const phonePrivkey = '1111111111111111111111111111111111111111111111111111111111111111';

    const enc = new TextEncoder();
    const saltBytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
    const ivBytes = globalThis.crypto.getRandomValues(new Uint8Array(12));
    const pinKey = await globalThis.crypto.subtle.importKey('raw', enc.encode('123456'), { name: 'PBKDF2' }, false, ['deriveKey']);
    const aesKey = await globalThis.crypto.subtle.deriveKey(
      { name: 'PBKDF2', salt: saltBytes, iterations: 100000, hash: 'SHA-256' },
      pinKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']
    );
    const ctBuffer = await globalThis.crypto.subtle.encrypt({ name: 'AES-GCM', iv: ivBytes }, aesKey, enc.encode(phonePrivkey));

    const initialStorage = {
      pcremote_vault_auth: JSON.stringify({
        laptopPubkey: validKey,
        phonePubkey: '2222222222222222222222222222222222222222222222222222222222222222',
        saltHex: Array.from(saltBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
        ivHex: Array.from(ivBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
        ciphertextHex: Array.from(new Uint8Array(ctBuffer)).map(b => b.toString(16).padStart(2, '0')).join(''),
        deviceName: 'Home-PC',
        isPaired: true
      }),
      pcremote_action_state: JSON.stringify({
        action: 'shutdown',
        timestamp: Date.now()
      })
    };

    const env = await runInMock('', initialStorage);
    assert.strictEqual(env.elements.pinView.style.display, 'flex', 'PIN view must be shown on fresh load');

    // Unlock session with correct PIN
    await env.mockWindow.__verifyPin('123456');

    // After shutdown, actionStateView MUST be shown instead of mainDashboard!
    assert.strictEqual(env.elements.actionStateView.style.display, 'flex', 'Action state view MUST be shown after refresh');
    assert.strictEqual(env.elements.mainDashboard.style.display, 'none', 'Dashboard MUST remain hidden while PC is in shutdown state');
    assert.strictEqual(env.elements.stateTitle.textContent, 'PC is Powered Off', 'Must indicate PC is Powered Off');
    console.log('  -> PASS: Action state persisted across refresh; action view shown, dashboard safely hidden.\n');
  }

  // Case K: Initial UI Flash Invariant (pinView must be hidden before evaluation)
  {
    console.log('Testing Case K: Initial UI Flash Invariant (pinView hidden by default)...');
    const htmlContent = fs.readFileSync('web/index.html', 'utf8');
    const cssContent = fs.readFileSync('web/style.css', 'utf8');

    // 1. Assert HTML template has pinView hidden by default
    assert.match(
      htmlContent,
      /<div\s+id="pinView"[^>]*style="[^"]*display:\s*none;?[^"]*"/i,
      'web/index.html must have pinView hidden by default (style="display: none;")'
    );

    // 2. Assert HTML template has landingStateView hidden by default
    assert.match(
      htmlContent,
      /<div\s+id="landingStateView"[^>]*style="[^"]*display:\s*none;?[^"]*"/i,
      'web/index.html must have landingStateView hidden by default'
    );

    // 3. Assert HTML template has mainDashboard hidden by default
    assert.match(
      htmlContent,
      /<div\s+id="mainDashboard"[^>]*style="[^"]*display:\s*none;?[^"]*"/i,
      'web/index.html must have mainDashboard hidden by default'
    );

    // 4. Assert CSS baseline has .pin-view hidden by default
    assert.match(
      cssContent,
      /\.pin-view\s*\{[^}]*display:\s*none;/s,
      'web/style.css must have .pin-view { display: none; } baseline'
    );

    console.log('  -> PASS: Initial UI flash invariant verified; all top-level views start hidden.\n');
  }

  // --- TEST L: Frontend Terminology & Hygiene Verification ---
  console.log('Testing Case L: Frontend Terminology & Hygiene Verification...');
  {
    const html = fs.readFileSync('web/index.html', 'utf8');
    const appJs = fs.readFileSync('web/app.js', 'utf8');
    const manifest = fs.readFileSync('web/manifest.json', 'utf8');

    // 1. Zero occurrences of 'LAN DIRECT'
    assert.strictEqual(html.includes('LAN DIRECT'), false, 'web/index.html must not contain LAN DIRECT');
    assert.strictEqual(appJs.includes('LAN DIRECT'), false, 'web/app.js must not contain LAN DIRECT');
    assert.strictEqual(manifest.includes('LAN DIRECT'), false, 'web/manifest.json must not contain LAN DIRECT');

    // 2. Zero instances of config.lanHost ? in app.js for transport inference
    assert.strictEqual(/config\.lanHost\s*\?/.test(appJs), false, 'web/app.js must not infer transport from config.lanHost');

    // 3. User-facing product copy: zero "Windows" in index.html and manifest.json
    assert.strictEqual(/Windows/i.test(html), false, 'web/index.html must not contain user-facing "Windows"');
    assert.strictEqual(/Windows/i.test(manifest), false, 'web/manifest.json must not contain user-facing "Windows"');

    // 4. In app.js: only user-agent check is allowed to mention Windows
    const appJsLines = appJs.split('\n');
    const userFacingWindows = appJsLines.filter((line) => {
      if (!line.includes('Windows')) return false;
      // Allow userAgent detection: if (/Windows/i.test(ua)) return 'Windows Remote';
      if (/navigator\.userAgent|\/Windows\/i\.test/i.test(line)) return false;
      return true;
    });
    assert.strictEqual(
      userFacingWindows.length,
      0,
      'web/app.js must have zero user-facing "Windows" copy. Found: ' + JSON.stringify(userFacingWindows)
    );

    console.log('  -> PASS: All user-facing Windows references cleaned, LAN DIRECT removed, no lanHost status guessing.\n');
  }

  // --- TEST M: Transport Status Label Transitions (LOCAL vs CONNECTED) ---
  console.log('Testing Case M: Transport Status Label Transitions (LOCAL vs CONNECTED)...');
  {
    async function createTestVault(phonePrivkey, laptopPubkey, lanHost = '192.168.1.50:8765') {
      const enc = new TextEncoder();
      const saltBytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
      const ivBytes = globalThis.crypto.getRandomValues(new Uint8Array(12));
      const pinKey = await globalThis.crypto.subtle.importKey('raw', enc.encode('123456'), { name: 'PBKDF2' }, false, ['deriveKey']);
      const aesKey = await globalThis.crypto.subtle.deriveKey(
        { name: 'PBKDF2', salt: saltBytes, iterations: 100000, hash: 'SHA-256' },
        pinKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']
      );
      const ctBuffer = await globalThis.crypto.subtle.encrypt({ name: 'AES-GCM', iv: ivBytes }, aesKey, enc.encode(phonePrivkey));
      const phonePubkey = NostrTools.getPublicKey(NostrTools.utils.hexToBytes(phonePrivkey));

      return {
        pcremote_vault_auth: JSON.stringify({
          laptopPubkey: laptopPubkey,
          phonePubkey: phonePubkey,
          saltHex: Array.from(saltBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
          ivHex: Array.from(ivBytes).map(b => b.toString(16).padStart(2, '0')).join(''),
          ciphertextHex: Array.from(new Uint8Array(ctBuffer)).map(b => b.toString(16).padStart(2, '0')).join(''),
          deviceName: 'Test-PC',
          lanHost: lanHost,
          isPaired: true
        })
      };
    }

    const laptopSk = NostrTools.generateSecretKey();
    const laptopPk = NostrTools.getPublicKey(laptopSk);
    const phoneSk = NostrTools.generateSecretKey();
    const phonePrivHex = Array.from(phoneSk).map(b => b.toString(16).padStart(2, '0')).join('');

    // 1. Direct LAN Success -> LOCAL
    {
      const vaultLan = await createTestVault(phonePrivHex, laptopPk, '192.168.1.50:8765');
      const envLan = await runInMock('', vaultLan);

      envLan.mockWindow.fetch = async (url, opts) => {
        assert(url.includes('192.168.1.50:8765/api/control'), 'Fetch must target LAN endpoint');
        const reqEvt = JSON.parse(opts.body);
        const convKey = NostrTools.nip44.v2.utils.getConversationKey(laptopSk, reqEvt.pubkey);
        const reqPlain = NostrTools.nip44.v2.decrypt(reqEvt.content, convKey);
        const reqData = JSON.parse(reqPlain);

        const respPayload = JSON.stringify({
          id: reqData.id,
          status: 'ok',
          telemetry: { cpu: 15, ram: 40, uptime: '3h', isLocked: false }
        });
        const respCipher = NostrTools.nip44.v2.encrypt(respPayload, convKey);
        const respEvt = NostrTools.finalizeEvent({
          kind: 4,
          created_at: Math.floor(Date.now() / 1000),
          tags: [['p', reqEvt.pubkey]],
          content: respCipher
        }, laptopSk);

        return {
          ok: true,
          json: async () => respEvt
        };
      };

      await envLan.mockWindow.__verifyPin('123456');
      await new Promise(r => setTimeout(r, 50));
      assert.strictEqual(envLan.elements.statusLabel.textContent, 'LOCAL', 'Direct LAN success MUST set status to LOCAL');
      assert.strictEqual(envLan.elements.statusIndicator.className, 'status-indicator', 'Status indicator must be online');
      console.log('  -> Subcase 1 PASS: Direct LAN success renders ● LOCAL.');
    }

    // 2. Relay Fallback Success (LAN host exists, LAN fails, Relay succeeds) -> CONNECTED
    {
      const vaultRelay = await createTestVault(phonePrivHex, laptopPk, '192.168.1.50:8765');
      const envRelay = await runInMock('', vaultRelay);
      // Wait for MockWebSockets to transition to OPEN
      await new Promise(r => setTimeout(r, 20));

      // LAN fetch fails (offline LAN / mobile data)
      envRelay.mockWindow.fetch = async () => {
        throw new Error('Failed to connect to LAN endpoint (Offline/Different Network)');
      };

      // Nostr relay WebSocket responds to signed event
      onWebSocketMessageSent = (sock, rawData) => {
        try {
          const msg = JSON.parse(rawData);
          if (Array.isArray(msg) && msg[0] === 'EVENT') {
            const reqEvt = msg[1];
            if (reqEvt && reqEvt.kind === 4) {
              const convKey = NostrTools.nip44.v2.utils.getConversationKey(laptopSk, reqEvt.pubkey);
              const reqPlain = NostrTools.nip44.v2.decrypt(reqEvt.content, convKey);
              const reqData = JSON.parse(reqPlain);

              const respPayload = JSON.stringify({
                id: reqData.id,
                status: 'ok',
                telemetry: { cpu: 22, ram: 55, uptime: '5h', isLocked: false }
              });
              const respCipher = NostrTools.nip44.v2.encrypt(respPayload, convKey);
              const respEvt = NostrTools.finalizeEvent({
                kind: 4,
                created_at: Math.floor(Date.now() / 1000),
                tags: [['p', reqEvt.pubkey]],
                content: respCipher
              }, laptopSk);

              // Deliver via WebSocket subscription asynchronously (simulating relay latency)
              setTimeout(() => {
                sock.emitMessage(['EVENT', 'sub_cmd', respEvt]);
              }, 5);
            }
          }
        } catch (e) {}
      };

      await envRelay.mockWindow.__verifyPin('123456');
      await new Promise(r => setTimeout(r, 80));
      assert.strictEqual(envRelay.elements.statusLabel.textContent, 'CONNECTED', 'Relay fallback MUST set status to CONNECTED');
      assert.notStrictEqual(envRelay.elements.statusLabel.textContent, 'LOCAL', 'MUST NOT show LOCAL when LAN failed, even if lanHost exists');
      assert.strictEqual(envRelay.elements.statusIndicator.className, 'status-indicator', 'Status indicator must be online');
      console.log('  -> Subcase 2 PASS: Stored lanHost with failing LAN correctly falls back to ● CONNECTED.');

      // 3. Telemetry refresh must retain CONNECTED transport label
      envRelay.elements.btnRefresh.click();
      await new Promise(r => setTimeout(r, 80));
      assert.strictEqual(envRelay.elements.statusLabel.textContent, 'CONNECTED', 'Subsequent refresh must keep CONNECTED transport label');
      console.log('  -> Subcase 3 PASS: Refresh retains CONNECTED transport label without overwriting.');

      onWebSocketMessageSent = null;
    }

    console.log('  -> PASS: Transport status accurately represents actual transport path in all conditions.\n');
  }

  console.log('========================================================');
  console.log('  ALL PWA STATE MACHINE & SECURITY TESTS PASSED (13/13)');
  console.log('========================================================\n');
}

main().catch(err => {
  console.error('\nTest Suite Failed:', err);
  process.exit(1);
});
