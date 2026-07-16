package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var ConfigKeys = []string{
	"TZ",
	"Memory Limit",
	"BattleEye",
	"API",
	"RCON Enabled",
	"POK Monitor Message",
	"Random Startup Delay",
	"CPU Optimization",
	"Update Server",
	"Update Interval",
	"Update Window Start",
	"Update Window End",
	"Restart Notice",
	"Save Wait Seconds",
	"MOTD Enabled",
	"MOTD",
	"MOTD Duration",
	"Map Name",
	"Session Name",
	"Admin Password",
	"Server Password",
	"ASA Port",
	"RCON Port",
	"Max Players",
	"Show Admin Commands In Chat",
	"Cluster ID",
	"Mod IDs",
	"Passive Mods",
	"Custom Server Args",
	"Discord Channel ID",
}

var DefaultConfigValues = map[string]string{
	"TZ":                          "America/New_York",
	"Memory Limit":                "16G",
	"BattleEye":                   "FALSE",
	"API":                         "FALSE",
	"RCON Enabled":                "TRUE",
	"POK Monitor Message":         "FALSE",
	"Random Startup Delay":        "TRUE",
	"CPU Optimization":            "FALSE",
	"Update Server":               "FALSE",
	"Update Interval":             "24",
	"Update Window Start":         "12:00 AM",
	"Update Window End":           "11:59 PM",
	"Restart Notice":              "30",
	"Save Wait Seconds":           "5",
	"MOTD Enabled":                "FALSE",
	"MOTD":                        "Welcome To my Server",
	"MOTD Duration":               "30",
	"Map Name":                    "TheIsland",
	"Session Name":                "MyServer",
	"Admin Password":              "myadminpassword",
	"Server Password":             "",
	"ASA Port":                    "7777",
	"RCON Port":                   "27020",
	"Max Players":                 "70",
	"Show Admin Commands In Chat": "FALSE",
	"Cluster ID":                  "cluster",
	"Mod IDs":                     "",
	"Passive Mods":                "",
	"Custom Server Args":          "",
	"Discord Channel ID":          "",
}

var EnvKeyMapping = map[string]string{
	"TZ":                          "TZ",
	"BattleEye":                   "BATTLEEYE",
	"API":                         "API",
	"RCON Enabled":                "RCON_ENABLED",
	"POK Monitor Message":         "DISPLAY_POK_MONITOR_MESSAGE",
	"Random Startup Delay":        "RANDOM_STARTUP_DELAY",
	"CPU Optimization":            "CPU_OPTIMIZATION",
	"Update Server":               "UPDATE_SERVER",
	"Update Interval":             "CHECK_FOR_UPDATE_INTERVAL",
	"Update Window Start":         "UPDATE_WINDOW_MINIMUM_TIME",
	"Update Window End":           "UPDATE_WINDOW_MAXIMUM_TIME",
	"Restart Notice":              "RESTART_NOTICE_MINUTES",
	"Save Wait Seconds":           "SAVE_WAIT_SECONDS",
	"MOTD Enabled":                "ENABLE_MOTD",
	"MOTD":                        "MOTD",
	"MOTD Duration":               "MOTD_DURATION",
	"Map Name":                    "MAP_NAME",
	"Session Name":                "SESSION_NAME",
	"Admin Password":              "SERVER_ADMIN_PASSWORD",
	"Server Password":             "SERVER_PASSWORD",
	"ASA Port":                    "ASA_PORT",
	"RCON Port":                   "RCON_PORT",
	"Max Players":                 "MAX_PLAYERS",
	"Show Admin Commands In Chat": "SHOW_ADMIN_COMMANDS_IN_CHAT",
	"Cluster ID":                  "CLUSTER_ID",
	"Mod IDs":                     "MOD_IDS",
	"Passive Mods":                "PASSIVE_MODS",
	"Custom Server Args":          "CUSTOM_SERVER_ARGS",
	"Discord Channel ID":          "DISCORD_CHANNEL_ID",
}

// GetInverseEnvKeyMapping returns maps from ENV_KEY to Friendly Name
func GetInverseEnvKeyMapping() map[string]string {
	inv := make(map[string]string)
	for k, v := range EnvKeyMapping {
		inv[v] = k
	}
	return inv
}

type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
}

type ComposeService struct {
	Image         string   `yaml:"image"`
	ContainerName string   `yaml:"container_name"`
	Restart       string   `yaml:"restart"`
	Environment   []string `yaml:"environment"`
	Ports         []string `yaml:"ports"`
	Volumes       []string `yaml:"volumes"`
	SecurityOpt   []string `yaml:"security_opt"`
	MemLimit      string   `yaml:"mem_limit"`
}

// InstanceConfig contains parsed configurations for an instance.
type InstanceConfig struct {
	Name     string            `json:"name"`
	ImageTag string            `json:"image_tag"`
	Settings map[string]string `json:"settings"`
}

// LoadInstanceConfig reads and parses the docker-compose file for a given instance.
func LoadInstanceConfig(baseDir, name string) (*InstanceConfig, error) {
	instanceDir := filepath.Join(baseDir, fmt.Sprintf("Instance_%s", name))
	composePath := filepath.Join(instanceDir, fmt.Sprintf("docker-compose-%s.yaml", name))

	data, err := os.ReadFile(composePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	service, exists := compose.Services["asaserver"]
	if !exists {
		return nil, fmt.Errorf("asaserver service not found in compose file")
	}

	// Parse Image Tag
	imageParts := strings.Split(service.Image, ":")
	imageTag := "2_1_latest"
	if len(imageParts) > 1 {
		imageTag = imageParts[len(imageParts)-1]
	}

	settings := make(map[string]string)
	// Initialize with defaults
	for k, v := range DefaultConfigValues {
		settings[k] = v
	}

	// Parse memory limit
	if service.MemLimit != "" {
		settings["Memory Limit"] = service.MemLimit
	}

	// Parse environment variables
	invMapping := GetInverseEnvKeyMapping()
	for _, envLine := range service.Environment {
		parts := strings.SplitN(envLine, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		val := parts[1]

		// Strip quotes if any
		val = strings.Trim(val, `"'`)

		if friendlyKey, matches := invMapping[key]; matches {
			settings[friendlyKey] = val
		}
	}

	return &InstanceConfig{
		Name:     name,
		ImageTag: imageTag,
		Settings: settings,
	}, nil
}

// SaveInstanceConfig serializes settings back to the docker-compose file.
func SaveInstanceConfig(baseDir, hostBaseDir, name, imageTag string, settings map[string]string) error {
	instanceDir := filepath.Join(baseDir, fmt.Sprintf("Instance_%s", name))
	composePath := filepath.Join(instanceDir, fmt.Sprintf("docker-compose-%s.yaml", name))

	// Ensure direct directories exist
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		return fmt.Errorf("failed to create instance dir: %w", err)
	}
	// Create API_Logs dir
	if err := os.MkdirAll(filepath.Join(instanceDir, "API_Logs"), 0755); err != nil {
		return fmt.Errorf("failed to create API_Logs dir: %w", err)
	}

	// Fallback hostBaseDir to baseDir if not provided
	if hostBaseDir == "" {
		hostBaseDir = baseDir
	}

	// Resolve image tag
	if imageTag == "" {
		imageTag = "2_1_latest"
	}

	// Build Environment list
	var environment []string
	environment = append(environment, fmt.Sprintf("INSTANCE_NAME=%s", name))

	// Map settings to Environment variables
	for _, key := range ConfigKeys {
		if key == "Memory Limit" {
			continue
		}
		envKey := EnvKeyMapping[key]
		val := settings[key]
		environment = append(environment, fmt.Sprintf("%s=%s", envKey, val))
	}

	// Setup Ports
	asaPort := settings["ASA Port"]
	rconPort := settings["RCON Port"]
	if asaPort == "" {
		asaPort = "7777"
	}
	if rconPort == "" {
		rconPort = "27020"
	}

	ports := []string{
		fmt.Sprintf("%s:%s/tcp", asaPort, asaPort),
		fmt.Sprintf("%s:%s/udp", asaPort, asaPort),
		fmt.Sprintf("%s:%s/tcp", rconPort, rconPort),
	}

	// Setup Volumes (referencing host path context)
	volumes := []string{
		fmt.Sprintf("%s/ServerFiles/arkserver:/home/pok/arkserver", hostBaseDir),
		fmt.Sprintf("%s/Instance_%s/Saved:/home/pok/arkserver/ShooterGame/Saved", hostBaseDir, name),
	}

	// Add API logs if API is enabled
	if strings.ToUpper(settings["API"]) == "TRUE" {
		volumes = append(volumes, fmt.Sprintf("%s/Instance_%s/API_Logs:/home/pok/arkserver/ShooterGame/Binaries/Win64/logs", hostBaseDir, name))
	}

	// Add Cluster volume
	volumes = append(volumes, fmt.Sprintf("%s/Cluster:/home/pok/arkserver/ShooterGame/Saved/clusters", hostBaseDir))

	memLimit := settings["Memory Limit"]
	if memLimit == "" {
		memLimit = "16G"
	}

	// Build the docker-compose file struct
	compose := ComposeFile{
		Version: "2.4",
		Services: map[string]ComposeService{
			"asaserver": {
				Image:         fmt.Sprintf("acekorneya/asa_server:%s", imageTag),
				ContainerName: fmt.Sprintf("asa_%s", name),
				Restart:       "unless-stopped",
				Environment:   environment,
				Ports:         ports,
				Volumes:       volumes,
				SecurityOpt:   []string{"seccomp=unconfined"},
				MemLimit:      memLimit,
			},
		},
	}

	data, err := yaml.Marshal(&compose)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml: %w", err)
	}

	// Write to file
	if err := os.WriteFile(composePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	// Ensure defaults copy if not exist
	savedConfigDir := filepath.Join(instanceDir, "Saved", "Config", "WindowsServer")
	if err := os.MkdirAll(savedConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	copyDefault := func(src, dst string) error {
		if _, err := os.Stat(dst); err == nil {
			return nil // already exists
		}
		input, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, input, 0644)
	}

	_ = copyDefault(filepath.Join(baseDir, "defaults", "GameUserSettings.ini"), filepath.Join(savedConfigDir, "GameUserSettings.ini"))
	_ = copyDefault(filepath.Join(baseDir, "defaults", "Game.ini"), filepath.Join(savedConfigDir, "Game.ini"))

	return nil
}

// InitDefaults overrides DefaultConfigValues using environment variables if set.
func InitDefaults() {
	if val := os.Getenv("DEFAULT_CLUSTER_ID"); val != "" {
		DefaultConfigValues["Cluster ID"] = val
	}
	if val := os.Getenv("DEFAULT_SERVER_PASSWORD"); val != "" {
		DefaultConfigValues["Server Password"] = val
	}
	if val := os.Getenv("DEFAULT_ADMIN_PASSWORD"); val != "" {
		DefaultConfigValues["Admin Password"] = val
	}
	if val := os.Getenv("DEFAULT_MOD_IDS"); val != "" {
		DefaultConfigValues["Mod IDs"] = val
	}
	if val := os.Getenv("DEFAULT_PASSIVE_MODS"); val != "" {
		DefaultConfigValues["Passive Mods"] = val
	}
	if val := os.Getenv("DEFAULT_CUSTOM_SERVER_ARGS"); val != "" {
		DefaultConfigValues["Custom Server Args"] = val
	}
}

// GetNextAvailablePorts scans all existing server instances to find next available ports.
func GetNextAvailablePorts(baseDir string) (int, int, error) {
	instances, err := ListInstances(baseDir)
	if err != nil {
		return 7777, 27020, err
	}

	maxAsaPort := 7776
	maxRconPort := 27019

	for _, inst := range instances {
		cfg, err := LoadInstanceConfig(baseDir, inst)
		if err != nil {
			continue
		}
		asaVal := cfg.Settings["ASA Port"]
		rconVal := cfg.Settings["RCON Port"]

		var asa, rcon int
		_, _ = fmt.Sscan(asaVal, &asa)
		_, _ = fmt.Sscan(rconVal, &rcon)

		if asa > maxAsaPort {
			maxAsaPort = asa
		}
		if rcon > maxRconPort {
			maxRconPort = rcon
		}
	}

	return maxAsaPort + 1, maxRconPort + 1, nil
}
