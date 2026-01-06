package systems.pkt.centaurx.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import systems.pkt.centaurx.viewmodel.AppViewModel
import systems.pkt.centaurx.viewmodel.UiState

@Composable
fun LoginScreen(state: UiState, viewModel: AppViewModel) {
    var username by rememberSaveable { mutableStateOf("") }
    var password by rememberSaveable { mutableStateOf("") }
    var totp by rememberSaveable { mutableStateOf("") }
    val config = LocalConfiguration.current
    val isCompact = config.screenWidthDp < 360 || config.screenHeightDp < 700
    val outerPadding = if (isCompact) 12.dp else 16.dp
    val innerPadding = if (isCompact) 12.dp else 16.dp
    val spacing = if (isCompact) 8.dp else 12.dp

    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.TopCenter) {
        Surface(
            modifier = Modifier
                .padding(outerPadding)
                .fillMaxWidth(),
            shape = RoundedCornerShape(12.dp),
            color = MaterialTheme.colorScheme.surface,
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.onSurface.copy(alpha = 0.2f)),
        ) {
            Column(
                modifier = Modifier.padding(innerPadding),
                verticalArrangement = Arrangement.spacedBy(spacing),
            ) {
                Text(
                    text = "Login",
                    style = if (isCompact) MaterialTheme.typography.titleSmall else MaterialTheme.typography.titleMedium,
                )
                OutlinedTextField(
                    value = username,
                    onValueChange = { username = it },
                    label = { Text("Username") },
                    singleLine = true,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginUsername),
                )
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = { Text("Password") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginPassword),
                )
                OutlinedTextField(
                    value = totp,
                    onValueChange = { totp = it },
                    label = { Text("TOTP") },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number),
                    visualTransformation = VisualTransformation.None,
                    modifier = Modifier
                        .fillMaxWidth()
                        .testTag(TestTags.LoginTotp),
                )
                Button(
                    onClick = { viewModel.login(username.trim(), password, totp.trim()) },
                    modifier = Modifier
                        .align(Alignment.End)
                        .testTag(TestTags.LoginSubmit),
                    enabled = !state.isBusy,
                ) {
                    Text(text = "Sign in")
                }
                if (!state.loginError.isNullOrBlank()) {
                    Text(
                        text = state.loginError ?: "",
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.testTag(TestTags.LoginError),
                    )
                }
            }
        }
    }
}
