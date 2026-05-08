# Vendored sources from termux-app

The `vendor/terminal-emulator/` and `vendor/terminal-view/` directories are
copies of the corresponding modules from
[termux/termux-app](https://github.com/termux/termux-app), which are
distributed under the Apache License, Version 2.0 (see [`NOTICE`](NOTICE)).

This vendoring is done because those two modules are not published to Maven
Central or any other public artifact repository. See SPEC.md § D2.

## Pinned upstream

| Field | Value |
|-------|-------|
| Repository | https://github.com/termux/termux-app |
| Commit SHA | `30ebb2dee381d292ade0f2868cfde0f9f20b89fe` |
| Branch (at vendor time) | `master` |
| Vendored on | 2026-05-07 |
| Modules vendored | `terminal-emulator/`, `terminal-view/` |

The Apache 2.0 NOTICE is preserved in `vendor/NOTICE` and the upstream license
text in `vendor/LICENSE-APACHE-2.0.txt`. The original Gradle Groovy build
files (`vendor/*/build.gradle`) were removed and replaced with self-contained
Kotlin DSL build files (`vendor/*/build.gradle.kts`) that drop upstream's
Maven publishing blocks and adapt module versions to match the rest of this
project.

## Re-vendoring

To bump to a newer upstream commit:

1. Update the SHA above to the chosen commit.
2. Replace the contents of `vendor/terminal-emulator/src` and
   `vendor/terminal-view/src` from upstream at that SHA.
3. Re-test `./gradlew :app:assembleDebug` and run the JNI build.
4. Update this file with the new vendoring date.
