import UIKit
import React
@objc(PrivacyPreview)
class PrivacyPreview: NSObject {
 private static var enabled = true
 private static var overlay: UIView?
 private static var observers: [NSObjectProtocol] = []
 static func prepare() {
  guard observers.isEmpty else { return }
  observers.append(NotificationCenter.default.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { _ in Self.mask() })
  observers.append(NotificationCenter.default.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { _ in Self.overlay?.removeFromSuperview(); Self.overlay = nil })
 }
 override init() { super.init(); Self.prepare() }
 @objc static func requiresMainQueueSetup() -> Bool { true }
 @objc(setEnabled:resolver:rejecter:)
 func setEnabled(_ value: Bool, resolver resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) {
  DispatchQueue.main.async { Self.enabled = value; if !value && UIApplication.shared.applicationState == .active { Self.overlay?.removeFromSuperview(); Self.overlay = nil }; if value && UIApplication.shared.applicationState != .active { Self.mask() }; resolve(nil) }
 }
 private static func mask() {
  guard overlay == nil, let window = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).flatMap({ $0.windows }).first(where: { $0.isKeyWindow }) else { return }
  let view = UIView(frame: window.bounds); view.backgroundColor = .black; view.autoresizingMask = [.flexibleWidth, .flexibleHeight]; view.isAccessibilityElement = true; view.accessibilityLabel = "Private Sidiora content hidden"; window.addSubview(view); overlay = view
 }
}

import AVFoundation
@objc(SidioraVideoView)
class SidioraVideoView: UIView {
 private var player: AVPlayer?
 private var playerLayer: AVPlayerLayer?
 private var observer: NSObjectProtocol?
 private let control = UIButton(type: .system)
 @objc var source: NSString? { didSet { configure(source as String?) } }
 override init(frame: CGRect) {
  super.init(frame: frame)
  backgroundColor = .black
  control.setTitle("Play stream", for: .normal); control.accessibilityLabel = "Play or pause livestream"
  control.backgroundColor = .darkGray; control.setTitleColor(.white, for: .normal)
  control.addTarget(self, action: #selector(togglePlayback), for: .touchUpInside); addSubview(control)
  observer = NotificationCenter.default.addObserver(forName: UIApplication.willResignActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.pause() }
 }
 required init?(coder: NSCoder) { fatalError("Programmatic native video view required") }
 deinit { if let observer = observer { NotificationCenter.default.removeObserver(observer) }; player?.pause() }
 override func layoutSubviews() { super.layoutSubviews(); playerLayer?.frame = bounds; control.frame = CGRect(x: 12, y: max(0, bounds.height - 56), width: max(44, bounds.width - 24), height: 44) }
 override func didMoveToWindow() { super.didMoveToWindow(); if window == nil { pause() } }
 private func configure(_ value: String?) {
  pause(); playerLayer?.removeFromSuperlayer(); player = nil; playerLayer = nil
  guard let value = value, let url = URL(string: value), url.scheme == "https", url.user == nil, url.password == nil else { control.setTitle("Playback unavailable", for: .normal); control.isEnabled = false; return }
  let instance = AVPlayer(url: url), layer = AVPlayerLayer(player: instance); layer.videoGravity = .resizeAspect
  self.layer.insertSublayer(layer, at: 0); player = instance; playerLayer = layer; control.isEnabled = true; control.setTitle("Play stream", for: .normal); setNeedsLayout()
 }
 private func pause() { player?.pause(); control.setTitle("Play stream", for: .normal) }
 @objc private func togglePlayback() {
  guard let player = player else { return }
  if player.timeControlStatus == .playing { pause() } else {
   do { try AVAudioSession.sharedInstance().setCategory(.playback); try AVAudioSession.sharedInstance().setActive(true); player.play(); control.setTitle("Pause stream", for: .normal) }
   catch { control.setTitle("Audio playback unavailable", for: .normal) }
  }
 }
}
@objc(SidioraVideoViewManager)
class SidioraVideoViewManager: RCTViewManager {
 override func view() -> UIView! { SidioraVideoView(frame: .zero) }
 override static func requiresMainQueueSetup() -> Bool { true }
}
