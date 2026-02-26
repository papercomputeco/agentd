package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

// MaterializePackages resolves bare Nix package attribute names to
// /nix/store paths by building each package via nix build and computing
// its transitive closure. All packages must build successfully or an
// error is returned.
//
// Package names are resolved against the system's nixpkgs flake
// (e.g. "ripgrep" becomes "nixpkgs#ripgrep").
func MaterializePackages(ctx context.Context, packages []string) ([]string, error) {
	if len(packages) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool)

	for _, pkg := range packages {
		ref := "nixpkgs#" + pkg

		log.Printf("sandbox: materializing package %s", ref)

		outPaths, err := nixBuild(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("building package %q: %w", pkg, err)
		}

		// For each output path, compute the full closure.
		for _, outPath := range outPaths {
			closure, err := nixStoreQueryRequisites(ctx, outPath)
			if err != nil {
				return nil, fmt.Errorf("computing closure for %s (package %q): %w", outPath, pkg, err)
			}
			for _, p := range closure {
				seen[p] = true
			}
		}
	}

	return sortedKeys(seen), nil
}

// MergePaths merges two lists of /nix/store paths, deduplicates, and
// returns the result sorted.
func MergePaths(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	for _, p := range base {
		seen[p] = true
	}
	for _, p := range extra {
		seen[p] = true
	}
	return sortedKeys(seen)
}

// nixBuild runs "nix build --no-link --print-out-paths <ref>" and returns
// the output store paths (one per line).
func nixBuild(ctx context.Context, ref string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "nix", "build", "--no-link", "--print-out-paths", ref)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("nix build %s: %w\nstderr: %s", ref, err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("nix build %s: %w", ref, err)
	}

	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && strings.HasPrefix(line, "/nix/store/") {
			paths = append(paths, line)
		}
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("nix build %s produced no output paths", ref)
	}

	return paths, nil
}

// sortedKeys returns the keys of a map as a sorted slice.
func sortedKeys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
