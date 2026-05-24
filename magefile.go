//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// default target to run if you just type 'mage'
var Default = Build

// Build compiles the binary locally into the ./bin directory.
func Build() error {
	fmt.Println("🔨 Building usgs-m2m locally...")

	// gather metadata for linker flags
	commit := getGitCommit()
	version := getGitVersion()
	buildTime := time.Now().UTC().Format(time.RFC3339)

	ldFlags := fmt.Sprintf("-s -w -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.Version=%s -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.Commit=%s -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.BuildTime=%s", version, commit, buildTime)

	// compiles directly into your local repo workspace at ./bin/usgs-m2m
	cmd := exec.Command("go", "build", "-ldflags", ldFlags, "-o", "bin/usgs-m2m", "./cmd/usgs-m2m")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build binary: %w", err)
	}

	fmt.Println("✅ Compiled: ./bin/usgs-m2m")
	return nil
}

// Install compiles and installs the binary globally into your $GOPATH/bin.
func Install() error {
	fmt.Println("🚀 Installing usgs-m2m globally...")

	commit := getGitCommit()
	version := getGitVersion()
	buildTime := time.Now().UTC().Format(time.RFC3339)

	ldFlags := fmt.Sprintf("-s -w -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.Version=%s -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.Commit=%s -X github.com/sixy6e/usgsm2m/pkg/usgsm2m.BuildTime=%s", version, commit, buildTime)

	// 'go install' ignores the "-o" flag and automatically places the executable
	// directly into global $GOPATH/bin directory (e.g., $HOME/go/bin)
	cmd := exec.Command("go", "install", "-ldflags", ldFlags, "./cmd/usgs-m2m")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install binary: %w", err)
	}

	fmt.Println("✅ Global installation complete! Ensure $GOPATH/bin (usually ~/go/bin) is in your shell PATH.")
	return nil
}

// Clean wipes out the local build directory.
func Clean() error {
	fmt.Println("🧹 Cleaning local bin directory...")
	return os.RemoveAll("bin")
}

// getGitCommit is a helper utilities to query git status safely
func getGitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return string(out[:len(out)-1]) // strip trailing newline
}

// getGitVersion is a helper utilities to query git status safely
func getGitVersion() string {
	out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "dev"
	}
	return string(out[:len(out)-1])
}
