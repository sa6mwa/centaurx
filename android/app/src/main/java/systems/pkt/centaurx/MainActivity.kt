package systems.pkt.centaurx

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import systems.pkt.centaurx.ui.CentaurxApp
import systems.pkt.centaurx.viewmodel.AppViewModel
import systems.pkt.centaurx.viewmodel.AppViewModelFactory

class MainActivity : ComponentActivity() {
    private val viewModel: AppViewModel by viewModels {
        val app = application as CentaurxApplication
        AppViewModelFactory(app.repository)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            CentaurxApp(viewModel)
        }
    }
}
