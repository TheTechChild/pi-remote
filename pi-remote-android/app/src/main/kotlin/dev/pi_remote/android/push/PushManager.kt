// SPDX-License-Identifier: MIT
package dev.pi_remote.android.push

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import android.util.Base64
import android.util.Log
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import com.goterl.lazysodium.interfaces.Box
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException

/**
 * Push pipeline orchestration (SPEC.md §§ 19.2-19.4, issues #28/#33/#34):
 * device X25519 keypair, coordinator registration, payload decryption, and
 * per-reason preference sync.
 */
object PushManager {
    private const val TAG = "pi-remote/push"
    private const val PREFS = "pi_remote_prefs"

    private const val KEY_ENDPOINT = "push_endpoint"
    private const val KEY_CLIENT_ID = "push_client_id"
    private const val KEY_DEVICE_PUB = "push_device_pubkey"
    private const val KEY_DEVICE_SEC = "push_device_seckey"
    private const val KEY_COORD_PUB = "push_coordinator_pubkey"
    private const val PREF_REASON_PREFIX = "push_reason_"

    private val sodium: LazySodiumAndroid by lazy { LazySodiumAndroid(SodiumAndroid()) }
    private val http = OkHttpClient()
    private val json = Json { ignoreUnknownKeys = true }

    /** SPEC.md § 19.6 canonical reasons and default toggles. */
    val reasonDefaults: Map<String, Boolean> = linkedMapOf(
        "agent_idle" to true,
        "extension_dialog" to true,
        "tool_failure" to true,
        "queue_update" to false,
        "compaction_complete" to false,
        "extension_error" to false,
        "unresponsive" to true,
        "session_ended" to false,
    )

    fun prefs(context: Context): SharedPreferences =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun clientId(context: Context): String? =
        prefs(context).getString(KEY_CLIENT_ID, null)

    fun isRegistered(context: Context): Boolean = clientId(context) != null

    fun localReasonToggles(context: Context): Map<String, Boolean> {
        val p = prefs(context)
        return reasonDefaults.mapValues { (reason, def) ->
            p.getBoolean(PREF_REASON_PREFIX + reason, def)
        }
    }

    /**
     * Device X25519 keypair: generated once with crypto_box_keypair,
     * persisted base64 in Keystore-backed encrypted prefs (SecureStore).
     */
    private fun ensureKeypair(context: Context): Pair<ByteArray, ByteArray> {
        val p = SecureStore.prefs(context)
        val pubB64 = p.getString(KEY_DEVICE_PUB, null)
        val secB64 = p.getString(KEY_DEVICE_SEC, null)
        if (pubB64 != null && secB64 != null) {
            return Base64.decode(pubB64, Base64.NO_WRAP) to Base64.decode(secB64, Base64.NO_WRAP)
        }
        val pub = ByteArray(Box.PUBLICKEYBYTES)
        val sec = ByteArray(Box.SECRETKEYBYTES)
        check((sodium as Box.Native).cryptoBoxKeypair(pub, sec)) { "crypto_box_keypair failed" }
        p.edit()
            .putString(KEY_DEVICE_PUB, Base64.encodeToString(pub, Base64.NO_WRAP))
            .putString(KEY_DEVICE_SEC, Base64.encodeToString(sec, Base64.NO_WRAP))
            .apply()
        return pub to sec
    }

    /**
     * Called from the UnifiedPush receiver when the distributor hands us a
     * (new) endpoint URL. Stores it and (re)registers with the coordinator
     * (SPEC.md § 19.2 steps 4-5).
     */
    fun onNewEndpoint(context: Context, endpointUrl: String) {
        val p = prefs(context)
        val previous = p.getString(KEY_ENDPOINT, null)
        p.edit().putString(KEY_ENDPOINT, endpointUrl).apply()
        if (previous != endpointUrl || !isRegistered(context)) {
            registerWithCoordinator(context)
        }
    }

    /** Derives http(s)://host:port from the stored ws(s) coordinator URL. */
    private fun coordinatorHttpBase(context: Context): String? {
        val raw = prefs(context).getString("coordinator_url", null)?.trim().orEmpty()
        if (raw.isEmpty()) return null
        return raw
            .replaceFirst("wss://", "https://")
            .replaceFirst("ws://", "http://")
            .removeSuffix("/")
            .removeSuffix("/v1/client")
    }

    private fun authedRequest(context: Context, url: String): Request.Builder {
        val b = Request.Builder().url(url)
        SecureStore.prefs(context).getString("mock_jwt", null)?.takeIf { it.isNotEmpty() }?.let {
            b.addHeader("Cookie", "CF_Authorization=$it")
        }
        return b
    }

    /**
     * POST /v1/clients/register (SPEC.md § 11.3): device name, UnifiedPush
     * endpoint, and our public key out; client_id and the coordinator's
     * public key back. Fire-and-forget async; safe to call repeatedly.
     */
    fun registerWithCoordinator(context: Context, onResult: ((Boolean) -> Unit)? = null) {
        val base = coordinatorHttpBase(context)
        val endpoint = prefs(context).getString(KEY_ENDPOINT, null)
        if (base == null || endpoint == null) {
            Log.i(TAG, "register skipped: coordinator url or push endpoint missing")
            onResult?.invoke(false)
            return
        }
        val (pub, _) = ensureKeypair(context)

        val body = buildString {
            append("{\"device_display_name\":")
            append(JsonPrimitive("${Build.MANUFACTURER} ${Build.MODEL}").toString())
            append(",\"unifiedpush_endpoint\":")
            append(JsonPrimitive(endpoint).toString())
            append(",\"x25519_pubkey\":\"")
            append(Base64.encodeToString(pub, Base64.NO_WRAP))
            append("\"}")
        }

        val req = authedRequest(context, "$base/v1/clients/register")
            .post(body.toRequestBody("application/json".toMediaType()))
            .build()

        http.newCall(req).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                Log.w(TAG, "register failed: ${e.message}")
                onResult?.invoke(false)
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    val text = it.body?.string().orEmpty()
                    if (!it.isSuccessful) {
                        Log.w(TAG, "register HTTP ${it.code}: $text")
                        onResult?.invoke(false)
                        return
                    }
                    try {
                        val obj = json.parseToJsonElement(text).jsonObject
                        val clientId = obj["client_id"]!!.jsonPrimitive.content
                        val coordPub = obj["coordinator_x25519_pubkey"]!!.jsonPrimitive.content
                        prefs(context).edit()
                            .putString(KEY_CLIENT_ID, clientId)
                            .putString(KEY_COORD_PUB, coordPub)
                            .apply()
                        Log.i(TAG, "registered with coordinator: client_id=$clientId")
                        // Sync the local toggle state so coordinator-side
                        // filtering matches what the UI shows.
                        postPreferences(context, localReasonToggles(context))
                        onResult?.invoke(true)
                    } catch (e: Exception) {
                        Log.w(TAG, "register response parse failed: ${e.message}")
                        onResult?.invoke(false)
                    }
                }
            }
        })
    }

    /**
     * Decrypts a push message body (SPEC.md §§ 10.4, 19.4): wire format is
     * nonce(24) || ciphertext || mac(16), sealed by the coordinator to our
     * public key. Returns plaintext JSON bytes, or null when undecryptable.
     */
    fun decrypt(context: Context, wire: ByteArray): ByteArray? {
        if (wire.size <= Box.NONCEBYTES + Box.MACBYTES) {
            Log.w(TAG, "push message too short: ${wire.size} bytes")
            return null
        }
        val coordPubB64 = prefs(context).getString(KEY_COORD_PUB, null) ?: run {
            Log.w(TAG, "push received before coordinator registration; dropping")
            return null
        }
        val (_, sec) = ensureKeypair(context)
        val coordPub = Base64.decode(coordPubB64, Base64.NO_WRAP)

        val nonce = wire.copyOfRange(0, Box.NONCEBYTES)
        val cipher = wire.copyOfRange(Box.NONCEBYTES, wire.size)
        val plain = ByteArray(cipher.size - Box.MACBYTES)
        val ok = (sodium as Box.Native).cryptoBoxOpenEasy(
            plain, cipher, cipher.size.toLong(), nonce, coordPub, sec,
        )
        if (!ok) {
            Log.w(TAG, "crypto_box_open_easy failed (key mismatch? coordinator keypair rotated?)")
            return null
        }
        return plain
    }

    /**
     * Persists toggles locally and POSTs them to
     * /v1/clients/{client_id}/preferences (SPEC.md § 19.6, issue #36).
     */
    fun postPreferences(
        context: Context,
        toggles: Map<String, Boolean>,
        onResult: ((Boolean) -> Unit)? = null,
    ) {
        val editor = prefs(context).edit()
        toggles.forEach { (reason, on) -> editor.putBoolean(PREF_REASON_PREFIX + reason, on) }
        editor.apply()

        val base = coordinatorHttpBase(context)
        val clientId = clientId(context)
        if (base == null || clientId == null) {
            onResult?.invoke(false)
            return
        }
        val body = toggles.entries.joinToString(",", "{", "}") { (r, v) -> "\"$r\":$v" }
        val req = authedRequest(context, "$base/v1/clients/$clientId/preferences")
            .post(body.toRequestBody("application/json".toMediaType()))
            .build()
        http.newCall(req).enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                Log.w(TAG, "preferences post failed: ${e.message}")
                onResult?.invoke(false)
            }

            override fun onResponse(call: Call, response: Response) {
                response.use {
                    if (!it.isSuccessful) Log.w(TAG, "preferences HTTP ${it.code}")
                    onResult?.invoke(it.isSuccessful)
                }
            }
        })
    }

    /** Parses the decrypted push payload (SPEC.md § 10.4). */
    fun parsePayload(plaintext: ByteArray): PushPayload? = try {
        val obj = json.parseToJsonElement(plaintext.decodeToString()).jsonObject
        PushPayload(
            reason = obj["reason"]!!.jsonPrimitive.content,
            machineDisplayName = obj["machine_display_name"]!!.jsonPrimitive.content,
            sessionId = obj["session_id"]!!.jsonPrimitive.content,
            projectName = obj["project_name"]!!.jsonPrimitive.content,
            projectDisplayName = obj["project_display_name"]?.jsonPrimitive?.takeIf { it.isString }?.content,
            summary = obj["summary"]!!.jsonPrimitive.content,
        )
    } catch (e: Exception) {
        Log.w(TAG, "push payload parse failed: ${e.message}")
        null
    }
}

data class PushPayload(
    val reason: String,
    val machineDisplayName: String,
    val sessionId: String,
    val projectName: String,
    val projectDisplayName: String?,
    val summary: String,
)
