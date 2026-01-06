package systems.pkt.centaurx.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import systems.pkt.centaurx.ui.theme.LocalCentaurxExtraColors
import systems.pkt.centaurx.viewmodel.StatusLevel
import systems.pkt.centaurx.viewmodel.StatusMessage
import systems.pkt.centaurx.ui.TestTags

@Composable
fun StatusBanner(status: StatusMessage?) {
    if (status == null || status.message.isBlank()) return
    val extras = LocalCentaurxExtraColors.current
    val (border, textColor) = when (status.level) {
        StatusLevel.Error -> MaterialTheme.colorScheme.error to MaterialTheme.colorScheme.error
        StatusLevel.Warn -> MaterialTheme.colorScheme.tertiary to MaterialTheme.colorScheme.tertiary
        StatusLevel.Info -> extras.border to extras.muted
    }

    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(8.dp),
        color = extras.inputBg,
        border = BorderStroke(1.dp, border.copy(alpha = 0.6f)),
    ) {
        Text(
            text = status.message,
            color = textColor,
            style = MaterialTheme.typography.labelMedium,
            modifier = Modifier
                .padding(horizontal = 10.dp, vertical = 6.dp)
                .testTag(TestTags.StatusBanner),
        )
    }
}
