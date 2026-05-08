// SPDX-License-Identifier: Apache-2.0
// Vendored from termux-app at 30ebb2dee381d292ade0f2868cfde0f9f20b89fe; see vendor/UPSTREAM.md.
// Original (Apache 2.0) Groovy build script replaced with Kotlin DSL self-contained build.

plugins {
    id("com.android.library")
}

android {
    namespace = "com.termux.view"
    compileSdk = 36

    defaultConfig {
        minSdk = 26
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
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
}

dependencies {
    implementation("androidx.annotation:annotation:1.9.0")
    api(project(":terminal-emulator"))
    testImplementation("junit:junit:4.13.2")
}
