// SPDX-License-Identifier: MIT
package dev.pi_remote.android.push

import android.content.Context
import android.util.Log
import org.unifiedpush.android.connector.MessagingReceiver
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

// UnifiedPush receive path (SPEC.md §§ 19.2-19.5, issues #33/#34/#35):
// the distributor (ntfy app) hands us endpoint changes and raw message
// bytes; we decrypt the crypto_box payload and surface a notification.
class PiRemoteUnifiedPushReceiver : MessagingReceiver() {

    override fun onMessage(context: Context, message: PushMessage, instance: String) {
        Log.i(TAG, "received UnifiedPush message; len=${message.content.size}")
        val plaintext = PushManager.decrypt(context, message.content) ?: return
        val payload = PushManager.parsePayload(plaintext) ?: return
        Notifications.show(context, payload)
    }

    override fun onNewEndpoint(context: Context, endpoint: PushEndpoint, instance: String) {
        Log.i(TAG, "new UnifiedPush endpoint: ${endpoint.url}")
        PushManager.onNewEndpoint(context, endpoint.url)
    }

    override fun onRegistrationFailed(
        context: Context,
        reason: org.unifiedpush.android.connector.FailedReason,
        instance: String,
    ) {
        Log.w(TAG, "UnifiedPush registration failed: $reason")
    }

    override fun onUnregistered(context: Context, instance: String) {
        Log.i(TAG, "UnifiedPush unregistered: instance=$instance")
    }

    private companion object {
        const val TAG = "pi-remote/push"
    }
}
