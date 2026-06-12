// SPDX-License-Identifier: MIT
package dev.pi_remote.android

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import dev.pi_remote.android.net.ConnectionStatus
import dev.pi_remote.android.net.WebSocketClient
import dev.pi_remote.android.proto.SessionInfo
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent
import dev.pi_remote.android.push.Notifications
import dev.pi_remote.android.push.PushManager
import dev.pi_remote.android.push.SecureStore
import org.unifiedpush.android.connector.UnifiedPush
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

    // Session id from a pi-remote://session/<id> deep link (push tap).
    // Compose state so AppNavigation reacts when onNewIntent updates it.
    private val deepLinkSessionId = mutableStateOf<String?>(null)

    private val notifPermission =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) { }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        webSocketClient = WebSocketClient()
        sharedPreferences = getSharedPreferences("pi_remote_prefs", Context.MODE_PRIVATE)

        // Push wiring (SPEC § 19.2, issues #33/#35): notification channels,
        // POST_NOTIFICATIONS runtime permission, and UnifiedPush
        // registration via the installed distributor (ntfy app).
        Notifications.ensureChannels(this)
        if (Build.VERSION.SDK_INT >= 33) {
            notifPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        UnifiedPush.tryUseCurrentOrDefaultDistributor(this) { success ->
            if (success) {
                UnifiedPush.register(this)
            }
        }

        // Identify as the registered push client when we have one; the
        // stub fixture id keeps dev setups working before registration.
        webSocketClient.clientId = PushManager.clientId(this) ?: "test-client-1"

        deepLinkSessionId.value = sessionIdFromIntent(intent)
        jwtFromIntent(intent)?.let { onCfJwtReceived(it) }

        // Load saved connection details and auto-connect if a URL is stored.
        // The JWT lives in Keystore-backed encrypted prefs (SPEC § D5);
        // SecureStore also migrates any legacy plaintext copy on first use.
        var savedUrl = sharedPreferences.getString("coordinator_url", "") ?: ""
        val savedJwt = SecureStore.prefs(this).getString("mock_jwt", "") ?: ""

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
                        initialJwt = savedJwt,
                        deepLinkSessionId = deepLinkSessionId,
                        onSavePushPrefs = { toggles ->
                            PushManager.postPreferences(this, toggles)
                        },
                        onCfSignIn = { launchCfSignIn() }
                    )
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        sessionIdFromIntent(intent)?.let { deepLinkSessionId.value = it }
        jwtFromIntent(intent)?.let { onCfJwtReceived(it) }
    }

    private fun sessionIdFromIntent(intent: Intent?): String? {
        val data = intent?.data ?: return null
        if (data.scheme != "pi-remote" || data.host != "session") return null
        return data.lastPathSegment
    }

    /** pi-remote://auth/callback?jwt=... — the CF Access reflection (D5). */
    private fun jwtFromIntent(intent: Intent?): String? {
        val data = intent?.data ?: return null
        if (data.scheme != "pi-remote" || data.host != "auth") return null
        return data.getQueryParameter("jwt")
    }

    /** Store the real CF Access JWT, reconnect, and retry registration. */
    private fun onCfJwtReceived(jwt: String) {
        SecureStore.prefs(this).edit().putString("mock_jwt", jwt).apply()
        val url = sharedPreferences.getString("coordinator_url", "") ?: ""
        if (url.isNotEmpty()) {
            webSocketClient.disconnect()
            webSocketClient.connect(url, jwt)
        }
        PushManager.registerWithCoordinator(this)
    }

    /** Launch the CF Access email-PIN flow in a Custom Tab (SPEC § D5). */
    private fun launchCfSignIn() {
        val ws = sharedPreferences.getString("coordinator_url", "")?.trim().orEmpty()
        if (ws.isEmpty()) return
        val httpBase = ws
            .replaceFirst("wss://", "https://")
            .replaceFirst("ws://", "http://")
            .removeSuffix("/")
        CustomTabsIntent.Builder().build()
            .launchUrl(this, Uri.parse("$httpBase/v1/auth/app-callback"))
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
    initialJwt: String,
    deepLinkSessionId: MutableState<String?> = mutableStateOf(null),
    onSavePushPrefs: (Map<String, Boolean>) -> Unit = {},
    onCfSignIn: () -> Unit = {}
) {
    var currentScreen by remember { mutableStateOf(Screen.SESSION_LIST) }
    var activeSession by remember { mutableStateOf<SessionInfo?>(null) }
    val context = LocalContext.current

    val connectionStatus by webSocketClient.connectionStatus.collectAsState()
    val machines by webSocketClient.machines.collectAsState()

    // Push deep link (SPEC § 19.5): once the machine list knows the
    // session, jump straight to its terminal and consume the link.
    LaunchedEffect(machines, deepLinkSessionId.value) {
        val target = deepLinkSessionId.value ?: return@LaunchedEffect
        val session = machines.flatMap { it.sessions }.find { it.sessionId == target }
        if (session != null) {
            activeSession = session
            currentScreen = Screen.TERMINAL
            deepLinkSessionId.value = null
        }
    }

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
                onSavePushPrefs = onSavePushPrefs,
                onCfSignIn = onCfSignIn,
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
                        .apply()
                    SecureStore.prefs(context).edit()
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

