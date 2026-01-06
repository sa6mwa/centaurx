package systems.pkt.centaurx

import android.app.Application
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import okhttp3.OkHttpClient
import systems.pkt.centaurx.data.ApiClient
import systems.pkt.centaurx.data.CentaurxRepository
import systems.pkt.centaurx.data.EndpointStore
import systems.pkt.centaurx.data.PersistentCookieJar
import systems.pkt.centaurx.data.StreamClient

private val Application.dataStore by preferencesDataStore(name = "centaurx")

class CentaurxApplication : Application() {
    lateinit var repository: CentaurxRepository
        private set

    private val appScope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onCreate() {
        super.onCreate()
        val endpointStore = EndpointStore(dataStore, appScope)
        val cookieJar = PersistentCookieJar(dataStore, appScope)
        val httpClient = OkHttpClient.Builder()
            .cookieJar(cookieJar)
            .build()
        val apiClient = ApiClient(httpClient, endpointStore)
        val streamClient = StreamClient(httpClient)
        repository = CentaurxRepository(apiClient, streamClient, endpointStore)
    }
}
