import UIKit
import React
import React_RCTAppDelegate
import ReactAppDependencyProvider
@main
class AppDelegate: UIResponder, UIApplicationDelegate {
 var window: UIWindow?
 var delegate: ReactNativeDelegate?
 var factory: RCTReactNativeFactory?
 func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
  PrivacyPreview.prepare()
  let delegate = ReactNativeDelegate()
  delegate.dependencyProvider = RCTAppDependencyProvider()
  let factory = RCTReactNativeFactory(delegate: delegate)
  self.delegate = delegate
  self.factory = factory
  window = UIWindow(frame: UIScreen.main.bounds)
  factory.startReactNative(withModuleName: "Sidiora", in: window, launchOptions: launchOptions)
  return true
 }
}
extension AppDelegate {
 func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) { SecureSession.receivedAPNSToken(deviceToken) }
 func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) { SecureSession.failedAPNSRegistration(error) }
 func application(_ app: UIApplication, open url: URL, options: [UIApplication.OpenURLOptionsKey: Any] = [:]) -> Bool { RCTLinkingManager.application(app, open: url, options: options) }
 func application(_ application: UIApplication, continue userActivity: NSUserActivity, restorationHandler: @escaping ([UIUserActivityRestoring]?) -> Void) -> Bool { RCTLinkingManager.application(application, continue: userActivity, restorationHandler: restorationHandler) }
}
class ReactNativeDelegate: RCTDefaultReactNativeFactoryDelegate {
 override func sourceURL(for bridge: RCTBridge) -> URL? { bundleURL() }
 override func bundleURL() -> URL? {
#if DEBUG
  RCTBundleURLProvider.sharedSettings().jsBundleURL(forBundleRoot: "index")
#else
  Bundle.main.url(forResource: "main", withExtension: "jsbundle")
#endif
 }
}
