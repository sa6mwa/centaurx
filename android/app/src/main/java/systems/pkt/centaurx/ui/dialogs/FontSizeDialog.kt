package systems.pkt.centaurx.ui.dialogs

import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import systems.pkt.centaurx.MaxTerminalFontSizeSp
import systems.pkt.centaurx.MinTerminalFontSizeSp
import systems.pkt.centaurx.ui.TestTags
import kotlin.math.roundToInt

@Composable
fun FontSizeDialog(
    fontSizeSp: Int,
    onDismiss: () -> Unit,
    onSave: (Int) -> Unit,
) {
    val minSize = MinTerminalFontSizeSp
    val maxSize = MaxTerminalFontSizeSp
    var value by rememberSaveable { mutableStateOf(fontSizeSp.coerceIn(minSize, maxSize).toFloat()) }
    val rounded = value.roundToInt().coerceIn(minSize, maxSize)

    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(
                onClick = { onSave(rounded) },
                modifier = Modifier.testTag(TestTags.FontSizeSave),
            ) {
                Text(text = "Save")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = "Cancel")
            }
        },
        title = { Text(text = "Font size") },
        text = {
            androidx.compose.foundation.layout.Column {
                Text(
                    text = "$rounded sp",
                    style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold),
                    modifier = Modifier.testTag(TestTags.FontSizeValue),
                )
                Text(
                    text = "AaBbCc 123 /command",
                    style = MaterialTheme.typography.bodyMedium.copy(fontSize = rounded.sp),
                )
                Slider(
                    value = value,
                    onValueChange = { next -> value = next },
                    valueRange = minSize.toFloat()..maxSize.toFloat(),
                    steps = (maxSize - minSize - 1).coerceAtLeast(0),
                    modifier = Modifier.testTag(TestTags.FontSizeSlider),
                )
            }
        },
    )
}
