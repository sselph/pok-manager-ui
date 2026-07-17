package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type UpdateState struct {
	Status        string    `json:"status"` // "idle", "checking", "updating", "starting", "error"
	Message       string    `json:"message"`
	CurrentBuild  string    `json:"currentBuild"`
	LatestBuild   string    `json:"latestBuild"`
	LastCheck     time.Time `json:"lastCheck"`
	UpdateRunning bool      `json:"updateRunning"`
	Progress      int       `json:"progress"` // percentage 0-100
}

type UpdateSettings struct {
	AutoUpdateEnabled  bool   `json:"autoUpdateEnabled"`
	CheckInterval      int    `json:"checkInterval"` // in minutes
	GracePeriod        int    `json:"gracePeriod"`   // in minutes
	IgnoreTimeOfDay    bool   `json:"ignoreTimeOfDay"`
	AllowedWindowStart string `json:"allowedWindowStart"` // "HH:MM"
	AllowedWindowEnd   string `json:"allowedWindowEnd"`   // "HH:MM"
}

var (
	updateState      UpdateState
	updateStateMu    sync.Mutex
	updateBaseDir    string
	updateRegistry   *Registry
	updateSettings   UpdateSettings
	updateSettingsMu sync.RWMutex
	tickerChan       chan bool
)

func init() {
	updateState = UpdateState{
		Status:  "idle",
		Message: "System idle",
	}
	updateSettings = UpdateSettings{
		AutoUpdateEnabled:  false,
		CheckInterval:      30,
		GracePeriod:        5,
		IgnoreTimeOfDay:    true,
		AllowedWindowStart: "00:00",
		AllowedWindowEnd:   "23:59",
	}
	tickerChan = make(chan bool, 1)
}

func getLocalBuildID() (string, error) {
	acfPath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver", "steamapps", "appmanifest_2430930.acf")
	if _, err := os.Stat(acfPath); os.IsNotExist(err) {
		return "0", nil // not installed yet
	}
	content, err := os.ReadFile(acfPath)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`"buildid"\s+"(\d+)"`)
	matches := re.FindSubmatch(content)
	if len(matches) < 2 {
		return "", fmt.Errorf("buildid not found in appmanifest")
	}
	return string(matches[1]), nil
}

type SteamAPIResponse struct {
	Status string `json:"status"`
	Data   map[string]struct {
		Depots struct {
			Branches map[string]struct {
				BuildID string `json:"buildid"`
			} `json:"branches"`
		} `json:"depots"`
	} `json:"data"`
}

func getLatestBuildID() (string, error) {
	// 1. Try public SteamCMD API first
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.steamcmd.net/v1/info/2430930", nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var apiResp SteamAPIResponse
			if err := json.NewDecoder(resp.Body).Decode(&apiResp); err == nil {
				if appData, ok := apiResp.Data["2430930"]; ok {
					if branchData, ok := appData.Depots.Branches["public"]; ok {
						if branchData.BuildID != "" {
							return branchData.BuildID, nil
						}
					}
				}
			}
		}
	}

	// 2. Fallback to running steamcmd in a temporary docker container
	log.Printf("SteamCMD API failed or returned invalid data. Falling back to temporary container check...")
	cmd := exec.Command("docker", "run", "--rm", "acekorneya/asa_server:2_1_latest", "/opt/steamcmd/steamcmd.sh", "+login", "anonymous", "+app_info_print", "2430930", "+quit")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run steamcmd in fallback container: %v, output: %s", err, string(output))
	}

	re := regexp.MustCompile(`"public"\s*\{[^}]*"buildid"\s*"(\d+)"`)
	matches := re.FindSubmatch(output)
	if len(matches) < 2 {
		re2 := regexp.MustCompile(`"buildid"\s+"(\d+)"`)
		matches2 := re2.FindAllSubmatch(output, -1)
		if len(matches2) > 0 {
			return string(matches2[0][1]), nil
		}
		return "", fmt.Errorf("buildid not found in steamcmd output")
	}

	return string(matches[1]), nil
}

func loadUpdateSettings() {
	updateSettingsMu.Lock()
	defer updateSettingsMu.Unlock()

	configDir := filepath.Join(updateBaseDir, "config")
	_ = os.MkdirAll(configDir, 0755)

	settingsPath := filepath.Join(configDir, "updates.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		data, _ := json.MarshalIndent(updateSettings, "", "  ")
		_ = os.WriteFile(settingsPath, data, 0644)
		return
	}

	data, err := os.ReadFile(settingsPath)
	if err == nil {
		var s UpdateSettings
		if err := json.Unmarshal(data, &s); err == nil {
			updateSettings = s
		}
	}
}

func saveUpdateSettings(s UpdateSettings) error {
	updateSettingsMu.Lock()
	updateSettings = s
	updateSettingsMu.Unlock()

	configDir := filepath.Join(updateBaseDir, "config")
	settingsPath := filepath.Join(configDir, "updates.json")

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	err = os.WriteFile(settingsPath, data, 0644)
	if err == nil {
		select {
		case tickerChan <- true:
		default:
		}
	}
	return err
}

func isWithinUpdateWindow() bool {
	updateSettingsMu.RLock()
	defer updateSettingsMu.RUnlock()

	if updateSettings.IgnoreTimeOfDay {
		return true
	}

	now := time.Now()
	nowMinutes := now.Hour()*60 + now.Minute()

	var startH, startM, endH, endM int
	_, _ = fmt.Sscanf(updateSettings.AllowedWindowStart, "%d:%d", &startH, &startM)
	_, _ = fmt.Sscanf(updateSettings.AllowedWindowEnd, "%d:%d", &endH, &endM)

	startMinutes := startH*60 + startM
	endMinutes := endH*60 + endM

	if startMinutes <= endMinutes {
		return nowMinutes >= startMinutes && nowMinutes <= endMinutes
	} else {
		// Overlap midnight (e.g. 22:00 to 04:00)
		return nowMinutes >= startMinutes || nowMinutes <= endMinutes
	}
}

func setUpdateState(status, message string, currentBuild, latestBuild string) {
	updateStateMu.Lock()
	defer updateStateMu.Unlock()
	updateState.Status = status
	updateState.Message = message
	if currentBuild != "" {
		updateState.CurrentBuild = currentBuild
	}
	if latestBuild != "" {
		updateState.LatestBuild = latestBuild
	}
	updateState.LastCheck = time.Now()
	if status == "idle" || status == "error" {
		updateState.Progress = 0
	}
}

func checkUpdatesNow() error {
	current, err := getLocalBuildID()
	if err != nil {
		return err
	}
	latest, err := getLatestBuildID()
	if err != nil {
		return err
	}

	updateStateMu.Lock()
	oldLatest := updateState.LatestBuild
	updateStateMu.Unlock()

	if latest != current && latest != "" && latest != "0" && latest != oldLatest {
		SendGlobalDiscordMessage(fmt.Sprintf("📢 **[POK Update]** A new game update is available!\n• **Installed Build**: `%s`\n• **New Build**: `%s`", current, latest))
	}

	setUpdateState("idle", fmt.Sprintf("Checked updates: local build %s, latest build %s", current, latest), current, latest)
	return nil
}

func hasPlayersOnline(port, pass string) bool {
	resp, err := runRconCommand(port, pass, "ListPlayers")
	if err != nil {
		return false
	}
	return resp != "" && !strings.Contains(strings.ToLower(resp), "no players")
}

func anyPlayersOnline(activeInstances []ActiveInstance) bool {
	for _, inst := range activeInstances {
		if inst.RconPort != "" && inst.Password != "" {
			if hasPlayersOnline(inst.RconPort, inst.Password) {
				return true
			}
		}
	}
	return false
}

type ActiveInstance struct {
	Name     string
	RconPort string
	Password string
}

func runUpdateOrchestration() {
	updateStateMu.Lock()
	if updateState.UpdateRunning {
		updateStateMu.Unlock()
		return
	}
	updateState.UpdateRunning = true
	updateState.Status = "updating"
	updateState.Message = "Starting optimized update process..."
	updateState.Progress = 0
	updateStateMu.Unlock()

	defer func() {
		updateStateMu.Lock()
		updateState.UpdateRunning = false
		updateState.Status = "idle"
		updateState.Message = "Update process completed"
		updateStateMu.Unlock()
	}()

	// 1. Get current and latest builds
	current, _ := getLocalBuildID()
	latest, err := getLatestBuildID()
	if err != nil {
		setUpdateState("error", fmt.Sprintf("Failed to check latest build: %v", err), current, "")
		return
	}

	SendGlobalDiscordMessage(fmt.Sprintf("🔄 **[POK Update]** Starting background download of update build `%s` (servers remain online)...", latest))

	// 2. Discover running instances
	instanceNames, err := ListInstances(updateBaseDir)
	if err != nil {
		setUpdateState("error", fmt.Sprintf("Failed to list instances: %v", err), current, latest)
		return
	}

	var activeInstances []ActiveInstance
	for _, instName := range instanceNames {
		status, _, _ := GetContainerStatus(instName)
		if status == "running" {
			cfg, err := LoadInstanceConfig(updateBaseDir, instName)
			if err == nil {
				activeInstances = append(activeInstances, ActiveInstance{
					Name:     instName,
					RconPort: cfg.Settings["RCON Port"],
					Password: cfg.Settings["Admin Password"],
				})
			}
		}
	}

	// 3. Prepare temp directory using cp -al to create instant space-efficient hardlinks
	setUpdateState("updating", "Preparing temporary update directory...", current, latest)

	tempPath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver_temp")
	livePath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver")
	oldPath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver_old")

	_ = os.RemoveAll(tempPath)
	_ = os.RemoveAll(oldPath)

	if _, err := os.Stat(livePath); err == nil {
		log.Printf("Cloning live server files to temp folder using cp -al...")
		cmd := exec.Command("cp", "-al", livePath, tempPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("Warning: cp -al failed: %v, output: %s. Falling back to standard copy/mkdir.", err, string(output))
			_ = os.RemoveAll(tempPath)
			_ = os.MkdirAll(tempPath, 0755)
		}
	} else {
		_ = os.MkdirAll(tempPath, 0755)
	}

	// Ensure correct ownership of the temp path so the container's pok user (UID 7777) can write to it
	log.Printf("Setting ownership of temp folder to 7777:7777...")
	_ = exec.Command("chown", "-R", "7777:7777", tempPath).Run()
	// Clean up stale downloads/temp folders and break hard links on the manifest to prevent lockups
	log.Printf("[INFO] Cleaning up stale Steam downloads and breaking hard links on manifest...")
	_ = os.RemoveAll(filepath.Join(tempPath, "steamapps", "downloading"))
	_ = os.RemoveAll(filepath.Join(tempPath, "steamapps", "temp"))

	// Generate a clean, basic appmanifest file in tempPath/steamapps/
	// This ensures SteamCMD always recognizes that the app is configured/installed (avoiding "Missing configuration"),
	// but strips any old manifest IDs or stuck update flags (avoiding "Access Denied" or format assertion crashes).
	log.Printf("[INFO] Generating clean basic appmanifest file to prevent Missing configuration / Access Denied loops...")
	_ = os.Remove(filepath.Join(tempPath, "steamapps", "appmanifest_2430930.acf"))
	_ = os.Remove(filepath.Join(tempPath, "appmanifest_2430930.acf"))

	_ = os.MkdirAll(filepath.Join(tempPath, "steamapps"), 0755)
	basicManifest := "\"AppState\"\n{\n\t\"appid\"\t\t\"2430930\"\n\t\"Universe\"\t\t\"1\"\n\t\"name\"\t\t\"ARK: Survival Ascended Dedicated Server\"\n\t\"StateFlags\"\t\t\"4\"\n}\n"
	err = os.WriteFile(filepath.Join(tempPath, "steamapps", "appmanifest_2430930.acf"), []byte(basicManifest), 0644)
	if err != nil {
		log.Printf("[WARNING] Failed to write basic appmanifest: %v", err)
	}

	// Break hard links on all executables and DLLs in the temp directory to prevent Text file busy lockups
	log.Printf("[INFO] Unlinking executables and DLLs to prevent Text file busy locks...")
	unlinkedCount := 0
	walkErr := filepath.Walk(tempPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[WARNING] Walk error on %s: %v", path, err)
			return nil
		}
		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".exe" || ext == ".dll" {
				content, err := os.ReadFile(path)
				if err != nil {
					log.Printf("[WARNING] Failed to read binary %s: %v", path, err)
					return nil
				}

				err = os.Remove(path)
				if err != nil {
					log.Printf("[WARNING] Failed to remove hard link %s: %v", path, err)
					return nil
				}

				err = os.WriteFile(path, content, info.Mode())
				if err != nil {
					log.Printf("[WARNING] Failed to recreate binary %s: %v", path, err)
					return nil
				}
				unlinkedCount++
			}
		}
		return nil
	})
	if walkErr != nil {
		log.Printf("[ERROR] Walk failed: %v", walkErr)
	} else {
		log.Printf("[INFO] Successfully unlinked %d binary/DLL files in temp directory.", unlinkedCount)
	}

	// 4. Run central download inside temporary container pointing to tempPath
	setUpdateState("updating", "Downloading updates in background via SteamCMD (servers remain online)...", current, latest)

	hostPath := os.Getenv("HOST_BASE_DIR")
	if hostPath == "" {
		hostPath = updateBaseDir
	}

	// Prepare diagnostic logs directory
	steamLogsPath := filepath.Join(updateBaseDir, "ServerFiles", "steam_logs")
	_ = os.MkdirAll(steamLogsPath, 0775)
	_ = exec.Command("chown", "-R", "7777:7777", steamLogsPath).Run()

	sharedVolume := fmt.Sprintf("%s/ServerFiles/arkserver_temp:/home/pok/arkserver", hostPath)
	logsVolume := fmt.Sprintf("%s/ServerFiles/steam_logs:/home/pok/.steam/steam/logs", hostPath)

	// Ensure any old container is cleaned up
	_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()

	log.Printf("[INFO] Running SteamCMD update container with mounts: sharedVolume=%s, logsVolume=%s", sharedVolume, logsVolume)
	cmdUpdate := exec.Command("docker", "run",
		"--name", "pok_updater",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--entrypoint", "/opt/steamcmd/steamcmd.sh",
		"--security-opt", "seccomp=unconfined",
		"-v", sharedVolume,
		"-v", logsVolume,
		"acekorneya/asa_server:2_1_latest",
		"+force_install_dir", "/home/pok/arkserver",
		"+login", "anonymous",
		"+@sSteamCmdForcePlatformType", "windows",
		"+app_update", "2430930", "validate",
		"+quit",
	)

	stdoutPipe, err := cmdUpdate.StdoutPipe()
	if err != nil {
		setUpdateState("error", fmt.Sprintf("Failed to open stdout pipe: %v", err), current, latest)
		_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()
		_ = os.RemoveAll(tempPath)
		return
	}
	cmdUpdate.Stderr = cmdUpdate.Stdout // Redirect stderr to stdout so we capture everything

	if err := cmdUpdate.Start(); err != nil {
		setUpdateState("error", fmt.Sprintf("Failed to start SteamCMD container: %v", err), current, latest)
		_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()
		_ = os.RemoveAll(tempPath)
		return
	}

	// Read output line-by-line, splitting on both newlines (\n) and carriage returns (\r)
	// to capture real-time progress ticks from SteamCMD.
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Split(ScanLinesOrCR)
	reProgress := regexp.MustCompile(`Update state[\s:]*(?:\([^)]*\))?[\s:]*(\w+).*?progress:\s*([0-9.]+)`)

	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[SteamCMD] %s", line)

			matches := reProgress.FindStringSubmatch(line)
			if len(matches) >= 3 {
				stateName := matches[1]   // "downloading" or "verifying"
				progressStr := matches[2] // "50.81"

				var progressFloat float64
				_, err := fmt.Sscanf(progressStr, "%f", &progressFloat)
				if err == nil {
					percent := int(progressFloat)
					stateLabel := "Downloading"
					if strings.ToLower(stateName) == "verifying" {
						stateLabel = "Verifying"
					}

					updateStateMu.Lock()
					updateState.Progress = percent
					updateState.Message = fmt.Sprintf("%s updates: %d%% completed...", stateLabel, percent)
					updateStateMu.Unlock()
				}
			}
		}
	}()

	// Wait for command to finish
	if err := cmdUpdate.Wait(); err != nil {
		setUpdateState("error", fmt.Sprintf("SteamCMD update container exited with error: %v", err), current, latest)
		_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()
		_ = os.RemoveAll(tempPath)
		return
	}

	SendGlobalDiscordMessage(fmt.Sprintf("💾 **[POK Update]** Update build `%s` downloaded successfully. Commencing deployment orchestration...", latest))

	// Copy appmanifest from the stopped container to our persistent temp folder
	log.Printf("[INFO] Copying appmanifest from stopped container...")
	_ = os.MkdirAll(filepath.Join(tempPath, "steamapps"), 0755)

	manifestCopied := false

	// 1. Check if the file is already on the host (written directly to the mount by SteamCMD)
	hostManifestSrc := filepath.Join(tempPath, "steamapps", "appmanifest_2430930.acf")
	if _, err := os.Stat(hostManifestSrc); err == nil {
		log.Printf("[INFO] Found appmanifest already on host mount: %s", hostManifestSrc)
		manifestCopied = true
	}

	// 2. If not on host, fallback to copying from various paths inside the container
	if !manifestCopied {
		manifestLocations := []string{
			"pok_updater:/home/pok/.steam/steam/steamapps/appmanifest_2430930.acf",
			"pok_updater:/home/pok/Steam/steamapps/appmanifest_2430930.acf",
			"pok_updater:/opt/steamcmd/steamapps/appmanifest_2430930.acf",
			"pok_updater:/home/pok/arkserver/steamapps/appmanifest_2430930.acf",
		}

		for _, loc := range manifestLocations {
			cpCmd := exec.Command("docker", "cp", loc, filepath.Join(tempPath, "steamapps", "appmanifest_2430930.acf"))
			if output, err := cpCmd.CombinedOutput(); err == nil {
				log.Printf("[INFO] Successfully copied appmanifest from %s", loc)
				manifestCopied = true
				break
			} else {
				log.Printf("[DEBUG] Failed to copy appmanifest from %s: %s", loc, strings.TrimSpace(string(output)))
			}
		}
	}

	if !manifestCopied {
		log.Printf("Warning: Could not find appmanifest_2430930.acf in any host or container paths.")
	} else {
		// Copy it to the parent directory too, since the container startup scripts expect it there
		// to verify that the server files are already installed (and skip the initial install check).
		srcManifest := filepath.Join(tempPath, "steamapps", "appmanifest_2430930.acf")
		dstManifest := filepath.Join(tempPath, "appmanifest_2430930.acf")
		if input, err := os.ReadFile(srcManifest); err == nil {
			_ = os.WriteFile(dstManifest, input, 0644)
			log.Printf("[INFO] Copied appmanifest to parent directory: %s", dstManifest)
		}
	}

	// Clean up conflicting Steam DLL files natively on the host
	log.Printf("[INFO] Cleaning up conflicting Steam DLL files...")
	dlls := []string{
		"steamclient.dll",
		"steamclient64.dll",
		"tier0_s.dll",
		"tier0_s64.dll",
		"vstdlib_s.dll",
		"vstdlib_s64.dll",
	}
	for _, dll := range dlls {
		path := filepath.Join(tempPath, "ShooterGame", "Binaries", "Win64", dll)
		_ = os.Remove(path)
	}

	// Remove the temporary container
	_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()

	// 5. Now that download is complete, warn players and save world on all active instances
	if len(activeInstances) > 0 {
		playersOnline := false
		for _, inst := range activeInstances {
			if inst.RconPort != "" && inst.Password != "" {
				if hasPlayersOnline(inst.RconPort, inst.Password) {
					playersOnline = true
					break
				}
			}
		}

		if playersOnline {
			updateSettingsMu.RLock()
			gracePeriod := updateSettings.GracePeriod
			updateSettingsMu.RUnlock()

			setUpdateState("updating", fmt.Sprintf("Updates downloaded. Players online: commencing %d-minute update warning countdown...", gracePeriod), current, latest)

			// Notify each server's specific channel of the countdown
			for _, inst := range activeInstances {
				cfg, err := LoadInstanceConfig(updateBaseDir, inst.Name)
				if err == nil {
					channelID := cfg.Settings["Discord Channel ID"]
					if channelID != "" {
						SendDiscordMessage(channelID, fmt.Sprintf("⚠️ **[Server Update]** A game update is ready. The server will save and restart in %d minute(s).", gracePeriod))
					}
				}
			}

			earlyLogout := false
			for remaining := gracePeriod; remaining > 0; remaining-- {
				msg := fmt.Sprintf("Server shutting down for update in %d minute(s). Please prepare to log out safely.", remaining)
				if remaining == 1 {
					msg = "Server shutting down for update in 1 minute. Save and shutdown starting soon!"
				}
				for _, inst := range activeInstances {
					if inst.RconPort != "" && inst.Password != "" {
						_, _ = runRconCommand(inst.RconPort, inst.Password, fmt.Sprintf("ServerChat %q", msg))
					}
				}

				// Sleep for 60 seconds in 10-second steps to check if players logged out early
				for step := 0; step < 6; step++ {
					time.Sleep(10 * time.Second)
					if !anyPlayersOnline(activeInstances) {
						earlyLogout = true
						break
					}
				}
				if earlyLogout {
					break
				}
			}

			if earlyLogout {
				log.Printf("[INFO] All players logged out early. Terminating countdown.")
				SendGlobalDiscordMessage("💾 **[POK Update]** All players logged out early. Skipping remaining countdown to apply update immediately...")
			} else {
				// Final 30s
				for _, inst := range activeInstances {
					if inst.RconPort != "" && inst.Password != "" {
						_, _ = runRconCommand(inst.RconPort, inst.Password, "ServerChat \"Server shutting down for update in 30 seconds!\"")
					}
				}

				// Check during the 20s sleep
				for step := 0; step < 4; step++ {
					time.Sleep(5 * time.Second)
					if !anyPlayersOnline(activeInstances) {
						earlyLogout = true
						break
					}
				}

				if !earlyLogout {
					// Final 10s
					for i := 10; i > 0; i-- {
						msg := fmt.Sprintf("Server shutting down in %d...", i)
						for _, inst := range activeInstances {
							if inst.RconPort != "" && inst.Password != "" {
								_, _ = runRconCommand(inst.RconPort, inst.Password, fmt.Sprintf("ServerChat %q", msg))
							}
						}
						time.Sleep(1 * time.Second)
					}
				} else {
					log.Printf("[INFO] All players logged out early during final seconds. Terminating countdown.")
					SendGlobalDiscordMessage("💾 **[POK Update]** All players logged out early. Skipping remaining countdown to apply update immediately...")
				}
			}
		} else {
			setUpdateState("updating", "Updates downloaded. No players online: saving and shutting down...", current, latest)
		}

		// Save world
		setUpdateState("updating", "Saving world data on active servers...", current, latest)
		for _, inst := range activeInstances {
			if inst.RconPort != "" && inst.Password != "" {
				_, _ = runRconCommand(inst.RconPort, inst.Password, "SaveWorld")
			}
			cfg, err := LoadInstanceConfig(updateBaseDir, inst.Name)
			if err == nil {
				channelID := cfg.Settings["Discord Channel ID"]
				if channelID != "" {
					SendDiscordMessage(channelID, "💾 **[Server Update]** Saving World and Restarting...")
				}
			}
		}

		time.Sleep(10 * time.Second)

		// Stop containers
		for _, inst := range activeInstances {
			setUpdateState("updating", fmt.Sprintf("Stopping server container: %s...", inst.Name), current, latest)
			_ = RunDockerCompose(updateBaseDir, inst.Name, "stop")
		}
	}

	// 6. Swap directories atomically (takes milliseconds)
	setUpdateState("updating", "Applying update files (swapping directories)...", current, latest)

	if _, err := os.Stat(livePath); err == nil {
		err = os.Rename(livePath, oldPath)
		if err != nil {
			setUpdateState("error", fmt.Sprintf("Failed to rename live directory: %v", err), current, latest)
			return
		}
	}
	err = os.Rename(tempPath, livePath)
	if err != nil {
		setUpdateState("error", fmt.Sprintf("Failed to swap temp directory to live: %v", err), current, latest)
		_ = os.Rename(oldPath, livePath) // attempt rollback
		return
	}

	// 7. Restart previously active containers
	for _, inst := range activeInstances {
		setUpdateState("updating", fmt.Sprintf("Restarting server container: %s...", inst.Name), current, latest)
		_ = RunDockerCompose(updateBaseDir, inst.Name, "start")

	}

	// 8. Clean up old files in separate goroutine
	go func() {
		log.Printf("Background cleaning up old server files directory...")
		_ = os.RemoveAll(oldPath)
		log.Printf("Background cleanup complete.")
	}()

	// 9. Refresh local build info
	newBuild, _ := getLocalBuildID()
	setUpdateState("idle", fmt.Sprintf("Update completed successfully! New build ID: %s", newBuild), newBuild, latest)

	SendGlobalDiscordMessage(fmt.Sprintf("🎉 **[POK Update]** Update completed successfully! Active servers restarted on build `%s`.", newBuild))
}

func StartUpdateManager(baseDir string, reg *Registry) {
	updateBaseDir = baseDir
	updateRegistry = reg

	loadUpdateSettings()

	// Run initial check on boot
	go func() {
		time.Sleep(5 * time.Second)
		_ = checkUpdatesNow()
	}()

	// Polling loop with dynamic interval rebuilding
	go func() {
		for {
			updateSettingsMu.RLock()
			interval := updateSettings.CheckInterval
			autoUpdate := updateSettings.AutoUpdateEnabled
			updateSettingsMu.RUnlock()

			if interval <= 0 {
				interval = 30
			}

			ticker := time.NewTicker(time.Duration(interval) * time.Minute)

			select {
			case <-ticker.C:
				ticker.Stop()
				_ = checkUpdatesNow()

				if autoUpdate {
					updateStateMu.Lock()
					isDiff := updateState.CurrentBuild != updateState.LatestBuild && updateState.LatestBuild != "0" && updateState.LatestBuild != ""
					isRunning := updateState.UpdateRunning
					updateStateMu.Unlock()

					if isDiff && !isRunning && isWithinUpdateWindow() {
						log.Printf("[INFO] Auto-update triggered centrally!")
						go runUpdateOrchestration()
					}
				}
			case <-tickerChan:
				ticker.Stop()
				log.Printf("[INFO] Update settings changed. Rebuilding checker ticker...")
			}
		}
	}()
}

func handleGetUpdateStatus(w http.ResponseWriter, r *http.Request) {
	updateStateMu.Lock()
	defer updateStateMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updateState)
}

func handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	err := checkUpdatesNow()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to check updates: %v", err), http.StatusInternalServerError)
		return
	}
	updateStateMu.Lock()
	defer updateStateMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updateState)
}

func handleTriggerUpdate(w http.ResponseWriter, r *http.Request) {
	updateStateMu.Lock()
	if updateState.UpdateRunning {
		updateStateMu.Unlock()
		writeJSONError(w, "Update is already running", http.StatusConflict)
		return
	}
	updateStateMu.Unlock()

	go runUpdateOrchestration()

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted","message":"Update process initiated"}`))
}

func handleGetUpdateSettings(w http.ResponseWriter, r *http.Request) {
	updateSettingsMu.RLock()
	defer updateSettingsMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updateSettings)
}

func handleSaveUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var s UpdateSettings
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request payload: %v", err), http.StatusBadRequest)
		return
	}

	if s.CheckInterval <= 0 {
		s.CheckInterval = 30
	}
	if s.GracePeriod < 0 {
		s.GracePeriod = 5
	}
	if s.AllowedWindowStart == "" {
		s.AllowedWindowStart = "00:00"
	}
	if s.AllowedWindowEnd == "" {
		s.AllowedWindowEnd = "23:59"
	}

	if err := saveUpdateSettings(s); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to save settings: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

// ScanLinesOrCR is a custom split function for bufio.Scanner that splits on either
// newlines (\n) or carriage returns (\r). This is essential for capturing inline
// terminal progress updates from tools like SteamCMD.
func ScanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i, c := range data {
		if c == '\n' || c == '\r' {
			return i + 1, data[0:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
