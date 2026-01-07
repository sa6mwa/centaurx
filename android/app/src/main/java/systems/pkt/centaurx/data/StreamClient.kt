package systems.pkt.centaurx.data

import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.time.Duration

class StreamClient(private val httpClient: OkHttpClient) {
    private val sseClient = httpClient.newBuilder()
        .readTimeout(Duration.ZERO)
        .retryOnConnectionFailure(true)
        .build()
    private val eventFactory = EventSources.createFactory(sseClient)

    fun streamEvents(baseUrl: HttpUrl, lastEventId: Long? = null): Flow<StreamEvent> = callbackFlow {
        val url = baseUrl.newBuilder().addPathSegments("api/stream").build()
        val requestBuilder = Request.Builder()
            .url(url)
            .header("Accept", "text/event-stream")
        if (lastEventId != null && lastEventId > 0) {
            requestBuilder.header("Last-Event-ID", lastEventId.toString())
        }
        val request = requestBuilder.build()
        val eventSource = eventFactory.newEventSource(request, object : EventSourceListener() {
            override fun onEvent(
                eventSource: EventSource,
                id: String?,
                type: String?,
                data: String,
            ) {
                val event = runCatching {
                    CentaurxJson.decodeFromString(StreamEvent.serializer(), data)
                }.getOrNull() ?: return
                trySend(event)
            }

            override fun onFailure(
                eventSource: EventSource,
                t: Throwable?,
                response: okhttp3.Response?,
            ) {
                if (response?.code == 401) {
                    close(ApiException("invalid session", response.code))
                    return
                }
                close(t)
            }

            override fun onClosed(eventSource: EventSource) {
                close()
            }
        })
        awaitClose { eventSource.cancel() }
    }
}
