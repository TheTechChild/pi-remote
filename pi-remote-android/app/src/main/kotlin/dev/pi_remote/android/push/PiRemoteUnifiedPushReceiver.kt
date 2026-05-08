// SPDX-License-Identifier: MIT
package dev.pi_remote.android.push

import android.content.Context
import android.util.Log
import org.unifiedpush.android.connector.MessagingReceiver
import org.unifiedpush.android.connector.data.PushEndpoint
import org.unifiedpush.android.connector.data.PushMessage

// Phase-0 skeleton. Real responsibilities (decrypt the crypto_box payload and
// surface a notification) are implemented in milestones M7-M9.
class PiRemoteUnifiedPushReceiver : MessagingReceiver() {

    override fun onMessage(context: Context, message: PushMessage, instance: String) {
        Log.i(TAG, "received UnifiedPush message; len=${message.content.size}")
    }

    override fun onNewEndpoint(context: Context, endpoint: PushEndpoint, instance: String) {
        Log.i(TAG, "new UnifiedPush endpoint: ${endpoint.url}")
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
