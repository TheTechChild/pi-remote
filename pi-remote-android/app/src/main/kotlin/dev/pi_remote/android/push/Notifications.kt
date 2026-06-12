// SPDX-License-Identifier: MIT
package dev.pi_remote.android.push

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.core.app.NotificationCompat

/**
 * Notification channels and rendering per SPEC.md § 19.5 (issue #35):
 *
 *  - attention (HIGH, sound):  extension_dialog, tool_failure, agent_idle
 *  - info      (DEFAULT):      queue_update, compaction_complete,
 *                              session_ended, machine_suspended
 *  - alert     (HIGH):         extension_error, unresponsive
 */
object Notifications {
    private const val CHANNEL_ATTENTION = "attention"
    private const val CHANNEL_INFO = "info"
    private const val CHANNEL_ALERT = "alert"

    private val reasonChannel = mapOf(
        "extension_dialog" to CHANNEL_ATTENTION,
        "tool_failure" to CHANNEL_ATTENTION,
        "agent_idle" to CHANNEL_ATTENTION,
        "queue_update" to CHANNEL_INFO,
        "compaction_complete" to CHANNEL_INFO,
        "session_ended" to CHANNEL_INFO,
        "machine_suspended" to CHANNEL_INFO,
        "extension_error" to CHANNEL_ALERT,
        "unresponsive" to CHANNEL_ALERT,
    )

    private val reasonHuman = mapOf(
        "agent_idle" to "Agent waiting",
        "extension_dialog" to "Permission needed",
        "tool_failure" to "Tool failed",
        "queue_update" to "Queue update",
        "compaction_complete" to "Compaction complete",
        "extension_error" to "Extension error",
        "unresponsive" to "Unresponsive",
        "session_ended" to "Session ended",
        "machine_suspended" to "Machine suspended",
    )

    fun ensureChannels(context: Context) {
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ATTENTION, "Agent needs attention",
                NotificationManager.IMPORTANCE_HIGH,
            ).apply {
                description = "Permission dialogs, failures, and idle agents waiting for input"
                enableVibration(true)
            },
        )
        nm.createNotificationChannel(
            NotificationChannel(
                CHANNEL_INFO, "Session info",
                NotificationManager.IMPORTANCE_DEFAULT,
            ).apply {
                description = "Queue updates and other informational events"
                setSound(null, null)
            },
        )
        nm.createNotificationChannel(
            NotificationChannel(
                CHANNEL_ALERT, "Session alerts",
                NotificationManager.IMPORTANCE_HIGH,
            ).apply { description = "Extension errors and unresponsive sessions" },
        )
    }

    /**
     * Renders one push payload: title `<machine> · <project>`, body =
     * summary, sub-text = human reason, tap → pi-remote://session/<id>.
     * One notification slot per session (latest wins).
     */
    fun show(context: Context, payload: PushPayload) {
        ensureChannels(context)

        val deepLink = Intent(
            Intent.ACTION_VIEW,
            Uri.parse("pi-remote://session/${payload.sessionId}"),
        ).apply {
            setPackage(context.packageName)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        }
        val tap = PendingIntent.getActivity(
            context,
            payload.sessionId.hashCode(),
            deepLink,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        val project = payload.projectDisplayName ?: payload.projectName
        val notification = NotificationCompat.Builder(
            context,
            reasonChannel[payload.reason] ?: CHANNEL_INFO,
        )
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentTitle("${payload.machineDisplayName} · $project")
            .setContentText(payload.summary)
            .setSubText(reasonHuman[payload.reason] ?: payload.reason)
            .setContentIntent(tap)
            .setAutoCancel(true)
            .build()

        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(payload.sessionId.hashCode(), notification)
    }
}
