package systems.pkt.centaurx.data

import kotlinx.serialization.json.Json

val CentaurxJson = Json {
    ignoreUnknownKeys = true
    isLenient = true
    explicitNulls = false
}
