package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CommandEnv holds context paths and configuration.
type CommandEnv struct {
	BaseDir     string
	HostBaseDir string
}

// Command represents a single administrative command.
type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error
}

// Registry stores commands.
type Registry struct {
	commands map[string]Command
	Env      *CommandEnv
}

func NewRegistry(env *CommandEnv) *Registry {
	return &Registry{
		commands: make(map[string]Command),
		Env:      env,
	}
}

func (r *Registry) Register(cmd Command) {
	r.commands[cmd.Name()] = cmd
}

func (r *Registry) Execute(ctx context.Context, name string, args []string, writer io.Writer) error {
	cmd, exists := r.commands[name]
	if !exists {
		return fmt.Errorf("command %q not found", name)
	}
	return cmd.Execute(ctx, r.Env, args, writer)
}

func (r *Registry) List() []Command {
	list := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		list = append(list, cmd)
	}
	return list
}

// ---- Implementations ----

// ListCommand lists all instances
type ListCommand struct{}

func (c *ListCommand) Name() string        { return "list" }
func (c *ListCommand) Description() string { return "List all server instances" }
func (c *ListCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	instances, err := ListInstances(env.BaseDir)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		_, _ = fmt.Fprintln(writer, "No instances found.")
		return nil
	}
	for _, inst := range instances {
		status, health, _ := GetContainerStatus(inst)
		_, _ = fmt.Fprintf(writer, "- %s (Status: %s, Health: %s)\n", inst, status, health)
	}
	return nil
}

// StartCommand starts an instance
type StartCommand struct{}

func (c *StartCommand) Name() string        { return "start" }
func (c *StartCommand) Description() string { return "Start a server instance container" }
func (c *StartCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]
	_, _ = fmt.Fprintf(writer, "Starting instance %s...\n", name)
	return RunDockerCompose(env.BaseDir, name, "start")
}

// StopCommand stops an instance
type StopCommand struct{}

func (c *StopCommand) Name() string        { return "stop" }
func (c *StopCommand) Description() string { return "Stop a server instance container" }
func (c *StopCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]
	_, _ = fmt.Fprintf(writer, "Stopping instance %s...\n", name)
	return RunDockerCompose(env.BaseDir, name, "stop")
}

// RestartCommand restarts an instance
type RestartCommand struct{}

func (c *RestartCommand) Name() string        { return "restart" }
func (c *RestartCommand) Description() string { return "Restart a server instance container" }
func (c *RestartCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]
	_, _ = fmt.Fprintf(writer, "Restarting instance %s...\n", name)
	return RunDockerCompose(env.BaseDir, name, "restart")
}

// UpdateCommand pulls image and restarts
type UpdateCommand struct{}

func (c *UpdateCommand) Name() string        { return "update" }
func (c *UpdateCommand) Description() string { return "Pull latest image and restart instance" }
func (c *UpdateCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]
	_, _ = fmt.Fprintf(writer, "Pulling latest image for %s...\n", name)
	if err := RunDockerCompose(env.BaseDir, name, "pull"); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "Restarting container for %s...\n", name)
	return RunDockerCompose(env.BaseDir, name, "restart")
}

// OrchestrateUpdateCommand runs the SteamCMD game files update process
type OrchestrateUpdateCommand struct{}

func (c *OrchestrateUpdateCommand) Name() string        { return "update-game" }
func (c *OrchestrateUpdateCommand) Description() string { return "Trigger SteamCMD game files update check and orchestration" }
func (c *OrchestrateUpdateCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	StartUpdateManager(env.BaseDir, NewRegistry(env))
	_, _ = fmt.Fprintln(writer, "Starting game update check and download...")
	freshInstall := false
	for _, arg := range args {
		if arg == "-fresh-install" || arg == "--fresh-install" {
			freshInstall = true
		}
	}
	runUpdateOrchestration(freshInstall)
	_, _ = fmt.Fprintln(writer, "Game update orchestration process finished.")
	return nil
}

// CreateCommand creates an instance
type CreateCommand struct{}

func (c *CreateCommand) Name() string        { return "create" }
func (c *CreateCommand) Description() string { return "Create a new server instance" }
func (c *CreateCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]

	// Create with default configs
	settings := make(map[string]string)
	for k, v := range DefaultConfigValues {
		settings[k] = v
	}
	settings["Session Name"] = name

	// Auto-detect next available ports
	asa, rcon, _ := GetNextAvailablePorts(env.BaseDir)
	settings["ASA Port"] = fmt.Sprintf("%d", asa)
	settings["RCON Port"] = fmt.Sprintf("%d", rcon)

	// Check if already exists
	instances, _ := ListInstances(env.BaseDir)
	for _, inst := range instances {
		if strings.EqualFold(inst, name) {
			return fmt.Errorf("instance %s already exists", name)
		}
	}

	_, _ = fmt.Fprintf(writer, "Creating new instance %s...\n", name)
	if err := SaveInstanceConfig(env.BaseDir, env.HostBaseDir, name, "2_1_latest", settings); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(writer, "Instance created successfully.")
	return nil
}

// StatusCommand checks container and resource usage
type StatusCommand struct{}

func (c *StatusCommand) Name() string        { return "status" }
func (c *StatusCommand) Description() string { return "View container status and metrics" }
func (c *StatusCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]
	status, health, err := GetContainerStatus(name)
	if err != nil {
		return err
	}
	cpu, mem, _ := GetContainerResourceUsage(name)

	_, _ = fmt.Fprintf(writer, "Instance: %s\n", name)
	_, _ = fmt.Fprintf(writer, "Status:   %s\n", status)
	_, _ = fmt.Fprintf(writer, "Health:   %s\n", health)
	_, _ = fmt.Fprintf(writer, "CPU %%:    %s\n", cpu)
	_, _ = fmt.Fprintf(writer, "Memory:   %s\n", mem)
	return nil
}

// DeleteCommand stops and removes an instance
type DeleteCommand struct{}

func (c *DeleteCommand) Name() string        { return "delete" }
func (c *DeleteCommand) Description() string { return "Permanently delete a server instance" }
func (c *DeleteCommand) Execute(ctx context.Context, env *CommandEnv, args []string, writer io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("instance name required")
	}
	name := args[0]

	// 1. Stop container first (ignore failure if not running)
	_ = RunDockerCompose(env.BaseDir, name, "stop")

	// 2. Delete the instance directory
	instanceDir := filepath.Join(env.BaseDir, fmt.Sprintf("Instance_%s", name))
	if err := os.RemoveAll(instanceDir); err != nil {
		return fmt.Errorf("failed to delete instance directory: %w", err)
	}

	_, _ = fmt.Fprintf(writer, "Instance %s deleted successfully.\n", name)
	return nil
}
