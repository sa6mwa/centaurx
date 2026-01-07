package systems.pkt.centaurx.data

import kotlinx.coroutines.flow.Flow
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

class CentaurxRepository(
    private val apiClient: ApiClient,
    private val streamClient: StreamClient,
    private val endpointStore: EndpointStore,
    private val fontSizeStore: FontSizeStore,
) {
    val endpointFlow: Flow<String> = endpointStore.endpointFlow
    val fontSizeFlow: Flow<Int> = fontSizeStore.fontSizeFlow

    fun setEndpoint(value: String) {
        endpointStore.setEndpoint(value)
    }

    fun setFontSize(value: Int) {
        fontSizeStore.setFontSize(value)
    }

    suspend fun currentEndpoint(): String {
        return endpointStore.getEndpoint()
    }

    suspend fun baseUrl(): HttpUrl {
        val raw = endpointStore.getEndpoint().trim()
        return raw.toHttpUrlOrNull() ?: throw ApiException("invalid endpoint URL")
    }

    suspend fun login(username: String, password: String, totp: String): LoginResponse {
        return apiClient.login(username, password, totp)
    }

    suspend fun logout() {
        apiClient.logout()
    }

    suspend fun me(): LoginResponse {
        return apiClient.me()
    }

    suspend fun listTabs(): ListTabsResponse {
        return apiClient.listTabs()
    }

    suspend fun activateTab(tabId: String) {
        apiClient.activateTab(tabId)
    }

    suspend fun submitPrompt(tabId: String?, input: String) {
        apiClient.submitPrompt(tabId, input)
    }

    suspend fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    ) {
        apiClient.changePassword(currentPassword, newPassword, confirmPassword, totp)
    }

    suspend fun uploadCodexAuth(rawJson: String) {
        apiClient.uploadCodexAuth(rawJson)
    }

    suspend fun getHistory(tabId: String?): HistoryResponse {
        return apiClient.getHistory(tabId)
    }

    suspend fun getBuffer(tabId: String?, limit: Int? = null): BufferResponse {
        return apiClient.getBuffer(tabId, limit)
    }

    suspend fun getSystemBuffer(limit: Int? = null): SystemBufferResponse {
        return apiClient.getSystemBuffer(limit)
    }

    suspend fun appendHistory(tabId: String, entry: String): HistoryResponse {
        return apiClient.appendHistory(tabId, entry)
    }

    suspend fun streamEvents(lastEventId: Long? = null): Flow<StreamEvent> {
        return streamClient.streamEvents(baseUrl(), lastEventId)
    }
}
