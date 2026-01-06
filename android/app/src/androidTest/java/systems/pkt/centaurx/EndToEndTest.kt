package systems.pkt.centaurx

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasTestTag
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isSelected
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onAllNodesWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import java.net.HttpURLConnection
import java.net.URL
import java.util.Locale
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import systems.pkt.centaurx.data.CentaurxJson
import systems.pkt.centaurx.data.ListTabsResponse
import systems.pkt.centaurx.data.TabSnapshot
import systems.pkt.centaurx.ui.TestTags

@RunWith(AndroidJUnit4::class)
class EndToEndTest {
    @get:Rule
    val composeRule = createAndroidComposeRule<MainActivity>()

    @Before
    fun ensureBackendReachable() {
        assertBackendReachable(EMULATOR_ENDPOINT)
    }

    @Test
    fun login_help_status_chpasswd() {
        assertTopBarSafe()
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        loginWithDefaultAdmin()
        waitForTag(TestTags.TerminalPrompt)
        assertSendButtonLabelVisible()

        sendCommand("/help")
        waitForDisplayedText("! <cmd> - run a shell command in the repo")

        val repoName = uniqueRepoName()
        sendCommand("/new $repoName")
        val tab = fetchTabForRepo(repoName)
        waitForTabVisible(tab.id)
        clickTab(tab.id)
        composeRule.waitForIdle()
        Thread.sleep(UI_SETTLE_DELAY_MS)
        waitForTextContains(repoName)

        sendStatusAndWait()

        sendCommand("/chpasswd")
        waitForText("Change password")
        composeRule.onNodeWithText("Cancel").performClick()
        waitForTextToDisappear("Change password")

        sendCommand("/rotatesshkey")
        waitForTag(TestTags.RotateSSHKeyInput)
        composeRule.onNodeWithTag(TestTags.RotateSSHKeyInput).performTextReplacement("NO")
        composeRule.onNodeWithTag(TestTags.RotateSSHKeyConfirm).performClick()
        waitForTextContains("type YES")
        composeRule.onNodeWithTag(TestTags.RotateSSHKeyInput).performTextReplacement("YES")
        composeRule.onNodeWithTag(TestTags.RotateSSHKeyConfirm).performClick()
        waitForDisplayedText("ssh key rotated")
    }

    @Test
    fun login_with_invalid_credentials_shows_error() {
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        attemptLogin("admin", "wrong-password", generateTotp(DEFAULT_TOTP_SECRET))
        waitForTag(TestTags.LoginError)
        waitForTag(TestTags.LoginUsername)
    }

    @Test
    fun login_with_unreachable_endpoint_shows_error() {
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        setEndpoint(UNREACHABLE_ENDPOINT)
        attemptLogin("admin", "admin", generateTotp(DEFAULT_TOTP_SECRET))
        waitForTag(TestTags.LoginError)
        waitForTag(TestTags.LoginUsername)
    }

    @Test
    fun tab_history_loads_on_switch_after_relogin() {
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        loginWithDefaultAdmin()
        val repoA = uniqueRepoName()
        sendCommand("/new $repoA")
        val tabA = fetchTabForRepo(repoA)
        waitForTabVisible(tabA.id)
        val repoB = uniqueRepoName()
        sendCommand("/new $repoB")
        val tabB = fetchTabForRepo(repoB)
        waitForTabVisible(tabB.id)

        logoutViaMenu()
        waitForTag(TestTags.LoginUsername)

        loginWithDefaultAdmin()
        waitForTabVisible(tabA.id)
        clickTab(tabA.id)
        composeRule.waitForIdle()
        Thread.sleep(UI_SETTLE_DELAY_MS)
        waitForTextContains(repoA)

        clickTab(tabB.id)
        composeRule.waitForIdle()
        Thread.sleep(UI_SETTLE_DELAY_MS)
        waitForTextContains(repoB)
    }

    @Test
    fun logout_does_not_cancel_shell_command() {
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        loginWithDefaultAdmin()
        val repoName = uniqueRepoName()
        sendCommand("/new $repoName")
        val tab = fetchTabForRepo(repoName)
        waitForTabVisible(tab.id)

        val marker = (System.currentTimeMillis() % 1_000_000L).toString().padStart(6, '0')
        val startMarker = "cx-e2e-start-$marker"
        val doneMarker = "cx-e2e-done-$marker"
        sendCommand("! sh -c \"echo $startMarker; sleep 2; echo $doneMarker\"")
        waitForTextContains(startMarker)

        logoutViaMenu()
        waitForTag(TestTags.LoginUsername)

        loginWithDefaultAdmin()
        waitForTextContains(doneMarker, timeoutMs = DEFAULT_TIMEOUT_MS + 10_000L)
    }

    @Test
    fun remote_tab_switch_does_not_change_local_active_tab() {
        setEndpoint(EMULATOR_ENDPOINT)
        ensureLoggedOut()

        loginWithDefaultAdmin()
        val repoA = uniqueRepoName()
        sendCommand("/new $repoA")
        val tabA = fetchTabForRepo(repoA)
        waitForTabVisible(tabA.id)
        val repoB = uniqueRepoName()
        sendCommand("/new $repoB")
        val tabB = fetchTabForRepo(repoB)
        waitForTabVisible(tabB.id)

        clickTab(tabA.id)
        composeRule.waitForIdle()
        assertTabSelected(tabA.id)

        runBlocking {
            activateTabInSecondarySession(repoB)
        }

        Thread.sleep(UI_SETTLE_DELAY_MS)
        assertTabSelected(tabA.id)
    }

    private fun setEndpoint(endpoint: String) {
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithTag(TestTags.EndpointInput).performTextReplacement(endpoint)
        composeRule.onNodeWithTag(TestTags.EndpointSave).performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
    }

    private fun ensureLoggedOut() {
        if (hasTag(TestTags.LoginUsername)) return
        waitForTag(TestTags.TerminalPrompt)
        sendCommand("/logout")
        waitForTag(TestTags.LoginUsername)
    }

    private fun logoutViaMenu() {
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        composeRule.onNodeWithTag(TestTags.LogoutButton).performClick()
    }

    private fun loginWithDefaultAdmin() {
        attemptLogin("admin", "admin", generateTotp(DEFAULT_TOTP_SECRET))
        waitForTag(TestTags.TerminalPrompt)
    }

    private fun attemptLogin(username: String, password: String, totp: String) {
        waitForTag(TestTags.LoginUsername)
        composeRule.onNodeWithTag(TestTags.LoginUsername).performTextReplacement(username)
        composeRule.onNodeWithTag(TestTags.LoginPassword).performTextReplacement(password)
        composeRule.onNodeWithTag(TestTags.LoginTotp).performTextReplacement(totp)
        composeRule.onNodeWithTag(TestTags.LoginSubmit).performClick()
    }

    private fun sendCommand(command: String) {
        composeRule.onNodeWithTag(TestTags.TerminalPrompt).performTextReplacement(command)
        composeRule.onNodeWithTag(TestTags.TerminalSend).performClick()
    }

    private fun waitForTag(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { hasTag(tag) }
    }

    private fun waitForTabVisible(tabId: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        val tabTag = TestTags.tabTag(tabId)
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (hasTag(tabTag)) return
            if (hasTag(TestTags.TabList)) {
                runCatching {
                    composeRule.onNodeWithTag(TestTags.TabList).performScrollToNode(hasTestTag(tabTag))
                }
            }
            composeRule.waitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("Timed out waiting for tab $tabId to become visible")
    }

    private fun clickTab(tabId: String) {
        waitForTabVisible(tabId)
        composeRule.onNode(
            hasTestTag(TestTags.tabTag(tabId)).and(hasClickAction()),
        ).performClick()
    }

    private fun assertTabSelected(tabId: String) {
        waitForTabVisible(tabId)
        composeRule.onNode(
            hasTestTag(TestTags.tabTag(tabId)).and(isSelected()),
        ).assertIsDisplayed()
    }


    private fun waitForTagToDisappear(tag: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { !hasTag(tag) }
    }

    private fun waitForText(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { hasTextNode(text) }
    }

    private fun waitForDisplayedText(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        runCatching {
            waitUntil(timeoutMs) {
                runCatching {
                    composeRule.onNodeWithText(text).assertIsDisplayed()
                    true
                }.getOrDefault(false)
            }
        }.onFailure {
            val foundAfterScroll = runCatching {
                composeRule.onNodeWithTag(TestTags.TerminalList).performScrollToNode(hasText(text))
                composeRule.onNodeWithText(text).assertIsDisplayed()
                true
            }.getOrDefault(false)
            val banner = statusBannerText().orEmpty()
            if (foundAfterScroll) {
                throw AssertionError(
                    "Output '$text' exists but terminal did not auto-scroll. Status: ${banner.ifBlank { "<none>" }}",
                )
            }
            throw AssertionError(
                "Output '$text' not found in terminal. Status: ${banner.ifBlank { "<none>" }}",
            )
        }
    }

    private fun waitForTextContains(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { hasTextContains(text) }
    }

    private fun sendStatusAndWait() {
        sendCommand("/status")
        waitForStatusOutput()
    }

    private fun waitForStatusOutput() {
        waitUntil(STATUS_WAIT_MS) { hasTextContains("Model:") || !statusBannerText().isNullOrBlank() }
        if (hasTextContains("Model:")) return
        val banner = statusBannerText().orEmpty()
        throw AssertionError("Expected stream output for /status, but none arrived. Status: $banner")
    }

    private fun waitForTextToDisappear(text: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS) {
        waitUntil(timeoutMs) { !hasTextNode(text) }
    }

    private fun hasTag(tag: String): Boolean {
        return composeRule.onAllNodesWithTag(tag).fetchSemanticsNodes().isNotEmpty()
    }

    private fun hasTextNode(text: String): Boolean {
        return composeRule.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()
    }

    private fun hasTextContains(text: String): Boolean {
        return composeRule.onAllNodesWithText(text, substring = true).fetchSemanticsNodes().isNotEmpty()
    }

    private fun statusBannerText(): String? {
        val nodes = composeRule.onAllNodesWithTag(TestTags.StatusBanner).fetchSemanticsNodes()
        if (nodes.isEmpty()) return null
        val text = runCatching { nodes.first().config[SemanticsProperties.Text] }.getOrNull()
        return text?.joinToString(separator = "") { it.text }
    }

    private fun assertTopBarSafe() {
        composeRule.waitForIdle()
        val node = composeRule.onNodeWithTag(TestTags.TopBarTitle).fetchSemanticsNode()
        val top = node.boundsInRoot.top
        val statusBarPx = statusBarHeightPx()
        if (statusBarPx > 0 && top < statusBarPx) {
            throw AssertionError("Top bar overlaps status bar (top=$top, statusBar=$statusBarPx).")
        }
        composeRule.onNodeWithTag(TestTags.TopBarMenuButton).performClick()
        assertMenuAnchoredToButton()
        composeRule.onNodeWithTag(TestTags.EndpointButton).performClick()
        waitForTag(TestTags.EndpointInput)
        composeRule.onNodeWithText("Cancel").performClick()
        waitForTagToDisappear(TestTags.EndpointInput)
    }

    private fun assertMenuAnchoredToButton() {
        composeRule.waitForIdle()
        val menu = composeRule.onNodeWithTag(TestTags.TopBarMenu).fetchSemanticsNode()
        val menuBounds = menu.boundsInRoot
        val windowHeight = composeRule.activity.window.decorView.height.toFloat()
        if (menuBounds.top > windowHeight * 0.5f) {
            throw AssertionError("Menu not positioned near the top bar.")
        }
    }

    private fun assertSendButtonLabelVisible() {
        composeRule.waitForIdle()
        val button = composeRule.onNodeWithTag(TestTags.TerminalSend).fetchSemanticsNode()
        val label = composeRule.onNodeWithText("Send").fetchSemanticsNode()
        val buttonBounds = button.boundsInRoot
        val labelBounds = label.boundsInRoot
        if (labelBounds.left < buttonBounds.left - BUTTON_LABEL_TOLERANCE_PX ||
            labelBounds.right > buttonBounds.right + BUTTON_LABEL_TOLERANCE_PX
        ) {
            throw AssertionError("Send label not fully within button bounds.")
        }
    }

    private fun statusBarHeightPx(): Int {
        val resources = composeRule.activity.resources
        val resId = resources.getIdentifier("status_bar_height", "dimen", "android")
        return if (resId > 0) resources.getDimensionPixelSize(resId) else 0
    }


    private fun waitUntil(timeoutMs: Long = DEFAULT_TIMEOUT_MS, condition: () -> Boolean) {
        val deadline = System.currentTimeMillis() + timeoutMs
        while (System.currentTimeMillis() < deadline) {
            if (condition()) return
            composeRule.waitForIdle()
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("Timed out waiting for UI condition after ${timeoutMs}ms")
    }

    private fun assertBackendReachable(endpoint: String) {
        val url = URL("${endpoint.trimEnd('/')}/api/me")
        val connection = (url.openConnection() as HttpURLConnection).apply {
            connectTimeout = 3000
            readTimeout = 3000
            instanceFollowRedirects = false
        }
        try {
            val code = connection.responseCode
            if (code != HttpURLConnection.HTTP_OK &&
                code != HttpURLConnection.HTTP_UNAUTHORIZED &&
                code != HttpURLConnection.HTTP_FORBIDDEN
            ) {
                throw AssertionError(
                    "centaurx backend responded with HTTP $code at $endpoint. " +
                        "Start centaurx on the host (e.g. `centaurx serve`) and expose :27480.",
                )
            }
        } catch (ex: Exception) {
            throw AssertionError(
                "centaurx backend not reachable at $endpoint. " +
                    "Start centaurx on the host (e.g. `centaurx serve`) and expose :27480.",
                ex,
            )
        } finally {
            connection.disconnect()
        }
    }

    private fun generateTotp(secret: String, timestampSeconds: Long = System.currentTimeMillis() / 1000L): String {
        val key = base32Decode(secret)
        val counter = timestampSeconds / 30L
        val data = ByteArray(8)
        var value = counter
        for (i in 7 downTo 0) {
            data[i] = (value and 0xFF).toByte()
            value = value shr 8
        }
        val mac = Mac.getInstance("HmacSHA1")
        mac.init(SecretKeySpec(key, "HmacSHA1"))
        val hash = mac.doFinal(data)
        val offset = hash.last().toInt() and 0x0F
        val binary =
            ((hash[offset].toInt() and 0x7F) shl 24) or
                ((hash[offset + 1].toInt() and 0xFF) shl 16) or
                ((hash[offset + 2].toInt() and 0xFF) shl 8) or
                (hash[offset + 3].toInt() and 0xFF)
        val otp = binary % 1_000_000
        return otp.toString().padStart(TOTP_DIGITS, '0')
    }

    private fun activateTabInSecondarySession(repoName: String) {
        val client = createSecondarySessionClient()
        val tab = waitForTabByRepo(client, repoName)
        val jsonMedia = "application/json; charset=utf-8".toMediaType()
        val activatePayload = mapOf("tab_id" to tab.id)
        val activateRequest = Request.Builder()
            .url("${EMULATOR_ENDPOINT.trimEnd('/')}/api/tabs/activate")
            .post(CentaurxJson.encodeToString(activatePayload).toRequestBody(jsonMedia))
            .build()
        client.newCall(activateRequest).execute().use { resp ->
            if (!resp.isSuccessful) {
                throw AssertionError("activate tab failed: ${resp.code}")
            }
        }
    }

    private fun fetchTabForRepo(repoName: String, timeoutMs: Long = DEFAULT_TIMEOUT_MS): TabSnapshot {
        val client = createSecondarySessionClient()
        return waitForTabByRepo(client, repoName, timeoutMs)
    }

    private fun createSecondarySessionClient(): OkHttpClient {
        val client = OkHttpClient.Builder()
            .cookieJar(InMemoryCookieJar())
            .build()
        val jsonMedia = "application/json; charset=utf-8".toMediaType()
        val loginPayload = mapOf(
            "username" to "admin",
            "password" to "admin",
            "totp" to generateTotp(DEFAULT_TOTP_SECRET),
        )
        val loginRequest = Request.Builder()
            .url("${EMULATOR_ENDPOINT.trimEnd('/')}/api/login")
            .post(CentaurxJson.encodeToString(loginPayload).toRequestBody(jsonMedia))
            .build()
        client.newCall(loginRequest).execute().use { resp ->
            if (!resp.isSuccessful) {
                throw AssertionError("secondary login failed: ${resp.code}")
            }
        }
        return client
    }

    private fun waitForTabByRepo(
        client: OkHttpClient,
        repoName: String,
        timeoutMs: Long = DEFAULT_TIMEOUT_MS,
    ): TabSnapshot {
        val deadline = System.currentTimeMillis() + timeoutMs
        val listTabsRequest = Request.Builder()
            .url("${EMULATOR_ENDPOINT.trimEnd('/')}/api/tabs")
            .get()
            .build()
        while (System.currentTimeMillis() < deadline) {
            val tabsResp = client.newCall(listTabsRequest).execute().use { resp ->
                if (!resp.isSuccessful) {
                    throw AssertionError("list tabs failed: ${resp.code}")
                }
                val body = resp.body?.string().orEmpty()
                CentaurxJson.decodeFromString<ListTabsResponse>(body)
            }
            val tab = tabsResp.tabs.firstOrNull { it.repo?.name == repoName }
            if (tab != null) {
                return tab
            }
            Thread.sleep(POLL_INTERVAL_MS)
        }
        throw AssertionError("tab for repo $repoName not found")
    }

    private fun base32Decode(input: String): ByteArray {
        val cleaned = input.trim().replace("=", "").uppercase(Locale.US)
        val output = ArrayList<Byte>(cleaned.length)
        var buffer = 0
        var bitsLeft = 0
        for (ch in cleaned) {
            val value = BASE32_ALPHABET.indexOf(ch)
            if (value < 0) continue
            buffer = (buffer shl 5) or value
            bitsLeft += 5
            if (bitsLeft >= 8) {
                bitsLeft -= 8
                output.add(((buffer shr bitsLeft) and 0xFF).toByte())
            }
        }
        return output.toByteArray()
    }

    private fun uniqueRepoName(): String {
        val suffix = (System.currentTimeMillis() % 100_000_000L).toString().padStart(8, '0')
        return "t$suffix"
    }

    private class InMemoryCookieJar : CookieJar {
        private val cookies = mutableMapOf<String, List<Cookie>>()

        override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
            this.cookies[url.host] = cookies
        }

        override fun loadForRequest(url: HttpUrl): List<Cookie> {
            return cookies[url.host].orEmpty()
        }
    }

    companion object {
        private const val EMULATOR_ENDPOINT = "http://10.0.2.2:27480"
        private const val UNREACHABLE_ENDPOINT = "http://10.0.2.2:65534"
        private const val DEFAULT_TOTP_SECRET = "JBSWY3DPEHPK3PXP"
        private const val TOTP_DIGITS = 6
        private const val DEFAULT_TIMEOUT_MS = 30_000L
        private const val POLL_INTERVAL_MS = 250L
        private const val UI_SETTLE_DELAY_MS = 750L
        private const val STATUS_WAIT_MS = 20_000L
        private const val BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
        private const val BUTTON_LABEL_TOLERANCE_PX = 6f
    }
}
