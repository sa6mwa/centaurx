package systems.pkt.centaurx.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.isActive
import kotlinx.coroutines.withTimeoutOrNull
import systems.pkt.centaurx.data.ApiException
import systems.pkt.centaurx.data.CentaurxRepository
import systems.pkt.centaurx.data.StreamEvent
import systems.pkt.centaurx.data.TabSnapshot

class AppViewModel(private val repository: CentaurxRepository) : ViewModel() {
    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state.asStateFlow()

    private var streamJob: Job? = null
    private var bufferPollJob: Job? = null
    private var lastSeq: Long = 0
    private val streamReady = MutableStateFlow(false)

    init {
        viewModelScope.launch {
            repository.endpointFlow.collectLatest { endpoint ->
                _state.update {
                    it.copy(
                        endpoint = endpoint,
                    )
                }
            }
        }
        viewModelScope.launch {
            repository.fontSizeFlow.collectLatest { size ->
                _state.update {
                    it.copy(fontSizeSp = size)
                }
            }
        }
        viewModelScope.launch {
            checkSession()
        }
    }

    fun showSettings(show: Boolean) {
        _state.update { it.copy(showSettings = show) }
    }

    fun showFontSize(show: Boolean) {
        _state.update { it.copy(showFontSize = show) }
    }

    fun showChangePassword(show: Boolean) {
        _state.update { it.copy(showChpasswd = show, chpasswdError = null) }
    }

    fun showCodexAuth(show: Boolean) {
        _state.update { it.copy(showCodexAuth = show, codexAuthError = null) }
    }

    fun setCodexAuthError(message: String?) {
        _state.update { it.copy(codexAuthError = message) }
    }

    fun showRotateSSHKey(show: Boolean) {
        _state.update { it.copy(showRotateSSHKey = show, rotateSSHKeyError = null) }
    }

    fun setRotateSSHKeyError(message: String?) {
        _state.update { it.copy(rotateSSHKeyError = message) }
    }

    fun updateEndpoint(value: String) {
        repository.setEndpoint(value)
        stopStream()
        resetClientState()
    }

    fun updateFontSize(value: Int) {
        repository.setFontSize(value)
    }

    fun login(username: String, password: String, totp: String) {
        viewModelScope.launch {
            setBusy(true)
            clearLoginError()
            try {
                val resp = repository.login(username, password, totp)
                _state.update {
                    it.copy(
                        username = resp.username,
                        loggedIn = true,
                        loginError = null,
                        status = null,
                    )
                }
                setBusy(false)
                startStream()
                refreshSnapshotAfterLogin()
            } catch (err: ApiException) {
                _state.update { it.copy(loginError = err.message ?: "login failed") }
            } catch (err: Exception) {
                _state.update { it.copy(loginError = err.message ?: "network error") }
            } finally {
                setBusy(false)
            }
        }
    }

    fun logout() {
        viewModelScope.launch {
            setBusy(true)
            try {
                repository.logout()
            } catch (err: ApiException) {
                setStatus(err.message ?: "logout failed", StatusLevel.Error)
            } finally {
                stopStream()
                resetClientState()
                setBusy(false)
            }
        }
    }

    fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    ) {
        viewModelScope.launch {
            setBusy(true)
            _state.update { it.copy(chpasswdError = null) }
            try {
                repository.changePassword(currentPassword, newPassword, confirmPassword, totp)
                setStatus("password updated", StatusLevel.Info)
                showChangePassword(false)
            } catch (err: ApiException) {
                _state.update { it.copy(chpasswdError = err.message ?: "password update failed") }
            } finally {
                setBusy(false)
            }
        }
    }

    fun uploadCodexAuth(rawJson: String) {
        viewModelScope.launch {
            setBusy(true)
            _state.update { it.copy(codexAuthError = null) }
            try {
                repository.uploadCodexAuth(rawJson)
                setStatus("codex auth updated", StatusLevel.Info)
                showCodexAuth(false)
            } catch (err: ApiException) {
                _state.update { it.copy(codexAuthError = err.message ?: "codex auth upload failed") }
            } finally {
                setBusy(false)
            }
        }
    }

    fun rotateSSHKey(confirmation: String) {
        viewModelScope.launch {
            setBusy(true)
            _state.update { it.copy(rotateSSHKeyError = null) }
            if (confirmation.trim() != "YES") {
                _state.update { it.copy(rotateSSHKeyError = "type YES to confirm") }
                setBusy(false)
                return@launch
            }
            try {
                repository.submitPrompt(_state.value.activeTabId, "/rotatesshkey affirm")
                _state.update { it.copy(status = null) }
                showRotateSSHKey(false)
            } catch (err: ApiException) {
                _state.update { it.copy(rotateSSHKeyError = err.message ?: "ssh key rotation failed") }
            } finally {
                setBusy(false)
            }
        }
    }

    fun activateTab(tabId: String) {
        viewModelScope.launch {
            try {
                repository.activateTab(tabId)
                _state.update { it.copy(activeTabId = tabId, status = null) }
                ensureHistoryLoaded(tabId)
                ensureBufferLoaded(tabId)
            } catch (err: ApiException) {
                setStatus(err.message ?: "activate failed", StatusLevel.Error)
            }
        }
    }

    fun submitPrompt(tabId: String?, input: String) {
        viewModelScope.launch {
            setBusy(true)
            try {
                awaitStreamReady()
                var resolvedTabId = tabId
                if (resolvedTabId.isNullOrBlank()) {
                    refreshSnapshotFallback()
                    resolvedTabId = _state.value.activeTabId
                }
                repository.submitPrompt(resolvedTabId, input)
                if (!resolvedTabId.isNullOrBlank()) {
                    appendHistoryLocal(resolvedTabId, input)
                }
                _state.update { it.copy(status = null) }
                if (isStreamDisconnected()) {
                    refreshSnapshotFallback()
                }
            } catch (err: ApiException) {
                setStatus(err.message ?: "prompt failed", StatusLevel.Error)
            } finally {
                setBusy(false)
            }
        }
    }

    fun loadHistory(tabId: String) {
        viewModelScope.launch {
            try {
                val resp = repository.getHistory(tabId)
                _state.update { state ->
                    val next = state.history.toMutableMap()
                    next[tabId] = HistoryState(entries = resp.entries.orEmpty(), index = -1, loaded = true)
                    state.copy(history = next)
                }
            } catch (err: ApiException) {
                setStatus(err.message ?: "history failed", StatusLevel.Error)
            }
        }
    }

    fun loadBuffer(tabId: String) {
        viewModelScope.launch {
            try {
                val buffer = repository.getBuffer(tabId, MAX_LINES).buffer
                _state.update { state ->
                    val next = state.buffers.toMutableMap()
                    next[tabId] = buffer.lines
                    state.copy(buffers = next)
                }
            } catch (err: ApiException) {
                setStatus(err.message ?: "buffer failed", StatusLevel.Error)
            }
        }
    }

    fun navigateHistory(tabId: String, direction: Int, currentInput: String): String? {
        val entry = _state.value.history[tabId] ?: return null
        if (!entry.loaded) {
            return null
        }
        val entries = entry.entries.toMutableList()
        var idx = entry.index
        if (currentInput.isNotBlank()) {
            val last = entries.lastOrNull()
            if (last != currentInput) {
                entries.add(currentInput)
            }
        }
        if (entries.isEmpty()) {
            return null
        }
        idx = when {
            idx == -1 && direction < 0 -> entries.size - 1
            idx == -1 && direction > 0 -> entries.size - 1
            direction < 0 && idx > 0 -> idx - 1
            direction > 0 && idx < entries.size - 1 -> idx + 1
            else -> idx
        }
        if (idx < 0 || idx >= entries.size) {
            return null
        }
        val nextValue = entries[idx]
        _state.update { state ->
            val next = state.history.toMutableMap()
            next[tabId] = entry.copy(entries = entries, index = idx, loaded = true)
            state.copy(history = next)
        }
        return nextValue
    }

    fun resetHistoryIndex(tabId: String) {
        _state.update { state ->
            val entry = state.history[tabId] ?: return@update state
            val next = state.history.toMutableMap()
            next[tabId] = entry.copy(index = -1)
            state.copy(history = next)
        }
    }

    private suspend fun checkSession() {
        try {
            val resp = repository.me()
            _state.update {
                it.copy(
                    username = resp.username,
                    loggedIn = true,
                    loginError = null,
                )
            }
            startStream()
            refreshSnapshotAfterLogin()
        } catch (_: Exception) {
            resetClientState()
        }
    }

    private fun startStream() {
        stopStream()
        streamJob = viewModelScope.launch {
            while (true) {
                try {
                    repository.streamEvents(lastSeq).collectLatest { event ->
                        clearStreamWarning()
                        handleStreamEvent(event)
                    }
                } catch (err: Exception) {
                    if (!isActive) return@launch
                }
                if (!isActive) return@launch
                setStreamDisconnected()
                delay(RETRY_DELAY_MS)
            }
        }
    }

    private fun setStreamDisconnected() {
        _state.update { state ->
            val current = state.status?.message
            if (!current.isNullOrBlank() && current != STREAM_DISCONNECTED_MESSAGE) {
                state
            } else {
                state.copy(status = StatusMessage(STREAM_DISCONNECTED_MESSAGE, StatusLevel.Warn))
            }
        }
        streamReady.value = false
        startBufferPolling()
    }

    private fun clearStreamWarning() {
        _state.update { state ->
            if (state.status?.message == STREAM_DISCONNECTED_MESSAGE) {
                state.copy(status = null)
            } else {
                state
            }
        }
        stopBufferPolling()
    }

    private fun stopStream() {
        streamJob?.cancel()
        streamJob = null
        streamReady.value = false
    }

    private fun handleStreamEvent(event: StreamEvent) {
        if (event.seq > 0) {
            lastSeq = event.seq
        }
        if (!streamReady.value) {
            streamReady.value = true
        }
        when (event.type) {
            "snapshot" -> applySnapshot(event)
            "tab" -> applyTabEvent(event)
            "output" -> appendLines(event.tabId, event.lines)
            "system" -> appendSystem(event.lines)
        }
    }

    private fun applySnapshot(event: StreamEvent) {
        val snapshot = event.snapshot ?: return
        val activeTab = resolveActiveTab(
            current = _state.value.activeTabId,
            candidate = snapshot.activeTab,
            tabs = snapshot.tabs,
        )
        _state.update {
            val buffers = snapshot.buffers.mapValues { it.value.lines }
            it.copy(
                tabs = snapshot.tabs,
                activeTabId = activeTab,
                buffers = buffers,
                systemLines = snapshot.system.lines,
                theme = snapshot.theme,
                status = null,
                history = emptyMap(),
            )
        }
        if (!activeTab.isNullOrBlank()) {
            ensureHistoryLoaded(activeTab)
        }
    }

    private fun applyTabEvent(event: StreamEvent) {
        val tab = event.tab ?: return
        val tabEvent = event.tabEvent.orEmpty()
        _state.update { state ->
            val tabs = state.tabs.toMutableList()
            val existingIdx = tabs.indexOfFirst { it.id == tab.id }
            var activeTab = state.activeTabId
            if (tabEvent == "closed") {
                if (existingIdx >= 0) {
                    tabs.removeAt(existingIdx)
                }
                if (activeTab == tab.id) {
                    activeTab = null
                }
            } else {
                if (existingIdx >= 0) {
                    tabs[existingIdx] = tab
                } else {
                    tabs.add(tab)
                }
            }
            if (activeTab != null && tabs.none { it.id == activeTab }) {
                activeTab = null
            }
            if (activeTab == null && tabs.isNotEmpty()) {
                activeTab = tabs.firstOrNull()?.id
            }
            val buffers = state.buffers.toMutableMap()
            if (tabEvent == "closed") {
                buffers.remove(tab.id)
            }
            val history = state.history.toMutableMap()
            if (tabEvent == "closed") {
                history.remove(tab.id)
            }
            state.copy(
                tabs = tabs,
                activeTabId = activeTab,
                buffers = buffers,
                history = history,
                theme = event.theme ?: state.theme,
            )
        }
        if (tabEvent != "closed") {
            ensureHistoryLoaded(tab.id)
        }
    }

    private fun appendLines(tabId: String?, lines: List<String>) {
        if (tabId.isNullOrBlank() || lines.isEmpty()) return
        _state.update { state ->
            val nextBuffers = state.buffers.toMutableMap()
            val existing = nextBuffers[tabId].orEmpty()
            val merged = (existing + lines)
            val trimmed = if (merged.size > MAX_LINES) {
                merged.takeLast(MAX_LINES)
            } else {
                merged
            }
            nextBuffers[tabId] = trimmed
            state.copy(buffers = nextBuffers)
        }
    }

    private fun appendSystem(lines: List<String>) {
        if (lines.isEmpty()) return
        _state.update { state ->
            val merged = state.systemLines + lines
            val trimmed = if (merged.size > MAX_LINES) {
                merged.takeLast(MAX_LINES)
            } else {
                merged
            }
            state.copy(systemLines = trimmed)
        }
    }

    private fun ensureHistoryLoaded(tabId: String) {
        val existing = _state.value.history[tabId]
        if (existing?.loaded == true) return
        loadHistory(tabId)
    }

    private fun ensureBufferLoaded(tabId: String) {
        val hasBuffer = _state.value.buffers.containsKey(tabId)
        if (hasBuffer) return
        loadBuffer(tabId)
    }

    private fun appendHistoryLocal(tabId: String, entry: String) {
        if (entry.isBlank()) return
        _state.update { state ->
            val next = state.history.toMutableMap()
            val existing = next[tabId] ?: HistoryState()
            val entries = existing.entries.toMutableList()
            val last = entries.lastOrNull()
            if (last != entry) {
                entries.add(entry)
            }
            next[tabId] = existing.copy(entries = entries, index = -1, loaded = true)
            state.copy(history = next)
        }
    }

    private fun resetClientState() {
        lastSeq = 0
        stopBufferPolling()
        streamReady.value = false
        _state.update {
            it.copy(
                username = null,
                loggedIn = false,
                tabs = emptyList(),
                activeTabId = null,
                buffers = emptyMap(),
                systemLines = emptyList(),
                status = null,
                theme = null,
                history = emptyMap(),
                showChpasswd = false,
                showCodexAuth = false,
                showRotateSSHKey = false,
                loginError = null,
                chpasswdError = null,
                codexAuthError = null,
                rotateSSHKeyError = null,
            )
        }
    }

    private fun setStatus(message: String, level: StatusLevel) {
        _state.update { it.copy(status = StatusMessage(message, level)) }
    }

    private fun setBusy(busy: Boolean) {
        _state.update { it.copy(isBusy = busy) }
    }

    private fun startBufferPolling() {
        if (bufferPollJob?.isActive == true) return
        bufferPollJob = viewModelScope.launch {
            while (isActive) {
                try {
                    refreshSnapshotFallback()
                } catch (_: Exception) {
                    // keep quiet; stream status already communicates issues
                }
                delay(BUFFER_POLL_MS)
            }
        }
    }

    private fun stopBufferPolling() {
        bufferPollJob?.cancel()
        bufferPollJob = null
    }

    private suspend fun refreshSnapshotFallback() {
        val tabsResp = repository.listTabs()
        val active = resolveActiveTab(
            current = _state.value.activeTabId,
            candidate = tabsResp.activeTab,
            tabs = tabsResp.tabs,
        )
        val buffers = _state.value.buffers.toMutableMap()
        if (!active.isNullOrBlank()) {
            val buffer = repository.getBuffer(active, MAX_LINES).buffer
            buffers[active] = buffer.lines
        }
        val systemLines = repository.getSystemBuffer(MAX_LINES).buffer.lines
        _state.update { state ->
            state.copy(
                tabs = tabsResp.tabs,
                activeTabId = active,
                buffers = buffers,
                systemLines = systemLines,
                theme = tabsResp.theme ?: state.theme,
            )
        }
    }

    private suspend fun refreshSnapshotAfterLogin() {
        try {
            refreshSnapshotFallback()
        } catch (_: Exception) {
            // Stream will retry; no need to surface transient errors here.
        }
    }

    private suspend fun awaitStreamReady(timeoutMs: Long = STREAM_READY_TIMEOUT_MS) {
        if (streamReady.value) return
        withTimeoutOrNull(timeoutMs) {
            streamReady.filter { it }.first()
        }
    }

    private fun isStreamDisconnected(): Boolean {
        return _state.value.status?.message == STREAM_DISCONNECTED_MESSAGE
    }

    private fun clearLoginError() {
        _state.update { it.copy(loginError = null) }
    }

    private fun resolveActiveTab(current: String?, candidate: String?, tabs: List<TabSnapshot>): String? {
        val ids = tabs.map { it.id }.toSet()
        if (!current.isNullOrBlank() && ids.contains(current)) {
            return current
        }
        if (!candidate.isNullOrBlank() && ids.contains(candidate)) {
            return candidate
        }
        return tabs.firstOrNull()?.id
    }

    companion object {
        private const val MAX_LINES = 2000
        private const val RETRY_DELAY_MS = 3000L
        private const val BUFFER_POLL_MS = 3000L
        private const val STREAM_DISCONNECTED_MESSAGE = "stream disconnected (polling output)"
        private const val STREAM_READY_TIMEOUT_MS = 8000L
    }
}
