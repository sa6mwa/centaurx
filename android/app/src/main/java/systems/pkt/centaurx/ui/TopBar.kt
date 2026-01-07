package systems.pkt.centaurx.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.wrapContentSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupPositionProvider

@Composable
fun TopBar(
    username: String?,
    onShowSettings: () -> Unit,
    onShowFontSize: () -> Unit,
    onLogout: () -> Unit,
) {
    var menuExpanded by remember { mutableStateOf(false) }
    val layoutDirection = LocalLayoutDirection.current
    val density = LocalDensity.current
    val menuMarginPx = with(density) { 8.dp.roundToPx() }
    var anchorBounds by remember { mutableStateOf<Rect?>(null) }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(horizontal = 12.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "centaurx",
            style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold),
            modifier = Modifier.testTag(TestTags.TopBarTitle),
        )
        Box(
            modifier = Modifier.wrapContentSize(Alignment.TopEnd),
            contentAlignment = Alignment.TopEnd,
        ) {
            IconButton(
                onClick = { menuExpanded = true },
                modifier = Modifier
                    .testTag(TestTags.TopBarMenuButton)
                    .onGloballyPositioned { coords -> anchorBounds = coords.boundsInWindow() },
            ) {
                Icon(
                    imageVector = Icons.Filled.Menu,
                    contentDescription = "Menu",
                    tint = MaterialTheme.colorScheme.onSurface,
                )
            }
            val anchor = anchorBounds
            if (menuExpanded && anchor != null) {
                Popup(
                    onDismissRequest = { menuExpanded = false },
                    popupPositionProvider = menuPositionProvider(anchor, layoutDirection, menuMarginPx),
                ) {
                    Surface(
                        shape = MaterialTheme.shapes.medium,
                        tonalElevation = 6.dp,
                        modifier = Modifier.testTag(TestTags.TopBarMenu),
                    ) {
                        androidx.compose.foundation.layout.Column {
                            if (!username.isNullOrBlank()) {
                                DropdownMenuItem(
                                    text = { Text("Signed in as $username") },
                                    onClick = {},
                                    enabled = false,
                                )
                            }
                            DropdownMenuItem(
                                text = { Text("Endpoint") },
                                onClick = {
                                    menuExpanded = false
                                    onShowSettings()
                                },
                                modifier = Modifier.testTag(TestTags.EndpointButton),
                            )
                            DropdownMenuItem(
                                text = { Text("Set font size") },
                                onClick = {
                                    menuExpanded = false
                                    onShowFontSize()
                                },
                                modifier = Modifier.testTag(TestTags.FontSizeButton),
                            )
                            if (!username.isNullOrBlank()) {
                                DropdownMenuItem(
                                    text = { Text("Logout") },
                                    onClick = {
                                        menuExpanded = false
                                        onLogout()
                                    },
                                    modifier = Modifier.testTag(TestTags.LogoutButton),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun menuPositionProvider(
    anchor: Rect,
    layoutDirection: androidx.compose.ui.unit.LayoutDirection,
    horizontalMarginPx: Int,
): PopupPositionProvider {
    val anchorBounds = androidx.compose.ui.unit.IntRect(
        anchor.left.toInt(),
        anchor.top.toInt(),
        anchor.right.toInt(),
        anchor.bottom.toInt(),
    )
    return object : PopupPositionProvider {
        override fun calculatePosition(
            anchorBounds: androidx.compose.ui.unit.IntRect,
            windowSize: IntSize,
            layoutDirection: androidx.compose.ui.unit.LayoutDirection,
            popupContentSize: IntSize,
        ): IntOffset {
            val x = when (layoutDirection) {
                androidx.compose.ui.unit.LayoutDirection.Ltr -> windowSize.width - popupContentSize.width - horizontalMarginPx
                androidx.compose.ui.unit.LayoutDirection.Rtl -> horizontalMarginPx
            }.coerceIn(0, windowSize.width - popupContentSize.width)
            val y = anchorBounds.bottom.coerceIn(0, windowSize.height - popupContentSize.height)
            return IntOffset(x, y)
        }
    }
}
