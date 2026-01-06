package systems.pkt.centaurx.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

class EndpointStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val endpointKey = stringPreferencesKey("api_base_url")
    private val overrideFlow = MutableStateFlow<String?>(null)

    private val storedFlow: Flow<String?> = dataStore.data.map { prefs ->
        prefs[endpointKey]
    }
    val endpointFlow: Flow<String> = combine(storedFlow, overrideFlow) { stored, override ->
        override ?: stored ?: DEFAULT_ENDPOINT
    }

    suspend fun getEndpoint(): String {
        return endpointFlow.first()
    }

    fun setEndpoint(value: String) {
        val cleaned = value.trim()
        overrideFlow.value = cleaned
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[endpointKey] = cleaned
            }
        }
    }

    companion object {
        const val DEFAULT_ENDPOINT = "http://localhost:27480"
    }
}
