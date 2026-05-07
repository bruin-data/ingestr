package uv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/bruin-data/ingestr/internal/config"
)

const (
	Version = "0.9.13"

	Shell               = "/bin/sh"
	ShellSubcommandFlag = "-c"
)

// Checker handles checking and installing the uv package manager.
type Checker struct {
	mut sync.Mutex
}

// EnsureUvInstalled checks if uv is installed and installs it if not present, then returns the full path of the binary.
func (u *Checker) EnsureUvInstalled(ctx context.Context) (string, error) {
	u.mut.Lock()
	defer u.mut.Unlock()

	homeDir, err := getGongHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get gong home directory: %w", err)
	}

	binaryName := "uv"
	if runtime.GOOS == "windows" {
		binaryName = "uv.exe"
	}

	uvBinaryPath := filepath.Join(homeDir, binaryName)
	if _, err := os.Stat(uvBinaryPath); os.IsNotExist(err) {
		err = u.installUvCommand(ctx, homeDir)
		if err != nil {
			return "", err
		}
		return uvBinaryPath, nil
	}

	// Check version
	cmd := exec.Command(uvBinaryPath, "self", "version", "--output-format", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to check uv version: %w -- Output: %s", err, output)
	}

	var uvVersion struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(output, &uvVersion); err != nil {
		return "", fmt.Errorf("failed to parse uv version: %w", err)
	}

	if uvVersion.Version != Version {
		err = u.installUvCommand(ctx, homeDir)
		if err != nil {
			return "", err
		}
		return uvBinaryPath, nil
	}

	return uvBinaryPath, nil
}

func (u *Checker) installUvCommand(ctx context.Context, dest string) error {
	config.Debug("[UV] Installing uv v%s...", Version)
	fmt.Println("Installing uv package manager (one-time setup)...")

	var commandInstance *exec.Cmd
	if runtime.GOOS == "windows" {
		commandInstance = exec.Command("powershell", "-ExecutionPolicy", "ByPass", "-c",
			fmt.Sprintf("$env:NO_MODIFY_PATH=1 ; $env:UV_INSTALL_DIR='%s' ; irm https://astral.sh/uv/%s/install.ps1 | iex", dest, Version))
	} else {
		commandInstance = exec.Command(Shell, ShellSubcommandFlag,
			fmt.Sprintf("set -e; curl -LsSf https://astral.sh/uv/%s/install.sh | UV_INSTALL_DIR=\"%s\" NO_MODIFY_PATH=1 sh", Version, dest))
	}

	commandInstance.Stdout = os.Stdout
	commandInstance.Stderr = os.Stderr
	if err := commandInstance.Run(); err != nil {
		return fmt.Errorf("failed to install uv: %w", err)
	}

	config.Debug("[UV] Installed uv v%s", Version)
	return nil
}

// getGongHomeDir returns the path to ~/.gong, creating it if it doesn't exist.
func getGongHomeDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	gongDir := filepath.Join(homeDir, ".gong")
	if err := os.MkdirAll(gongDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create gong directory: %w", err)
	}

	return gongDir, nil
}
