package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// Standard directories to create in the rootfs.
var rootfsDirs = []string{
	"bin",
	"dev",
	"etc",
	"home/agent",
	"proc",
	"sys",
	"tmp",
	"var",
	"run",
	"nix/store",
	"usr/bin",
}

const etcPasswd = `root:x:0:0:root:/root:/bin/bash
agent:x:1000:1000:AI Agent:/home/agent:/bin/bash
`

const etcGroup = `root:x:0:
agent:x:1000:
`

// PrepareRootfs creates a minimal rootfs directory structure at the given
// path. It creates standard directories, writes minimal /etc files, and
// symlinks binaries from /nix/store paths into /bin.
func PrepareRootfs(rootfsDir string, storePaths []string) error {
	// Create the directory skeleton.
	for _, dir := range rootfsDirs {
		if err := os.MkdirAll(filepath.Join(rootfsDir, dir), 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Write minimal /etc files.
	etcFiles := map[string]string{
		"etc/passwd":      etcPasswd,
		"etc/group":       etcGroup,
		"etc/hostname":    "sandbox\n",
		"etc/hosts":       "127.0.0.1 localhost\n::1 localhost\n",
		"etc/resolv.conf": "nameserver 8.8.8.8\nnameserver 8.8.4.4\n",
	}

	for name, content := range etcFiles {
		path := filepath.Join(rootfsDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}

	// Symlink binaries from /nix/store paths into /rootfs/bin/.
	binDir := filepath.Join(rootfsDir, "bin")
	if err := symlinkBinaries(binDir, storePaths); err != nil {
		return fmt.Errorf("symlinking binaries: %w", err)
	}

	return nil
}

// symlinkBinaries scans /nix/store paths for bin/ directories and creates
// symlinks in the rootfs /bin for each executable found.
func symlinkBinaries(binDir string, storePaths []string) error {
	bashLinked := false

	for _, storePath := range storePaths {
		nixBinDir := filepath.Join(storePath, "bin")
		entries, err := os.ReadDir(nixBinDir)
		if err != nil {
			// Not every store path has a bin/ directory.
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			srcPath := filepath.Join(nixBinDir, entry.Name())

			// Check if the file is executable.
			info, err := os.Stat(srcPath)
			if err != nil {
				continue
			}
			if info.Mode()&0111 == 0 {
				continue
			}

			linkPath := filepath.Join(binDir, entry.Name())

			// Don't overwrite existing symlinks (first one wins).
			if _, err := os.Lstat(linkPath); err == nil {
				continue
			}

			if err := os.Symlink(srcPath, linkPath); err != nil {
				// Non-fatal: log and continue.
				continue
			}

			// Track if we've linked bash for the /bin/sh symlink.
			if entry.Name() == "bash" {
				bashLinked = true
			}
		}
	}

	// Ensure /bin/sh exists as a symlink to bash.
	if bashLinked {
		shPath := filepath.Join(binDir, "sh")
		if _, err := os.Lstat(shPath); os.IsNotExist(err) {
			bashPath := filepath.Join(binDir, "bash")
			// Symlink sh -> bash (relative within /bin).
			_ = os.Symlink(bashPath, shPath)
		}
	}

	return nil
}
