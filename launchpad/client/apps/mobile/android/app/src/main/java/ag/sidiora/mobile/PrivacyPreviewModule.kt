package ag.sidiora.mobile
import android.view.WindowManager
import com.facebook.react.bridge.*
class PrivacyPreviewModule(context:ReactApplicationContext):ReactContextBaseJavaModule(context){
 override fun getName()="PrivacyPreview"
 @ReactMethod fun setEnabled(enabled:Boolean,promise:Promise){val activity=currentActivity;if(activity==null){promise.reject("PRIVACY_UNAVAILABLE","No foreground window");return};activity.runOnUiThread{if(enabled||!activity.hasWindowFocus())activity.window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)else activity.window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE);promise.resolve(null)}}
}
class SidioraVideoView(context:com.facebook.react.uimanager.ThemedReactContext):android.widget.VideoView(context),LifecycleEventListener {
 private val reactContext=context
 init{reactContext.addLifecycleEventListener(this);val controls=android.widget.MediaController(context);controls.setAnchorView(this);setMediaController(controls);contentDescription="Livestream video player";setOnErrorListener{_,_,_->announceForAccessibility("Livestream playback unavailable");true}}
 fun source(value:String?){stopPlayback();if(value==null)return;val uri=android.net.Uri.parse(value);require(uri.scheme=="https"&&uri.host!=null&&uri.userInfo==null){"Managed HTTPS playback required"};setVideoURI(uri)}
 override fun onHostPause(){pause()}
 override fun onHostResume(){}
 override fun onHostDestroy(){release()}
 fun release(){stopPlayback();reactContext.removeLifecycleEventListener(this)}
}
class SidioraVideoViewManager:com.facebook.react.uimanager.SimpleViewManager<SidioraVideoView>() {
 override fun getName()="SidioraVideoView"
 override fun createViewInstance(context:com.facebook.react.uimanager.ThemedReactContext)=SidioraVideoView(context)
 @com.facebook.react.uimanager.annotations.ReactProp(name="source") fun setSource(view:SidioraVideoView,value:String?){view.source(value)}
 override fun onDropViewInstance(view:SidioraVideoView){view.release();super.onDropViewInstance(view)}
}
