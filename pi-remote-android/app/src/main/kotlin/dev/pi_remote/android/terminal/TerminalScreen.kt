// SPDX-License-Identifier: MIT
package dev.pi_remote.android.terminal

import android.util.Base64
import android.util.Log
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.ScaleGestureDetector
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
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
import dev.pi_remote.android.sessions.TextPrimary
import dev.pi_remote.android.sessions.TextSecondary
import kotlinx.coroutines.Dispatchers
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

    // Reference to standard Termux TerminalSession
    var termSession by remember { mutableStateOf<TerminalSession?>(null) }
    var termViewRef by remember { mutableStateOf<TerminalView?>(null) }

    // Statically calculate ideal, legible terminal font size based on device density and screen width
    val fontSizePx = remember {
        val displayMetrics = context.resources.displayMetrics
        val density = displayMetrics.density
        val widthDp = displayMetrics.widthPixels / density
        // We want the font size to scale nicely with the screen width.
        // For a standard 411dp phone, 411 * 0.038 = 15.6dp (which becomes ~47px).
        // Clamped between 13dp and 20dp for perfect readability across foldables, tablets, and small phones.
        val calculatedFontSizeDp = (widthDp * 0.038f).coerceIn(13f, 20f)
        (calculatedFontSizeDp * density).toInt()
    }

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
        }

        // Handle terminal grid size resizes and notify coordinator (M11)
        session.setResizeListener { cols, rows ->
            Log.d("pi-remote/terminal", "Resized grid: $cols x $rows")
            webSocketClient.sendPtyResize(sessionId, cols, rows)
        }

        termSession = session

        // Stream base64-encoded bytes in from WebSocketClient PTY flow
        val job = coroutineScope.launch(Dispatchers.IO) {
            var nextExpectedSeq: Long? = null
            val pendingPackets = mutableMapOf<Long, dev.pi_remote.android.proto.SessionPty>()

            webSocketClient.sessionPty.collect { pty ->
                if (pty.sessionId == sessionId) {
                    try {
                        val seq = pty.seq
                        Log.d("pi-remote/terminal", "Received PTY update: seq=$seq, base64Length=${pty.bytes.length}")

                        if (nextExpectedSeq == null || seq < nextExpectedSeq!!) {
                            nextExpectedSeq = seq
                        }

                        if (seq >= nextExpectedSeq!!) {
                            pendingPackets[seq] = pty

                            while (pendingPackets.containsKey(nextExpectedSeq)) {
                                val currentPty = pendingPackets.remove(nextExpectedSeq)!!
                                val decodedBytes = Base64.decode(currentPty.bytes, Base64.DEFAULT)
                                Log.d("pi-remote/terminal", "Injecting ordered PTY update for seq=$nextExpectedSeq, size=${decodedBytes.size}")
                                session.writeToEmulator(decodedBytes, 0, decodedBytes.size)
                                nextExpectedSeq = nextExpectedSeq!! + 1
                            }
                        } else {
                            Log.w("pi-remote/terminal", "Discarding old or duplicate packet: seq=$seq, expected=$nextExpectedSeq")
                        }
                    } catch (e: Exception) {
                        Log.e("pi-remote/terminal", "Failed to decode/inject PTY payload for seq=${pty.seq}", e)
                    }
                }
            }
        }

        // Attach session to coordinator websocket to receive historical stream + live frames
        webSocketClient.attach(sessionId)
        webSocketClient.sendClientFocus(sessionId, true)

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
                                    override fun onScale(scale: Float): Float {
                                        return scale
                                    }
                                    override fun onSingleTapUp(e: MotionEvent) {
                                        val imm = ctx.getSystemService(android.content.Context.INPUT_METHOD_SERVICE) as? android.view.inputmethod.InputMethodManager
                                        imm?.showSoftInput(this@apply, android.view.inputmethod.InputMethodManager.SHOW_IMPLICIT)
                                    }
                                    override fun shouldBackButtonBeMappedToEscape(): Boolean = true
                                    // Char-based input gives the most reliable per-character
                                    // behavior with the Android soft keyboard and disables
                                    // autocorrect/suggestions/composing underlines.
                                    override fun shouldEnforceCharBasedInput(): Boolean = true
                                    override fun shouldUseCtrlSpaceWorkaround(): Boolean = false
                                    override fun isTerminalViewSelected(): Boolean = true
                                    override fun copyModeChanged(copyMode: Boolean) {}

                                    override fun onKeyDown(keyCode: Int, e: KeyEvent, session: TerminalSession): Boolean {
                                        return false
                                    }
                                    override fun onKeyUp(keyCode: Int, e: KeyEvent): Boolean = false
                                    override fun onLongPress(event: MotionEvent): Boolean = false

                                    override fun readControlKey(): Boolean = false
                                    override fun readAltKey(): Boolean = false
                                    override fun readShiftKey(): Boolean = false
                                    override fun readFnKey(): Boolean = false

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
                                setTextSize(fontSizePx)
                                attachSession(currentSession)
                                termViewRef = this
                                isFocusable = true
                                isFocusableInTouchMode = true
                                requestFocus()
                            }
                        },
                        update = { view ->
                            view.setTextSize(fontSizePx)
                        },
                        modifier = Modifier.fillMaxSize()
                    )
                }
            }
        }
    }
}
