package systems.pkt.centaurx.data

import kotlinx.coroutines.flow.Flow

interface CentaurxClient {
    val endpointFlow: Flow<String>
    val fontSizeFlow: Flow<Int>

    fun setEndpoint(value: String)
    fun setFontSize(value: Int)

    suspend fun login(username: String, password: String, totp: String): LoginResponse
    suspend fun logout()
    suspend fun me(): LoginResponse
    suspend fun listTabs(): ListTabsResponse
    suspend fun activateTab(tabId: String)
    suspend fun submitPrompt(tabId: String?, input: String)
    suspend fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    )
    suspend fun uploadCodexAuth(rawJson: String)
    suspend fun getHistory(tabId: String?): HistoryResponse
    suspend fun getBuffer(tabId: String?, limit: Int? = null): BufferResponse
    suspend fun getSystemBuffer(limit: Int? = null): SystemBufferResponse
    suspend fun appendHistory(tabId: String, entry: String): HistoryResponse
    suspend fun streamEvents(lastEventId: Long? = null): Flow<StreamEvent>
}
