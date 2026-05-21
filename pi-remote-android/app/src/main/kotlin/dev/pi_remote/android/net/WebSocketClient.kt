// SPDX-License-Identifier: MIT
package dev.pi_remote.android.net

import android.util.Base64
import android.util.Log
import dev.pi_remote.android.proto.*
import kotlinx.coroutines.*
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.serialization.json.*
import okhttp3.*
import java.util.concurrent.TimeUnit

enum class ConnectionStatus {
    DISCONNECTED,
    CONNECTING,
    CONNECTED,
    FAILED
}

class WebSocketClient(
    private val json: Json = Json { 
        ignoreUnknownKeys = true
        encodeDefaults = true
    }
) {
    private val clientScope = CoroutineScope(Dispatchers.Default + SupervisorJob())

    private var okHttpClient: OkHttpClient? = null
    private var webSocket: WebSocket? = null
    private var isExplicitlyClosed = false
    private var currentUrl: String? = null
    private var currentJwt: String? = null

    // Outgoing message channel for thread-safe serialized writes
    private val writeChannel = Channel<String>(Channel.UNLIMITED)
    private var writeJob: Job? = null

    // Reconnection variables
    private var reconnectDelayMs = 1000L
    private val maxReconnectDelayMs = 60000L
    private var reconnectJob: Job? = null

    // Flow states
    private val _connectionStatus = MutableStateFlow(ConnectionStatus.DISCONNECTED)
    val connectionStatus: StateFlow<ConnectionStatus> = _connectionStatus.asStateFlow()

    private val _machines = MutableStateFlow<List<Machine>>(emptyList())
    val machines: StateFlow<List<Machine>> = _machines.asStateFlow()

    private val _sessionPty = MutableSharedFlow<SessionPty>(extraBufferCapacity = 128)
    val sessionPty: SharedFlow<SessionPty> = _sessionPty.asSharedFlow()

    private val _spawnResponse = MutableSharedFlow<SpawnResponse>(extraBufferCapacity = 8)
    val spawnResponse: SharedFlow<SpawnResponse> = _spawnResponse.asSharedFlow()

    private val _replayUnavailable = MutableSharedFlow<ReplayUnavailable>(extraBufferCapacity = 8)
    val replayUnavailable: SharedFlow<ReplayUnavailable> = _replayUnavailable.asSharedFlow()

    init {
        startWriteLoop()
    }

    private fun startWriteLoop() {
        writeJob?.cancel()
        writeJob = clientScope.launch {
            for (message in writeChannel) {
                var sent = false
                while (!sent) {
                    val socket = webSocket
                    if (socket != null && _connectionStatus.value == ConnectionStatus.CONNECTED) {
                        try {
                            Log.i(TAG, "Actually sending WebSocket message: $message")
                            sent = socket.send(message)
                            if (!sent) {
                                Log.w(TAG, "Socket send returned false, waiting...")
                                delay(100)
                            }
                        } catch (e: Exception) {
                            Log.e(TAG, "Error in socket send", e)
                            delay(100)
                        }
                    } else {
                        delay(200)
                    }
                }
            }
        }
    }

    private fun activeSocket(): WebSocket? {
        return if (_connectionStatus.value == ConnectionStatus.CONNECTED) webSocket else null
    }

    fun connect(url: String, jwt: String? = null) {
        clientScope.launch {
            isExplicitlyClosed = false
            currentUrl = url
            currentJwt = jwt
            establishConnection()
        }
    }

    @Synchronized private fun establishConnection() {
        var url = currentUrl ?: return
        if (_connectionStatus.value == ConnectionStatus.CONNECTED || _connectionStatus.value == ConnectionStatus.CONNECTING) {
            return
        }

        // Normalize URL if it lacks a protocol scheme
        var trimmed = url.trim()
        if (trimmed.isNotEmpty() && !trimmed.contains("://")) {
            trimmed = "ws://$trimmed"
        }

        // Ensure path ends with /v1/client if no path was provided or /v1/client is missing
        if (trimmed.isNotEmpty()) {
            try {
                val uri = java.net.URI(trimmed)
                val path = uri.path
                if (path == null || path.isEmpty() || path == "/") {
                    trimmed = trimmed.removeSuffix("/") + "/v1/client"
                }
            } catch (e: Exception) {
                Log.w(TAG, "Failed to parse URI for path normalization: $trimmed", e)
            }
        }
        url = trimmed
        currentUrl = url

        _connectionStatus.value = ConnectionStatus.CONNECTING
        Log.i(TAG, "Connecting to $url")

        try {
            val clientBuilder = OkHttpClient.Builder()
                .connectTimeout(10, TimeUnit.SECONDS)
                .readTimeout(0, TimeUnit.MILLISECONDS) // infinite read timeout for WebSocket

            okHttpClient = clientBuilder.build()

            val requestBuilder = Request.Builder().url(url)
            currentJwt?.let {
                requestBuilder.addHeader("Cookie", "CF_Authorization=$it")
            }

            val request = requestBuilder.build()
            webSocket = okHttpClient?.newWebSocket(request, SocketListener())
        } catch (e: Exception) {
            Log.e(TAG, "Failed to initiate WebSocket connection to $url", e)
            _connectionStatus.value = ConnectionStatus.FAILED
            okHttpClient = null
            webSocket = null
        }
    }

    fun disconnect() {
        isExplicitlyClosed = true
        reconnectJob?.cancel()
        webSocket?.close(1000, "Explicit disconnect")
        webSocket = null
        okHttpClient = null
        _connectionStatus.value = ConnectionStatus.DISCONNECTED
        _machines.value = emptyList()
        Log.i(TAG, "Disconnected successfully")
    }

    private fun triggerReconnect() {
        if (isExplicitlyClosed) return

        reconnectJob?.cancel()
        reconnectJob = clientScope.launch {
            _connectionStatus.value = ConnectionStatus.CONNECTING
            Log.i(TAG, "Scheduling reconnect in ${reconnectDelayMs}ms")
            delay(reconnectDelayMs)
            reconnectDelayMs = (reconnectDelayMs * 2).coerceAtMost(maxReconnectDelayMs)
            establishConnection()
        }
    }

    private fun resetReconnectDelay() {
        reconnectDelayMs = 1000L
    }

    private fun sendMessage(msg: String) {
        Log.i(TAG, "Queuing outbound message: $msg")
        val result = writeChannel.trySend(msg)
        if (result.isFailure) {
            Log.e(TAG, "Failed to queue message: $result")
        }
    }

    // --- Outbound Messages perfectly matching SPEC.md ---

    fun sendClientHello(clientId: String) {
        val msg = json.encodeToString(ClientHello.serializer(), ClientHello(clientId = clientId))
        sendMessage(msg)
    }

    fun subscribeMachineList() {
        val msg = json.encodeToString(SubscribeMachineList.serializer(), SubscribeMachineList())
        sendMessage(msg)
    }

    fun attach(sessionId: String, lastSeq: Long? = null) {
        val msg = json.encodeToString(AttachMessage.serializer(), AttachMessage(sessionId = sessionId, lastSeq = lastSeq))
        sendMessage(msg)
    }

    fun detach(sessionId: String) {
        val msg = json.encodeToString(DetachMessage.serializer(), DetachMessage(sessionId = sessionId))
        sendMessage(msg)
    }

    fun sendPtyInput(sessionId: String, data: ByteArray) {
        val base64Str = Base64.encodeToString(data, Base64.NO_WRAP)
        val msg = json.encodeToString(PtyInput.serializer(), PtyInput(sessionId = sessionId, bytes = base64Str))
        sendMessage(msg)
    }

    fun spawnSession(requestId: String, machineId: String, cwd: String, projectOverride: String? = null) {
        val msg = json.encodeToString(
            SpawnSession.serializer(),
            SpawnSession(requestId = requestId, machineId = machineId, cwd = cwd, projectOverride = projectOverride)
        )
        sendMessage(msg)
    }

    fun sendClientFocus(sessionId: String, focused: Boolean) {
        val msg = json.encodeToString(ClientFocus.serializer(), ClientFocus(sessionId = sessionId, focused = focused))
        sendMessage(msg)
    }

    fun sendPtyResize(sessionId: String, cols: Int, rows: Int) {
        val msg = json.encodeToString(PtyResize.serializer(), PtyResize(sessionId = sessionId, cols = cols, rows = rows))
        sendMessage(msg)
    }


    // --- Inner WebSocket Listener ---

    private inner class SocketListener : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            Log.i(TAG, "WebSocket Opened")
            _connectionStatus.value = ConnectionStatus.CONNECTED
            resetReconnectDelay()
            // Auto-send registration once connected in Batch-5 mode
            sendClientHello("test-client-1")
            subscribeMachineList()
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            clientScope.launch(Dispatchers.Default.limitedParallelism(1)) {
                try {
                    val root = json.parseToJsonElement(text).jsonObject
                    val type = root["type"]?.jsonPrimitive?.content ?: return@launch
                    Log.v(TAG, "Incoming WebSocket message type: $type")

                    when (type) {
                        "machine_list" -> {
                            val msg = json.decodeFromString(MachineList.serializer(), text)
                            _machines.value = msg.machines
                        }
                        "session_pty" -> {
                            val msg = json.decodeFromString(SessionPty.serializer(), text)
                            Log.i(TAG, "Received session_pty message for sessionId=${msg.sessionId}, seq=${msg.seq}, base64Length=${msg.bytes.length}")
                            _sessionPty.emit(msg)
                        }
                        "spawn_response" -> {
                            val msg = json.decodeFromString(SpawnResponse.serializer(), text)
                            _spawnResponse.emit(msg)
                        }
                        "replay_unavailable" -> {
                            val msg = json.decodeFromString(ReplayUnavailable.serializer(), text)
                            _replayUnavailable.emit(msg)
                        }
                        "session_started", "session_ended", "session_state_change", "machine_state_change" -> {
                            // These changes trigger updates. We'll simply request a fresh machine list
                            // or wait for the coordinator to push the updated machine list.
                            // Coordinator automatically broadcasts updated machine list, but we can also handle them.
                            Log.d(TAG, "Event message received: $type")
                        }
                    }
                } catch (e: Exception) {
                    Log.e(TAG, "Error decoding message: $text", e)
                }
            }
        }

        override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
            Log.i(TAG, "WebSocket Closing: $code / $reason")
            _connectionStatus.value = ConnectionStatus.DISCONNECTED
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
            Log.i(TAG, "WebSocket Closed: $code / $reason")
            _connectionStatus.value = ConnectionStatus.DISCONNECTED
            if (!isExplicitlyClosed) {
                triggerReconnect()
            }
        }

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            Log.e(TAG, "WebSocket Failure", t)
            _connectionStatus.value = ConnectionStatus.FAILED
            if (!isExplicitlyClosed) {
                triggerReconnect()
            }
        }
    }

    companion object {
        private const val TAG = "pi-remote/ws"
    }
}
