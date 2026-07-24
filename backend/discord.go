package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorcon/rcon"
)

type DiscordServer struct {
	Name      string
	ChannelID string
	RconPort  string
	Password  string
}

var (
	richRegexp = regexp.MustCompile(`<RichColor Color=".*?>`)
	idRegexp   = regexp.MustCompile(`ID \d+: `)
)

var (
	discordSession  *discordgo.Session
	discordGuildID  string
	baseDirectory   string
	commandRegistry *Registry

	serversMutex   sync.RWMutex
	discordServers = make(map[string]DiscordServer)
)

// StartDiscordBot initializes the Discord bot if the DISCORD_TOKEN env is set.
func StartDiscordBot(baseDir string, reg *Registry) {
	baseDirectory = baseDir
	commandRegistry = reg
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Println("DISCORD_TOKEN not set. Discord bot integration disabled.")
		return
	}

	discordGuildID = os.Getenv("DISCORD_GUILD_ID")

	var err error
	discordSession, err = discordgo.New("Bot " + token)
	if err != nil {
		log.Printf("Error creating Discord session: %v", err)
		return
	}

	// Register event handlers
	discordSession.AddHandler(handleDiscordInteraction)
	discordSession.AddHandler(handleDiscordMessageCreate)

	err = discordSession.Open()
	if err != nil {
		log.Printf("Error opening Discord connection: %v", err)
		return
	}

	log.Println("Discord bot connected successfully!")

	// Perform initial sync of servers and register commands
	SyncDiscordServers()

	// Start RCON chat polling loop
	go pollRconChatLoop()
}

// SyncDiscordServers scans the instances and updates the local registry and Discord commands.
func SyncDiscordServers() {
	if discordSession == nil {
		return
	}

	instances, err := ListInstances(baseDirectory)
	if err != nil {
		log.Printf("Discord sync: failed to list instances: %v", err)
		return
	}

	newServers := make(map[string]DiscordServer)
	var choices []*discordgo.ApplicationCommandOptionChoice

	for _, inst := range instances {
		cfg, err := LoadInstanceConfig(baseDirectory, inst)
		if err != nil {
			continue
		}

		channelID := cfg.Settings["Discord Channel ID"]
		rconEnabled := strings.ToUpper(cfg.Settings["RCON Enabled"]) == "TRUE"
		rconPort := cfg.Settings["RCON Port"]
		adminPassword := cfg.Settings["Admin Password"]

		if channelID != "" && rconEnabled && rconPort != "" {
			key := strings.ToLower(inst)
			newServers[key] = DiscordServer{
				Name:      inst,
				ChannelID: channelID,
				RconPort:  rconPort,
				Password:  adminPassword,
			}

			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  inst,
				Value: key,
			})
		}
	}

	serversMutex.Lock()
	discordServers = newServers
	serversMutex.Unlock()

	// Re-register Discord application commands with updated choices
	registerDiscordCommands(choices)
}

func registerDiscordCommands(choices []*discordgo.ApplicationCommandOptionChoice) {
	if discordSession == nil {
		return
	}

	dmPermission := false
	var defaultMemberPermissions int64 = discordgo.PermissionManageServer

	// Define slash commands dynamically inserting the discovered server choices
	commands := []*discordgo.ApplicationCommand{
		{
			Name:                     "saveworld",
			Description:              "Command to force an ARK world save (via RCON)",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:                     "start",
			Description:              "Start the server container",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:                     "stop",
			Description:              "Stop the server container cleanly",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:                     "restart",
			Description:              "Restart the server container cleanly",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:                     "update",
			Description:              "Pull latest image and restart the server container",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:        "listplayers",
			Description: "Command to list players currently connected (via RCON)",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Description: "The server instance",
					Name:        "server",
					Required:    true,
					Choices:     choices,
				},
			},
		},
		{
			Name:        "chat",
			Description: "Send a message in game chat to the current server channel",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "msg",
					Description: "The message to send",
					Required:    true,
				},
			},
		},
		{
			Name:        "status",
			Description: "Show status, RAM usage, version, and player count for all cluster servers",
		},
		{
			Name:                     "forceupdate",
			Description:              "Trigger global ARK game update check & deployment",
			DefaultMemberPermissions: &defaultMemberPermissions,
			DMPermission:             &dmPermission,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "fresh_install",
					Description: "Perform a clean fresh install (skips file copy)",
					Required:    false,
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "force_reboot",
					Description: "Force immediate reboot of active servers even if minor update",
					Required:    false,
				},
			},
		},
	}

	log.Printf("Syncing Discord slash commands (%d server choices)...", len(choices))
	_, err := discordSession.ApplicationCommandBulkOverwrite(discordSession.State.User.ID, discordGuildID, commands)
	if err != nil {
		log.Printf("Error registering slash commands: %v", err)
	}
}

func handleDiscordInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	cmdName := i.ApplicationCommandData().Name
	options := i.ApplicationCommandData().Options
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	serversMutex.RLock()
	defer serversMutex.RUnlock()

	// Acknowledge immediately using a deferred response
	// This tells Discord "the bot is thinking..." and prevents the 3-second timeout.
	errDefer := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if errDefer != nil {
		log.Printf("Discord bot: failed to defer interaction: %v", errDefer)
		return
	}

	updateResponse := func(content string) {
		_, errEdit := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: &content,
		})
		if errEdit != nil {
			log.Printf("Discord bot: failed to update interaction response: %v", errEdit)
		}
	}

	switch cmdName {
	case "status":
		go func() {
			output := generateClusterStatusOutput(baseDirectory)
			updateResponse(output)
		}()

	case "forceupdate":
		freshInstall := false
		forceReboot := false
		if opt, exists := optionMap["fresh_install"]; exists {
			freshInstall = opt.BoolValue()
		}
		if opt, exists := optionMap["force_reboot"]; exists {
			forceReboot = opt.BoolValue()
		}

		updateResponse(fmt.Sprintf("🔄 **[POK Update]** Triggering game update orchestration (Fresh Install: %t, Force Immediate Reboot: %t)...", freshInstall, forceReboot))
		go runUpdateOrchestration(freshInstall, forceReboot)

	case "start", "stop", "restart", "update", "saveworld", "listplayers":
		serverOpt, exists := optionMap["server"]
		if !exists {
			updateResponse("Error: Server parameter required")
			return
		}
		serverKey := serverOpt.StringValue()
		server, ok := discordServers[serverKey]
		if !ok {
			updateResponse(fmt.Sprintf("Error: Server %q not configured in POK Manager", serverKey))
			return
		}

		if cmdName == "saveworld" || cmdName == "listplayers" {
			go func() {
				rconCmd := "SaveWorld"
				if cmdName == "listplayers" {
					rconCmd = "ListPlayers"
				}
				resp, err := runRconCommand(server.RconPort, server.Password, rconCmd)
				if err != nil {
					updateResponse(fmt.Sprintf("Error executing RCON command: %v", err))
				} else {
					if resp == "" {
						resp = "Command executed successfully (no output)"
					}
					updateResponse(resp)
				}
			}()
		} else {
			// Container actions: start, stop, restart, update
			updateResponse(fmt.Sprintf("Running '%s' action for server %s. Please wait...", cmdName, server.Name))

			go func() {
				err := commandRegistry.Execute(context.Background(), cmdName, []string{server.Name}, io.Discard)
				if err != nil {
					log.Printf("Discord bot: failed to execute container action '%s' for %s: %v", cmdName, server.Name, err)
					updateResponse(fmt.Sprintf("Error executing action '%s' for server %s: %v", cmdName, server.Name, err))
				} else {
					updateResponse(fmt.Sprintf("Action '%s' completed successfully for server %s!", cmdName, server.Name))
				}
			}()
		}

	case "chat":
		msgOpt, exists := optionMap["msg"]
		if !exists {
			updateResponse("Error: Message parameter required")
			return
		}

		// Find which server matches this Discord Channel ID
		var targetServer *DiscordServer
		for _, srv := range discordServers {
			if srv.ChannelID == i.ChannelID {
				ts := srv
				targetServer = &ts
				break
			}
		}

		if targetServer == nil {
			updateResponse("Error: This channel is not mapped to any running ARK server.")
			return
		}

		user := i.Member.Nick
		if user == "" {
			user = i.Member.User.Username
		}

		chatMsg := fmt.Sprintf("(%s) %s", user, msgOpt.StringValue())

		go func() {
			_, err := runRconCommand(targetServer.RconPort, targetServer.Password, fmt.Sprintf("ServerChat %q", chatMsg))
			if err != nil {
				updateResponse(fmt.Sprintf("Error sending message: %v", err))
			} else {
				updateResponse("Message sent to game chat.")
			}
		}()
	}
}

// handleDiscordMessageCreate relays standard chat typed in Discord channels back to game chat
func handleDiscordMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return
	}

	serversMutex.RLock()
	defer serversMutex.RUnlock()

	// Check if message was posted in a mapped channel
	var targetServer *DiscordServer
	for _, srv := range discordServers {
		if srv.ChannelID == m.ChannelID {
			ts := srv
			targetServer = &ts
			break
		}
	}

	if targetServer == nil {
		return
	}

	// Relay message to game RCON
	user := m.Member.Nick
	if user == "" {
		user = m.Author.Username
	}

	// Escape quotes inside message
	safeContent := strings.ReplaceAll(m.Content, `"`, `\"`)
	chatMsg := fmt.Sprintf("(Discord) %s: %s", user, safeContent)

	_, _ = runRconCommand(targetServer.RconPort, targetServer.Password, fmt.Sprintf("ServerChat %q", chatMsg))
}

func runRconCommand(rconPort, password, cmd string) (string, error) {
	host := os.Getenv("RCON_HOST")
	if host == "" {
		host = os.Getenv("HOST_IP")
	}
	if host == "" {
		host = "host.docker.internal"
	}
	addr := fmt.Sprintf("%s:%s", host, rconPort)
	client, err := rcon.Dial(addr, password, rcon.SetDialTimeout(10*time.Second), rcon.SetDeadline(10*time.Second))
	if err != nil {
		return "", fmt.Errorf("RCON connection failed to %s: %w", addr, err)
	}
	defer client.Close()

	return client.Execute(cmd)
}

func pollRconChatLoop() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		serversMutex.RLock()
		// Copy list to avoid holding read lock during network execution
		activeServers := make([]DiscordServer, 0, len(discordServers))
		for _, srv := range discordServers {
			activeServers = append(activeServers, srv)
		}
		serversMutex.RUnlock()

		for _, srv := range activeServers {
			status, _, _ := GetContainerStatus(srv.Name)
			if status != "running" {
				continue
			}

			result, err := runRconCommand(srv.RconPort, srv.Password, "getchat")
			if err != nil {
				continue
			}

			if strings.HasPrefix(strings.TrimSpace(result), "Server received") {
				continue
			}

			sendRelayedChat(result, srv.ChannelID)
		}
	}
}

func sendRelayedChat(message, channelID string) {
	if discordSession == nil {
		return
	}

	lines := strings.Split(strings.ReplaceAll(message, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = richRegexp.ReplaceAllString(line, "")
		line = idRegexp.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, `</>`, "")
		line = strings.TrimSuffix(line, ")")
		line = strings.TrimSpace(line)

		if line == "" || line == "Keep Alive" {
			continue
		}

		// Skip admin log echoes to keep chat clean (unless user wants them)
		if strings.Contains(line, "AdminCmd: ") {
			continue
		}

		// Skip echo of messages sent by Discord itself to avoid infinite feedback loops
		if strings.HasPrefix(line, "Server: (Discord)") || strings.Contains(line, ": (Discord)") {
			continue
		}

		_, err := discordSession.ChannelMessageSend(channelID, line)
		if err != nil {
			log.Printf("Failed to send message to Discord channel %s: %v", channelID, err)
		}
	}
}

// SendDiscordMessage sends a plain text message to a specific channel
func SendDiscordMessage(channelID, message string) {
	if discordSession == nil || channelID == "" {
		return
	}
	_, err := discordSession.ChannelMessageSend(channelID, message)
	if err != nil {
		log.Printf("[ERROR] Failed to send Discord message: %v", err)
	}
}

// SendGlobalDiscordMessage sends a message to the global manager's channel (configured via environment DISCORD_CHANNEL_ID)
func SendGlobalDiscordMessage(message string) {
	globalChannel := os.Getenv("DISCORD_CHANNEL_ID")
	if globalChannel != "" {
		SendDiscordMessage(globalChannel, message)
	}
}

// MonitorInstanceStartup monitors the container health status and version, posting to Discord when healthy
func MonitorInstanceStartup(instanceName, channelID string) {
	if channelID == "" {
		return
	}

	containerName := fmt.Sprintf("asa_%s", instanceName)
	log.Printf("[INFO] Monitoring startup for %s using health checks...", containerName)

	// Set a maximum timeout of 15 minutes
	timeout := time.After(15 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Printf("[WARNING] Timeout reached monitoring startup for %s", instanceName)
			return
		case <-ticker.C:
			status, health, err := GetContainerStatus(instanceName)
			if err != nil {
				log.Printf("[ERROR] Monitor %s: failed to get container status: %v", instanceName, err)
				continue
			}

			// If container stopped or failed to start
			if status == "stopped" || status == "exited" {
				log.Printf("[INFO] Monitor %s: container is stopped or exited. Aborting monitor.", instanceName)
				return
			}

			// If container is healthy (or running and has no healthcheck configured)
			if health == "healthy" || (status == "running" && health == "none") {
				// Container is fully booted! Let's get the version from the logs.
				version := "unknown"
				// Check ShooterGame.log first for latest entry
				logPath := filepath.Join(baseDirectory, fmt.Sprintf("Instance_%s", instanceName), "Saved", "Logs", "ShooterGame.log")
				if content, err := os.ReadFile(logPath); err == nil {
					reVersion := regexp.MustCompile(`ARK Version:\s*([0-9.]+)`)
					if matches := reVersion.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
						last := matches[len(matches)-1]
						if len(last) >= 2 {
							version = strings.TrimSpace(last[1])
						}
					}
				}
				if version == "unknown" {
					cmd := exec.Command("docker", "logs", containerName)
					if output, err := cmd.CombinedOutput(); err == nil {
						reVersion := regexp.MustCompile(`ARK Version:\s*([0-9.]+)`)
						if matches := reVersion.FindAllStringSubmatch(string(output), -1); len(matches) > 0 {
							last := matches[len(matches)-1]
							if len(last) >= 2 {
								version = strings.TrimSpace(last[1])
							}
						}
					}
				}

				msg := fmt.Sprintf("🟢 **[Server Status]** Server `%s` is up and running (v%s)!", instanceName, version)
				SendDiscordMessage(channelID, msg)
				log.Printf("[INFO] Monitor %s: container became healthy (v%s). Posted to Discord.", instanceName, version)
				return
			}
		}
	}
}

func getPlayerCount(port, pass string) string {
	if port == "" || pass == "" {
		return "-"
	}
	resp, err := runRconCommand(port, pass, "ListPlayers")
	if err != nil || resp == "" || strings.Contains(strings.ToLower(resp), "no players") {
		return "0"
	}
	lines := strings.Split(resp, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "ID") {
			count++
		}
	}
	return fmt.Sprintf("%d", count)
}

func getInstanceVersion(baseDir, instName string) string {
	logPath := filepath.Join(baseDir, fmt.Sprintf("Instance_%s", instName), "Saved", "Logs", "ShooterGame.log")
	if content, err := os.ReadFile(logPath); err == nil {
		reVersion := regexp.MustCompile(`ARK Version:\s*([0-9.]+)`)
		if matches := reVersion.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
			last := matches[len(matches)-1]
			if len(last) >= 2 {
				return "v" + strings.TrimSpace(last[1])
			}
		}
	}
	updateStateMu.Lock()
	curVer := updateState.CurrentVersion
	updateStateMu.Unlock()

	if curVer != "" {
		return "v" + curVer
	}
	return "-"
}

func generateClusterStatusOutput(baseDir string) string {
	instances, err := ListInstances(baseDir)
	if err != nil || len(instances) == 0 {
		return "⚠️ No configured server instances found."
	}

	var sb strings.Builder
	sb.WriteString("📊 **[ARK Cluster Status]**\n\n")

	var offlineServers []string

	for _, instName := range instances {
		status, health, _ := GetContainerStatus(instName)
		cfg, _ := LoadInstanceConfig(baseDir, instName)
		rconPort := ""
		adminPass := ""
		if cfg != nil {
			rconPort = cfg.Settings["RCON Port"]
			adminPass = cfg.Settings["Admin Password"]
		}

		if status == "running" {
			stateEmoji := "🟢"
			stateText := "ONLINE"
			if health == "starting" {
				stateEmoji = "🟡"
				stateText = "STARTING"
			} else if health == "unhealthy" {
				stateEmoji = "🔴"
				stateText = "UNHEALTHY"
			}

			_, mem, _ := GetContainerResourceUsage(instName)
			memStr := strings.TrimSpace(mem)
			if memStr == "" || memStr == "0B / 0B" {
				memStr = "N/A"
			}

			versionStr := getInstanceVersion(baseDir, instName)
			playersStr := getPlayerCount(rconPort, adminPass)

			sb.WriteString(fmt.Sprintf("%s **%s** (`%s`)\n", stateEmoji, instName, versionStr))
			sb.WriteString(fmt.Sprintf("• Status: `%s` | Players: `%s` | RAM: `%s`\n\n", stateText, playersStr, memStr))
		} else {
			offlineServers = append(offlineServers, fmt.Sprintf("`%s`", instName))
		}
	}

	if len(offlineServers) > 0 {
		sb.WriteString(fmt.Sprintf("🔴 **Offline**: %s\n\n", strings.Join(offlineServers, ", ")))
	}

	updateStateMu.Lock()
	curVer := updateState.CurrentVersion
	latestVer := updateState.LatestVersion
	upType := updateState.UpdateType
	updateStateMu.Unlock()

	if curVer != "" {
		sb.WriteString(fmt.Sprintf("_Cluster Version: **v%s**_", curVer))
		if upType == "minor" && latestVer != "" {
			sb.WriteString(fmt.Sprintf(" (🟡 Minor Update **v%s** staged)", latestVer))
		} else if upType == "major" && latestVer != "" {
			sb.WriteString(fmt.Sprintf(" (🔴 Major Update **v%s** available)", latestVer))
		}
	}

	return sb.String()
}
