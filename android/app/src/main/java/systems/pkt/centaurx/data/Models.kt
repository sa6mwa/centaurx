@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package systems.pkt.centaurx.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonNames

@Serializable
enum class TabStatus {
    @SerialName("idle")
    Idle,
    @SerialName("running")
    Running,
    @SerialName("stopped")
    Stopped,
}

@Serializable
data class RepoRef(
    @JsonNames("name", "Name")
    val name: String? = null,
    @JsonNames("path", "Path")
    val path: String? = null,
)

@Serializable
data class TabSnapshot(
    @JsonNames("id", "ID")
    val id: String = "",
    @JsonNames("name", "Name")
    val name: String? = null,
    @JsonNames("repo", "Repo")
    val repo: RepoRef? = null,
    @JsonNames("model", "Model")
    val model: String? = null,
    @JsonNames("model_reasoning_effort", "ModelReasoningEffort")
    val modelReasoningEffort: String? = null,
    @JsonNames("session_id", "SessionID")
    val sessionId: String? = null,
    @JsonNames("status", "Status")
    val status: TabStatus? = null,
    @JsonNames("active", "Active")
    val active: Boolean = false,
)

@Serializable
data class BufferSnapshot(
    @JsonNames("tab_id", "TabID")
    val tabId: String? = null,
    @JsonNames("lines", "Lines")
    val lines: List<String> = emptyList(),
    @JsonNames("total_lines", "TotalLines")
    val totalLines: Int = 0,
    @JsonNames("scroll_offset", "ScrollOffset")
    val scrollOffset: Int = 0,
    @JsonNames("at_bottom", "AtBottom")
    val atBottom: Boolean = true,
)

@Serializable
data class SystemBufferSnapshot(
    @JsonNames("lines", "Lines")
    val lines: List<String> = emptyList(),
    @JsonNames("total_lines", "TotalLines")
    val totalLines: Int = 0,
    @JsonNames("scroll_offset", "ScrollOffset")
    val scrollOffset: Int = 0,
    @JsonNames("at_bottom", "AtBottom")
    val atBottom: Boolean = true,
)

@Serializable
data class SnapshotPayload(
    @JsonNames("tabs", "Tabs")
    val tabs: List<TabSnapshot> = emptyList(),
    @JsonNames("active_tab", "ActiveTab")
    val activeTab: String? = null,
    @JsonNames("buffers", "Buffers")
    val buffers: Map<String, BufferSnapshot> = emptyMap(),
    @JsonNames("system", "System")
    val system: SystemBufferSnapshot = SystemBufferSnapshot(),
    @JsonNames("theme", "Theme")
    val theme: String? = null,
)

@Serializable
data class StreamEvent(
    @JsonNames("seq", "Seq")
    val seq: Long = 0,
    @JsonNames("type", "Type")
    val type: String = "",
    @JsonNames("tab_event", "TabEvent")
    val tabEvent: String? = null,
    @JsonNames("tab_id", "TabID")
    val tabId: String? = null,
    @JsonNames("lines", "Lines")
    val lines: List<String> = emptyList(),
    @JsonNames("tab", "Tab")
    val tab: TabSnapshot? = null,
    @JsonNames("active_tab", "ActiveTab")
    val activeTab: String? = null,
    @JsonNames("theme", "Theme")
    val theme: String? = null,
    @JsonNames("snapshot", "Snapshot")
    val snapshot: SnapshotPayload? = null,
    @JsonNames("timestamp", "Timestamp")
    val timestamp: String? = null,
)

@Serializable
data class LoginResponse(
    @SerialName("username")
    val username: String = "",
)

@Serializable
data class ErrorResponse(
    @SerialName("error")
    val error: String = "request failed",
)

@Serializable
data class ListTabsResponse(
    @JsonNames("tabs", "Tabs")
    val tabs: List<TabSnapshot> = emptyList(),
    @JsonNames("active_tab", "ActiveTab")
    val activeTab: String? = null,
    @JsonNames("theme", "Theme")
    val theme: String? = null,
)

@Serializable
data class HistoryResponse(
    @JsonNames("entries", "Entries")
    val entries: List<String>? = emptyList(),
)

@Serializable
data class BufferResponse(
    @JsonNames("buffer", "Buffer")
    val buffer: BufferSnapshot = BufferSnapshot(),
)

@Serializable
data class SystemBufferResponse(
    @JsonNames("buffer", "Buffer")
    val buffer: SystemBufferSnapshot = SystemBufferSnapshot(),
)
