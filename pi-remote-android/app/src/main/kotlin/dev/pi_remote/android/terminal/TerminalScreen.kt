// SPDX-License-Identifier: MIT
package dev.pi_remote.android.terminal

import android.util.Base64
import android.util.Log
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import com.termux.terminal.TerminalSession
import com.termux.terminal.TerminalSessionClient
import com.termux.view.TerminalView
import com.termux.view.TerminalViewClient
import dev.pi_remote.android.net.WebSocketClient
import dev.pi_remote.android.sessions.DeepSpaceBackground
import dev.pi_remote.android.sessions.CardBackground
import dev.pi_remote.android.sessions.BorderAccent
import dev.pi_remote.android.sessions.TextPrimary
import dev.pi_remote.android.sessions.TextSecondary
import dev.pi_remote.android.sessions.IceBlue
import dev.pi_remote.android.sessions.MutedGray
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TerminalScreen(
    sessionId: String,
    projectName: String,
    webSocketClient: WebSocketClient,
    onNavigateBack: () -> Unit
) {
    val context = LocalContext.current
    val coroutineScope = rememberCoroutineScope()

    // Key modifier states
    var ctrlActive by remember { mutableStateOf(false) }
    var altActive by remember { mutableStateOf(false) }
    var fnActive by remember { mutableStateOf(false) }

    // Reference to standard Termux TerminalSession
    var termSession by remember { mutableStateOf<TerminalSession?>(null) }
    var termViewRef by remember { mutableStateOf<TerminalView?>(null) }

    // Initialize session and wire listeners
    DisposableEffect(sessionId) {
        val sessionClient = object : TerminalSessionClient {
            override fun onTextChanged(changedSession: TerminalSession) {
                termViewRef?.onScreenUpdated()
            }
            override fun onTitleChanged(changedSession: TerminalSession) {}
            override fun onSessionFinished(changedSession: TerminalSession) {}
            override fun onCopyTextToClipboard(session: TerminalSession, text: String) {
                // Clipboard integration if needed
            }
            override fun onPasteTextFromClipboard(session: TerminalSession?) {}
            override fun onBell(session: TerminalSession) {}
            override fun onColorsChanged(session: TerminalSession) {}
            override fun onTerminalCursorStateChange(state: Boolean) {}
            override fun setTerminalShellPid(session: TerminalSession, pid: Int) {}
            override fun getTerminalCursorStyle(): Int = 0

            override fun logError(tag: String, message: String) { Log.e(tag, message) }
            override fun logWarn(tag: String, message: String) { Log.w(tag, message) }
            override fun logInfo(tag: String, message: String) { Log.i(tag, message) }
            override fun logDebug(tag: String, message: String) { Log.d(tag, message) }
            override fun logVerbose(tag: String, message: String) { Log.v(tag, message) }
            override fun logStackTraceWithMessage(tag: String, message: String, e: Exception) { Log.e(tag, message, e) }
            override fun logStackTrace(tag: String, e: Exception) { Log.e(tag, "Stack trace", e) }
        }

        // 10000 rows scrollback
        val session = TerminalSession(10000, sessionClient)

        // Capture keystrokes and send them over WebSocket
        session.setWriteListener { data, offset, count ->
            val chunk = data.copyOfRange(offset, offset + count)
            webSocketClient.sendPtyInput(sessionId, chunk)
            // Auto reset key modifiers after a key is pressed (typical Termux behavior)
            ctrlActive = false
            altActive = false
            fnActive = false
        }

        // Handle terminal grid size resizes and notify coordinator (M11)
        session.setResizeListener { cols, rows ->
            Log.d("pi-remote/terminal", "Resized grid: $cols x $rows")
            webSocketClient.sendPtyResize(sessionId, cols, rows)
        }

        termSession = session

        // Attach session to coordinator websocket to receive historical stream + live frames
        webSocketClient.attach(sessionId)
        webSocketClient.sendClientFocus(sessionId, true)

        // Stream base64-encoded bytes in from WebSocketClient PTY flow
        val job = coroutineScope.launch {
            webSocketClient.sessionPty.collectLatest { pty ->
                if (pty.sessionId == sessionId) {
                    try {
                        val decodedBytes = Base64.decode(pty.bytes, Base64.DEFAULT)
                        session.writeToEmulator(decodedBytes, 0, decodedBytes.size)
                    } catch (e: Exception) {
                        Log.e("pi-remote/terminal", "Failed to decode/inject PTY payload", e)
                    }
                }
            }
        }

        onDispose {
            job.cancel()
            webSocketClient.sendClientFocus(sessionId, false)
            webSocketClient.detach(sessionId)
            termSession = null
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            text = projectName.ifEmpty { "System Terminal" },
                            color = TextPrimary,
                            fontWeight = FontWeight.Bold,
                            fontSize = 16.sp
                        )
                        Text(
                            text = "Session: " + sessionId.take(8),
                            color = TextSecondary,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 11.sp
                        )
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onNavigateBack) {
                        Icon(
                            imageVector = Icons.Default.ArrowBack,
                            contentDescription = "Back",
                            tint = TextPrimary
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = DeepSpaceBackground
                )
            )
        },
        containerColor = DeepSpaceBackground
    ) { paddingValues ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(paddingValues)
                .background(Color.Black)
        ) {
            // Terminal Emulator View
            Box(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth()
            ) {
                termSession?.let { currentSession ->
                    AndroidView(
                        factory = { ctx ->
                            TerminalView(ctx, null).apply {
                                val viewClient = object : TerminalViewClient {
                                    override fun onScale(scale: Float): Float { return scale }
                                    override fun onSingleTapUp(e: MotionEvent) {}
                                    override fun shouldBackButtonBeMappedToEscape(): Boolean = true
                                    override fun shouldEnforceCharBasedInput(): Boolean = false
                                    override fun shouldUseCtrlSpaceWorkaround(): Boolean = false
                                    override fun isTerminalViewSelected(): Boolean = true
                                    override fun copyModeChanged(copyMode: Boolean) {}

                                    override fun onKeyDown(keyCode: Int, e: KeyEvent, session: TerminalSession): Boolean {
                                        return false
                                    }
                                    override fun onKeyUp(keyCode: Int, e: KeyEvent): Boolean = false
                                    override fun onLongPress(event: MotionEvent): Boolean = false

                                    override fun readControlKey(): Boolean = ctrlActive
                                    override fun readAltKey(): Boolean = altActive
                                    override fun readShiftKey(): Boolean = false
                                    override fun readFnKey(): Boolean = fnActive

                                    override fun onCodePoint(codePoint: Int, ctrlDown: Boolean, session: TerminalSession): Boolean {
                                        return false
                                    }
                                    override fun onEmulatorSet() {}

                                    override fun logError(tag: String, message: String) { Log.e(tag, message) }
                                    override fun logWarn(tag: String, message: String) { Log.w(tag, message) }
                                    override fun logInfo(tag: String, message: String) { Log.i(tag, message) }
                                    override fun logDebug(tag: String, message: String) { Log.d(tag, message) }
                                    override fun logVerbose(tag: String, message: String) { Log.v(tag, message) }
                                    override fun logStackTraceWithMessage(tag: String, message: String, ex: Exception) { Log.e(tag, message, ex) }
                                    override fun logStackTrace(tag: String, ex: Exception) { Log.e(tag, "Stack trace", ex) }
                                }
                                setTerminalViewClient(viewClient)
                                attachSession(currentSession)
                                termViewRef = this
                                requestFocus()
                            }
                        },
                        update = { view ->
                            // Update binding if needed
                        },
                        modifier = Modifier.fillMaxSize()
                    )
                }
            }

            // Keyboard Accessory Bar
            AccessoryKeyboardBar(
                ctrlActive = ctrlActive,
                altActive = altActive,
                fnActive = fnActive,
                onCtrlToggle = { ctrlActive = !ctrlActive },
                onAltToggle = { altActive = !altActive },
                onFnToggle = { fnActive = !fnActive },
                onSpecialKeyClick = { specialKey ->
                    termSession?.let { session ->
                        val bytesToSend = when (specialKey) {
                            "ESC" -> byteArrayOf(27)
                            "TAB" -> byteArrayOf(9)
                            "UP" -> "\u001b[A".toByteArray()
                            "DOWN" -> "\u001b[B".toByteArray()
                            "LEFT" -> "\u001b[D".toByteArray()
                            "RIGHT" -> "\u001b[C".toByteArray()
                            else -> null
                        }
                        bytesToSend?.let { bytes ->
                            webSocketClient.sendPtyInput(sessionId, bytes)
                        }
                    }
                }
            )
        }
    }
}

@Composable
fun AccessoryKeyboardBar(
    ctrlActive: Boolean,
    altActive: Boolean,
    fnActive: Boolean,
    onCtrlToggle: () -> Unit,
    onAltToggle: () -> Unit,
    onFnToggle: () -> Unit,
    onSpecialKeyClick: (String) -> Unit
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(CardBackground)
            .border(1.dp, BorderAccent)
            .padding(horizontal = 8.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        // Modifiers
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            ModifierKeyButton(label = "CTRL", active = ctrlActive, onClick = onCtrlToggle)
            ModifierKeyButton(label = "ALT", active = altActive, onClick = onAltToggle)
            ModifierKeyButton(label = "FN", active = fnActive, onClick = onFnToggle)
        }

        // Separator/Spacer
        Spacer(modifier = Modifier.width(8.dp))

        // Special Actions
        Row(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.weight(1f)
        ) {
            SpecialActionButton(label = "ESC", onClick = { onSpecialKeyClick("ESC") })
            SpecialActionButton(label = "TAB", onClick = { onSpecialKeyClick("TAB") })
        }

        // Arrows Group
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            SpecialActionButton(label = "▲", onClick = { onSpecialKeyClick("UP") })
            SpecialActionButton(label = "▼", onClick = { onSpecialKeyClick("DOWN") })
            SpecialActionButton(label = "◀", onClick = { onSpecialKeyClick("LEFT") })
            SpecialActionButton(label = "▶", onClick = { onSpecialKeyClick("RIGHT") })
        }
    }
}

@Composable
fun ModifierKeyButton(label: String, active: Boolean, onClick: () -> Unit) {
    val bg = if (active) IceBlue else Color(0xFF232A46)
    val text = if (active) DeepSpaceBackground else TextPrimary

    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(bg)
            .border(1.dp, BorderAccent, RoundedCornerShape(8.dp))
            .clickable { onClick() }
            .padding(horizontal = 10.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center
    ) {
        Text(
            text = label,
            color = text,
            fontWeight = FontWeight.Bold,
            fontSize = 11.sp,
            fontFamily = FontFamily.Monospace
        )
    }
}

@Composable
fun SpecialActionButton(label: String, onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(Color(0xFF1D223B))
            .border(1.dp, Color(0xFF2E355E), RoundedCornerShape(8.dp))
            .clickable { onClick() }
            .padding(horizontal = 8.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center
    ) {
        Text(
            text = label,
            color = TextSecondary,
            fontWeight = FontWeight.Bold,
            fontSize = 11.sp,
            fontFamily = FontFamily.Monospace
        )
    }
}
