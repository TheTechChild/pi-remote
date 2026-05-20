// SPDX-License-Identifier: MIT
package dev.pi_remote.android.proto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Common base or standalone models for coordinator-to-app and app-to-coordinator WebSocket protocols.
 * Matches SPEC.md § 10.3 perfectly.
 */

@Serializable
data class ClientHello(
    @SerialName("type") val type: String = "client_hello",
    @SerialName("v") val v: Int = 1,
    @SerialName("client_id") val clientId: String,
    @SerialName("app_version") val appVersion: String = "1.0.0"
)

@Serializable
data class SubscribeMachineList(
    @SerialName("type") val type: String = "subscribe_machine_list",
    @SerialName("v") val v: Int = 1
)

@Serializable
data class AttachMessage(
    @SerialName("type") val type: String = "attach",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("last_seq") val lastSeq: Long? = null
)

@Serializable
data class DetachMessage(
    @SerialName("type") val type: String = "detach",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String
)

@Serializable
data class PtyInput(
    @SerialName("type") val type: String = "pty_input",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("bytes") val bytes: String // base64 encoded
)

@Serializable
data class SpawnSession(
    @SerialName("type") val type: String = "spawn_session",
    @SerialName("v") val v: Int = 1,
    @SerialName("request_id") val requestId: String,
    @SerialName("machine_id") val machineId: String,
    @SerialName("cwd") val cwd: String,
    @SerialName("project_override") val projectOverride: String? = null
)

@Serializable
data class ClientFocus(
    @SerialName("type") val type: String = "client_focus",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("focused") val focused: Boolean
)

@Serializable
data class MachineList(
    @SerialName("type") val type: String = "machine_list",
    @SerialName("v") val v: Int = 1,
    @SerialName("machines") val machines: List<Machine> = emptyList()
)

@Serializable
data class Machine(
    @SerialName("machine_id") val machineId: String,
    @SerialName("machine_display_name") val machineDisplayName: String,
    @SerialName("state") val state: String, // online, offline, suspended
    @SerialName("sessions") val sessions: List<SessionInfo> = emptyList()
)

@Serializable
data class SessionInfo(
    @SerialName("session_id") val sessionId: String,
    @SerialName("metadata") val metadata: SessionMetadata,
    @SerialName("state") val state: String // running, idle, paused, attention, ended
)

@Serializable
data class SessionMetadata(
    @SerialName("cwd") val cwd: String = "",
    @SerialName("hostname") val hostname: String = "",
    @SerialName("model") val model: String = "",
    @SerialName("project_name") val projectName: String = "",
    @SerialName("project_display_name") val projectDisplayName: String? = null,
    @SerialName("spawn_token") val spawnToken: String? = null,
    @SerialName("started_at") val startedAt: Long = 0,
    @SerialName("last_seq") val lastSeq: Long? = null
)

@Serializable
data class SessionPty(
    @SerialName("type") val type: String = "session_pty",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("seq") val seq: Long,
    @SerialName("bytes") val bytes: String // base64 encoded
)

@Serializable
data class SessionStarted(
    @SerialName("type") val type: String = "session_started",
    @SerialName("v") val v: Int = 1,
    @SerialName("machine_id") val machineId: String,
    @SerialName("session_id") val sessionId: String,
    @SerialName("metadata") val metadata: SessionMetadata
)

@Serializable
data class SessionEnded(
    @SerialName("type") val type: String = "session_ended",
    @SerialName("v") val v: Int = 1,
    @SerialName("machine_id") val machineId: String,
    @SerialName("session_id") val sessionId: String
)

@Serializable
data class SessionStateChange(
    @SerialName("type") val type: String = "session_state_change",
    @SerialName("v") val v: Int = 1,
    @SerialName("machine_id") val machineId: String,
    @SerialName("session_id") val sessionId: String,
    @SerialName("state") val state: String
)

@Serializable
data class MachineStateChange(
    @SerialName("type") val type: String = "machine_state_change",
    @SerialName("v") val v: Int = 1,
    @SerialName("machine_id") val machineId: String,
    @SerialName("state") val state: String
)

@Serializable
data class ReplayUnavailable(
    @SerialName("type") val type: String = "replay_unavailable",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("earliest_available_seq") val earliestAvailableSeq: Long,
    @SerialName("current_seq") val currentSeq: Long
)

@Serializable
data class SpawnResponse(
    @SerialName("type") val type: String = "spawn_response",
    @SerialName("v") val v: Int = 1,
    @SerialName("request_id") val requestId: String,
    @SerialName("success") val success: Boolean,
    @SerialName("error") val error: String? = null,
    @SerialName("session_id") val sessionId: String? = null
)

@Serializable
data class PtyResize(
    @SerialName("type") val type: String = "pty_resize",
    @SerialName("v") val v: Int = 1,
    @SerialName("session_id") val sessionId: String,
    @SerialName("cols") val cols: Int,
    @SerialName("rows") val rows: Int
)

