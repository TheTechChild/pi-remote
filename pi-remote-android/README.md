# pi-remote-android

Native Kotlin Android app that attaches to live Pi sessions on coding machines
through the [pi-remote-coordinator](https://github.com/TheTechChild/pi-remote-coordinator),
renders the pty stream with Termux's `terminal-emulator`, sends keystrokes back
over WebSocket, and surfaces push notifications via UnifiedPush.

## Overview

This is the **Android client** for [Pi Remote](https://github.com/TheTechChild/pi-remote-spec).
It auths to the coordinator with Cloudflare Access (email PIN via Custom Tab),
maintains a WebSocket while in foreground, and wakes via UnifiedPush against a
self-hosted ntfy distributor for push notifications.

See [`pi-remote-spec/SPEC.md`](https://github.com/TheTechChild/pi-remote-spec/blob/main/SPEC.md) §§ 9,
10.3, 10.4, 11.2, 19 for the contract.

## Setup

Requires JDK 17 and the Android SDK with a `compileSdk = 35` platform image
plus NDK 27 (for the vendored `terminal-emulator` JNI build).

`local.properties` (untracked) must point at your SDK install:

```properties
sdk.dir=/Users/<you>/Library/Android/sdk
```

## Build

```sh
./gradlew assembleDebug
```

The resulting APK is in `app/build/outputs/apk/debug/`.

## Test

```sh
./gradlew test
./gradlew lint
```

## Codegen

Wire-protocol types live in `app/src/main/kotlin/dev/pi_remote/android/proto/`
and are generated from the JSON Schemas in
[`pi-remote-spec`](https://github.com/TheTechChild/pi-remote-spec) via
`quicktype`. Regenerate with:

```sh
bash scripts/codegen.sh
```

The pinned spec commit is recorded in
[`scripts/spec-version.txt`](scripts/spec-version.txt).

## Vendored modules

`vendor/terminal-emulator/` and `vendor/terminal-view/` are vendored from
[termux-app](https://github.com/termux/termux-app) under the Apache License,
Version 2.0. See [`vendor/UPSTREAM.md`](vendor/UPSTREAM.md) for the pinned
upstream commit and [`vendor/NOTICE`](vendor/NOTICE) for attribution.

## License

MIT — see [LICENSE](LICENSE). The vendored Termux modules retain their
original Apache 2.0 license; see `vendor/`.
