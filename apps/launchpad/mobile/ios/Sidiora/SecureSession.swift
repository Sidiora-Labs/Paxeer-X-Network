import Foundation
import Security
import CryptoKit
import LocalAuthentication
import UserNotifications
import UIKit
import React
@objc(SecureSession)
class SecureSession: NSObject {
 private let service = "ag.sidiora.mobile.session"
 private let cookieQueue = DispatchQueue(label: "ag.sidiora.mobile.cookie-http")
 private let cookieTasks = NSLock()
 private var activeCookieTasks: [URLSessionTask] = []
 private var cookieRevision = 0
 private let keys: Set<String> = ["session", "pushToken", "pending", "installation", "privacy", "walletconnect", "cookies", "approval"]
 @objc static func requiresMainQueueSetup() -> Bool { false }
 private func query(_ key: String) throws -> [String: Any] {
  guard keys.contains(key) else { throw NSError(domain: service, code: 1) }
  return [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: key]
 }
 @objc(get:resolver:rejecter:)
 func get(_ key: String, resolver resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { var request = try query(key); request[kSecReturnData as String] = true; request[kSecMatchLimit as String] = kSecMatchLimitOne
   var result: CFTypeRef?; let status = SecItemCopyMatching(request as CFDictionary, &result)
   if status == errSecItemNotFound { resolve(nil); return }
   guard status == errSecSuccess, let bytes = result as? Data, let value = String(data: bytes, encoding: .utf8) else { throw NSError(domain: service, code: Int(status)) }
   resolve(value)
  } catch { reject("SECURE_READ_FAILED", "Protected storage unavailable", nil) }
 }
 @objc(set:value:resolver:rejecter:)
 func set(_ key: String, value: String, resolver resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { var request = try query(key); guard let data = value.data(using: .utf8), data.count <= 65536 else { throw NSError(domain: service, code: 2) }
   let update = SecItemUpdate(request as CFDictionary, [kSecValueData as String: data] as CFDictionary)
   if update == errSecItemNotFound { request[kSecValueData as String] = data; request[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly; guard SecItemAdd(request as CFDictionary, nil) == errSecSuccess else { throw NSError(domain: service, code: 3) } }
   else if update != errSecSuccess { throw NSError(domain: service, code: Int(update)) }
   resolve(nil)
  } catch { reject("SECURE_WRITE_FAILED", "Protected storage unavailable", nil) }
 }
 @objc(remove:resolver:rejecter:)
 func remove(_ key: String, resolver resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { let status = SecItemDelete(try query(key) as CFDictionary); guard status == errSecSuccess || status == errSecItemNotFound else { throw NSError(domain: service, code: Int(status)) }; resolve(nil) }
  catch { reject("SECURE_DELETE_FAILED", "Protected deletion failed", nil) }
 }
 @objc(clear:rejecter:)
 func clear(_ resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  cookieTasks.lock(); defer { cookieTasks.unlock() }; cookieRevision += 1; for task in activeCookieTasks { task.cancel() }; activeCookieTasks.removeAll()
  let status = SecItemDelete([kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service] as CFDictionary)
  if status == errSecSuccess || status == errSecItemNotFound { resolve(nil) } else { reject("SECURE_PURGE_FAILED", "Protected purge failed", nil) }
 }
 @objc(unlock:rejecter:)
 func unlock(_ resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) {
  let context = LAContext(); var error: NSError?
  guard context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error) else { reject("BIOMETRICS_UNAVAILABLE", "Biometric unlock unavailable", nil); return }
  context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: "Unlock private Sidiora screens") { allowed, error in if allowed { resolve(true) } else { reject("LOCAL_UNLOCK_DENIED", "Local unlock denied", nil) } }
 }
 @objc(requestId:rejecter:)
 func requestId(_ resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) { resolve(UUID().uuidString.lowercased()) }
 @objc(configuration:rejecter:)
 func configuration(_ resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  guard let text = Bundle.main.object(forInfoDictionaryKey: "SidioraPublicConfiguration") as? String, let data = text.data(using: .utf8) else { reject("CONFIGURATION_UNAVAILABLE", "Release configuration unavailable", nil); return }
  do { resolve(try JSONSerialization.jsonObject(with: data)) } catch { reject("INVALID_CONFIGURATION", "Invalid public release configuration", nil) }
 }
}

extension SecureSession {
 private func publicCacheDirectory() throws -> URL {
  let parent = try FileManager.default.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
  var directory = parent.appendingPathComponent("sidiora-public-snapshots", isDirectory: true)
  try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
  var values = URLResourceValues(); values.isExcludedFromBackup = true; try directory.setResourceValues(values)
  return directory
 }
 private func publicFile(_ key: String) throws -> URL {
  let address = "(?:pool|profile):0x[0-9a-fA-F]{40}"
  guard key == "pools" || key == "live" || key.range(of: "^" + address + "$", options: .regularExpression) != nil || (key.hasPrefix("search:") && key.count <= 256) else { throw NSError(domain: "PublicSnapshot", code: 1) }
  let digest = SHA256.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
  return try publicCacheDirectory().appendingPathComponent(digest + ".json", isDirectory: false)
 }
 private func trimPublicCache() throws {
  let files = try FileManager.default.contentsOfDirectory(at: publicCacheDirectory(), includingPropertiesForKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]).filter { $0.pathExtension == "json" }
  let inventory = try files.map { file -> (URL, Int, Date) in let values = try file.resourceValues(forKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]); guard values.isRegularFile == true else { throw NSError(domain: "PublicSnapshot", code: 2) }; return (file, values.fileSize ?? 0, values.contentModificationDate ?? .distantPast) }.sorted { $0.2 < $1.2 }
  var total = inventory.reduce(0) { $0 + $1.1 }
  for (file, bytes, _) in inventory { if total <= 100 * 1024 * 1024 { break }; try FileManager.default.removeItem(at: file); total -= bytes }
 }
 @objc(readPublicSnapshot:resolver:rejecter:)
 func readPublicSnapshot(_ key: String, resolver resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { let file = try publicFile(key); if !FileManager.default.fileExists(atPath: file.path) { resolve(nil); return }; let data = try Data(contentsOf: file); guard data.count <= 100 * 1024 * 1024, let text = String(data: data, encoding: .utf8) else { throw NSError(domain: "PublicSnapshot", code: 3) }; try FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: file.path); resolve(text) }
  catch { reject("PUBLIC_CACHE_READ_FAILED", "Public offline snapshot unavailable", nil) }
 }
 @objc(writePublicSnapshot:value:resolver:rejecter:)
 func writePublicSnapshot(_ key: String, value: String, resolver resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { let file = try publicFile(key), data = Data(value.utf8); guard data.count <= 100 * 1024 * 1024, let object = try JSONSerialization.jsonObject(with: data) as? [String: Any], object["account"] is NSNull, object["value"] != nil, object["storedAt"] is NSNumber, object["expiresAt"] is NSNumber else { throw NSError(domain: "PublicSnapshot", code: 4) }; try data.write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication]); try trimPublicCache(); resolve(nil) }
  catch { reject("PUBLIC_CACHE_WRITE_FAILED", "Public offline snapshot persistence unavailable", nil) }
 }
 @objc(clearPublicSnapshots:rejecter:)
 func clearPublicSnapshots(_ resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) {
  do { let directory = try publicCacheDirectory(); for file in try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) { try FileManager.default.removeItem(at: file) }; resolve(nil) } catch { reject("PUBLIC_CACHE_PURGE_FAILED", "Public offline snapshot purge failed", nil) }
 }
}

private final class CookieRedirectGuard: NSObject, URLSessionTaskDelegate {
 func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping (URLRequest?) -> Void) { completionHandler(nil) }
}
extension SecureSession {
 private func readCookieVault() throws -> [String: String] {
  var query = try self.query("cookies"); query[kSecReturnData as String] = true; query[kSecMatchLimit as String] = kSecMatchLimitOne
  var result: CFTypeRef?; let status = SecItemCopyMatching(query as CFDictionary, &result)
  if status == errSecItemNotFound { return [:] }
  guard status == errSecSuccess, let data = result as? Data, let object = try JSONSerialization.jsonObject(with: data) as? [String: String] else { throw NSError(domain: service, code: 50) }
  return object
 }
 private func writeCookieVault(_ cookies: [String: String]) throws {
  var query = try self.query("cookies"); let data = try JSONSerialization.data(withJSONObject: cookies); guard data.count <= 16384 else { throw NSError(domain: service, code: 51) }
  let status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
  if status == errSecItemNotFound { query[kSecValueData as String] = data; query[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly; guard SecItemAdd(query as CFDictionary, nil) == errSecSuccess else { throw NSError(domain: service, code: 52) } }
  else if status != errSecSuccess { throw NSError(domain: service, code: 53) }
 }
 @objc(cancelCookieRequests:rejecter:)
 func cancelCookieRequests(_ resolve: RCTPromiseResolveBlock, rejecter reject: RCTPromiseRejectBlock) { cookieTasks.lock(); cookieRevision += 1; for task in activeCookieTasks { task.cancel() }; activeCookieTasks.removeAll(); cookieTasks.unlock(); resolve(nil) }
 @objc(cookieRequest:method:headers:body:resolver:rejecter:)
 func cookieRequest(_ rawURL: String, method: String, headers: [String: String], body: String?, resolver resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) {
  cookieTasks.lock(); let requestedRevision = cookieRevision; cookieTasks.unlock()
  cookieQueue.async {
   do {
    guard let text = Bundle.main.object(forInfoDictionaryKey: "SidioraPublicConfiguration") as? String, let configuration = try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any], let origin = configuration["canonicalOrigin"] as? String, let approved = URLComponents(string: origin), let url = URL(string: rawURL), let target = URLComponents(url: url, resolvingAgainstBaseURL: false), target.scheme == "https", target.host == approved.host, target.port == approved.port, target.user == nil, target.password == nil, target.fragment == nil, ["GET","POST","PUT","PATCH","DELETE"].contains(method), rawURL.count <= 8192, (body?.utf8.count ?? 0) <= 1048576 else { throw NSError(domain: self.service, code: 54) }
    var request = URLRequest(url: url); request.httpMethod = method; request.timeoutInterval = 30; request.httpBody = body.map { Data($0.utf8) }
    for (name, value) in headers { guard name.lowercased() != "cookie", name.lowercased() != "authorization", !value.contains("\r"), !value.contains("\n") else { throw NSError(domain: self.service, code: 55) }; request.setValue(value, forHTTPHeaderField: name) }
    let (revision, initialCookies) = try { () throws -> (Int, [String: String]) in self.cookieTasks.lock(); defer { self.cookieTasks.unlock() }; guard requestedRevision == self.cookieRevision else { throw NSError(domain: self.service, code: 58) }; return (requestedRevision, try self.readCookieVault()) }(); var cookies = initialCookies; if cookies["_origin"] != origin { cookies = ["_origin": origin] }; request.setValue(cookies.keys.filter { $0.hasPrefix("__Host-kindle_") }.sorted().map { "\($0)=\(cookies[$0]!)" }.joined(separator: "; "), forHTTPHeaderField: "Cookie")
    let sessionConfiguration = URLSessionConfiguration.ephemeral; sessionConfiguration.httpCookieStorage = nil; sessionConfiguration.httpShouldSetCookies = false; sessionConfiguration.urlCache = nil
    if method != "GET", request.value(forHTTPHeaderField: "X-CSRF-Token") == nil, let csrf = cookies["__Host-kindle_csrf"] { request.setValue(csrf, forHTTPHeaderField: "X-CSRF-Token") }
    let session = URLSession(configuration: sessionConfiguration, delegate: CookieRedirectGuard(), delegateQueue: nil)
    let task = session.dataTask(with: request) { data, response, error in
     self.cookieQueue.async {
      defer { session.finishTasksAndInvalidate() }
      do {
       if let error = error { throw error }; guard let response = response as? HTTPURLResponse, let data = data, data.count <= 8388608, let text = String(data: data, encoding: .utf8), !(300...399).contains(response.statusCode) else { throw NSError(domain: self.service, code: 56) }
       let fields = response.allHeaderFields.reduce(into: [String: String]()) { result, item in if let key = item.key as? String, let value = item.value as? String { result[key] = value } }
       self.cookieTasks.lock(); defer { self.cookieTasks.unlock() }; guard revision == self.cookieRevision else { throw NSError(domain: self.service, code: 58) }; cookies = try self.readCookieVault(); if cookies["_origin"] != origin { cookies = ["_origin": origin] }
       for cookie in HTTPCookie.cookies(withResponseHeaderFields: fields, for: url) {
        guard ["__Host-kindle_access","__Host-kindle_refresh","__Host-kindle_csrf"].contains(cookie.name) else { continue }
        guard cookie.isSecure, cookie.path == "/", cookie.domain == url.host, cookie.value.count <= 8192, cookie.properties?[.init(rawValue: "HttpOnly")] != nil || cookie.name == "__Host-kindle_csrf" else { throw NSError(domain: self.service, code: 57) }
        if cookie.value.isEmpty || (cookie.expiresDate.map { $0 <= Date() } ?? false) { cookies.removeValue(forKey: cookie.name) } else { cookies[cookie.name] = cookie.value }
       }
       try self.writeCookieVault(cookies)
       let safeHeaders = fields.filter { $0.key.lowercased() != "set-cookie" }; resolve(["status": response.statusCode, "headers": safeHeaders, "body": text])
      } catch { reject("COOKIE_HTTP_FAILED", "Protected cookie request failed", nil) }
     }
    }
    self.cookieTasks.lock(); self.activeCookieTasks.removeAll { $0.state == .completed }; guard revision == self.cookieRevision else { self.cookieTasks.unlock(); task.cancel(); session.invalidateAndCancel(); throw NSError(domain: self.service, code: 58) }; self.activeCookieTasks.append(task); self.cookieTasks.unlock(); task.resume()
   } catch { reject("COOKIE_HTTP_FAILED", "Protected cookie request unavailable", nil) }
  }
 }
}

extension SecureSession {
 private static var pushWaiters: [(RCTPromiseResolveBlock, RCTPromiseRejectBlock)] = []
 private static let pushLock = NSLock()
 private static var pushRegistrationId: UInt64 = 0
 static func receivedAPNSToken(_ token: Data) {
  let value = token.map { String(format: "%02x", $0) }.joined()
  do {
   guard !value.isEmpty, value.count <= 4096 else { throw NSError(domain: "NativePush", code: 1) }
   var query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: "ag.sidiora.mobile.session", kSecAttrAccount as String: "pushToken"]
   let data = Data(value.utf8), status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
   if status == errSecItemNotFound { query[kSecValueData as String] = data; query[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly; guard SecItemAdd(query as CFDictionary, nil) == errSecSuccess else { throw NSError(domain: "NativePush", code: 2) } } else if status != errSecSuccess { throw NSError(domain: "NativePush", code: Int(status)) }
   let installation = try installationIdentifier(); pushLock.lock(); let waiters = pushWaiters; pushWaiters.removeAll(); pushLock.unlock()
   for (resolve, _) in waiters { resolve(["installationId": installation, "token": value, "platform": "ios", "provider": "apns"]) }
  } catch { failedAPNSRegistration(error) }
 }
 static func failedAPNSRegistration(_ error: Error) { pushLock.lock(); let waiters = pushWaiters; pushWaiters.removeAll(); pushLock.unlock(); for (_, reject) in waiters { reject("APNS_TOKEN_UNAVAILABLE", "APNs registration unavailable", nil) } }
 private static func installationIdentifier() throws -> String {
  var query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: "ag.sidiora.mobile.installation", kSecAttrAccount as String: "installation"]
  var read = query; read[kSecReturnData as String] = true; read[kSecMatchLimit as String] = kSecMatchLimitOne; var item: CFTypeRef?; let status = SecItemCopyMatching(read as CFDictionary, &item)
  if status == errSecSuccess, let data = item as? Data, let text = String(data: data, encoding: .utf8), UUID(uuidString: text) != nil { return text }
  guard status == errSecItemNotFound else { throw NSError(domain: "NativePush", code: Int(status)) }
  let id = UUID().uuidString.lowercased(); query[kSecValueData as String] = Data(id.utf8); query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly; guard SecItemAdd(query as CFDictionary, nil) == errSecSuccess else { throw NSError(domain: "NativePush", code: 3) }; return id
 }
 @objc(pushPermission:rejecter:)
 func pushPermission(_ resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) { UNUserNotificationCenter.current().getNotificationSettings { settings in resolve(settings.authorizationStatus == .notDetermined ? "not-determined" : settings.authorizationStatus == .denied ? "denied" : "granted") } }
 @objc(requestPushPermission:rejecter:)
 func requestPushPermission(_ resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) { UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { granted, error in if let error = error { reject("PUSH_PERMISSION_UNAVAILABLE", "Notification permission unavailable", nil) } else { resolve(granted ? "granted" : "denied") } } }
 @objc(nativePushRegistration:rejecter:)
 func nativePushRegistration(_ resolve: @escaping RCTPromiseResolveBlock, rejecter reject: @escaping RCTPromiseRejectBlock) {
  UNUserNotificationCenter.current().getNotificationSettings { settings in
   guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional || settings.authorizationStatus == .ephemeral else { reject("PUSH_PERMISSION_DENIED", "Notification permission required", nil); return }
   Self.pushLock.lock(); guard Self.pushWaiters.isEmpty else { Self.pushLock.unlock(); reject("APNS_REQUEST_ACTIVE", "APNs registration already active", nil); return }; Self.pushRegistrationId += 1; let requestId = Self.pushRegistrationId; Self.pushWaiters.append((resolve, reject)); Self.pushLock.unlock()
   DispatchQueue.main.async { UIApplication.shared.registerForRemoteNotifications() }
   DispatchQueue.main.asyncAfter(deadline: .now() + 60) { Self.pushLock.lock(); guard requestId == Self.pushRegistrationId else { Self.pushLock.unlock(); return }; let waiters = Self.pushWaiters; Self.pushWaiters.removeAll(); Self.pushLock.unlock(); for (_, reject) in waiters { reject("APNS_TOKEN_UNAVAILABLE", "APNs did not return a registration token", nil) } }
  }
 }
}
