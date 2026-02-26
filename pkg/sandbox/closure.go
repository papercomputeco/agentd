package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	// DefaultClosureManifest is the well-known path where stereOS writes
	// the pre-computed sandbox closure at build time.
	DefaultClosureManifest = "/etc/stereos/sandbox-closure.txt"
)

// ComputeClosureFromManifest reads a pre-computed closure manifest file.
// Each line is expected to be a /nix/store path. Empty lines and lines
// starting with # are skipped.
func ComputeClosureFromManifest(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening manifest %s: %w", path, err)
	}
	defer f.Close()

	var paths []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "/nix/store/") {
			continue
		}
		paths = append(paths, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}

	return paths, nil
}

// nixStoreQueryRequisites runs nix-store -qR on a path and returns the
// list of store paths in the closure.
func nixStoreQueryRequisites(ctx context.Context, path string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "nix-store", "-qR", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nix-store -qR %s: %w", path, err)
	}

	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && strings.HasPrefix(line, "/nix/store/") {
			paths = append(paths, line)
		}
	}

	return paths, nil
}
