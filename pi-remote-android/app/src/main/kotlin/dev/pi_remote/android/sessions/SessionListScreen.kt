// SPDX-License-Identifier: MIT
package dev.pi_remote.android.sessions

import androidx.compose.animation.*
import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.pi_remote.android.net.ConnectionStatus
import dev.pi_remote.android.proto.Machine
import dev.pi_remote.android.proto.SessionInfo
import java.util.UUID

// Premium harmonious palette
val DeepSpaceBackground = Color(0xFF0F111A)
val CardBackground = Color(0xFF161A2B)
val BorderAccent = Color(0xFF282F4E)
val TextPrimary = Color(0xFFECEFF4)
val TextSecondary = Color(0xFF8FBCBB)
val AccentGradient = Brush.horizontalGradient(listOf(Color(0xFF81A1C1), Color(0xFFB48EAD)))
val EmeraldGreen = Color(0xFFA3BE8C)
val GlowingEmerald = Color(0xFF5E81AC) // Glow variant
val AmberGold = Color(0xFFEBCB8B)
val CrimsonRed = Color(0xFFBF616A)
val IceBlue = Color(0xFF88C0D0)
val MutedGray = Color(0xFF4C566A)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SessionListScreen(
    connectionStatus: ConnectionStatus,
    machines: List<Machine>,
    onSessionSelected: (SessionInfo) -> Unit,
    onSpawnRequested: (machineId: String, cwd: String) -> Unit,
    onNavigateToSettings: () -> Unit
) {
    var showSpawnDialog by remember { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        Text(
                            text = "Pi Remote",
                            color = TextPrimary,
                            fontWeight = FontWeight.Bold,
                            fontFamily = FontFamily.Monospace,
                            fontSize = 22.sp
                        )
                        ConnectionStatusBadge(connectionStatus)
                    }
                },
                actions = {
                    IconButton(onClick = onNavigateToSettings) {
                        Icon(
                            imageVector = Icons.Default.Settings,
                            contentDescription = "Settings",
                            tint = TextSecondary
                        )
                    }
                },
                colors = TopAppBarDefaults.topAppBarColors(
                    containerColor = DeepSpaceBackground,
                    titleContentColor = TextPrimary
                )
            )
        },
        floatingActionButton = {
            if (connectionStatus == ConnectionStatus.CONNECTED && machines.isNotEmpty()) {
                FloatingActionButton(
                    onClick = { showSpawnDialog = true },
                    containerColor = Color(0xFF88C0D0),
                    contentColor = DeepSpaceBackground,
                    shape = RoundedCornerShape(16.dp),
                    modifier = Modifier.padding(16.dp)
                ) {
                    Icon(imageVector = Icons.Default.Add, contentDescription = "Spawn Session")
                }
            }
        },
        containerColor = DeepSpaceBackground
    ) { paddingValues ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(paddingValues)
                .background(DeepSpaceBackground)
        ) {
            if (machines.isEmpty()) {
                EmptyStateView(connectionStatus, onNavigateToSettings)
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(horizontal = 16.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp)
                ) {
                    items(machines) { machine ->
                        MachineCard(
                            machine = machine,
                            onSessionSelected = onSessionSelected
                        )
                    }
                    item {
                        Spacer(modifier = Modifier.height(80.dp))
                    }
                }
            }

            if (showSpawnDialog && machines.isNotEmpty()) {
                SpawnSessionDialog(
                    availableMachines = machines.map { it.machineId },
                    onDismiss = { showSpawnDialog = false },
                    onConfirm = { machineId, cwd ->
                        onSpawnRequested(machineId, cwd)
                        showSpawnDialog = false
                    }
                )
            }
        }
    }
}

@Composable
fun ConnectionStatusBadge(status: ConnectionStatus) {
    val infiniteTransition = rememberInfiniteTransition(label = "pulse")
    val alpha by infiniteTransition.animateFloat(
        initialValue = 0.4f,
        targetValue = 1.0f,
        animationSpec = infiniteRepeatable(
            animation = tween(1000, easing = EaseInOutQuad),
            repeatMode = RepeatMode.Reverse
        ),
        label = "alphaPulse"
    )

    val (color, text) = when (status) {
        ConnectionStatus.CONNECTED -> EmeraldGreen to "ACTIVE"
        ConnectionStatus.CONNECTING -> AmberGold to "CONNECTING"
        ConnectionStatus.DISCONNECTED -> MutedGray to "DISCONNECTED"
        ConnectionStatus.FAILED -> CrimsonRed to "FAILED"
    }

    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        modifier = Modifier
            .clip(RoundedCornerShape(12.dp))
            .background(color.copy(alpha = 0.15f))
            .border(1.dp, color.copy(alpha = 0.3f), RoundedCornerShape(12.dp))
            .padding(horizontal = 8.dp, vertical = 4.dp)
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(CircleShape)
                .background(
                    if (status == ConnectionStatus.CONNECTING) color.copy(alpha = alpha) else color
                )
        )
        Text(
            text = text,
            color = color,
            fontWeight = FontWeight.Bold,
            fontSize = 10.sp,
            letterSpacing = 1.sp
        )
    }
}

@Composable
fun EmptyStateView(status: ConnectionStatus, onNavigateToSettings: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Text(
            text = "No Machines Registered",
            color = TextPrimary,
            fontWeight = FontWeight.Bold,
            fontSize = 18.sp,
            fontFamily = FontFamily.Monospace
        )
        Spacer(modifier = Modifier.height(8.dp))
        Text(
            text = if (status == ConnectionStatus.CONNECTED) {
                "Make sure your remote Pi daemon is connected to the coordinator."
            } else {
                "Configure your coordinator URL to start monitoring sessions."
            },
            color = MutedGray,
            fontSize = 14.sp,
            modifier = Modifier.padding(horizontal = 24.dp),
            lineHeight = 20.sp
        )
        Spacer(modifier = Modifier.height(24.dp))
        if (status != ConnectionStatus.CONNECTED) {
            Button(
                onClick = onNavigateToSettings,
                colors = ButtonDefaults.buttonColors(containerColor = Color(0xFF81A1C1)),
                shape = RoundedCornerShape(12.dp)
            ) {
                Text(text = "Configure Server", color = DeepSpaceBackground, fontWeight = FontWeight.Bold)
            }
        }
    }
}

@Composable
fun MachineCard(
    machine: Machine,
    onSessionSelected: (SessionInfo) -> Unit
) {
    val isOnline = machine.state == "online"
    val borderGlowColor = if (isOnline) EmeraldGreen.copy(alpha = 0.2f) else MutedGray.copy(alpha = 0.1f)

    Card(
        modifier = Modifier
            .fillMaxWidth()
            .border(1.dp, BorderAccent, RoundedCornerShape(16.dp))
            .shadow(4.dp, shape = RoundedCornerShape(16.dp)),
        colors = CardDefaults.cardColors(containerColor = CardBackground),
        shape = RoundedCornerShape(16.dp)
    ) {
        Column(
            modifier = Modifier.padding(16.dp)
        ) {
            // Machine Header
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.SpaceBetween,
                modifier = Modifier.fillMaxWidth()
            ) {
                Column {
                    Text(
                        text = machine.machineDisplayName,
                        color = TextPrimary,
                        fontWeight = FontWeight.Bold,
                        fontSize = 16.sp
                    )
                    Text(
                        text = "ID: ${machine.machineId}",
                        color = MutedGray,
                        fontSize = 12.sp,
                        fontFamily = FontFamily.Monospace
                    )
                }

                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .background(if (isOnline) EmeraldGreen.copy(alpha = 0.1f) else MutedGray.copy(alpha = 0.1f))
                        .padding(horizontal = 8.dp, vertical = 4.dp)
                ) {
                    Box(
                        modifier = Modifier
                            .size(6.dp)
                            .clip(CircleShape)
                            .background(if (isOnline) EmeraldGreen else MutedGray)
                    )
                    Text(
                        text = machine.state.uppercase(),
                        color = if (isOnline) EmeraldGreen else MutedGray,
                        fontWeight = FontWeight.Bold,
                        fontSize = 9.sp
                    )
                }
            }

            Divider(color = BorderAccent, modifier = Modifier.padding(vertical = 12.dp))

            // Sessions grouped by Project
            val sessionsByProject = machine.sessions.groupBy {
                it.metadata.projectDisplayName ?: it.metadata.projectName.ifEmpty { "System Terminal" }
            }

            if (sessionsByProject.isEmpty()) {
                Text(
                    text = "No active PTY sessions",
                    color = MutedGray,
                    fontSize = 13.sp,
                    modifier = Modifier.padding(vertical = 8.dp)
                )
            } else {
                sessionsByProject.forEach { (project, sessions) ->
                    Text(
                        text = project,
                        color = TextSecondary,
                        fontWeight = FontWeight.SemiBold,
                        fontSize = 13.sp,
                        modifier = Modifier.padding(vertical = 4.dp)
                    )

                    sessions.forEach { session ->
                        SessionItemRow(session = session, onSessionSelected = onSessionSelected)
                    }
                }
            }
        }
    }
}

@Composable
fun SessionItemRow(
    session: SessionInfo,
    onSessionSelected: (SessionInfo) -> Unit
) {
    val (statusColor, statusLabel) = when (session.state) {
        "running" -> EmeraldGreen to "RUNNING"
        "idle" -> AmberGold to "IDLE"
        "attention" -> CrimsonRed to "ATTENTION"
        "paused" -> IceBlue to "PAUSED"
        else -> MutedGray to session.state.uppercase()
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 6.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(Color(0xFF1D223B))
            .border(1.dp, Color(0xFF2E355E), RoundedCornerShape(12.dp))
            .clickable { onSessionSelected(session) }
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = "Session " + session.sessionId.take(8),
                color = TextPrimary,
                fontWeight = FontWeight.Medium,
                fontSize = 14.sp,
                fontFamily = FontFamily.Monospace
            )
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                text = "dir: ${session.metadata.cwd}",
                color = MutedGray,
                fontSize = 11.sp,
                fontFamily = FontFamily.Monospace
            )
        }

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(statusColor.copy(alpha = 0.1f))
                .border(1.dp, statusColor.copy(alpha = 0.2f), RoundedCornerShape(8.dp))
                .padding(horizontal = 8.dp, vertical = 4.dp)
        ) {
            Box(
                modifier = Modifier
                    .size(6.dp)
                    .clip(CircleShape)
                    .background(statusColor)
            )
            Text(
                text = statusLabel,
                color = statusColor,
                fontWeight = FontWeight.Bold,
                fontSize = 9.sp
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SpawnSessionDialog(
    availableMachines: List<String>,
    onDismiss: () -> Unit,
    onConfirm: (machineId: String, cwd: String) -> Unit
) {
    var selectedMachine by remember { mutableStateOf(availableMachines.firstOrNull() ?: "") }
    var cwd by remember { mutableStateOf("/Users/clayton/projects/") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                text = "Spawn New PTY Session",
                color = TextPrimary,
                fontWeight = FontWeight.Bold,
                fontSize = 18.sp
            )
        },
        text = {
            Column(
                verticalArrangement = Arrangement.spacedBy(16.dp),
                modifier = Modifier.fillMaxWidth()
            ) {
                // Machine Select
                Column {
                    Text(
                        text = "Target Machine",
                        color = TextSecondary,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.Medium
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    availableMachines.forEach { machine ->
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable { selectedMachine = machine }
                                .padding(vertical = 4.dp)
                        ) {
                            RadioButton(
                                selected = (selectedMachine == machine),
                                onClick = { selectedMachine = machine },
                                colors = RadioButtonDefaults.colors(selectedColor = IceBlue)
                            )
                            Text(text = machine, color = TextPrimary, fontSize = 14.sp)
                        }
                    }
                }

                // CWD Input
                OutlinedTextField(
                    value = cwd,
                    onValueChange = { cwd = it },
                    label = { Text("Working Directory (CWD)", color = TextSecondary) },
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
            }
        },
        confirmButton = {
            Button(
                onClick = { onConfirm(selectedMachine, cwd) },
                colors = ButtonDefaults.buttonColors(containerColor = IceBlue),
                shape = RoundedCornerShape(8.dp)
            ) {
                Text(text = "Spawn", color = DeepSpaceBackground, fontWeight = FontWeight.Bold)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(text = "Cancel", color = TextSecondary)
            }
        },
        containerColor = CardBackground
    )
}
