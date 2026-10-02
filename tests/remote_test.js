// Automated Test Suite for PC Remote - Remote Desktop Client Logic
const fs = require('fs');
const assert = require('assert');

console.log('========================================================');
console.log('  Running Remote Desktop Client & Gesture Test Suite');
console.log('========================================================\n');

function createMockRemoteEnv() {
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
          contains(c) { return this.classes.has(c); },
          toggle(c) {
            if (this.classes.has(c)) { this.classes.delete(c); return false; }
            else { this.classes.add(c); return true; }
          }
        },
        textContent: '',
        innerHTML: '',
        disabled: false,
        listeners: {},
        addEventListener(event, fn) {
          if (!this.listeners[event]) this.listeners[event] = [];
          this.listeners[event].push(fn);
        },
        removeEventListener(event, fn) {
          if (this.listeners[event]) {
            this.listeners[event] = this.listeners[event].filter(f => f !== fn);
          }
        },
        click() {
          if (this.listeners['click']) {
            for (const fn of this.listeners['click']) fn({ stopPropagation: () => {} });
          }
        },
        dispatchEvent(evt) {
          if (this.listeners[evt.type]) {
            for (const fn of this.listeners[evt.type]) fn(evt);
          }
        },
        videoWidth: 1920,
        videoHeight: 1080,
        getBoundingClientRect() {
          return { left: 0, top: 0, width: 800, height: 600 };
        }
      };
    }
    return elements[id];
  }

  const ids = [
    'landingStateView', 'pinView', 'mainDashboard', 'btnOpenRemoteDesktop',
    'remoteConsentModal', 'btnCancelRemoteConsent', 'btnConfirmRemoteConsent',
    'remoteDesktopView', 'remoteHeader', 'btnRemoteBack', 'btnToggleOrientation',
    'remoteTransportBadge', 'remoteTransportLabel', 'remoteBatteryText',
    'remoteScreenContainer', 'remoteVideo', 'remoteConnectingOverlay',
    'remoteConnectingText', 'remoteTouchpadArea', 'btnFloatingQuick',
    'quickActionsSheet', 'btnQuickLock', 'btnQuickSleep', 'btnQuickRestart',
    'btnQuickShutdown', 'btnLock', 'btnSleep', 'btnRestart', 'btnShutdown',
    'confirmModal', 'toast', 'toastText', 'actionStateView'
  ];
  ids.forEach(id => getElement(id));

  const mockWindow = {
    location: { hash: '', pathname: '/', protocol: 'https:', host: 'pc-remote.local' },
    history: { replaceState: () => {} },
    localStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    sessionStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    WebSocket: class { constructor() {} send() {} close() {} },
    fetch: async () => ({ ok: true, json: async () => ({ status: 'ok' }) }),
    document: {
      getElementById: (id) => getElement(id),
      querySelectorAll: () => [],
      addEventListener: () => {},
      removeEventListener: () => {}
    },
    navigator: {
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)',
      serviceWorker: { register: async () => {} },
      vibrate: () => true
    },
    crypto: globalThis.crypto,
    addEventListener: () => {},
    removeEventListener: () => {},
    innerWidth: 390,
    innerHeight: 844,
    requestAnimationFrame: (cb) => setTimeout(cb, 16)
  };

  const code = fs.readFileSync('web/app.js', 'utf8');
  const mockFunc = new Function('window', 'document', 'localStorage', 'sessionStorage', 'navigator', 'fetch', code);
  mockFunc(mockWindow, mockWindow.document, mockWindow.localStorage, mockWindow.sessionStorage, mockWindow.navigator, mockWindow.fetch);

  return { mockWindow, getElement };
}

async function runTests() {
  console.log('Testing Remote Desktop View Invariant...');
  const { mockWindow, getElement } = createMockRemoteEnv();
  const remoteView = getElement('remoteDesktopView');
  const consentModal = getElement('remoteConsentModal');

  assert.strictEqual(remoteView.style.display, 'none', 'remoteDesktopView must start hidden');
  assert.strictEqual(consentModal.style.display, 'none', 'remoteConsentModal must start hidden');
  console.log('  -> PASS: Remote Desktop view and consent modal start hidden.\n');

  console.log('Testing Aspect-Ratio Letterbox Coordinate Mapping...');
  const remoteVideo = getElement('remoteVideo');
  const getCoords = mockWindow.__remoteDesktop.getNormalizedVideoCoordinates;

  // Case 1: 16:9 PC Video (1920x1080) rendered inside wider 2:1 container (800x400)
  // Expected: Pillarbox on left and right.
  // Video aspect = 1.7778, container aspect = 2.0.
  // Rendered height = 400, rendered width = 400 * (16/9) = 711.11px.
  // offsetX = (800 - 711.11) / 2 = 44.44px.
  remoteVideo.videoWidth = 1920;
  remoteVideo.videoHeight = 1080;
  remoteVideo.getBoundingClientRect = () => ({ left: 0, top: 0, width: 800, height: 400 });

  // Touch outside visible video on left letterbox
  const outsideLeft = getCoords(10, 200);
  assert.strictEqual(outsideLeft, null, 'Touch inside left pillarbox must be ignored');

  // Touch outside visible video on right letterbox
  const outsideRight = getCoords(790, 200);
  assert.strictEqual(outsideRight, null, 'Touch inside right pillarbox must be ignored');

  // Center touch
  const centerCoord = getCoords(400, 200);
  assert(centerCoord !== null, 'Center touch must be valid');
  assert(Math.abs(centerCoord.x - 0.5) < 0.01, `Center X expected ~0.5, got ${centerCoord.x}`);
  assert(Math.abs(centerCoord.y - 0.5) < 0.01, `Center Y expected ~0.5, got ${centerCoord.y}`);

  // Case 2: 16:9 PC Video inside taller container (400x800)
  // Expected: Letterbox on top and bottom.
  remoteVideo.getBoundingClientRect = () => ({ left: 0, top: 0, width: 400, height: 800 });
  const outsideTop = getCoords(200, 10);
  assert.strictEqual(outsideTop, null, 'Touch inside top letterbox must be ignored');

  const outsideBottom = getCoords(200, 790);
  assert.strictEqual(outsideBottom, null, 'Touch inside bottom letterbox must be ignored');

  const centerCoordTall = getCoords(200, 400);
  assert(centerCoordTall !== null, 'Center touch must be valid in letterbox');
  assert(Math.abs(centerCoordTall.x - 0.5) < 0.01, `Center X expected ~0.5, got ${centerCoordTall.x}`);
  assert(Math.abs(centerCoordTall.y - 0.5) < 0.01, `Center Y expected ~0.5, got ${centerCoordTall.y}`);
  console.log('  -> PASS: Letterbox & pillarbox coordinate transforms correctly map to [0, 1].\n');

  console.log('Testing Consent Modal Interactions...');
  const btnOpen = getElement('btnOpenRemoteDesktop');
  const btnCancel = getElement('btnCancelRemoteConsent');

  btnOpen.click();
  // Since mock environment default isPcOnline is false, clicking when offline should not open consent
  assert.strictEqual(consentModal.style.display, 'none', 'Must not open consent modal when PC is offline');
  console.log('  -> PASS: Consent modal respects offline PC state.\n');

  console.log('Testing Quick Actions Popover & Triggering...');
  const btnFloating = getElement('btnFloatingQuick');
  const sheet = getElement('quickActionsSheet');
  const btnQuickLock = getElement('btnQuickLock');
  const btnLock = getElement('btnLock');

  let lockClicked = false;
  btnLock.listeners['click'] = [() => { lockClicked = true; }];

  btnFloating.click();
  assert(sheet.classList.contains('active'), 'Clicking floating button toggles quick action sheet active');

  btnQuickLock.click();
  assert(!sheet.classList.contains('active'), 'Clicking quick lock closes quick action sheet');
  assert(lockClicked, 'Clicking quick lock routes directly to main lock handler');
  console.log('  -> PASS: Floating quick actions sheet opens, closes, and delegates to power actions.\n');

  console.log('Testing Portrait Gesture Config Tolerances...');
  const config = mockWindow.__remoteDesktop.GESTURE_CONFIG;
  assert(config.TAP_MAX_DURATION_MS <= 300, 'Tap max duration must be responsive (<= 300ms)');
  assert(config.LONG_PRESS_DURATION_MS >= 400, 'Long press must avoid accidental triggers (>= 400ms)');
  assert(config.MOVE_TOLERANCE_PX >= 5 && config.MOVE_TOLERANCE_PX <= 15, 'Movement tolerance must prevent jitter');
  assert(config.POINTER_SENSITIVITY > 0, 'Pointer sensitivity must be positive');
  assert(config.SCROLL_SENSITIVITY > 0, 'Scroll sensitivity must be positive');
  console.log('  -> PASS: Gesture thresholds and parameters within specified tolerances.\n');

  console.log('========================================================');
  console.log('  ALL REMOTE DESKTOP TESTS PASSED SUCCESSFULLY');
  console.log('========================================================\n');
}

runTests().catch(err => {
  console.error('\nTest Failed:', err);
  process.exit(1);
});
