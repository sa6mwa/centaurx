package systems.pkt.centaurx.viewmodel

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.test.resetMain
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TestWatcher
import org.junit.runner.Description
import systems.pkt.centaurx.data.ApiException
import systems.pkt.centaurx.data.BufferResponse
import systems.pkt.centaurx.data.BufferSnapshot
import systems.pkt.centaurx.data.CentaurxClient
import systems.pkt.centaurx.data.HistoryResponse
import systems.pkt.centaurx.data.ListTabsResponse
import systems.pkt.centaurx.data.LoginResponse
import systems.pkt.centaurx.data.StreamEvent
import systems.pkt.centaurx.data.SystemBufferResponse
import systems.pkt.centaurx.data.SystemBufferSnapshot
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.awaitCancellation
import systems.pkt.centaurx.data.SnapshotPayload

@OptIn(ExperimentalCoroutinesApi::class)
class AppViewModelTest {
    @get:Rule
    val mainDispatcherRule = MainDispatcherRule()

    @Test
    fun sessionInvalidatedWhenStreamReturns401() = runTest {
        val repository = FakeRepository().apply {
            meResult = Result.success(LoginResponse(username = "mike"))
            streamEventsFlow = flow {
                throw ApiException("invalid session", 401)
            }
        }

        val viewModel = AppViewModel(repository)
        advanceUntilIdle()

        val state = viewModel.state.value
        assertFalse(state.loggedIn)
        assertEquals("session expired", state.loginError)
    }

    @Test
    fun sessionInvalidatedWhenPromptReturns401() = runTest {
        val repository = FakeRepository().apply {
            meResult = Result.success(LoginResponse(username = "mike"))
            streamEventsFlow = flow {
                emit(StreamEvent(type = "snapshot", snapshot = SnapshotPayload()))
                awaitCancellation()
            }
            submitPromptResult = Result.failure(ApiException("invalid session", 401))
        }

        val viewModel = AppViewModel(repository)
        advanceUntilIdle()
        assertTrue(viewModel.state.value.loggedIn)

        viewModel.submitPrompt("tab-1", "ls")
        advanceUntilIdle()

        val state = viewModel.state.value
        assertFalse(state.loggedIn)
        assertEquals("session expired", state.loginError)
    }
}

@OptIn(ExperimentalCoroutinesApi::class)
class MainDispatcherRule(
    private val dispatcher: TestDispatcher = StandardTestDispatcher(),
) : TestWatcher() {
    override fun starting(description: Description) {
        Dispatchers.setMain(dispatcher)
    }

    override fun finished(description: Description) {
        Dispatchers.resetMain()
    }
}

private class FakeRepository : CentaurxClient {
    override val endpointFlow = MutableStateFlow("http://localhost:27480")
    override val fontSizeFlow = MutableStateFlow(12)

    var loginResult: Result<LoginResponse> = Result.success(LoginResponse(username = "mike"))
    var logoutResult: Result<Unit> = Result.success(Unit)
    var meResult: Result<LoginResponse> = Result.success(LoginResponse(username = "mike"))
    var listTabsResult: Result<ListTabsResponse> = Result.success(ListTabsResponse())
    var activateTabResult: Result<Unit> = Result.success(Unit)
    var submitPromptResult: Result<Unit> = Result.success(Unit)
    var changePasswordResult: Result<Unit> = Result.success(Unit)
    var uploadCodexAuthResult: Result<Unit> = Result.success(Unit)
    var getHistoryResult: Result<HistoryResponse> = Result.success(HistoryResponse())
    var getBufferResult: Result<BufferResponse> = Result.success(
        BufferResponse(BufferSnapshot(lines = emptyList())),
    )
    var getSystemBufferResult: Result<SystemBufferResponse> = Result.success(
        SystemBufferResponse(SystemBufferSnapshot(lines = emptyList())),
    )
    var appendHistoryResult: Result<HistoryResponse> = Result.success(HistoryResponse())
    var streamEventsFlow: Flow<StreamEvent> = flow { awaitCancellation() }

    override fun setEndpoint(value: String) {
        endpointFlow.value = value
    }

    override fun setFontSize(value: Int) {
        fontSizeFlow.value = value
    }

    override suspend fun login(username: String, password: String, totp: String): LoginResponse {
        return loginResult.getOrThrow()
    }

    override suspend fun logout() {
        logoutResult.getOrThrow()
    }

    override suspend fun me(): LoginResponse {
        return meResult.getOrThrow()
    }

    override suspend fun listTabs(): ListTabsResponse {
        return listTabsResult.getOrThrow()
    }

    override suspend fun activateTab(tabId: String) {
        activateTabResult.getOrThrow()
    }

    override suspend fun submitPrompt(tabId: String?, input: String) {
        submitPromptResult.getOrThrow()
    }

    override suspend fun changePassword(
        currentPassword: String,
        newPassword: String,
        confirmPassword: String,
        totp: String,
    ) {
        changePasswordResult.getOrThrow()
    }

    override suspend fun uploadCodexAuth(rawJson: String) {
        uploadCodexAuthResult.getOrThrow()
    }

    override suspend fun getHistory(tabId: String?): HistoryResponse {
        return getHistoryResult.getOrThrow()
    }

    override suspend fun getBuffer(tabId: String?, limit: Int?): BufferResponse {
        return getBufferResult.getOrThrow()
    }

    override suspend fun getSystemBuffer(limit: Int?): SystemBufferResponse {
        return getSystemBufferResult.getOrThrow()
    }

    override suspend fun appendHistory(tabId: String, entry: String): HistoryResponse {
        return appendHistoryResult.getOrThrow()
    }

    override suspend fun streamEvents(lastEventId: Long?): Flow<StreamEvent> {
        return streamEventsFlow
    }
}
