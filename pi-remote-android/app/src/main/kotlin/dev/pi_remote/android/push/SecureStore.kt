// SPDX-License-Identifier: MIT
package dev.pi_remote.android.push

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * Keystore-backed storage for the app's secrets (SPEC.md § D5):
 * the CF Access JWT and the device's X25519 keypair. Values are encrypted
 * with an AES256-GCM master key held in the Android Keystore, so they are
 * unreadable in app-data backups and on rooted-device file dumps.
 *
 * Non-secret state (coordinator URL, push endpoint, client_id, the
 * coordinator's *public* key, reason toggles) stays in the plain
 * `pi_remote_prefs` file.
 *
 * First access migrates any legacy plaintext copies of the secret keys out
 * of `pi_remote_prefs` (installs that predate this class) and deletes them.
 */
object SecureStore {
    private const val TAG = "pi-remote/secure"
    private const val FILE = "pi_remote_secure_prefs"
    private const val LEGACY_FILE = "pi_remote_prefs"

    /** Keys that must only ever live in the encrypted file. */
    val SECRET_KEYS = listOf("mock_jwt", "push_device_pubkey", "push_device_seckey")

    @Volatile
    private var cached: SharedPreferences? = null

    fun prefs(context: Context): SharedPreferences {
        cached?.let { return it }
        synchronized(this) {
            cached?.let { return it }
            val masterKey = MasterKey.Builder(context.applicationContext)
                .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                .build()
            val secure = EncryptedSharedPreferences.create(
                context.applicationContext,
                FILE,
                masterKey,
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
            migrateLegacyPlaintext(context.applicationContext, secure)
            cached = secure
            return secure
        }
    }

    /**
     * Moves secrets written by pre-SecureStore builds out of the plaintext
     * prefs file. Existing encrypted values win over plaintext leftovers,
     * and the plaintext copies are removed either way.
     */
    private fun migrateLegacyPlaintext(context: Context, secure: SharedPreferences) {
        val plain = context.getSharedPreferences(LEGACY_FILE, Context.MODE_PRIVATE)
        var migrated = 0
        val secureEdit = secure.edit()
        val plainEdit = plain.edit()
        for (key in SECRET_KEYS) {
            val value = plain.getString(key, null) ?: continue
            if (!secure.contains(key)) {
                secureEdit.putString(key, value)
            }
            plainEdit.remove(key)
            migrated++
        }
        if (migrated > 0) {
            secureEdit.apply()
            plainEdit.apply()
            Log.i(TAG, "migrated $migrated secret(s) from plaintext prefs to Keystore-backed storage")
        }
    }
}
