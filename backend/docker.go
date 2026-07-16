package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ListInstances scans the base directory for folders starting with "Instance_"
// and returns their names.
func ListInstances(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read base directory: %w", err)
	}

	var instances []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "Instance_") {
			instanceName := strings.TrimPrefix(name, "Instance_")
			// Double check if compose file exists
			composePath := filepath.Join(baseDir, name, fmt.Sprintf("docker-compose-%s.yaml", instanceName))
			if _, err := os.Stat(composePath); err == nil {
				instances = append(instances, instanceName)
			}
		}
	}
	return instances, nil
}

// GetContainerStatus returns the status (e.g. "running", "exited", "none")
// and health (e.g. "healthy", "unhealthy", "none") of the instance's container.
func GetContainerStatus(instanceName string) (string, string, error) {
	containerName := fmt.Sprintf("asa_%s", instanceName)

	// Run docker inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}'
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If command fails, it likely means the container doesn't exist yet
		return "stopped", "none", nil
	}

	outStr := strings.TrimSpace(string(output))
	parts := strings.Split(outStr, "|")
	if len(parts) != 2 {
		return "unknown", "none", nil
	}

	return parts[0], parts[1], nil
}

// GetContainerResourceUsage runs docker stats once and returns CPU/Memory usage
func GetContainerResourceUsage(instanceName string) (cpu string, mem string, err error) {
	containerName := fmt.Sprintf("asa_%s", instanceName)
	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}|{{.MemUsage}}", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "0.00%", "0B / 0B", nil // container is stopped or doesn't exist
	}
	outStr := strings.TrimSpace(string(output))
	parts := strings.Split(outStr, "|")
	if len(parts) != 2 {
		return "0.00%", "0B / 0B", nil
	}
	return parts[0], parts[1], nil
}

// RunDockerCompose executes a docker compose operation for a specific instance.
func RunDockerCompose(baseDir, instanceName string, action string) error {
	instanceDir := filepath.Join(baseDir, fmt.Sprintf("Instance_%s", instanceName))
	composePath := filepath.Join(instanceDir, fmt.Sprintf("docker-compose-%s.yaml", instanceName))

	if _, err := os.Stat(composePath); err != nil {
		return fmt.Errorf("compose file not found for instance %s", instanceName)
	}

	// Prepare the command arguments
	var args []string
	switch action {
	case "start":
		args = []string{"compose", "-f", composePath, "up", "-d"}
	case "stop":
		args = []string{"compose", "-f", composePath, "down"}
	case "restart":
		args = []string{"compose", "-f", composePath, "restart"}
	case "pull":
		args = []string{"compose", "-f", composePath, "pull"}
	default:
		return fmt.Errorf("unsupported docker compose action: %s", action)
	}

	cmd := exec.Command("docker", args...)
	cmd.Dir = instanceDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose %s failed: %s: %w", action, string(output), err)
	}

	// Trigger Discord status updates and boot monitoring if configured
	if action == "start" || action == "restart" || action == "stop" {
		cfg, err := LoadInstanceConfig(baseDir, instanceName)
		if err == nil {
			channelID := cfg.Settings["Discord Channel ID"]
			if channelID != "" {
				switch action {
				case "start":
					SendDiscordMessage(channelID, fmt.Sprintf("🔄 **[Server Status]** `%s` is starting up...", instanceName))
					go MonitorInstanceStartup(instanceName, channelID)
				case "restart":
					SendDiscordMessage(channelID, fmt.Sprintf("🔄 **[Server Status]** `%s` is restarting...", instanceName))
					go MonitorInstanceStartup(instanceName, channelID)
				case "stop":
					SendDiscordMessage(channelID, fmt.Sprintf("🔴 **[Server Status]** `%s` is stopping...", instanceName))
				}
			}
		}
	}

	return nil
}

// StreamLogs runs `docker logs -f --tail 200 asa_<name>` and streams the output to the writer.
func StreamLogs(ctx context.Context, instanceName string, out io.Writer) error {
	containerName := fmt.Sprintf("asa_%s", instanceName)

	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", "--tail", "200", containerName)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // Merge stderr to stdout for convenience

	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReader(stdout)
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			_, _ = fmt.Fprint(out, line)
		}
	}
}
