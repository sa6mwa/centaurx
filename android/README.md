# Centaurx Android app

The Centaurx Android app is a native companion to the web UI, built for fast
session work on the go while staying consistent with the web and SSH flows.

## Prereqs
- Java 21+
- Linux amd64 (SDK installer script is Linux-only for now)

## Quick start (debug)
```bash
cd android
make sdk          # install Android SDK + emulator (API 35 target + API 36 compile)
make avd          # create an AVD under ~/.android/avd (host keyboard enabled)
make emulator     # start the emulator (forces keyboard + IME settings + adb reverse)
make build        # assemble debug APK (signed with debug key)
make install      # install debug APK
make run          # launch the app
```

Output APK:
- Debug: `android/app/build/outputs/apk/debug/app-debug.apk`

## Release APK (signed)
`make release` generates a local development keystore (if one does not exist),
creates `android/signing.properties`, and builds a signed release APK. This is
intended for local installs only; replace it with your own keystore for any
distribution.

```bash
cd android
make release
```

Output APK:
- Release: `android/app/build/outputs/apk/release/app-release.apk`

Files created (git-ignored):
- `android/.keystore/centaurx-release.jks`
- `android/signing.properties`

### Use your own signing key
Generate a keystore:
```bash
keytool -genkeypair -v \
  -keystore /absolute/path/to/centaurx-release.jks \
  -alias centaurx \
  -keyalg RSA -keysize 2048 -validity 10000 \
  -storepass "your-store-pass" -keypass "your-key-pass" \
  -dname "CN=Centaurx, OU=Dev, O=Centaurx, L=Local, S=Local, C=US"
```

Create `android/signing.properties`:
```properties
storeFile=/absolute/path/to/centaurx-release.jks
storePassword=your-store-pass
keyAlias=centaurx
keyPassword=your-key-pass
```

Then build:
```bash
cd android
make release
```

## UI tests
UI tests run on a Gradle Managed Device (API 35) and require a Centaurx backend
reachable from the emulator.

```bash
cd android
make ui-test
```

Notes:
- Start the backend on the host before running UI tests (default `centaurx serve` on `:27480`).
- The emulator reaches the host via `http://10.0.2.2:27480`.

## Emulator presets
Use `PRESET` to select a device profile:
```bash
make PRESET=small emulator
make PRESET=medium emulator
make PRESET=pixel7 emulator
make PRESET=pixel9 emulator
```

List presets:
```bash
make list-presets
```

## Notes
- SDK installs to `~/Android/Sdk` by default (override with `ANDROID_SDK_ROOT`).
- The installer fetches the Android 15 AOSP system image (API 35) plus the API 36 platform for compileSdk.
- The app default endpoint is `http://localhost:27480` and can be changed in-app.
- A warning appears when using HTTP endpoints.
- `make emulator` configures `adb reverse tcp:27480` so `localhost` hits the host backend.
