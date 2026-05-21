// SPDX-License-Identifier: MIT
package dev.pi_remote.android

import android.content.Context
import android.content.SharedPreferences
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import dev.pi_remote.android.net.ConnectionStatus
import dev.pi_remote.android.net.WebSocketClient
import dev.pi_remote.android.proto.SessionInfo
import dev.pi_remote.android.sessions.DeepSpaceBackground
import dev.pi_remote.android.sessions.SessionListScreen
import dev.pi_remote.android.sessions.SettingsScreen
import dev.pi_remote.android.terminal.TerminalScreen
import java.util.UUID

enum class Screen {
    SESSION_LIST,
    SETTINGS,
    TERMINAL
}

class MainActivity : ComponentActivity() {

    private lateinit var webSocketClient: WebSocketClient
    private lateinit var sharedPreferences: SharedPreferences

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        webSocketClient = WebSocketClient()
        sharedPreferences = getSharedPreferences("pi_remote_prefs", Context.MODE_PRIVATE)

        // Load saved connection details and auto-connect if a URL is stored
        var savedUrl = sharedPreferences.getString("coordinator_url", "") ?: ""
        val savedJwt = sharedPreferences.getString("mock_jwt", "") ?: ""

        if (savedUrl.isNotEmpty()) {
            val trimmed = savedUrl.trim()
            if (!trimmed.contains("://")) {
                savedUrl = "ws://$trimmed"
                sharedPreferences.edit().putString("coordinator_url", savedUrl).apply()
            }
            webSocketClient.connect(savedUrl, savedJwt.ifEmpty { null })
        }

        setContent {
            MaterialTheme {
                Surface(
                    modifier = Modifier.fillMaxSize(),
                    color = DeepSpaceBackground,
                ) {
                    AppNavigation(
                        webSocketClient = webSocketClient,
                        sharedPreferences = sharedPreferences,
                        initialUrl = savedUrl,
                        initialJwt = savedJwt
                    )
                }
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        webSocketClient.disconnect()
    }
}

@Composable
fun AppNavigation(
    webSocketClient: WebSocketClient,
    sharedPreferences: SharedPreferences,
    initialUrl: String,
    initialJwt: String
) {
    var currentScreen by remember { mutableStateOf(Screen.SESSION_LIST) }
    var activeSession by remember { mutableStateOf<SessionInfo?>(null) }

    val connectionStatus by webSocketClient.connectionStatus.collectAsState()
    val machines by webSocketClient.machines.collectAsState()

    var url by remember { mutableStateOf(initialUrl) }
    var jwt by remember { mutableStateOf(initialJwt) }

    // Intercept back key and navigate correctly
    BackHandler(enabled = currentScreen != Screen.SESSION_LIST) {
        when (currentScreen) {
            Screen.SETTINGS -> currentScreen = Screen.SESSION_LIST
            Screen.TERMINAL -> {
                activeSession = null
                currentScreen = Screen.SESSION_LIST
            }
            Screen.SESSION_LIST -> { /* N/A */ }
        }
    }

    when (currentScreen) {
        Screen.SESSION_LIST -> {
            SessionListScreen(
                connectionStatus = connectionStatus,
                machines = machines,
                onSessionSelected = { session ->
                    activeSession = session
                    currentScreen = Screen.TERMINAL
                },
                onSpawnRequested = { machineId, cwd ->
                    val requestId = UUID.randomUUID().toString()
                    webSocketClient.spawnSession(requestId, machineId, cwd)
                },
                onNavigateToSettings = {
                    currentScreen = Screen.SETTINGS
                }
            )
        }
        Screen.SETTINGS -> {
            SettingsScreen(
                currentUrl = url,
                currentJwt = jwt,
                connectionStatus = connectionStatus,
                onSaveAndConnect = { newUrl, newJwt ->
                    val trimmed = newUrl.trim()
                    val normalizedUrl = if (trimmed.isNotEmpty() && !trimmed.contains("://")) {
                        "ws://$trimmed"
                    } else {
                        trimmed
                    }
                    url = normalizedUrl
                    jwt = newJwt
                    sharedPreferences.edit()
                        .putString("coordinator_url", normalizedUrl)
                        .putString("mock_jwt", newJwt)
                        .apply()
                    webSocketClient.disconnect()
                    webSocketClient.connect(normalizedUrl, newJwt.ifEmpty { null })
                    currentScreen = Screen.SESSION_LIST
                },
                onDisconnect = {
                    webSocketClient.disconnect()
                },
                onNavigateBack = {
                    currentScreen = Screen.SESSION_LIST
                }
            )
        }
        Screen.TERMINAL -> {
            val session = activeSession
            if (session != null) {
                TerminalScreen(
                    sessionId = session.sessionId,
                    projectName = session.metadata.projectDisplayName ?: session.metadata.projectName,
                    webSocketClient = webSocketClient,
                    onNavigateBack = {
                        activeSession = null
                        currentScreen = Screen.SESSION_LIST
                    }
                )
            } else {
                currentScreen = Screen.SESSION_LIST
            }
        }
    }
}

