# ADR-0002: Android compileSdk = 36 with targetSdk = 35

## Status

Accepted

Date: 2026-05-07

## Context

SPEC.md § 22.4 pins `Min SDK = 26 (Android 8.0)` and
`Target SDK = 35 (Android 15)` but does not specify `compileSdk`. AGP 8.7+
recommends `compileSdk >= targetSdk`. compileSdk controls which SDK API
surface is available when compiling; targetSdk controls runtime behavior
quirks.

Practically, `compileSdk = 35` and `compileSdk = 36` are interchangeable for
this app — there is no API used from android-36 specifically — but the
operator's local Android SDK install had `android-34` and `android-36`
platform images and not `android-35`. CI can install whatever; locally the
choice biases `compileSdk = 36` for quicker iteration.

## Decision

All three Android Gradle modules in `pi-remote-android`
(`:app`, `:terminal-emulator`, `:terminal-view`) use:

```
compileSdk = 36
minSdk     = 26
targetSdk  = 35   // (only :app sets targetSdk)
```

CI installs `platforms;android-36` and `build-tools;36.0.0`.

If a future API requires bumping `targetSdk` to 36, both knobs become 36 in
one PR.

## Consequences

**Positive:**
- Latest deprecation warnings surface at compile time.
- Aligns with Android Studio's preferred recent-platform default.

**Negative:**
- Newcomers must install the android-36 platform; the README documents this.

## References

- SPEC.md § 22.4.
- AGP guide on compileSdk vs targetSdk:
  https://developer.android.com/studio/build/configure-app-module
