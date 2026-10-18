# Native bootstrap candidate

React Native0.86.0, React19.2.4, Hermes and New Architecture are pinned. iOS17+ and Android11/API30+ are the minimum application targets. The official React Native Community Template0.86.0 supplied the checked-in Sidiora.xcodeproj, shared scheme, platform assets and Gradle9.3.1 wrapper. Archive SHA512 and official Gradle wrapper/distribution checksums were verified; provenance and source hashes are retained in the lane evidence.

Framework source declares minimum iOS15.1/Xcode16.1 and AndroidAPI24, SDK36/build-tools36.0.0/NDK27.1.12297006, AGP8.12.0 and Kotlin2.1.20. The app targets are inside these declared OS bounds. Installed Node24.16.0 and pinned React19.2.4 satisfy framework engines/peers. These source checks and a successful Gradle wrapper startup do not establish native application compilation or physical-device support.

Both platform projects bootstrap an explicitly unavailable-service screen. Shared generated DTO imports compile in the contract check. Pending operations retain only request ID, chain125, timestamp, permitted route and optional transaction hash; callbacks wake reconciliation and never establish settlement. Platform storage and external-wallet adapters are explicit interfaces, not implemented custody, secure storage or wallet support.

Expected real outputs: Android app/build/outputs/bundle/release/app-release.aab and APK for device qualification; iOS archive Sidiora.xcarchive and exported signed Sidiora.ipa. None exists. Android project configuration currently fails because serial workspace dependency integration has not created app-local @react-native/gradle-plugin links. This failure is retained. iOS compilation requires macOS/Xcode and reviewed CocoaPods lock integration; neither was performed on Linux.

Canonical link origin must be supplied by deployment owner; association registration remains unavailable. Push routes only open the authorized inbox; private payload fields and expired payloads fail closed. Foreground execution requires all freshness gates; retry jitter is bounded30s. No financial mutation is queued offline. Storage/session/push purge and privacy masking require device adapter implementation and filesystem/device tests.

## Build commands after serial dependency integration

From client/apps/mobile on Android with SDK36/build-tools36.0.0/NDK27.1.12297006 and JDK21:

```sh
export NATIVE_BUILD_CACHE="$(mktemp -d /tmp/sidiora-native-build.XXXXXX)"
GRADLE_USER_HOME="$NATIVE_BUILD_CACHE/gradle-user" ./android/gradlew -p android --project-cache-dir "$NATIVE_BUILD_CACHE/android-project" --no-daemon --max-workers=2 :app:assembleDebug
GRADLE_USER_HOME="$NATIVE_BUILD_CACHE/gradle-user" ./android/gradlew -p android --project-cache-dir "$NATIVE_BUILD_CACHE/android-project" --no-daemon --max-workers=2 :app:assembleRelease :app:bundleRelease
```

Release outputs remain unsigned until the release operator supplies managed signing configuration; a generated unsigned artifact never qualifies store release.

On macOS with Xcode16.1 or later, Ruby and CocoaPods1.16.2 pinned by the serial toolchain integration, from client/apps/mobile:

```sh
cd ios
pod _1.16.2_ install
xcodebuild -workspace Sidiora.xcworkspace -scheme Sidiora -configuration Release -sdk iphonesimulator -destination 'generic/platform=iOS Simulator' -derivedDataPath ../build/ios CODE_SIGNING_ALLOWED=NO build
xcodebuild -workspace Sidiora.xcworkspace -scheme Sidiora -configuration Release -destination 'generic/platform=iOS' -archivePath ../build/Sidiora.xcarchive CODE_SIGNING_ALLOWED=NO archive
```

The second command checks unsigned simulator compilation; the third prepares an unsigned device archive, not an installable signed IPA. Signed archive/export commands require approved team, profile and export options from the operator and remain a release gate. Pod installation may update the project/lock and must be reviewed serially. No Xcode project generator is required.

Native wrapper checks must use an isolated GRADLE_USER_HOME and --project-cache-dir outside the source tree. Historical generated Android .gradle data is retained outside the candidate workspace and excluded from source manifests.
