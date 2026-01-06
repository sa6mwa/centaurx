package systems.pkt.centaurx.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

private val Context.dataStore by preferencesDataStore(name = "centaurx")

class DebugEndpointReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_SET_ENDPOINT) return
        val endpoint = intent.getStringExtra(EXTRA_ENDPOINT)?.trim().orEmpty()
        if (endpoint.isBlank()) return
        val pending = goAsync()
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        scope.launch {
            context.dataStore.edit { prefs ->
                prefs[ENDPOINT_KEY] = endpoint
            }
            pending.finish()
        }
    }

    companion object {
        const val ACTION_SET_ENDPOINT = "systems.pkt.centaurx.DEBUG_SET_ENDPOINT"
        const val EXTRA_ENDPOINT = "endpoint"
        private val ENDPOINT_KEY = stringPreferencesKey("api_base_url")
    }
}
