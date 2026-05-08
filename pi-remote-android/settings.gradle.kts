// SPDX-License-Identifier: MIT
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "pi-remote-android"
include(":app")
include(":terminal-emulator")
include(":terminal-view")

project(":terminal-emulator").projectDir = file("vendor/terminal-emulator")
project(":terminal-view").projectDir = file("vendor/terminal-view")
