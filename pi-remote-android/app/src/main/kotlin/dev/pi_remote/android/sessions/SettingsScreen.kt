// SPDX-License-Identifier: MIT
package dev.pi_remote.android.sessions

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.BorderStroke
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
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.pi_remote.android.net.ConnectionStatus
import dev.pi_remote.android.push.PushManager

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    currentUrl: String,
    currentJwt: String,
    connectionStatus: ConnectionStatus,
    onSaveAndConnect: (url: String, jwt: String) -> Unit,
    onDisconnect: () -> Unit,
    onNavigateBack: () -> Unit,
    onSavePushPrefs: (Map<String, Boolean>) -> Unit = {}
) {
    var urlInput by remember { mutableStateOf(currentUrl) }
    var jwtInput by remember { mutableStateOf(currentJwt) }

    val context = LocalContext.current
    val pushRegistered = remember { PushManager.isRegistered(context) }
    val pushToggles = remember {
        mutableStateOf(PushManager.localReasonToggles(context))
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(
                        text = "Server Configuration",
                        color = TextPrimary,
                        fontWeight = FontWeight.Bold,
                        fontSize = 18.sp
                    )
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
                colors = TopAppBarDefaults.topAppBarColors(containerColor = DeepSpaceBackground)
            )
        },
        containerColor = DeepSpaceBackground
    ) { paddingValues ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(paddingValues)
                .padding(24.dp)
                .background(DeepSpaceBackground)
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(20.dp),
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Text(
                text = "Developer/Mock Connection Mode",
                color = TextSecondary,
                fontSize = 14.sp,
                fontWeight = FontWeight.Medium
            )

            // URL input field
            OutlinedTextField(
                value = urlInput,
                onValueChange = { urlInput = it },
                label = { Text("Coordinator WebSocket URL", color = TextSecondary) },
                textStyle = androidx.compose.ui.text.TextStyle(
                    color = TextPrimary,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 13.sp
                ),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = IceBlue,
                    unfocusedBorderColor = BorderAccent,
                    focusedLabelColor = IceBlue,
                    cursorColor = IceBlue
                ),
                modifier = Modifier.fillMaxWidth()
            )

            // Optional JWT input field
            OutlinedTextField(
                value = jwtInput,
                onValueChange = { jwtInput = it },
                label = { Text("Mock JWT Token (Optional)", color = TextSecondary) },
                textStyle = androidx.compose.ui.text.TextStyle(
                    color = TextPrimary,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 13.sp
                ),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = IceBlue,
                    unfocusedBorderColor = BorderAccent,
                    focusedLabelColor = IceBlue,
                    cursorColor = IceBlue
                ),
                modifier = Modifier.fillMaxWidth()
            )

            // Status Indicator Info
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(CardBackground)
                    .border(1.dp, BorderAccent, RoundedCornerShape(12.dp))
                    .padding(16.dp)
            ) {
                Text(text = "Current Status", color = TextPrimary, fontSize = 14.sp)
                ConnectionStatusBadge(status = connectionStatus)
            }

            Spacer(modifier = Modifier.height(16.dp))

            // Action Buttons
            Button(
                onClick = { onSaveAndConnect(urlInput, jwtInput) },
                colors = ButtonDefaults.buttonColors(containerColor = IceBlue),
                shape = RoundedCornerShape(12.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp)
            ) {
                Text(
                    text = "Save & Connect",
                    color = DeepSpaceBackground,
                    fontWeight = FontWeight.Bold,
                    fontSize = 14.sp
                )
            }

            if (connectionStatus != ConnectionStatus.DISCONNECTED) {
                OutlinedButton(
                    onClick = onDisconnect,
                    colors = ButtonDefaults.outlinedButtonColors(contentColor = CrimsonRed),
                    border = BorderStroke(1.dp, CrimsonRed),
                    shape = RoundedCornerShape(12.dp),
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(48.dp)
                ) {
                    Text(
                        text = "Disconnect",
                        fontWeight = FontWeight.Bold,
                        fontSize = 14.sp
                    )
                }
            }

            // Push notification preferences (SPEC § 19.6, issue #36).
            Spacer(modifier = Modifier.height(8.dp))
            Column(
                verticalArrangement = Arrangement.spacedBy(4.dp),
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(CardBackground)
                    .border(1.dp, BorderAccent, RoundedCornerShape(12.dp))
                    .padding(16.dp)
            ) {
                Text(
                    text = "Push Notifications",
                    color = TextPrimary,
                    fontSize = 14.sp,
                    fontWeight = FontWeight.Bold
                )
                Text(
                    text = if (pushRegistered) "Registered with coordinator"
                    else "Not registered yet — install the ntfy app, then reopen pi-remote",
                    color = TextSecondary,
                    fontSize = 12.sp
                )
                Spacer(modifier = Modifier.height(4.dp))

                val labels = mapOf(
                    "agent_idle" to "Agent waiting for input",
                    "extension_dialog" to "Permission dialogs",
                    "tool_failure" to "Tool failures",
                    "queue_update" to "Queue updates",
                    "compaction_complete" to "Compaction complete",
                    "extension_error" to "Extension errors",
                    "unresponsive" to "Unresponsive sessions",
                    "session_ended" to "Session ended",
                )
                pushToggles.value.forEach { (reason, enabled) ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.SpaceBetween,
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Text(
                            text = labels[reason] ?: reason,
                            color = TextPrimary,
                            fontSize = 13.sp
                        )
                        Switch(
                            checked = enabled,
                            onCheckedChange = { on ->
                                pushToggles.value = pushToggles.value + (reason to on)
                            },
                            colors = SwitchDefaults.colors(checkedTrackColor = IceBlue)
                        )
                    }
                }

                Button(
                    onClick = { onSavePushPrefs(pushToggles.value) },
                    colors = ButtonDefaults.buttonColors(containerColor = IceBlue),
                    shape = RoundedCornerShape(12.dp),
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(44.dp)
                ) {
                    Text(
                        text = "Save Push Preferences",
                        color = DeepSpaceBackground,
                        fontWeight = FontWeight.Bold,
                        fontSize = 13.sp
                    )
                }
            }
        }
    }
}
