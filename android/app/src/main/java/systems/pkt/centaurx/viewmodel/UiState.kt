package systems.pkt.centaurx.viewmodel

import systems.pkt.centaurx.DefaultTerminalFontSizeSp
import systems.pkt.centaurx.data.TabSnapshot

enum class StatusLevel {
    Info,
    Warn,
    Error,
}

data class StatusMessage(
    val message: String,
    val level: StatusLevel = StatusLevel.Info,
)

data class HistoryState(
    val entries: List<String> = emptyList(),
    val index: Int = -1,
    val loaded: Boolean = false,
)

data class UiState(
    val endpoint: String = "",
    val username: String? = null,
    val loggedIn: Boolean = false,
    val tabs: List<TabSnapshot> = emptyList(),
    val activeTabId: String? = null,
    val buffers: Map<String, List<String>> = emptyMap(),
    val systemLines: List<String> = emptyList(),
    val status: StatusMessage? = null,
    val theme: String? = null,
    val loginError: String? = null,
    val chpasswdError: String? = null,
    val codexAuthError: String? = null,
    val rotateSSHKeyError: String? = null,
    val isBusy: Boolean = false,
    val showSettings: Boolean = false,
    val showFontSize: Boolean = false,
    val showChpasswd: Boolean = false,
    val showCodexAuth: Boolean = false,
    val showRotateSSHKey: Boolean = false,
    val fontSizeSp: Int = DefaultTerminalFontSizeSp,
    val history: Map<String, HistoryState> = emptyMap(),
)
