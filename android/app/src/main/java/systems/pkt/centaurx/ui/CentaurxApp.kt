package systems.pkt.centaurx.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import systems.pkt.centaurx.ui.dialogs.ChangePasswordDialog
import systems.pkt.centaurx.ui.dialogs.CodexAuthDialog
import systems.pkt.centaurx.ui.dialogs.EndpointDialog
import systems.pkt.centaurx.ui.dialogs.FontSizeDialog
import systems.pkt.centaurx.ui.dialogs.RotateSSHKeyDialog
import systems.pkt.centaurx.ui.dialogs.ThemeDialog
import systems.pkt.centaurx.ui.theme.CentaurxTheme
import systems.pkt.centaurx.viewmodel.AppViewModel
import systems.pkt.centaurx.viewmodel.StatusLevel

@Composable
fun CentaurxApp(viewModel: AppViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val clipboard = LocalClipboardManager.current
    val activeTabId = state.activeTabId
    val activeBuffer = if (!activeTabId.isNullOrBlank()) state.buffers[activeTabId].orEmpty() else emptyList()

    CentaurxTheme(themeName = state.theme) {
        Surface(color = MaterialTheme.colorScheme.background) {
            Column(modifier = Modifier.fillMaxSize()) {
                TopBar(
                    username = state.username,
                    onShowSettings = { viewModel.showSettings(true) },
                    onShowTheme = { viewModel.showThemePicker(true) },
                    onShowFontSize = { viewModel.showFontSize(true) },
                    onCopyAll = {
                        if (activeBuffer.isEmpty()) {
                            viewModel.showStatus("no output to copy", StatusLevel.Warn)
                            return@TopBar
                        }
                        clipboard.setText(AnnotatedString(activeBuffer.joinToString("\n")))
                        viewModel.showStatus("copied ${activeBuffer.size} lines", StatusLevel.Info)
                    },
                    onLogout = { viewModel.logout() },
                )
                HorizontalDivider(color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f))
                if (state.loggedIn) {
                    TerminalScreen(state = state, viewModel = viewModel)
                } else {
                    LoginScreen(state = state, viewModel = viewModel)
                }
            }
        }

        if (state.showSettings) {
            EndpointDialog(
                endpoint = state.endpoint,
                onDismiss = { viewModel.showSettings(false) },
                onSave = { value ->
                    viewModel.updateEndpoint(value)
                    viewModel.showSettings(false)
                },
            )
        }

        if (state.showThemePicker) {
            ThemeDialog(
                currentTheme = state.theme,
                onDismiss = { viewModel.showThemePicker(false) },
                onSave = { theme ->
                    viewModel.showThemePicker(false)
                    viewModel.submitPrompt(state.activeTabId, "/theme $theme")
                },
            )
        }

        if (state.showFontSize) {
            FontSizeDialog(
                fontSizeSp = state.fontSizeSp,
                onDismiss = { viewModel.showFontSize(false) },
                onSave = { value ->
                    viewModel.updateFontSize(value)
                    viewModel.showFontSize(false)
                },
            )
        }

        if (state.showChpasswd) {
            ChangePasswordDialog(
                errorMessage = state.chpasswdError,
                onDismiss = { viewModel.showChangePassword(false) },
                onSubmit = { current, next, confirm, totp ->
                    viewModel.changePassword(current, next, confirm, totp)
                },
            )
        }

        if (state.showCodexAuth) {
            CodexAuthDialog(
                errorMessage = state.codexAuthError,
                busy = state.isBusy,
                onDismiss = { viewModel.showCodexAuth(false) },
                onUpload = { json -> viewModel.uploadCodexAuth(json) },
                onError = { message -> viewModel.setCodexAuthError(message) },
            )
        }

        if (state.showRotateSSHKey) {
            RotateSSHKeyDialog(
                errorMessage = state.rotateSSHKeyError,
                busy = state.isBusy,
                onDismiss = { viewModel.showRotateSSHKey(false) },
                onConfirm = { value -> viewModel.rotateSSHKey(value) },
                onError = { message -> viewModel.setRotateSSHKeyError(message) },
            )
        }
    }
}
