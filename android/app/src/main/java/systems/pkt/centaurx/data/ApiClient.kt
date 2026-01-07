package systems.pkt.centaurx.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

class ApiClient(
    private val httpClient: OkHttpClient,
    private val endpointStore: EndpointStore,
) {
    suspend fun login(username: String, password: String, totp: String): LoginResponse {
        val payload = mapOf(
            "username" to username,
            "password" to password,
            "totp" to totp,
        )
        val request = buildPost("api/login", payload)
        return executeJson(request)
    }

    suspend fun logout() {
        val request = buildPost("api/logout", emptyMap<String, String>())
        executeJson<Unit>(request)
    }

    suspend fun me(): LoginResponse {
        val request = buildGet("api/me")
        return executeJson(request)
    }

    suspend fun listTabs(): ListTabsResponse {
        val request = buildGet("api/tabs")
        return executeJson(request)
    }

    suspend fun activateTab(tabId: String) {
        val payload = mapOf("tab_id" to tabId)
        val request = buildPost("api/tabs/activate", payload)
        executeJson<Unit>(request)
    }

    suspend fun submitPrompt(tabId: String?, input: String) {
        val payload = mapOf(
            "tab_id" to (tabId ?: ""),
            "input" to input,
        )
        val request = buildPost("api/prompt", payload)
        executeJson<Unit>(request)
    }

    suspend fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    ) {
        val payload = mapOf(
            "current_password" to currentPassword,
            "new_password" to newPassword,
            "confirm_password" to confirmPassword,
            "totp" to totp,
        )
        val request = buildPost("api/chpasswd", payload)
        executeJson<Unit>(request)
    }

    suspend fun uploadCodexAuth(rawJson: String) {
        val url = buildUrl("api/codexauth")
        val body = rawJson.toRequestBody(JSON)
        val request = Request.Builder().url(url).post(body).build()
        executeJson<Unit>(request)
    }

    suspend fun getHistory(tabId: String?): HistoryResponse {
        val url = buildUrl("api/history") { builder ->
            if (!tabId.isNullOrBlank()) {
                builder.addQueryParameter("tab_id", tabId)
            }
        }
        val request = Request.Builder().url(url).get().build()
        return executeJson(request)
    }

    suspend fun getBuffer(tabId: String?, limit: Int? = null): BufferResponse {
        val url = buildUrl("api/buffer") { builder ->
            if (!tabId.isNullOrBlank()) {
                builder.addQueryParameter("tab_id", tabId)
            }
            if (limit != null) {
                builder.addQueryParameter("limit", limit.toString())
            }
        }
        val request = Request.Builder().url(url).get().build()
        return executeJson(request)
    }

    suspend fun getSystemBuffer(limit: Int? = null): SystemBufferResponse {
        val url = buildUrl("api/system") { builder ->
            if (limit != null) {
                builder.addQueryParameter("limit", limit.toString())
            }
        }
        val request = Request.Builder().url(url).get().build()
        return executeJson(request)
    }

    suspend fun appendHistory(tabId: String, entry: String): HistoryResponse {
        val payload = mapOf("tab_id" to tabId, "entry" to entry)
        val request = buildPost("api/history", payload)
        return executeJson(request)
    }

    private suspend fun buildGet(path: String): Request {
        val url = buildUrl(path)
        return Request.Builder().url(url).get().build()
    }

    private suspend fun buildPost(path: String, payload: Map<String, String>): Request {
        val url = buildUrl(path)
        val jsonBody = CentaurxJson.encodeToString(payload)
        val body = jsonBody.toRequestBody(JSON)
        return Request.Builder().url(url).post(body).build()
    }

    private suspend fun buildUrl(path: String, configure: ((HttpUrl.Builder) -> Unit)? = null): HttpUrl {
        val rawBase = endpointStore.getEndpoint().trim()
        val base = rawBase.toHttpUrlOrNull() ?: throw ApiException("invalid endpoint URL")
        val builder = base.newBuilder()
        val cleaned = path.trimStart('/')
        builder.addPathSegments(cleaned)
        configure?.invoke(builder)
        return builder.build()
    }

    private suspend inline fun <reified T> executeJson(request: Request): T {
        return withContext(Dispatchers.IO) {
            httpClient.newCall(request).execute().use { response ->
                val body = response.body?.string().orEmpty()
                if (!response.isSuccessful) {
                    val message = runCatching {
                        CentaurxJson.decodeFromString(ErrorResponse.serializer(), body).error
                    }.getOrNull() ?: "request failed"
                    throw ApiException(message, response.code)
                }
                if (T::class == Unit::class) {
                    @Suppress("UNCHECKED_CAST")
                    return@use Unit as T
                }
                CentaurxJson.decodeFromString(body)
            }
        }
    }

    companion object {
        private val JSON = "application/json; charset=utf-8".toMediaType()
    }
}

class ApiException(message: String, val statusCode: Int? = null) : Exception(message)
