// SPDX-License-Identifier: Apache-2.0
// Vendored from termux-app at 30ebb2dee381d292ade0f2868cfde0f9f20b89fe; see vendor/UPSTREAM.md.
// Original (Apache 2.0) Groovy build script replaced with Kotlin DSL self-contained build.

plugins {
    id("com.android.library")
}

android {
    namespace = "com.termux.emulator"
    compileSdk = 36
    ndkVersion = "27.0.12077973"

    defaultConfig {
        minSdk = 26

        externalNativeBuild {
            ndkBuild {
                cFlags("-std=c11", "-Wall", "-Wextra", "-Os", "-fno-stack-protector")
            }
        }

        ndk {
            abiFilters += listOf("x86", "x86_64", "armeabi-v7a", "arm64-v8a")
        }
    }

    externalNativeBuild {
        ndkBuild {
            path = file("src/main/jni/Android.mk")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android.txt"), "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    implementation("androidx.annotation:annotation:1.9.0")
    testImplementation("junit:junit:4.13.2")
}
