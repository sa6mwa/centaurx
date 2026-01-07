package systems.pkt.centaurx.data

import kotlinx.coroutines.flow.Flow
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

class CentaurxRepository(
    private val apiClient: ApiClient,
    private val streamClient: StreamClient,
    private val endpointStore: EndpointStore,
    private val fontSizeStore: FontSizeStore,
) : CentaurxClient {
    override val endpointFlow: Flow<String> = endpointStore.endpointFlow
    override val fontSizeFlow: Flow<Int> = fontSizeStore.fontSizeFlow

    override fun setEndpoint(value: String) {
        endpointStore.setEndpoint(value)
    }

    override fun setFontSize(value: Int) {
        fontSizeStore.setFontSize(value)
    }

    suspend fun currentEndpoint(): String {
        return endpointStore.getEndpoint()
    }

    suspend fun baseUrl(): HttpUrl {
        val raw = endpointStore.getEndpoint().trim()
        return raw.toHttpUrlOrNull() ?: throw ApiException("invalid endpoint URL")
    }

    override suspend fun login(username: String, password: String, totp: String): LoginResponse {
        return apiClient.login(username, password, totp)
    }

    override suspend fun logout() {
        apiClient.logout()
    }

    override suspend fun me(): LoginResponse {
        return apiClient.me()
    }

    override suspend fun listTabs(): ListTabsResponse {
        return apiClient.listTabs()
    }

    override suspend fun activateTab(tabId: String) {
        apiClient.activateTab(tabId)
    }

    override suspend fun submitPrompt(tabId: String?, input: String) {
        apiClient.submitPrompt(tabId, input)
    }

    override suspend fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    ) {
        apiClient.changePassword(currentPassword, newPassword, confirmPassword, totp)
    }

    override suspend fun uploadCodexAuth(rawJson: String) {
        apiClient.uploadCodexAuth(rawJson)
    }

    override suspend fun getHistory(tabId: String?): HistoryResponse {
        return apiClient.getHistory(tabId)
    }

    override suspend fun getBuffer(tabId: String?, limit: Int?): BufferResponse {
        return apiClient.getBuffer(tabId, limit)
    }

    override suspend fun getSystemBuffer(limit: Int?): SystemBufferResponse {
        return apiClient.getSystemBuffer(limit)
    }

    override suspend fun appendHistory(tabId: String, entry: String): HistoryResponse {
        return apiClient.appendHistory(tabId, entry)
    }

    override suspend fun streamEvents(lastEventId: Long?): Flow<StreamEvent> {
        return streamClient.streamEvents(baseUrl(), lastEventId)
    }
}
