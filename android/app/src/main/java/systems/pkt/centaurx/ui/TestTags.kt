package systems.pkt.centaurx.ui

object TestTags {
    const val TopBarTitle = "topbar_title"
    const val TopBarMenuButton = "topbar_menu"
    const val TopBarMenu = "topbar_menu_popup"
    const val EndpointButton = "endpoint_button"
    const val EndpointInput = "endpoint_input"
    const val EndpointSave = "endpoint_save"
    const val ThemeButton = "theme_button"
    const val ThemeDialog = "theme_dialog"
    const val CopyAllButton = "copy_all_button"
    const val FontSizeButton = "font_size_button"
    const val FontSizeSlider = "font_size_slider"
    const val FontSizeSave = "font_size_save"
    const val FontSizeValue = "font_size_value"
    const val LogoutButton = "logout_button"

    const val LoginUsername = "login_username"
    const val LoginPassword = "login_password"
    const val LoginTotp = "login_totp"
    const val LoginSubmit = "login_submit"
    const val LoginError = "login_error"

    const val TerminalPrompt = "terminal_prompt"
    const val TerminalSend = "terminal_send"
    const val TerminalSpinner = "terminal_spinner"
    const val TerminalList = "terminal_list"
    const val TabList = "tab_list"
    const val StatusBanner = "status_banner"
    const val RotateSSHKeyInput = "rotatesshkey_input"
    const val RotateSSHKeyConfirm = "rotatesshkey_confirm"
    const val RotateSSHKeyCancel = "rotatesshkey_cancel"

    fun tabTag(label: String): String = "tab_$label"
}
