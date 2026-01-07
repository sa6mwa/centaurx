package systems.pkt.centaurx.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.key.isCtrlPressed
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import systems.pkt.centaurx.MaxTerminalFontSizeSp
import systems.pkt.centaurx.MinTerminalFontSizeSp
import kotlinx.coroutines.delay
import systems.pkt.centaurx.data.TabSnapshot
import systems.pkt.centaurx.data.TabStatus
import systems.pkt.centaurx.ui.terminal.TerminalView
import systems.pkt.centaurx.viewmodel.AppViewModel
import systems.pkt.centaurx.viewmodel.UiState

@Composable
fun TerminalScreen(state: UiState, viewModel: AppViewModel) {
    var promptValue by remember { mutableStateOf(TextFieldValue("")) }
    var promptFocused by remember { mutableStateOf(false) }
    val promptFocusRequester = remember { FocusRequester() }
    val activeTab = state.activeTabId
    val activeTabRunning = state.tabs.firstOrNull { it.id == activeTab }?.status == TabStatus.Running
    val busyTarget = state.isBusy || activeTabRunning
    var showSpinner by remember { mutableStateOf(false) }

    LaunchedEffect(busyTarget) {
        if (busyTarget) {
            delay(500)
            if (busyTarget) {
                showSpinner = true
            }
        } else {
            showSpinner = false
        }
    }

    LaunchedEffect(activeTab) {
        if (!activeTab.isNullOrBlank()) {
            viewModel.loadHistory(activeTab)
            viewModel.loadBuffer(activeTab)
        }
    }

    val lines = when {
        !activeTab.isNullOrBlank() -> state.buffers[activeTab].orEmpty()
        state.systemLines.isNotEmpty() -> state.systemLines
        else -> listOf("no active tab; use /new <repo>")
    }

    fun sendPrompt() {
        val raw = promptValue.text
        val cleaned = raw.replace("\r\n", "\n").replace("\r", "\n")
        val trimmed = cleaned.trim()
        if (trimmed.isEmpty()) return
        var payload = cleaned
        if ((trimmed.startsWith("/") || trimmed.startsWith("!")) && cleaned.contains("\n")) {
            payload = trimmed.lineSequence().firstOrNull().orEmpty()
        }
        when {
            isChpasswdInput(payload) -> viewModel.showChangePassword(true)
            isCodexAuthInput(payload) -> viewModel.showCodexAuth(true)
            isRotateSSHKeyInput(payload) -> viewModel.showRotateSSHKey(true)
            isLogoutInput(payload) -> viewModel.logout()
            else -> viewModel.submitPrompt(activeTab, payload)
        }
        promptValue = TextFieldValue("")
        promptFocusRequester.requestFocus()
    }

    val config = LocalConfiguration.current
    val isCompact = config.screenWidthDp < 360 || config.screenHeightDp < 700
    val screenPadding = if (isCompact) 8.dp else 12.dp
    val spacing = if (isCompact) 6.dp else 8.dp
    val terminalPadding = if (isCompact) 6.dp else 10.dp
    val promptHeight = if (isCompact) 46.dp else 50.dp
    val promptMaxHeight = if (isCompact) 130.dp else 160.dp
    val promptMaxLines = if (isCompact) 4 else 6

    val terminalTextStyle = rememberTerminalTextStyle(fontSizeSp = state.fontSizeSp)
    val promptTextStyle = terminalTextStyle
    val sendButtonWidth = if (isCompact) 76.dp else 88.dp

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = screenPadding)
            .padding(top = if (isCompact) 6.dp else 8.dp),
        verticalArrangement = Arrangement.spacedBy(spacing),
    ) {
        TabsRow(
            tabs = state.tabs,
            activeTabId = activeTab,
            onSelect = viewModel::activateTab,
            compact = isCompact,
        )
        StatusBanner(status = state.status)
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f),
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surfaceVariant,
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f)),
        ) {
            TerminalView(
                lines = lines,
                textStyle = terminalTextStyle,
                contentPadding = terminalPadding,
                resetScrollKey = activeTab ?: "system",
                forceScrollToBottom = promptFocused,
            )
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .imePadding()
                .padding(bottom = if (isCompact) 6.dp else 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            OutlinedTextField(
                value = promptValue,
                onValueChange = {
                    promptValue = it
                    if (!activeTab.isNullOrBlank()) {
                        viewModel.resetHistoryIndex(activeTab)
                    }
                },
                textStyle = promptTextStyle,
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = promptHeight, max = promptMaxHeight)
                    .testTag(TestTags.TerminalPrompt)
                    .focusRequester(promptFocusRequester)
                    .onFocusChanged { state -> promptFocused = state.isFocused }
                    .onPreviewKeyEvent { event ->
                        if (event.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                        if ((event.key == Key.Enter || event.key == Key.NumPadEnter) && event.isCtrlPressed) {
                            sendPrompt()
                            return@onPreviewKeyEvent true
                        }
                        if (activeTab.isNullOrBlank()) return@onPreviewKeyEvent false
                        if (event.key != Key.DirectionUp && event.key != Key.DirectionDown) return@onPreviewKeyEvent false
                        val selection = promptValue.selection
                        val atEdge = selection.start == selection.end &&
                            (selection.start == 0 || selection.start == promptValue.text.length)
                        if (!atEdge) return@onPreviewKeyEvent false
                        val direction = if (event.key == Key.DirectionUp) -1 else 1
                        val next = viewModel.navigateHistory(activeTab, direction, promptValue.text)
                        if (next != null) {
                            promptValue = TextFieldValue(next, selection = androidx.compose.ui.text.TextRange(next.length))
                        }
                        true
                },
                placeholder = { Text("Type a prompt or /command", style = promptTextStyle) },
                trailingIcon = {
                    if (showSpinner) {
                        CircularProgressIndicator(
                            modifier = Modifier
                                .size(if (isCompact) 16.dp else 18.dp)
                                .testTag(TestTags.TerminalSpinner),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Default),
                keyboardActions = KeyboardActions(onSend = { sendPrompt() }),
                singleLine = false,
                maxLines = promptMaxLines,
            )
            Spacer(modifier = Modifier.width(8.dp))
            Button(
                onClick = { sendPrompt() },
                enabled = !state.isBusy,
                shape = RoundedCornerShape(10.dp),
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp, vertical = 0.dp),
                modifier = Modifier
                    .height(promptHeight)
                    .width(sendButtonWidth)
                    .testTag(TestTags.TerminalSend),
            ) {
                Text(
                    text = "Send",
                    style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold),
                    maxLines = 1,
                )
            }
        }
    }
}

@Composable
private fun TabsRow(
    tabs: List<TabSnapshot>,
    activeTabId: String?,
    onSelect: (String) -> Unit,
    compact: Boolean,
) {
    if (tabs.isEmpty()) return
    val listState = rememberLazyListState()
    val activeIndex = tabs.indexOfFirst { it.id == activeTabId }
    val tabPadding = if (compact) 6.dp else 8.dp
    val tabVerticalPadding = if (compact) 4.dp else 6.dp
    val tabTextStyle = MaterialTheme.typography.labelMedium.copy(
        fontSize = if (compact) 10.sp else 11.sp,
        lineHeight = if (compact) 12.sp else 14.sp,
    )

    LaunchedEffect(activeIndex, tabs.size) {
        if (activeIndex >= 0) {
            listState.animateScrollToItem(activeIndex)
        }
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        TabNavButton(
            label = "<",
            enabled = activeIndex > 0,
            onClick = { onSelect(tabs[activeIndex - 1].id) },
            compact = compact,
        )
        Spacer(modifier = Modifier.width(4.dp))
        TabNavButton(
            label = ">",
            enabled = activeIndex >= 0 && activeIndex < tabs.lastIndex,
            onClick = { onSelect(tabs[activeIndex + 1].id) },
            compact = compact,
        )
        Spacer(modifier = Modifier.width(8.dp))
        LazyRow(
            state = listState,
            horizontalArrangement = Arrangement.spacedBy(if (compact) 6.dp else 8.dp),
            modifier = Modifier
                .weight(1f)
                .testTag(TestTags.TabList),
        ) {
            itemsIndexed(tabs) { _, tab ->
                val isActive = tab.id == activeTabId
                val label = tab.name ?: tab.id
                Surface(
                    shape = RoundedCornerShape(6.dp),
                    border = BorderStroke(
                        1.dp,
                        if (isActive) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f),
                    ),
                    color = MaterialTheme.colorScheme.surfaceVariant,
                    modifier = Modifier
                        .clickable { onSelect(tab.id) }
                        .testTag(TestTags.tabTag(tab.id))
                        .semantics { selected = isActive },
                ) {
                    Text(
                        text = label,
                        modifier = Modifier.padding(horizontal = tabPadding, vertical = tabVerticalPadding),
                        color = if (isActive) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        style = tabTextStyle,
                    )
                }
            }
        }
    }
}

@Composable
private fun TabNavButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    compact: Boolean,
) {
    val size = if (compact) 28.dp else 32.dp
    val textStyle = MaterialTheme.typography.labelMedium.copy(
        fontSize = if (compact) 10.sp else 12.sp,
        fontWeight = FontWeight.SemiBold,
    )
    val textColor = if (enabled) {
        MaterialTheme.colorScheme.onSurface
    } else {
        MaterialTheme.colorScheme.onSurface.copy(alpha = 0.4f)
    }
    Surface(
        shape = RoundedCornerShape(6.dp),
        color = if (enabled) MaterialTheme.colorScheme.surfaceVariant else MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.4f),
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f)),
        modifier = Modifier
            .size(size)
            .clickable(enabled = enabled) { onClick() },
    ) {
        Row(
            modifier = Modifier.fillMaxSize(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text = label, style = textStyle, color = textColor)
        }
    }
}

@Composable
private fun rememberTerminalTextStyle(
    fontSizeSp: Int,
): TextStyle {
    val baseStyle = MaterialTheme.typography.bodyMedium
    val size = fontSizeSp.coerceIn(MinTerminalFontSizeSp, MaxTerminalFontSizeSp).sp
    return baseStyle.copy(
        fontSize = size,
        lineHeight = (size.value * 1.25f).sp,
        fontWeight = FontWeight.Normal,
    )
}

private fun isLogoutInput(input: String): Boolean {
    return when (input.trim()) {
        "/quit", "/exit", "/logout", "/q" -> true
        else -> false
    }
}

private fun isChpasswdInput(input: String): Boolean {
    return input.trim() == "/chpasswd"
}

private fun isCodexAuthInput(input: String): Boolean {
    return input.trim() == "/codexauth"
}

private fun isRotateSSHKeyInput(input: String): Boolean {
    return input.trim() == "/rotatesshkey"
}
