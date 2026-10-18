package ag.sidiora.mobile
import com.facebook.react.ReactActivity
import com.facebook.react.ReactActivityDelegate
import com.facebook.react.defaults.DefaultNewArchitectureEntryPoint.fabricEnabled
import com.facebook.react.defaults.DefaultReactActivityDelegate
class MainActivity : ReactActivity() {
 override fun onCreate(savedInstanceState:android.os.Bundle?){window.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);super.onCreate(savedInstanceState)}
 override fun onPause(){window.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);super.onPause()}
 override fun getMainComponentName() = "Sidiora"
 override fun createReactActivityDelegate(): ReactActivityDelegate = DefaultReactActivityDelegate(this, mainComponentName, fabricEnabled)
}
