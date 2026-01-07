package systems.pkt.centaurx.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.graphics.SolidColor
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
    val promptHeight = if (isCompact) 36.dp else 40.dp
    val promptMaxHeight = if (isCompact) 110.dp else 140.dp
    val promptMaxLines = if (isCompact) 4 else 6

    val terminalTextStyle = rememberTerminalTextStyle(fontSizeSp = state.fontSizeSp)
    val promptTextStyle = terminalTextStyle
    val sendButtonWidth = if (isCompact) 70.dp else 80.dp

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
            shape = RoundedCornerShape(6.dp),
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
            PromptInput(
                value = promptValue,
                onValueChange = {
                    promptValue = it
                    if (!activeTab.isNullOrBlank()) {
                        viewModel.resetHistoryIndex(activeTab)
                    }
                },
                onSend = { sendPrompt() },
                onHistoryNavigate = { direction ->
                    if (activeTab.isNullOrBlank()) return@PromptInput
                    val selection = promptValue.selection
                    val atEdge = selection.start == selection.end &&
                        (selection.start == 0 || selection.start == promptValue.text.length)
                    if (!atEdge) return@PromptInput
                    val next = viewModel.navigateHistory(activeTab, direction, promptValue.text)
                    if (next != null) {
                        promptValue = TextFieldValue(next, selection = androidx.compose.ui.text.TextRange(next.length))
                    }
                },
                textStyle = promptTextStyle,
                placeholder = "Type a prompt or /command",
                busy = showSpinner,
                height = promptHeight,
                maxHeight = promptMaxHeight,
                maxLines = promptMaxLines,
                modifier = Modifier.weight(1f),
                focusRequester = promptFocusRequester,
                onFocusChanged = { state -> promptFocused = state.isFocused },
            )
            Spacer(modifier = Modifier.width(8.dp))
            Button(
                onClick = { sendPrompt() },
                enabled = !state.isBusy,
                shape = RoundedCornerShape(6.dp),
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
private fun PromptInput(
    value: TextFieldValue,
    onValueChange: (TextFieldValue) -> Unit,
    onSend: () -> Unit,
    onHistoryNavigate: (Int) -> Unit,
    textStyle: TextStyle,
    placeholder: String,
    busy: Boolean,
    height: androidx.compose.ui.unit.Dp,
    maxHeight: androidx.compose.ui.unit.Dp,
    maxLines: Int,
    modifier: Modifier = Modifier,
    focusRequester: FocusRequester,
    onFocusChanged: (androidx.compose.ui.focus.FocusState) -> Unit,
) {
    val shape = RoundedCornerShape(6.dp)
    val borderColor = MaterialTheme.colorScheme.primary
    val containerColor = MaterialTheme.colorScheme.surfaceVariant
    val padding = androidx.compose.foundation.layout.PaddingValues(horizontal = 8.dp, vertical = 8.dp)
    var lineCount by remember { mutableStateOf(1) }
    val heightModifier = if (lineCount <= 1) {
        Modifier.height(height)
    } else {
        Modifier.heightIn(min = height, max = maxHeight)
    }

    Surface(
        modifier = modifier
            .then(heightModifier)
            .testTag(TestTags.TerminalPrompt)
            .focusRequester(focusRequester)
            .onFocusChanged(onFocusChanged)
            .onPreviewKeyEvent { event ->
                if (event.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                if ((event.key == Key.Enter || event.key == Key.NumPadEnter) && event.isCtrlPressed) {
                    onSend()
                    return@onPreviewKeyEvent true
                }
                if (event.key == Key.DirectionUp) {
                    onHistoryNavigate(-1)
                    return@onPreviewKeyEvent true
                }
                if (event.key == Key.DirectionDown) {
                    onHistoryNavigate(1)
                    return@onPreviewKeyEvent true
                }
                false
            },
        shape = shape,
        color = containerColor,
        border = BorderStroke(1.dp, borderColor),
    ) {
        BasicTextField(
            value = value,
            onValueChange = onValueChange,
            textStyle = textStyle.copy(color = MaterialTheme.colorScheme.onSurface),
            cursorBrush = SolidColor(MaterialTheme.colorScheme.onSurface),
            onTextLayout = { result -> lineCount = result.lineCount },
            keyboardOptions = KeyboardOptions(
                imeAction = ImeAction.Default,
                capitalization = KeyboardCapitalization.None,
            ),
            keyboardActions = KeyboardActions(onSend = { onSend() }),
            singleLine = false,
            maxLines = maxLines,
            modifier = Modifier
                .fillMaxWidth()
                .then(heightModifier)
                .padding(padding)
                .semantics { contentDescription = "Prompt input" },
            decorationBox = { innerTextField ->
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Box(modifier = Modifier.weight(1f)) {
                        if (value.text.isEmpty()) {
                            Text(
                                text = placeholder,
                                style = textStyle,
                                color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                            )
                        }
                        innerTextField()
                    }
                    if (busy) {
                        Spacer(modifier = Modifier.width(8.dp))
                        CircularProgressIndicator(
                            modifier = Modifier
                                .size(14.dp)
                                .testTag(TestTags.TerminalSpinner),
                            strokeWidth = 2.dp,
                            color = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
            },
        )
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
