package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Initialize environment-driven defaults first
	InitDefaults()

	// Parse CLI flags
	portFlag := flag.String("port", "8084", "Port to run the web server on")
	baseDirFlag := flag.String("base-dir", "", "Local path to the workspace directory containing instance files")
	hostBaseDirFlag := flag.String("host-base-dir", "", "Path to the workspace on the host machine (for compose volumes)")
	runCmdFlag := flag.String("run-cmd", "", "Run a single command from CLI and exit (e.g., 'start MyServer')")
	flag.Parse()

	// Determine BaseDir
	baseDir := *baseDirFlag
	if baseDir == "" {
		// Default to directory above backend or current dir
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("failed to get current working directory: %v", err)
		}
		if filepath.Base(wd) == "backend" {
			baseDir = filepath.Dir(wd) // use parent folder
		} else {
			baseDir = wd
		}
	}

	// Create directories if they don't exist
	if err := os.MkdirAll(filepath.Join(baseDir, "config", "POK-manager"), 0755); err != nil {
		log.Fatalf("failed to create config directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(baseDir, "defaults"), 0755); err != nil {
		log.Fatalf("failed to create defaults directory: %v", err)
	}

	// Copy defaults from container-baked defaults if running in Docker and files are missing
	containerDefaults := "/app/defaults"
	if _, err := os.Stat(containerDefaults); err == nil {
		copyFile := func(srcName, dstName string) {
			dstPath := filepath.Join(baseDir, "defaults", dstName)
			if _, err := os.Stat(dstPath); err == nil {
				return // already exists
			}
			srcPath := filepath.Join(containerDefaults, srcName)
			input, err := os.ReadFile(srcPath)
			if err != nil {
				log.Printf("warning: failed to read default %s: %v", srcName, err)
				return
			}
			if err := os.WriteFile(dstPath, input, 0644); err != nil {
				log.Printf("warning: failed to write default %s: %v", dstName, err)
			}
		}
		copyFile("GameUserSettings.ini", "GameUserSettings.ini")
		copyFile("Game.ini", "Game.ini")
	}

	// Determine HostBaseDir
	hostBaseDir := *hostBaseDirFlag
	if hostBaseDir == "" {
		hostBaseDir = os.Getenv("HOST_BASE_DIR")
		if hostBaseDir == "" {
			hostBaseDir = baseDir
		}
	}

	// Initialize Command Registry
	cmdEnv := &CommandEnv{
		BaseDir:     baseDir,
		HostBaseDir: hostBaseDir,
	}
	registry := NewRegistry(cmdEnv)

	// Register commands
	registry.Register(&ListCommand{})
	registry.Register(&StartCommand{})
	registry.Register(&StopCommand{})
	registry.Register(&RestartCommand{})
	registry.Register(&UpdateCommand{})
	registry.Register(&CreateCommand{})
	registry.Register(&StatusCommand{})
	registry.Register(&DeleteCommand{})

	// Handle run-cmd flag
	if *runCmdFlag != "" {
		parts := strings.Fields(*runCmdFlag)
		if len(parts) == 0 {
			log.Fatalf("empty command specified in -run-cmd")
		}
		cmdName := parts[0]
		cmdArgs := parts[1:]

		err := registry.Execute(context.Background(), cmdName, cmdArgs, os.Stdout)
		if err != nil {
			log.Fatalf("command failed: %v", err)
		}
		return
	}

	// Initialize Session Manager and Auth Handlers
	sm := NewSessionManager(baseDir)
	handlers := NewHandlers(sm, registry, baseDir)

	// Set up HTTP mux
	mux := http.NewServeMux()

	// Enable CORS for development
	corsMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		mux.ServeHTTP(w, r)
	})

	// Register REST API handlers
	handlers.RegisterRoutes(mux)

	// Serve Frontend Static Files
	// Serve built frontend if exists, otherwise fallback to simple status
	frontendPath := filepath.Join(baseDir, "frontend", "dist")
	if _, err := os.Stat(frontendPath); err != nil {
		// Fallback to container path where built assets are copied inside the Docker image
		containerPath := "/app/frontend/dist"
		if _, err := os.Stat(containerPath); err == nil {
			frontendPath = containerPath
		}
	}

	if _, err := os.Stat(frontendPath); err == nil {
		fs := http.FileServer(http.Dir(frontendPath))
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If request path doesn't have an extension (meaning it's not a static asset like CSS/JS),
			// serve index.html to allow SPA client routing to work correctly.
			if !strings.Contains(r.URL.Path, ".") && !strings.HasPrefix(r.URL.Path, "/api") {
				http.ServeFile(w, r, filepath.Join(frontendPath, "index.html"))
				return
			}
			fs.ServeHTTP(w, r)
		}))
		log.Printf("Serving frontend from %s", frontendPath)
	} else {
		// Fallback root endpoint
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`
					<!DOCTYPE html>
					<html>
					<head><title>POK Manager Backend</title></head>
					<body style="font-family: sans-serif; background: #0f172a; color: #f8fafc; text-align: center; padding-top: 100px;">
						<h1>POK Manager Backend is running!</h1>
						<p>Frontend is not built. Please run <code>npm run build</code> inside the frontend folder.</p>
					</body>
					</html>
				`))
				return
			}
			http.NotFound(w, r)
		})
	}

	// Start Discord Bot Integration in the background
	StartDiscordBot(baseDir, registry)

	// Start Central Update Coordination in the background
	StartUpdateManager(baseDir, registry)

	addr := ":" + *portFlag
	log.Printf("POK Manager running at http://localhost%s", addr)
	log.Printf("Base directory: %s", baseDir)
	log.Printf("Host base directory: %s", hostBaseDir)
	if err := http.ListenAndServe(addr, corsMux); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
