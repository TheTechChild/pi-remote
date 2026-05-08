# ADR-0003: Exclude legacy `tink-android` from `androidx.security:security-crypto`

## Status

Accepted

Date: 2026-05-07

## Context

SPEC.md § 22.4 pins `androidx.security:security-crypto:1.1.0-beta01` for
encrypted preferences (used by SPEC.md § D5 / § 11.2 to store the
CF_Authorization JWT). At the pinned version, the artifact declares **two**
transitive dependencies whose class-spaces overlap:

| Artifact | Version | Source |
|----------|---------|--------|
| `com.google.crypto.tink:tink-android` | 1.8.0 | legacy Android-targeted artifact |
| `com.google.crypto.tink:tink` | 1.16.0 | consolidated, AAR-distributed artifact (replaces tink-android) |

D8 (the dexer) refuses the build with hundreds of `Duplicate class
com.google.crypto.tink.subtle.*` errors when both end up on the classpath.

Removing the modern `tink:1.16.0` is wrong — it is the artifact the rest of
Tink depends on and the migration target. Removing the legacy
`tink-android:1.8.0` is correct.

## Decision

In `pi-remote-android/app/build.gradle.kts`, the `security-crypto` dependency
is declared with an explicit exclusion:

```kotlin
implementation("androidx.security:security-crypto:1.1.0-beta01") {
    exclude(group = "com.google.crypto.tink", module = "tink-android")
}
```

This leaves `tink:1.16.0` on the classpath. `crypto/tink` symbols resolve to
that one artifact and the build proceeds.

## Consequences

**Positive:**
- Build succeeds.
- App ships only the maintained Tink artifact.

**Negative:**
- This exclusion lives in app-side build config; if `security-crypto` is
  bumped to a version that no longer pulls `tink-android` transitively, the
  exclusion becomes dead config — annotate to revisit.

**Risk to monitor:** if a future Tink release renames or splits
`com.google.crypto.tink.*` packages, the exclusion may stop being sufficient
and the dependency tree should be re-audited.

## References

- SPEC.md § 22.4.
- Tracking issue (Android Security Library): https://issuetracker.google.com/issues/302322834
