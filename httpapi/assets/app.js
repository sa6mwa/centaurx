(() => {
  const loginPanel = document.getElementById('login-panel');
  const terminalPanel = document.getElementById('terminal-panel');
  const loginForm = document.getElementById('login-form');
  const loginError = document.getElementById('login-error');
  const sessionEl = document.getElementById('session');
  const tabsBarEl = document.getElementById('tabs-bar');
  const tabsLeftEl = document.getElementById('tabs-left');
  const tabsRightEl = document.getElementById('tabs-right');
  const tabsEl = document.getElementById('tabs');
  const statusEl = document.getElementById('status');
  const statusTextEl = document.getElementById('status-text');
  const promptSpinnerEl = document.getElementById('prompt-spinner');
  const terminalEl = document.getElementById('terminal');
  const promptForm = document.getElementById('prompt-form');
  const promptInput = document.getElementById('prompt-input');
  const chpasswdModal = document.getElementById('chpasswd-modal');
  const chpasswdForm = document.getElementById('chpasswd-form');
  const chpasswdCurrent = document.getElementById('chpasswd-current');
  const chpasswdTotp = document.getElementById('chpasswd-totp');
  const chpasswdNew = document.getElementById('chpasswd-new');
  const chpasswdConfirm = document.getElementById('chpasswd-confirm');
  const chpasswdError = document.getElementById('chpasswd-error');
  const chpasswdCancel = document.getElementById('chpasswd-cancel');
  const chpasswdSubmit = document.getElementById('chpasswd-submit');
  const codexauthModal = document.getElementById('codexauth-modal');
  const codexauthForm = document.getElementById('codexauth-form');
  const codexauthFile = document.getElementById('codexauth-file');
  const codexauthError = document.getElementById('codexauth-error');
  const codexauthCancel = document.getElementById('codexauth-cancel');
  const codexauthSubmit = document.getElementById('codexauth-submit');
  const rotatesshkeyModal = document.getElementById('rotatesshkey-modal');
  const rotatesshkeyForm = document.getElementById('rotatesshkey-form');
  const rotatesshkeyConfirm = document.getElementById('rotatesshkey-confirm');
  const rotatesshkeyError = document.getElementById('rotatesshkey-error');
  const rotatesshkeyCancel = document.getElementById('rotatesshkey-cancel');
  const rotatesshkeySubmit = document.getElementById('rotatesshkey-submit');

  const clientConfig = window.centaurxConfig || {};
  const uiMaxLines = Number(clientConfig.uiMaxBufferLines || 0);
  const MAX_LINES =
    Number.isFinite(uiMaxLines) && uiMaxLines > 0 ? uiMaxLines : 2000;
  const COMMAND_MARKER = '\u001a';
  const AGENT_MARKER = '\u001c';
  const REASONING_MARKER = '\u001d';
  const STDERR_MARKER = '\u001f';
  const WORKED_MARKER = '\u001e';
  const HELP_MARKER = '\u0016';
  const ABOUT_VERSION_MARKER = '\u0017';
  const ABOUT_COPYRIGHT_MARKER = '\u0018';
  const ABOUT_LINK_MARKER = '\u0019';
  const tabWindow = window.CentaurxTabWindow || {};

  const state = {
    tabs: [],
    activeTab: null,
    buffers: new Map(),
    scroll: new Map(),
    eventSource: null,
    user: null,
    systemLines: [],
    theme: null,
    history: new Map(),
    tabWindowStart: 0,
  };

  async function api(path, options = {}) {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      ...options,
    });
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'request failed' }));
      throw new Error(err.error || 'request failed');
    }
    return res.json();
  }

  async function checkSession() {
    try {
      const data = await api('api/me');
      state.user = data.username;
      showTerminal();
      startStream();
    } catch (err) {
      showLogin();
    }
  }

  function showLogin() {
    loginPanel.classList.remove('hidden');
    terminalPanel.classList.add('hidden');
    sessionEl.textContent = '';
    closeChpasswdDialog();
    closeCodexAuthDialog();
    closeRotateSSHKeyDialog();
  }

  function showTerminal() {
    loginPanel.classList.add('hidden');
    terminalPanel.classList.remove('hidden');
    sessionEl.textContent = state.user ? `signed in as ${state.user}` : '';
    focusPrompt();
  }

  const statusState = {
    message: '',
    level: 'info',
  };
  const busyState = {
    count: 0,
    timer: null,
    visible: false,
  };

  function isTabRunning(tab) {
    if (!tab) return false;
    const status = String(tab.status || tab.Status || '').toLowerCase();
    return status === 'running';
  }

  function isActiveTabRunning() {
    if (!state.activeTab) return false;
    const tab = state.tabs.find((entry) => entry.id === state.activeTab);
    return isTabRunning(tab);
  }

  function updateStatusUI() {
    if (!statusEl) return;
    const busy = busyState.visible || isActiveTabRunning();
    const message = statusState.message || '';
    const level = statusState.message ? statusState.level : 'info';
    if (statusTextEl) {
      statusTextEl.textContent = message || '';
    } else {
      statusEl.textContent = message || '';
    }
    statusEl.dataset.level = level;
    if (promptSpinnerEl) {
      promptSpinnerEl.classList.toggle('hidden', !busy);
    }
    statusEl.classList.toggle('hidden', !message);
  }

  function setStatus(message, level = 'info') {
    statusState.message = message || '';
    statusState.level = level;
    updateStatusUI();
  }

  function startBusy() {
    busyState.count += 1;
    if (busyState.count === 1) {
      if (busyState.timer) {
        clearTimeout(busyState.timer);
      }
      busyState.timer = setTimeout(() => {
        busyState.timer = null;
        if (busyState.count > 0) {
          busyState.visible = true;
          updateStatusUI();
        }
      }, 500);
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (busyState.count > 0) {
        busyState.count -= 1;
      }
      if (busyState.count <= 0) {
        busyState.count = 0;
        if (busyState.timer) {
          clearTimeout(busyState.timer);
          busyState.timer = null;
        }
        if (busyState.visible) {
          busyState.visible = false;
          updateStatusUI();
        }
      }
    };
  }

  function resetBusy() {
    busyState.count = 0;
    busyState.visible = false;
    if (busyState.timer) {
      clearTimeout(busyState.timer);
      busyState.timer = null;
    }
  }

  function focusPrompt() {
    if (!promptInput) return;
    if (terminalPanel.classList.contains('hidden')) return;
    if (document.activeElement === promptInput) return;
    requestAnimationFrame(() => {
      try {
        promptInput.focus({ preventScroll: true });
      } catch (err) {
        promptInput.focus();
      }
    });
  }

  function stopStream() {
    if (state.eventSource) {
      state.eventSource.close();
      state.eventSource = null;
    }
  }

  function resetClientState() {
    state.tabs = [];
    state.activeTab = null;
    state.buffers.clear();
    state.scroll.clear();
    state.systemLines = [];
    state.user = null;
    state.history.clear();
    resetBusy();
    setStatus('');
    closeChpasswdDialog();
    closeCodexAuthDialog();
    closeRotateSSHKeyDialog();
    renderTabs();
    renderTerminal();
  }

  function getHistoryState(tabId) {
    if (!tabId) return null;
    let entry = state.history.get(tabId);
    if (!entry) {
      entry = { entries: [], index: -1, loaded: false };
      state.history.set(tabId, entry);
    }
    return entry;
  }

  async function loadHistory(tabId) {
    if (!tabId) return null;
    const entry = getHistoryState(tabId);
    if (entry.loaded) return entry;
    try {
      const data = await api(`api/history?tab_id=${encodeURIComponent(tabId)}`);
      entry.entries = data.entries || data.Entries || [];
      entry.index = -1;
      entry.loaded = true;
    } catch (err) {
      reportError(err.message);
    }
    return entry;
  }

  async function appendHistory(tabId, value) {
    if (!tabId) return false;
    if (!value || !value.trim()) return false;
    const entry = getHistoryState(tabId);
    const prevLast = entry.entries[entry.entries.length - 1];
    try {
      const data = await api('api/history', {
        method: 'POST',
        body: JSON.stringify({ tab_id: tabId, entry: value }),
      });
      entry.entries = data.entries || data.Entries || [];
      entry.loaded = true;
    } catch (err) {
      reportError(err.message);
    }
    return prevLast !== value;
  }

  function isLogoutInput(input) {
    const trimmed = (input || '').trim();
    return trimmed === '/quit' || trimmed === '/exit' || trimmed === '/logout' || trimmed === '/q';
  }

  function isChpasswdInput(input) {
    return (input || '').trim() === '/chpasswd';
  }

  function isCodexAuthInput(input) {
    return (input || '').trim() === '/codexauth';
  }

  function isRotateSSHKeyInput(input) {
    return (input || '').trim() === '/rotatesshkey';
  }

  function openChpasswdDialog() {
    if (!chpasswdModal) return;
    if (chpasswdError) chpasswdError.textContent = '';
    if (chpasswdCurrent) chpasswdCurrent.value = '';
    if (chpasswdTotp) chpasswdTotp.value = '';
    if (chpasswdNew) chpasswdNew.value = '';
    if (chpasswdConfirm) chpasswdConfirm.value = '';
    chpasswdModal.classList.remove('hidden');
    requestAnimationFrame(() => {
      if (chpasswdCurrent) {
        chpasswdCurrent.focus({ preventScroll: true });
      }
    });
  }

  function isChpasswdOpen() {
    return chpasswdModal && !chpasswdModal.classList.contains('hidden');
  }

  function closeChpasswdDialog() {
    if (!chpasswdModal) return;
    chpasswdModal.classList.add('hidden');
    if (chpasswdError) chpasswdError.textContent = '';
    if (chpasswdCurrent) chpasswdCurrent.value = '';
    if (chpasswdTotp) chpasswdTotp.value = '';
    if (chpasswdNew) chpasswdNew.value = '';
    if (chpasswdConfirm) chpasswdConfirm.value = '';
    focusPrompt();
  }

  function openCodexAuthDialog() {
    if (!codexauthModal) return;
    if (codexauthError) codexauthError.textContent = '';
    if (codexauthFile) codexauthFile.value = '';
    codexauthModal.classList.remove('hidden');
    requestAnimationFrame(() => {
      if (codexauthFile) {
        codexauthFile.focus({ preventScroll: true });
      }
    });
  }

  function closeCodexAuthDialog() {
    if (!codexauthModal) return;
    codexauthModal.classList.add('hidden');
    if (codexauthError) codexauthError.textContent = '';
    if (codexauthFile) codexauthFile.value = '';
    focusPrompt();
  }

  function openRotateSSHKeyDialog() {
    if (!rotatesshkeyModal) return;
    if (rotatesshkeyError) rotatesshkeyError.textContent = '';
    if (rotatesshkeyConfirm) rotatesshkeyConfirm.value = '';
    rotatesshkeyModal.classList.remove('hidden');
    requestAnimationFrame(() => {
      if (rotatesshkeyConfirm) {
        rotatesshkeyConfirm.focus({ preventScroll: true });
      }
    });
  }

  function closeRotateSSHKeyDialog() {
    if (!rotatesshkeyModal) return;
    rotatesshkeyModal.classList.add('hidden');
    if (rotatesshkeyError) rotatesshkeyError.textContent = '';
    if (rotatesshkeyConfirm) rotatesshkeyConfirm.value = '';
    focusPrompt();
  }

  async function logoutFromPrompt() {
    try {
      await api('api/logout', { method: 'POST' });
    } catch (err) {
      reportError(err.message);
      return err;
    }
    stopStream();
    resetClientState();
    showLogin();
    return null;
  }

  function appendSystem(lines) {
    if (!lines || !lines.length) return;
    const next = state.systemLines.concat(lines);
    if (next.length > MAX_LINES) {
      state.systemLines = next.slice(next.length - MAX_LINES);
    } else {
      state.systemLines = next;
    }
  }

  function reportError(message) {
    const text = message || 'request failed';
    setStatus(text, 'error');
    const line = `error: ${text}`;
    if (state.activeTab) {
      appendLines(state.activeTab, [line]);
    } else {
      appendSystem([line]);
      renderTerminal(true);
    }
  }

  loginForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    loginError.textContent = '';
    const payload = {
      username: document.getElementById('login-username').value.trim(),
      password: document.getElementById('login-password').value,
      totp: document.getElementById('login-totp').value.trim(),
    };
    try {
      const data = await api('api/login', {
        method: 'POST',
        body: JSON.stringify(payload),
      });
      state.user = data.username;
      showTerminal();
      startStream();
      setStatus('');
      focusPrompt();
    } catch (err) {
      loginError.textContent = err.message;
    }
  });

  if (chpasswdCancel) {
    chpasswdCancel.addEventListener('click', () => {
      closeChpasswdDialog();
    });
  }

  if (chpasswdForm) {
    chpasswdForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (chpasswdError) chpasswdError.textContent = '';
      const current = chpasswdCurrent ? chpasswdCurrent.value : '';
      const next = chpasswdNew ? chpasswdNew.value : '';
      const confirm = chpasswdConfirm ? chpasswdConfirm.value : '';
      const totp = chpasswdTotp ? chpasswdTotp.value : '';
      if (!current.trim()) {
        if (chpasswdError) chpasswdError.textContent = 'current password is required';
        return;
      }
      if (!next.trim()) {
        if (chpasswdError) chpasswdError.textContent = 'new password is required';
        return;
      }
      if (!confirm.trim()) {
        if (chpasswdError) chpasswdError.textContent = 'confirm password is required';
        return;
      }
      if (next !== confirm) {
        if (chpasswdError) chpasswdError.textContent = 'passwords do not match';
        return;
      }
      if (!totp.trim()) {
        if (chpasswdError) chpasswdError.textContent = 'totp is required';
        return;
      }
      if (chpasswdSubmit) chpasswdSubmit.disabled = true;
      try {
        await api('api/chpasswd', {
          method: 'POST',
          body: JSON.stringify({
            current_password: current,
            new_password: next,
            confirm_password: confirm,
            totp: totp,
          }),
        });
        setStatus('password updated');
        closeChpasswdDialog();
      } catch (err) {
        if (chpasswdError) chpasswdError.textContent = err.message;
      } finally {
        if (chpasswdSubmit) chpasswdSubmit.disabled = false;
      }
    });
  }

  if (codexauthCancel) {
    codexauthCancel.addEventListener('click', () => {
      closeCodexAuthDialog();
    });
  }

  if (rotatesshkeyCancel) {
    rotatesshkeyCancel.addEventListener('click', () => {
      closeRotateSSHKeyDialog();
    });
  }

  if (codexauthForm) {
    codexauthForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (codexauthError) codexauthError.textContent = '';
      const file = codexauthFile && codexauthFile.files ? codexauthFile.files[0] : null;
      if (!file) {
        if (codexauthError) codexauthError.textContent = 'auth.json file is required';
        return;
      }
      if (!file.name.toLowerCase().endsWith('.json')) {
        if (codexauthError) codexauthError.textContent = 'auth.json must be a .json file';
        return;
      }
      if (codexauthSubmit) codexauthSubmit.disabled = true;
      try {
        const text = await file.text();
        if (!text.trim()) {
          if (codexauthError) codexauthError.textContent = 'auth.json file is empty';
          return;
        }
        await api('api/codexauth', {
          method: 'POST',
          body: text,
        });
        setStatus('codex auth updated');
        closeCodexAuthDialog();
      } catch (err) {
        if (codexauthError) codexauthError.textContent = err.message;
      } finally {
        if (codexauthSubmit) codexauthSubmit.disabled = false;
      }
    });
  }

  if (rotatesshkeyForm) {
    rotatesshkeyForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (rotatesshkeyError) rotatesshkeyError.textContent = '';
      const value = rotatesshkeyConfirm ? rotatesshkeyConfirm.value.trim() : '';
      if (value !== 'YES') {
        if (rotatesshkeyError) rotatesshkeyError.textContent = 'type YES to confirm';
        return;
      }
      if (rotatesshkeySubmit) rotatesshkeySubmit.disabled = true;
      const stopBusy = startBusy();
      try {
        await api('api/prompt', {
          method: 'POST',
          body: JSON.stringify({ tab_id: state.activeTab || '', input: '/rotatesshkey affirm' }),
        });
        closeRotateSSHKeyDialog();
      } catch (err) {
        if (rotatesshkeyError) rotatesshkeyError.textContent = err.message;
      } finally {
        stopBusy();
        if (rotatesshkeySubmit) rotatesshkeySubmit.disabled = false;
      }
    });
  }

  const resizePrompt = () => {
    if (!promptInput) return;
    promptInput.style.height = 'auto';
    const next = Math.min(promptInput.scrollHeight, 180);
    promptInput.style.height = `${next}px`;
  };

  let lastEnterWasShift = false;

  async function navigateHistory(direction) {
    const tabId = state.activeTab;
    if (!tabId) return;
    const entry = getHistoryState(tabId);
    const pos = promptInput.selectionStart || 0;
    const atEdge =
      promptInput.selectionStart === promptInput.selectionEnd &&
      (pos === 0 || pos === promptInput.value.length);
    if (!atEdge) return;
    let appended = false;
    if (promptInput.value.trim()) {
      appended = await appendHistory(tabId, promptInput.value);
    } else if (!entry.loaded) {
      await loadHistory(tabId);
    }
    if (!entry.entries.length) return;
    if (entry.index === -1) {
      entry.index =
        appended && entry.entries.length > 1
          ? entry.entries.length - 2
          : entry.entries.length - 1;
    } else if (direction < 0 && entry.index > 0) {
      entry.index -= 1;
    } else if (direction > 0 && entry.index < entry.entries.length - 1) {
      entry.index += 1;
    } else {
      return;
    }
    promptInput.value = entry.entries[entry.index] || '';
    resizePrompt();
    const cursorPos = promptInput.value.length;
    promptInput.setSelectionRange(cursorPos, cursorPos);
  }

  promptInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      if (promptForm.requestSubmit) {
        promptForm.requestSubmit();
      } else {
        const evt = new Event('submit', { cancelable: true, bubbles: true });
        promptForm.dispatchEvent(evt);
      }
      return;
    }
    if (e.key === 'Enter' && e.shiftKey) {
      lastEnterWasShift = true;
      return;
    }
    if (e.key === 'ArrowUp') {
      const pos = promptInput.selectionStart || 0;
      if (
        promptInput.selectionStart === promptInput.selectionEnd &&
        (pos === 0 || pos === promptInput.value.length)
      ) {
        e.preventDefault();
        navigateHistory(-1);
      }
    }
    if (e.key === 'ArrowDown') {
      const pos = promptInput.selectionStart || 0;
      if (
        promptInput.selectionStart === promptInput.selectionEnd &&
        (pos === 0 || pos === promptInput.value.length)
      ) {
        e.preventDefault();
        navigateHistory(1);
      }
    }
  });

  promptInput.addEventListener('input', (e) => {
    resizePrompt();
    const value = promptInput.value;
    const singleTrailingNewline =
      value.endsWith('\n') && value.indexOf('\n') === value.length - 1;
    if (
      value.includes('\n') &&
      (e.inputType === 'insertLineBreak' ||
        e.inputType === 'insertParagraph' ||
        e.data === '\n' ||
        singleTrailingNewline)
    ) {
      if (lastEnterWasShift) {
        lastEnterWasShift = false;
      } else {
        if (promptForm.requestSubmit) {
          promptForm.requestSubmit();
        } else {
          const evt = new Event('submit', { cancelable: true, bubbles: true });
          promptForm.dispatchEvent(evt);
        }
      }
    }
    if (!state.activeTab) return;
    const entry = getHistoryState(state.activeTab);
    if (entry) {
      entry.index = -1;
    }
  });

  promptForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const raw = promptInput.value;
    let input = raw.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
    if (input.endsWith('\n')) {
      const trimmedEnd = input.slice(0, -1);
      if (!trimmedEnd.includes('\n')) {
        input = trimmedEnd;
      }
    }
    const trimmed = input.trim();
    let payloadInput = input;
    if ((trimmed.startsWith('/') || trimmed.startsWith('!')) && input.includes('\n')) {
      payloadInput = trimmed.split('\n')[0].trim();
    }
    if (!trimmed) return;
    promptInput.value = '';
    resizePrompt();
    if (isChpasswdInput(payloadInput)) {
      openChpasswdDialog();
      return;
    }
    if (isCodexAuthInput(payloadInput)) {
      openCodexAuthDialog();
      return;
    }
    if (isRotateSSHKeyInput(payloadInput)) {
      openRotateSSHKeyDialog();
      return;
    }
    if (isLogoutInput(payloadInput)) {
      const err = await logoutFromPrompt();
      if (err) {
        promptInput.value = raw;
        resizePrompt();
      }
      return;
    }
    const historyTabId = state.activeTab;
    const isSlashCommand = payloadInput.trim().startsWith('/');
    const stopBusy = startBusy();
    try {
      await api('api/prompt', {
        method: 'POST',
        body: JSON.stringify({ tab_id: state.activeTab || '', input: payloadInput }),
      });
      if (isSlashCommand) {
        await refreshTabs();
      }
      if (historyTabId) {
        const historyEntry = getHistoryState(historyTabId);
        if (historyEntry) {
          const last = historyEntry.entries[historyEntry.entries.length - 1];
          if (input.trim() && last !== input) {
            historyEntry.entries.push(input);
          }
          historyEntry.index = -1;
          historyEntry.loaded = true;
        }
      }
      if (state.activeTab) {
        setAutoScroll(state.activeTab, true);
      }
      renderTerminal(true);
      setStatus('');
      focusPrompt();
    } catch (err) {
      promptInput.value = raw;
      resizePrompt();
      reportError(err.message);
    } finally {
      stopBusy();
    }
  });

  promptInput.addEventListener('focus', () => {
    if (!state.activeTab) return;
    setAutoScroll(state.activeTab, true);
    renderTerminal(true);
    resizePrompt();
  });

  if (tabsLeftEl) {
    tabsLeftEl.addEventListener('click', () => {
      void cycleTab(-1);
    });
  }

  if (tabsRightEl) {
    tabsRightEl.addEventListener('click', () => {
      void cycleTab(1);
    });
  }

  document.addEventListener(
    'keydown',
    (e) => {
      if (e.key !== 'Tab') return;
      if (isChpasswdOpen()) return;
      if (terminalPanel.classList.contains('hidden')) return;
      if (!state.tabs.length) return;
      e.preventDefault();
      e.stopPropagation();
      void cycleTab(e.shiftKey ? -1 : 1);
    },
    true,
  );

  window.addEventListener('resize', () => {
    requestAnimationFrame(layoutTabs);
  });

  terminalEl.addEventListener('scroll', () => {
    if (!state.activeTab) return;
    const atBottom = isAtBottom(terminalEl);
    const entry = getScrollState(state.activeTab);
    entry.scrollTop = terminalEl.scrollTop;
    entry.autoScroll = atBottom;
    state.scroll.set(state.activeTab, entry);
  });

  function startStream() {
    if (state.eventSource) return;
    state.eventSource = new EventSource('api/stream');
    state.eventSource.onmessage = (e) => {
      if (!e.data) return;
      const event = JSON.parse(e.data);
      handleEvent(event);
    };
    state.eventSource.onerror = () => {
      console.warn('stream error');
      setStatus('stream disconnected', 'warn');
    };
  }

  async function cycleTab(step) {
    if (!state.tabs.length) return;
    const idx = state.tabs.findIndex((t) => t.id === state.activeTab);
    let nextIndex = idx >= 0 ? idx + step : 0;
    if (nextIndex < 0) nextIndex = state.tabs.length - 1;
    if (nextIndex >= state.tabs.length) nextIndex = 0;
    const next = state.tabs[nextIndex];
    if (!next || next.id === state.activeTab) return;
    try {
      await api('api/tabs/activate', {
        method: 'POST',
        body: JSON.stringify({ tab_id: next.id }),
      });
      if (state.activeTab && state.activeTab !== next.id) {
        saveScrollState(state.activeTab);
      }
      state.activeTab = next.id;
      setStatus('');
      renderTabs();
      renderTerminal(true);
      if (state.activeTab) {
        void loadHistory(state.activeTab);
      }
      focusPrompt();
    } catch (err) {
      reportError(err.message);
    }
  }

  function handleEvent(event) {
    switch (event.type) {
      case 'snapshot':
        applySnapshot(event.snapshot);
        break;
      case 'tab':
        applyTabEvent(event);
        break;
      case 'output':
        appendLines(event.tab_id, event.lines || []);
        break;
      case 'system':
        appendSystem(event.lines || []);
        if (!state.activeTab) {
          renderTerminal(true);
        }
        break;
      default:
        break;
    }
  }

  function applySnapshot(snapshot) {
    if (!snapshot) return;
    state.tabs = (snapshot.tabs || []).map(normalizeTab).filter(Boolean);
    state.activeTab = snapshot.active_tab || null;
    if (state.activeTab && !state.tabs.some((tab) => tab.id === state.activeTab)) {
      state.activeTab = null;
    }
    applyTheme(snapshot.theme || snapshot.Theme || null);
    state.buffers = new Map();
    state.scroll = new Map();
    state.history = new Map();
    const system = snapshot.system || {};
    state.systemLines = system.lines || system.Lines || [];
    const buffers = snapshot.buffers || {};
    Object.keys(buffers).forEach((tabId) => {
      const buf = buffers[tabId];
      const lines = (buf && (buf.lines || buf.Lines)) || [];
      state.buffers.set(tabId, lines);
      state.scroll.set(tabId, { scrollTop: 0, autoScroll: true });
    });
    renderTabs();
    renderTerminal(true);
    updateStatusUI();
    focusPrompt();
    if (state.activeTab) {
      void loadHistory(state.activeTab);
    }
  }

  function applyTabEvent(event) {
    if (event && (event.theme || event.Theme)) {
      applyTheme(event.theme || event.Theme);
    }
    const tab = normalizeTab(event.tab);
    if (!tab || !tab.id) return;
    const type = event.tab_event || '';
    if (type === 'closed') {
      state.tabs = state.tabs.filter((t) => t.id !== tab.id);
      state.buffers.delete(tab.id);
      state.scroll.delete(tab.id);
      state.history.delete(tab.id);
      if (state.activeTab === tab.id) {
        state.activeTab = null;
      }
    } else {
      const idx = state.tabs.findIndex((t) => t.id === tab.id);
      if (idx >= 0) {
        state.tabs[idx] = tab;
      } else {
        state.tabs.push(tab);
      }
    }
    const activeExists = state.activeTab && state.tabs.some((t) => t.id === state.activeTab);
    if (!activeExists) {
      state.activeTab = null;
    }
    renderTabs();
    renderTerminal();
    updateStatusUI();
    focusPrompt();
    if (state.activeTab) {
      void loadHistory(state.activeTab);
    }
  }

  async function refreshTabs() {
    try {
      const resp = await api('api/tabs');
      const tabs = (resp.tabs || resp.Tabs || []).map(normalizeTab).filter(Boolean);
      const ids = new Set(tabs.map((tab) => tab.id));
      state.tabs = tabs;
      state.activeTab = resp.active_tab || resp.ActiveTab || null;
      if (state.activeTab && !ids.has(state.activeTab)) {
        state.activeTab = null;
      }
      for (const key of state.buffers.keys()) {
        if (!ids.has(key)) {
          state.buffers.delete(key);
        }
      }
      for (const key of state.scroll.keys()) {
        if (!ids.has(key)) {
          state.scroll.delete(key);
        }
      }
      for (const key of state.history.keys()) {
        if (!ids.has(key)) {
          state.history.delete(key);
        }
      }
      applyTheme(resp.theme || resp.Theme || state.theme);
      renderTabs();
      renderTerminal(true);
      updateStatusUI();
      if (state.activeTab) {
        void loadHistory(state.activeTab);
      }
      focusPrompt();
    } catch (err) {
      reportError(err.message);
    }
  }

  function appendLines(tabId, lines) {
    if (!tabId || !lines || lines.length === 0) return;
    const existing = state.buffers.get(tabId) || [];
    const next = existing.concat(lines);
    if (next.length > MAX_LINES) {
      state.buffers.set(tabId, next.slice(next.length - MAX_LINES));
    } else {
      state.buffers.set(tabId, next);
    }
    if (tabId === state.activeTab) {
      const entry = getScrollState(tabId);
      renderTerminal(entry.autoScroll);
    }
  }

  function renderTabs() {
    tabsEl.innerHTML = '';
    state.tabs.forEach((tab) => {
      const btn = document.createElement('button');
      btn.className = 'tab' + (tab.id === state.activeTab ? ' active' : '');
      btn.textContent = tab.name || tab.id;
      btn.onclick = async () => {
        try {
          await api('api/tabs/activate', {
            method: 'POST',
            body: JSON.stringify({ tab_id: tab.id }),
          });
          if (state.activeTab && state.activeTab !== tab.id) {
            saveScrollState(state.activeTab);
          }
          state.activeTab = tab.id;
          setStatus('');
          renderTabs();
          renderTerminal(true);
          updateStatusUI();
          if (state.activeTab) {
            void loadHistory(state.activeTab);
          }
          focusPrompt();
        } catch (err) {
          reportError(err.message);
        }
      };
      tabsEl.appendChild(btn);
    });
    if (!state.tabs.length) {
      state.tabWindowStart = 0;
    }
    requestAnimationFrame(layoutTabs);
  }

  function getIndicatorWidth() {
    if (!tabsBarEl) return 0;
    const value = getComputedStyle(tabsBarEl).getPropertyValue('--tabs-indicator-size');
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) ? parsed : 24;
  }

  function layoutTabs() {
    if (!tabsBarEl || !tabsEl) return;
    const buttons = Array.from(tabsEl.children);
    const tabGap = (() => {
      const gapValue = getComputedStyle(tabsEl).gap || getComputedStyle(tabsEl).columnGap;
      const parsed = Number.parseFloat(gapValue);
      return Number.isFinite(parsed) ? parsed : 0;
    })();
    const widths = buttons.map((btn) => btn.offsetWidth || 0);
    const barWidth = tabsBarEl.clientWidth || 0;
    if (!buttons.length || barWidth <= 0 || typeof tabWindow.resolveWindow !== 'function') {
      buttons.forEach((btn) => {
        btn.hidden = false;
      });
      if (tabsLeftEl) tabsLeftEl.hidden = true;
      if (tabsRightEl) tabsRightEl.hidden = true;
      return;
    }
    const activeIndex = state.tabs.findIndex((tab) => tab.id === state.activeTab);
    const indicatorWidth = getIndicatorWidth();
    const barGapValue = getComputedStyle(tabsBarEl).gap || getComputedStyle(tabsBarEl).columnGap;
    const barGap = (() => {
      const parsed = Number.parseFloat(barGapValue);
      return Number.isFinite(parsed) ? parsed : 0;
    })();
    const indicatorWithGap = indicatorWidth + barGap;
    const window = tabWindow.resolveWindow(
      widths,
      activeIndex,
      state.tabWindowStart,
      barWidth,
      indicatorWithGap,
      indicatorWithGap,
      tabGap,
    );
    state.tabWindowStart = window.start;
    if (tabsLeftEl) tabsLeftEl.hidden = !window.leftHidden;
    if (tabsRightEl) tabsRightEl.hidden = !window.rightHidden;
    buttons.forEach((btn, idx) => {
      btn.hidden = idx < window.start || idx >= window.end;
    });
  }

  function renderTerminal(scrollToBottom = false) {
    let lines = state.buffers.get(state.activeTab) || [];
    if (!state.activeTab) {
      if (state.systemLines.length) {
        lines = state.systemLines;
      } else {
        lines = ['no active tab; use /new <repo>'];
      }
    }
    terminalEl.innerHTML = '';
    const frag = document.createDocumentFragment();
    lines.forEach((line) => {
      const el = document.createElement('div');
      el.className = 'line';
      const parsed = parseLine(line);
      if (parsed.klass) {
        el.classList.add(parsed.klass);
      }
      if (parsed.link) {
        const link = document.createElement('a');
        link.href = parsed.text;
        link.textContent = parsed.text === '' ? '\u00a0' : parsed.text;
        link.target = '_blank';
        link.rel = 'noreferrer noopener';
        el.appendChild(link);
      } else if (parsed.markdown) {
        renderMarkdownInto(el, parsed.text);
      } else {
        el.textContent = parsed.text === '' ? '\u00a0' : parsed.text;
      }
      frag.appendChild(el);
    });
    terminalEl.appendChild(frag);
    if (scrollToBottom) {
      terminalEl.scrollTop = terminalEl.scrollHeight;
      if (state.activeTab) {
        const entry = getScrollState(state.activeTab);
        entry.scrollTop = terminalEl.scrollTop;
        entry.autoScroll = true;
        state.scroll.set(state.activeTab, entry);
      }
      return;
    }
    if (state.activeTab) {
      const entry = getScrollState(state.activeTab);
      terminalEl.scrollTop = entry.scrollTop || 0;
    }
  }

  function parseLine(line) {
    let text = line || '';
    if (text.startsWith(WORKED_MARKER)) {
      return { text: text.slice(WORKED_MARKER.length), klass: 'worked' };
    }
    if (text.startsWith(HELP_MARKER)) {
      return { text: text.slice(HELP_MARKER.length), klass: 'help', markdown: true };
    }
    if (text.startsWith(ABOUT_VERSION_MARKER)) {
      return { text: text.slice(ABOUT_VERSION_MARKER.length), klass: 'about-version' };
    }
    if (text.startsWith(ABOUT_COPYRIGHT_MARKER)) {
      return { text: text.slice(ABOUT_COPYRIGHT_MARKER.length), klass: 'about-copyright' };
    }
    if (text.startsWith(ABOUT_LINK_MARKER)) {
      return { text: text.slice(ABOUT_LINK_MARKER.length), klass: 'about-link', link: true };
    }
    if (text.startsWith(AGENT_MARKER)) {
      return { text: text.slice(AGENT_MARKER.length), klass: 'agent', markdown: true };
    }
    if (text.startsWith(REASONING_MARKER)) {
      return { text: text.slice(REASONING_MARKER.length), klass: 'reasoning', markdown: true };
    }
    if (text.startsWith(COMMAND_MARKER)) {
      return { text: text.slice(COMMAND_MARKER.length), klass: 'command' };
    }
    let stderr = false;
    if (text.startsWith(STDERR_MARKER)) {
      stderr = true;
      text = text.slice(STDERR_MARKER.length);
    }
    if (text.startsWith('error:')) return { text, klass: 'error' };
    if (text.startsWith('command failed:') || text.startsWith('command error:')) return { text, klass: 'error' };
    if (text.startsWith('--- command finished')) return { text, klass: 'meta' };
    if (stderr) return { text, klass: 'stderr' };
    return { text, klass: '' };
  }

  function renderMarkdownInto(el, text) {
    const spans = parseMarkdown(text || '');
    if (!spans.length) {
      el.textContent = '\u00a0';
      return;
    }
    spans.forEach((span) => {
      if (!span.text) return;
      const node = document.createElement('span');
      node.textContent = span.text;
      if (span.bold) node.classList.add('md-bold');
      if (span.italic) node.classList.add('md-italic');
      if (span.code) node.classList.add('md-code');
      el.appendChild(node);
    });
  }

  function parseMarkdown(text) {
    const spans = [];
    if (!text) return spans;
    let buf = '';
    let bold = false;
    let italic = false;
    let code = false;
    const flush = () => {
      if (!buf) return;
      spans.push({ text: buf, bold, italic, code });
      buf = '';
    };
    for (let i = 0; i < text.length; ) {
      const ch = text[i];
      if (ch === '\\' && i + 1 < text.length) {
        buf += text[i + 1];
        i += 2;
        continue;
      }
      if (ch === '`') {
        if (code) {
          flush();
          code = false;
          i += 1;
          continue;
        }
        if (hasClosing(text.slice(i + 1), '`')) {
          flush();
          code = true;
          i += 1;
          continue;
        }
      }
      if (!code && ch === '*') {
        if (text.startsWith('**', i)) {
          if (bold) {
            flush();
            bold = false;
            i += 2;
            continue;
          }
          if (hasClosing(text.slice(i + 2), '**')) {
            flush();
            bold = true;
            i += 2;
            continue;
          }
          buf += '**';
          i += 2;
          continue;
        }
        if (italic) {
          flush();
          italic = false;
          i += 1;
          continue;
        }
        if (hasClosing(text.slice(i + 1), '*')) {
          flush();
          italic = true;
          i += 1;
          continue;
        }
      }
      buf += ch;
      i += 1;
    }
    flush();
    return spans;
  }

  function hasClosing(remaining, marker) {
    if (!marker || !remaining) return false;
    return remaining.indexOf(marker) !== -1;
  }

  function normalizeTab(tab) {
    if (!tab) return null;
    const normalized = { ...tab };
    if (!normalized.id && normalized.ID) normalized.id = normalized.ID;
    if (!normalized.name && normalized.Name) normalized.name = normalized.Name;
    if (!normalized.repo && normalized.Repo) normalized.repo = normalized.Repo;
    if (!normalized.status && normalized.Status) normalized.status = normalized.Status;
    if (normalized.repo) {
      if (!normalized.repo.name && normalized.repo.Name) normalized.repo.name = normalized.repo.Name;
      if (!normalized.repo.path && normalized.repo.Path) normalized.repo.path = normalized.repo.Path;
    }
    return normalized;
  }

  function applyTheme(name) {
    if (!name) return;
    state.theme = name;
    document.documentElement.dataset.theme = name;
  }

  function saveScrollState(tabId) {
    if (!tabId) return;
    const entry = getScrollState(tabId);
    entry.scrollTop = terminalEl.scrollTop;
    entry.autoScroll = isAtBottom(terminalEl);
    state.scroll.set(tabId, entry);
  }

  function getScrollState(tabId) {
    const existing = state.scroll.get(tabId);
    if (existing) return { ...existing };
    return { scrollTop: 0, autoScroll: true };
  }

  function setAutoScroll(tabId, enabled) {
    const entry = getScrollState(tabId);
    entry.autoScroll = enabled;
    state.scroll.set(tabId, entry);
  }

  function isAtBottom(el) {
    const threshold = 4;
    return el.scrollTop + el.clientHeight >= el.scrollHeight - threshold;
  }

  checkSession();
})();
