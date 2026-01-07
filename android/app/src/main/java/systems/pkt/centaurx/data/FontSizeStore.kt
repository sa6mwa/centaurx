package systems.pkt.centaurx.data

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.intPreferencesKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import systems.pkt.centaurx.DefaultTerminalFontSizeSp
import systems.pkt.centaurx.MaxTerminalFontSizeSp
import systems.pkt.centaurx.MinTerminalFontSizeSp

class FontSizeStore(
    private val dataStore: DataStore<Preferences>,
    private val scope: CoroutineScope,
) {
    private val fontSizeKey = intPreferencesKey("terminal_font_size_sp")

    val fontSizeFlow: Flow<Int> = dataStore.data.map { prefs ->
        normalize(prefs[fontSizeKey] ?: DefaultTerminalFontSizeSp)
    }

    suspend fun getFontSize(): Int {
        return fontSizeFlow.first()
    }

    fun setFontSize(value: Int) {
        val normalized = normalize(value)
        scope.launch(Dispatchers.IO) {
            dataStore.edit { prefs ->
                prefs[fontSizeKey] = normalized
            }
        }
    }

    private fun normalize(value: Int): Int {
        return value.coerceIn(MinTerminalFontSizeSp, MaxTerminalFontSizeSp)
    }
}
