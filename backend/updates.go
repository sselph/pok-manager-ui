package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UpdateState struct {
	Status                string    `json:"status"` // "idle", "checking", "updating", "starting", "error"
	Message               string    `json:"message"`
	CurrentBuild          string    `json:"currentBuild"`
	LatestBuild           string    `json:"latestBuild"`
	CurrentVersion        string    `json:"currentVersion"`
	LatestVersion         string    `json:"latestVersion"`
	UpdateType            string    `json:"updateType"` // "none", "minor", "major"
	PendingRestartServers []string  `json:"pendingRestartServers"`
	LastCheck             time.Time `json:"lastCheck"`
	UpdateRunning         bool      `json:"updateRunning"`
	Progress              int       `json:"progress"` // percentage 0-100
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
	updateState             UpdateState
	updateStateMu           sync.Mutex
	updateBaseDir           string
	updateRegistry          *Registry
	updateSettings          UpdateSettings
	updateSettingsMu        sync.RWMutex
	tickerChan              chan bool
	pendingRestarts         = make(map[string]bool)
	pendingRestartsMu       sync.Mutex
	cachedCurrentArkVersion string
	deferredWorkerStarted   bool
	deferredWorkerMu        sync.Mutex
)

type ArkVersion struct {
	Major int
	Minor int
	Raw   string
}

func parseArkVersion(raw string) ArkVersion {
	v := ArkVersion{Raw: raw}
	rawClean := strings.TrimSpace(raw)
	rawClean = strings.TrimPrefix(rawClean, "v")
	parts := strings.Split(rawClean, ".")
	if len(parts) >= 1 {
		v.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		v.Minor, _ = strconv.Atoi(parts[1])
	}
	return v
}

// CompareArkVersions returns:
// "major" if newVer.Major > currentVer.Major
// "minor" if newVer.Major == currentVer.Major && newVer.Minor > currentVer.Minor
// "none" otherwise
func CompareArkVersions(currentRaw, newRaw string) string {
	if currentRaw == "" || newRaw == "" {
		return "minor" // fallback to minor if one version is unknown
	}
	cur := parseArkVersion(currentRaw)
	next := parseArkVersion(newRaw)

	if next.Major > cur.Major {
		return "major"
	}
	if next.Major == cur.Major && next.Minor > cur.Minor {
		return "minor"
	}
	return "none"
}

func SetCachedCurrentArkVersion(version string) {
	if version == "" {
		return
	}
	updateStateMu.Lock()
	cachedCurrentArkVersion = version
	updateState.CurrentVersion = version
	updateStateMu.Unlock()
}

func init() {
	updateState = UpdateState{
		Status:                "idle",
		Message:               "System idle",
		UpdateType:            "none",
		PendingRestartServers: []string{},
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

func probeGameVersion(tempPath string) (string, error) {
	log.Printf("[INFO] Probing ARK game version from temp directory...")
	hostPath := os.Getenv("HOST_BASE_DIR")
	if hostPath == "" {
		hostPath = updateBaseDir
	}
	hostTempPath := fmt.Sprintf("%s/ServerFiles/arkserver_temp", hostPath)

	_ = exec.Command("docker", "rm", "-f", "pok_version_probe").Run()
	cmd := exec.Command("docker", "run", "--rm",
		"--name", "pok_version_probe",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--entrypoint", "/bin/bash",
		"-e", "MAP_NAME=InvalidMapName",
		"-v", fmt.Sprintf("%s:/home/pok/arkserver", hostTempPath),
		"acekorneya/asa_server:2_1_latest",
		"-c", "timeout 25 /home/pok/scripts/launch_ASA.sh",
	)

	outBuf := &bytes.Buffer{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	_ = cmd.Run()

	output := outBuf.String()
	reVersion := regexp.MustCompile(`ARK Version:\s*([0-9.]+)`)
	if matches := reVersion.FindAllStringSubmatch(output, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) >= 2 {
			ver := strings.TrimSpace(last[1])
			log.Printf("[INFO] Successfully probed ARK Version: %s", ver)
			return ver, nil
		}
	}

	return "", fmt.Errorf("version string not found in probe output")
}

func startDeferredRestartWorker() {
	deferredWorkerMu.Lock()
	if deferredWorkerStarted {
		deferredWorkerMu.Unlock()
		return
	}
	deferredWorkerStarted = true
	deferredWorkerMu.Unlock()

	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			pendingRestartsMu.Lock()
			if len(pendingRestarts) == 0 {
				pendingRestartsMu.Unlock()
				deferredWorkerMu.Lock()
				deferredWorkerStarted = false
				deferredWorkerMu.Unlock()
				return
			}

			var toCheck []string
			for name := range pendingRestarts {
				toCheck = append(toCheck, name)
			}
			pendingRestartsMu.Unlock()

			for _, instName := range toCheck {
				cfg, err := LoadInstanceConfig(updateBaseDir, instName)
				if err != nil {
					continue
				}
				port := cfg.Settings["RCON Port"]
				pass := cfg.Settings["Admin Password"]

				status, _, _ := GetContainerStatus(instName)
				if status != "running" || (port != "" && pass != "" && !hasPlayersOnline(port, pass)) {
					log.Printf("[INFO] Deferred restart: server %s is empty/stopped. Rebooting instance to load updated minor version...", instName)
					if status == "running" && port != "" && pass != "" {
						_, _ = runRconCommand(port, pass, "SaveWorld")
					}

					_ = RunDockerCompose(updateBaseDir, instName, "restart")

					pendingRestartsMu.Lock()
					delete(pendingRestarts, instName)
					var remaining []string
					for k := range pendingRestarts {
						remaining = append(remaining, k)
					}
					pendingRestartsMu.Unlock()

					updateStateMu.Lock()
					updateState.PendingRestartServers = remaining
					latestVer := updateState.LatestVersion
					if latestVer != "" {
						updateState.CurrentVersion = latestVer
						cachedCurrentArkVersion = latestVer
					}
					if len(remaining) == 0 {
						updateState.UpdateType = "none"
					}
					updateStateMu.Unlock()

					verStr := ""
					if latestVer != "" {
						verStr = fmt.Sprintf(" **v%s**", latestVer)
					}

					channelID := cfg.Settings["Discord Channel ID"]
					if channelID != "" {
						SendDiscordMessage(channelID, fmt.Sprintf("🟢 **[Server Update]** Server `%s` restarted to load Minor Update%s (0 players online).", instName, verStr))
					}

					if len(remaining) == 0 {
						SendGlobalDiscordMessage(fmt.Sprintf("🎉 **[POK Update]** All servers have successfully updated to Minor Version%s!", verStr))
					}
				}
			}
		}
	}()
}

func runUpdateOrchestration(forceFreshInstall bool, forceImmediateReboot bool) {
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

	// 3. Prepare temp directory using cp -a --reflink=always for instant copy-on-write
	setUpdateState("updating", "Preparing temporary update directory...", current, latest)

	tempPath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver_temp")
	livePath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver")
	oldPath := filepath.Join(updateBaseDir, "ServerFiles", "arkserver_old")
	_ = os.RemoveAll(tempPath)
	_ = os.RemoveAll(oldPath)

	hasExistingFiles := false
	if !forceFreshInstall {
		if _, err := os.Stat(filepath.Join(livePath, "ShooterGame")); err == nil {
			hasExistingFiles = true
		}
	}

	if hasExistingFiles {
		log.Printf("Cloning live server files to temp folder using cp -a --reflink=always...")
		cmd := exec.Command("cp", "-a", "--reflink=always", livePath, tempPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[WARNING] Reflink copy failed: %v, output: %s. Falling back to standard cp -a.", err, string(output))
			_ = os.RemoveAll(tempPath)
			cmdFallback := exec.Command("cp", "-a", livePath, tempPath)
			if outFallback, errFallback := cmdFallback.CombinedOutput(); errFallback != nil {
				log.Printf("[WARNING] Fallback cp -a failed: %v, output: %s. Falling back to empty directory.", errFallback, string(outFallback))
				_ = os.RemoveAll(tempPath)
				_ = os.MkdirAll(tempPath, 0775)
			}
		}
	} else {
		_ = os.MkdirAll(tempPath, 0775)
	}

	// Ensure correct ownership of the temp path so the container's pok user (UID 7777) can write to it
	log.Printf("Setting ownership of temp folder to 7777:7777...")
	_ = exec.Command("chown", "-R", "7777:7777", tempPath).Run()
	// Clean up stale Steam downloads and temporary files in the staged update directory
	log.Printf("[INFO] Cleaning up stale Steam downloads and temporary files...")
	_ = os.RemoveAll(filepath.Join(tempPath, "steamapps", "downloading"))
	_ = os.RemoveAll(filepath.Join(tempPath, "steamapps", "temp"))

	// Make sure the steamapps directory exists in tempPath
	_ = os.MkdirAll(filepath.Join(tempPath, "steamapps"), 0775)

	// 4. Run central download inside temporary container pointing to tempPath
	setUpdateState("updating", "Downloading updates in background via SteamCMD (servers remain online)...", current, latest)

	hostPath := os.Getenv("HOST_BASE_DIR")
	if hostPath == "" {
		hostPath = updateBaseDir
	}

	// Prepare diagnostic logs directory
	steamLogsPath := filepath.Join(updateBaseDir, "ServerFiles", "steam_logs")
	_ = os.MkdirAll(steamLogsPath, 0775)

	// Ensure group-write and setgid permissions so container files are writeable/deleteable by host
	if out, err := exec.Command("chmod", "-R", "2775", tempPath).CombinedOutput(); err != nil {
		log.Printf("[WARNING] chmod tempPath failed: %v, output: %s", err, string(out))
	}
	if out, err := exec.Command("chmod", "-R", "2775", steamLogsPath).CombinedOutput(); err != nil {
		log.Printf("[WARNING] chmod steamLogsPath failed: %v, output: %s", err, string(out))
	}

	sharedVolume := fmt.Sprintf("%s/ServerFiles/arkserver_temp:/home/pok/arkserver", hostPath)
	logsVolume := fmt.Sprintf("%s/ServerFiles/steam_logs:/home/pok/.steam/steam/logs", hostPath)

	// Ensure any old container is cleaned up
	_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()

	log.Printf("[INFO] Running SteamCMD update container with mounts: sharedVolume=%s, logsVolume=%s", sharedVolume, logsVolume)
	cmdLine := "UPDATE_SERVER=TRUE STEAMCMD_VALIDATE=TRUE /home/pok/scripts/install_server.sh"

	cmdUpdate := exec.Command("docker", "run",
		"--name", "pok_updater",
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--entrypoint", "/bin/bash",
		"--security-opt", "seccomp=unconfined",
		"-e", "TEMP_DOWNLOAD_ROOT=/home/pok/arkserver/steamapps/temp",
		"-v", sharedVolume,
		"-v", logsVolume,
		"acekorneya/asa_server:2_1_latest",
		"-c", cmdLine,
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

	// Scanner loop for terminal progress
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Split(ScanLinesOrCR)
		reProgress := regexp.MustCompile(`progress:\s+([0-9.]+)\s+\((\d+)\s+/\s+(\d+)\)`)

		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("[SteamCMD] %s", line)

			matches := reProgress.FindStringSubmatch(line)
			if len(matches) >= 2 {
				progressFloat, err := strconv.ParseFloat(matches[1], 64)
				if err == nil {
					percent := int(progressFloat)
					updateStateMu.Lock()
					updateState.Progress = percent
					updateState.Message = fmt.Sprintf("Downloading updates: %d%% completed...", percent)
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

	_ = exec.Command("docker", "rm", "-f", "pok_updater").Run()
	SendGlobalDiscordMessage(fmt.Sprintf("💾 **[POK Update]** Update build `%s` downloaded successfully.", latest))

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

	// 5. Version Probing & Classification
	probedVersion, probeErr := probeGameVersion(tempPath)
	currentVer := cachedCurrentArkVersion
	if currentVer == "" {
		updateStateMu.Lock()
		currentVer = updateState.CurrentVersion
		updateStateMu.Unlock()
	}

	updateType := "minor"
	if probeErr == nil && probedVersion != "" {
		updateStateMu.Lock()
		updateState.LatestVersion = probedVersion
		updateStateMu.Unlock()
		if currentVer != "" {
			updateType = CompareArkVersions(currentVer, probedVersion)
		}
	}

	if forceImmediateReboot {
		updateType = "major"
	}

	updateStateMu.Lock()
	updateState.UpdateType = updateType
	updateStateMu.Unlock()

	// 6. Perform directory swap atomically (takes milliseconds)
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

	// 8. Clean up old files in separate goroutine
	go func() {
		log.Printf("Background cleaning up old server files directory...")
		_ = os.RemoveAll(oldPath)
		log.Printf("Background cleanup complete.")
	}()

	newBuild, _ := getLocalBuildID()

	// 7. Execute update branch (Major vs Minor)
	if updateType == "major" {
		SendGlobalDiscordMessage(fmt.Sprintf("🚨 **[POK Major Update]** ARK Version **v%s** (Build `%s`) is a **MAJOR UPDATE**! Initiating server reboot sequence...", updateState.LatestVersion, newBuild))

		if len(activeInstances) > 0 {
			updateSettingsMu.RLock()
			gracePeriod := updateSettings.GracePeriod
			updateSettingsMu.RUnlock()

			setUpdateState("updating", fmt.Sprintf("Major update: commencing %d-minute countdown...", gracePeriod), current, latest)

			for _, inst := range activeInstances {
				cfg, err := LoadInstanceConfig(updateBaseDir, inst.Name)
				if err == nil {
					channelID := cfg.Settings["Discord Channel ID"]
					if channelID != "" {
						SendDiscordMessage(channelID, fmt.Sprintf("⚠️ **[Major Server Update]** A major game update (**v%s**) is ready. Server will save and restart in %d minute(s).", updateState.LatestVersion, gracePeriod))
					}
				}
			}

			earlyLogout := false
			for remaining := gracePeriod; remaining > 0; remaining-- {
				msg := fmt.Sprintf("Server shutting down for MAJOR update (v%s) in %d minute(s).", updateState.LatestVersion, remaining)
				for _, inst := range activeInstances {
					if inst.RconPort != "" && inst.Password != "" {
						_, _ = runRconCommand(inst.RconPort, inst.Password, fmt.Sprintf("ServerChat %q", msg))
					}
				}

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
				SendGlobalDiscordMessage("💾 **[POK Update]** All players logged out early. Rebooting active servers immediately...")
			}

			// Save world
			for _, inst := range activeInstances {
				if inst.RconPort != "" && inst.Password != "" {
					_, _ = runRconCommand(inst.RconPort, inst.Password, "SaveWorld")
				}
			}

			time.Sleep(5 * time.Second)

			// Restart active instances
			for _, inst := range activeInstances {
				setUpdateState("updating", fmt.Sprintf("Restarting server container: %s...", inst.Name), current, latest)
				_ = RunDockerCompose(updateBaseDir, inst.Name, "restart")
			}
		}

		if probedVersion != "" {
			SetCachedCurrentArkVersion(probedVersion)
		}
		setUpdateState("idle", fmt.Sprintf("Major Update (v%s, Build %s) completed successfully!", updateState.LatestVersion, newBuild), newBuild, latest)
		SendGlobalDiscordMessage(fmt.Sprintf("🎉 **[POK Update]** Major update **v%s** deployed successfully!", updateState.LatestVersion))

	} else {
		// Minor Update Path
		verLabel := ""
		if updateState.LatestVersion != "" {
			verLabel = " **v" + updateState.LatestVersion + "**"
		}

		log.Printf("[INFO] Minor Update%s staged. Servers remain online and will reboot when empty.", verLabel)
		SendGlobalDiscordMessage(fmt.Sprintf("📢 **[POK Update]** Minor Update%s (Build `%s`) staged! Servers remain online and will restart individually when empty (0 players).", verLabel, newBuild))

		pendingRestartsMu.Lock()
		var pendingNames []string
		for _, inst := range activeInstances {
			pendingRestarts[inst.Name] = true
			pendingNames = append(pendingNames, inst.Name)
		}
		pendingRestartsMu.Unlock()

		updateStateMu.Lock()
		updateState.PendingRestartServers = pendingNames
		if len(pendingNames) == 0 {
			updateState.UpdateType = "none"
		}
		updateStateMu.Unlock()

		if len(pendingNames) > 0 {
			startDeferredRestartWorker()
		}

		if probedVersion != "" {
			SetCachedCurrentArkVersion(probedVersion)
		}
		setUpdateState("idle", fmt.Sprintf("Minor Update%s staged. Pending restarts: %v", verLabel, pendingNames), newBuild, latest)
	}
}

func initCurrentVersion(baseDir string) {
	instanceNames, err := ListInstances(baseDir)
	if err != nil {
		return
	}
	reVersion := regexp.MustCompile(`ARK Version:\s*([0-9.]+)`)

	for _, instName := range instanceNames {
		status, _, _ := GetContainerStatus(instName)
		if status == "running" {
			// 1. Try reading directly from Instance_<name>/Saved/Logs/ShooterGame.log on disk
			logPath := filepath.Join(baseDir, fmt.Sprintf("Instance_%s", instName), "Saved", "Logs", "ShooterGame.log")
			if content, err := os.ReadFile(logPath); err == nil {
				if matches := reVersion.FindAllStringSubmatch(string(content), -1); len(matches) > 0 {
					last := matches[len(matches)-1]
					if len(last) >= 2 {
						ver := strings.TrimSpace(last[1])
						log.Printf("[INFO] Initialized current ARK Version from ShooterGame.log for instance %s: v%s", instName, ver)
						SetCachedCurrentArkVersion(ver)
						return
					}
				}
			}

			// 2. Fallback to docker logs stdout
			containerName := fmt.Sprintf("asa_%s", instName)
			cmd := exec.Command("docker", "logs", "--tail", "2000", containerName)
			if output, err := cmd.CombinedOutput(); err == nil {
				if matches := reVersion.FindAllStringSubmatch(string(output), -1); len(matches) > 0 {
					last := matches[len(matches)-1]
					if len(last) >= 2 {
						ver := strings.TrimSpace(last[1])
						log.Printf("[INFO] Initialized current ARK Version from docker logs for instance %s: v%s", instName, ver)
						SetCachedCurrentArkVersion(ver)
						return
					}
				}
			}
		}
	}

	livePath := filepath.Join(baseDir, "ServerFiles", "arkserver")
	if _, err := os.Stat(filepath.Join(livePath, "ShooterGame")); err == nil {
		if ver, err := probeGameVersion(livePath); err == nil && ver != "" {
			log.Printf("[INFO] Initialized current ARK Version from live directory probe: v%s", ver)
			SetCachedCurrentArkVersion(ver)
		}
	}
}

func StartUpdateManager(baseDir string, reg *Registry) {
	updateBaseDir = baseDir
	updateRegistry = reg

	loadUpdateSettings()

	// Run initial check on boot
	go func() {
		time.Sleep(2 * time.Second)
		initCurrentVersion(baseDir)
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
						go runUpdateOrchestration(false, false)
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
	type TriggerRequest struct {
		FreshInstall bool `json:"freshInstall"`
		ForceReboot  bool `json:"forceReboot"`
	}
	var req TriggerRequest
	if r.Header.Get("Content-Type") == "application/json" {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	updateStateMu.Lock()
	if updateState.UpdateRunning {
		updateStateMu.Unlock()
		writeJSONError(w, "Update is already running", http.StatusConflict)
		return
	}
	updateStateMu.Unlock()

	go runUpdateOrchestration(req.FreshInstall, req.ForceReboot)

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
