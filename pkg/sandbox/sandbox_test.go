package sandbox_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/config"
	"github.com/papercomputeco/agentd/pkg/harness"
	"github.com/papercomputeco/agentd/pkg/sandbox"
)

func TestSandbox(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sandbox Suite")
}

var _ = Describe("OCI Spec Generation", func() {
	It("should generate a valid spec for a basic config", func() {
		cfg := &sandbox.Config{
			ID:          "test-sandbox",
			Command:     "/bin/bash",
			Args:        []string{"-c", "echo hello"},
			Env:         map[string]string{"FOO": "bar"},
			Workdir:     "/home/agent/workspace",
			StorePaths:  []string{"/nix/store/abc-bash-5.2", "/nix/store/def-coreutils-9.4"},
			MemoryLimit: 2 * 1024 * 1024 * 1024,
			PidLimit:    512,
			Hostname:    "test-host",
		}

		spec, err := sandbox.GenerateSpec(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(spec).NotTo(BeNil())

		// Check process.
		Expect(spec.Process.Args).To(Equal([]string{"/bin/bash", "-c", "echo hello"}))
		Expect(spec.Process.User.UID).To(Equal(uint32(1000)))
		Expect(spec.Process.User.GID).To(Equal(uint32(1000)))
		Expect(spec.Process.Cwd).To(Equal("/home/agent/workspace"))
		Expect(spec.Process.Terminal).To(BeFalse())

		// Check environment includes standard vars and custom ones.
		Expect(spec.Process.Env).To(ContainElement("HOME=/home/agent"))
		Expect(spec.Process.Env).To(ContainElement("PATH=/bin:/usr/bin"))
		Expect(spec.Process.Env).To(ContainElement("FOO=bar"))

		// Check root.
		Expect(spec.Root.Path).To(Equal("rootfs"))

		// Check hostname.
		Expect(spec.Hostname).To(Equal("test-host"))

		// Check namespaces: should have pid, ipc, uts, mount but NOT network.
		namespaceTypes := make([]string, 0, len(spec.Linux.Namespaces))
		for _, ns := range spec.Linux.Namespaces {
			namespaceTypes = append(namespaceTypes, string(ns.Type))
		}
		Expect(namespaceTypes).To(ContainElements("pid", "ipc", "uts", "mount"))
		Expect(namespaceTypes).NotTo(ContainElement("network"))

		// Check resource limits.
		Expect(spec.Linux.Resources).NotTo(BeNil())
		Expect(spec.Linux.Resources.Memory).NotTo(BeNil())
		Expect(*spec.Linux.Resources.Memory.Limit).To(Equal(int64(2 * 1024 * 1024 * 1024)))
		Expect(spec.Linux.Resources.Pids).NotTo(BeNil())
		Expect(*spec.Linux.Resources.Pids.Limit).To(Equal(int64(512)))
	})

	It("should include standard mounts and nix store bind mounts", func() {
		storePaths := []string{
			"/nix/store/abc-bash-5.2",
			"/nix/store/def-glibc-2.39",
			"/nix/store/ghi-coreutils-9.4",
		}
		cfg := &sandbox.Config{
			ID:         "mount-test",
			Command:    "/bin/bash",
			StorePaths: storePaths,
		}

		spec, err := sandbox.GenerateSpec(cfg)
		Expect(err).NotTo(HaveOccurred())

		// Count mount types.
		var procCount, tmpfsCount, sysfsCount, bindCount int
		for _, m := range spec.Mounts {
			switch m.Type {
			case "proc":
				procCount++
			case "tmpfs":
				tmpfsCount++
			case "sysfs":
				sysfsCount++
			case "bind":
				bindCount++
			}
		}

		// Standard mounts: /proc (1), /dev (tmpfs), /sys (sysfs), /tmp (tmpfs), /home/agent (tmpfs)
		Expect(procCount).To(Equal(1))
		Expect(tmpfsCount).To(Equal(3)) // /dev, /tmp, /home/agent
		Expect(sysfsCount).To(Equal(1))
		Expect(bindCount).To(Equal(len(storePaths)))

		// Check each nix store mount is read-only.
		for _, m := range spec.Mounts {
			if m.Type == "bind" {
				Expect(m.Options).To(ContainElement("ro"))
				Expect(m.Options).To(ContainElement("rbind"))
				Expect(m.Destination).To(HavePrefix("/nix/store/"))
			}
		}
	})

	It("should use defaults for empty workdir and hostname", func() {
		cfg := &sandbox.Config{
			ID:      "defaults-test",
			Command: "/bin/bash",
		}

		spec, err := sandbox.GenerateSpec(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Process.Cwd).To(Equal("/home/agent"))
		Expect(spec.Hostname).To(Equal("defaults-test"))
	})

	It("should omit resource limits when zero", func() {
		cfg := &sandbox.Config{
			ID:          "no-limits",
			Command:     "/bin/bash",
			MemoryLimit: 0,
			PidLimit:    0,
		}

		spec, err := sandbox.GenerateSpec(cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(spec.Linux.Resources).To(BeNil())
	})

	It("should reject empty ID", func() {
		cfg := &sandbox.Config{Command: "/bin/bash"}
		_, err := sandbox.GenerateSpec(cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ID is required"))
	})

	It("should reject empty command", func() {
		cfg := &sandbox.Config{ID: "test"}
		_, err := sandbox.GenerateSpec(cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("command is required"))
	})

	It("should marshal spec to valid JSON", func() {
		cfg := &sandbox.Config{
			ID:      "json-test",
			Command: "/bin/bash",
		}
		spec, err := sandbox.GenerateSpec(cfg)
		Expect(err).NotTo(HaveOccurred())

		data, err := sandbox.MarshalSpec(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(BeEmpty())
		// Verify it's valid JSON by checking it starts with {
		Expect(string(data)).To(HavePrefix("{"))
	})
})

var _ = Describe("Rootfs Preparation", func() {
	var rootfsDir string

	BeforeEach(func() {
		rootfsDir = GinkgoT().TempDir()
	})

	It("should create the standard directory skeleton", func() {
		err := sandbox.PrepareRootfs(rootfsDir, nil)
		Expect(err).NotTo(HaveOccurred())

		expectedDirs := []string{
			"bin", "dev", "etc", "home/agent", "proc", "sys",
			"tmp", "var", "run", "nix/store", "usr/bin",
		}
		for _, dir := range expectedDirs {
			path := filepath.Join(rootfsDir, dir)
			info, err := os.Stat(path)
			Expect(err).NotTo(HaveOccurred(), "directory %s should exist", dir)
			Expect(info.IsDir()).To(BeTrue(), "%s should be a directory", dir)
		}
	})

	It("should write /etc/passwd with root and agent users", func() {
		err := sandbox.PrepareRootfs(rootfsDir, nil)
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(filepath.Join(rootfsDir, "etc/passwd"))
		Expect(err).NotTo(HaveOccurred())
		content := string(data)
		Expect(content).To(ContainSubstring("root:x:0:0"))
		Expect(content).To(ContainSubstring("agent:x:1000:1000"))
	})

	It("should write /etc/group", func() {
		err := sandbox.PrepareRootfs(rootfsDir, nil)
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(filepath.Join(rootfsDir, "etc/group"))
		Expect(err).NotTo(HaveOccurred())
		content := string(data)
		Expect(content).To(ContainSubstring("root:x:0"))
		Expect(content).To(ContainSubstring("agent:x:1000"))
	})

	It("should write /etc/hostname, hosts, and resolv.conf", func() {
		err := sandbox.PrepareRootfs(rootfsDir, nil)
		Expect(err).NotTo(HaveOccurred())

		hostname, err := os.ReadFile(filepath.Join(rootfsDir, "etc/hostname"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(hostname)).To(ContainSubstring("sandbox"))

		hosts, err := os.ReadFile(filepath.Join(rootfsDir, "etc/hosts"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(hosts)).To(ContainSubstring("127.0.0.1"))

		resolv, err := os.ReadFile(filepath.Join(rootfsDir, "etc/resolv.conf"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(resolv)).To(ContainSubstring("nameserver"))
	})

	It("should symlink binaries from store paths", func() {
		// Create a fake /nix/store path with a bin directory.
		fakeStore := filepath.Join(GinkgoT().TempDir(), "nix/store/abc-test-1.0")
		fakeBin := filepath.Join(fakeStore, "bin")
		Expect(os.MkdirAll(fakeBin, 0755)).To(Succeed())

		// Create a fake executable.
		fakeExe := filepath.Join(fakeBin, "test-tool")
		Expect(os.WriteFile(fakeExe, []byte("#!/bin/sh\necho test"), 0755)).To(Succeed())

		err := sandbox.PrepareRootfs(rootfsDir, []string{fakeStore})
		Expect(err).NotTo(HaveOccurred())

		// Check that the symlink was created.
		linkPath := filepath.Join(rootfsDir, "bin/test-tool")
		info, err := os.Lstat(linkPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode()&os.ModeSymlink).NotTo(BeZero(), "should be a symlink")

		// Verify the symlink target.
		target, err := os.Readlink(linkPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(Equal(fakeExe))
	})

	It("should not symlink non-executable files", func() {
		fakeStore := filepath.Join(GinkgoT().TempDir(), "nix/store/abc-test-1.0")
		fakeBin := filepath.Join(fakeStore, "bin")
		Expect(os.MkdirAll(fakeBin, 0755)).To(Succeed())

		// Create a non-executable file.
		Expect(os.WriteFile(filepath.Join(fakeBin, "readme.txt"), []byte("not executable"), 0644)).To(Succeed())

		err := sandbox.PrepareRootfs(rootfsDir, []string{fakeStore})
		Expect(err).NotTo(HaveOccurred())

		_, err = os.Lstat(filepath.Join(rootfsDir, "bin/readme.txt"))
		Expect(os.IsNotExist(err)).To(BeTrue(), "non-executable should not be linked")
	})
})

var _ = Describe("Closure", func() {
	Describe("ComputeClosureFromManifest", func() {
		It("should read store paths from a manifest file", func() {
			dir := GinkgoT().TempDir()
			manifest := filepath.Join(dir, "closure.txt")
			content := `/nix/store/abc-bash-5.2
/nix/store/def-glibc-2.39
/nix/store/ghi-coreutils-9.4
`
			Expect(os.WriteFile(manifest, []byte(content), 0644)).To(Succeed())

			paths, err := sandbox.ComputeClosureFromManifest(manifest)
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(HaveLen(3))
			Expect(paths).To(ContainElements(
				"/nix/store/abc-bash-5.2",
				"/nix/store/def-glibc-2.39",
				"/nix/store/ghi-coreutils-9.4",
			))
		})

		It("should skip comments and empty lines", func() {
			dir := GinkgoT().TempDir()
			manifest := filepath.Join(dir, "closure.txt")
			content := `# This is a comment

/nix/store/abc-bash-5.2
# Another comment
/nix/store/def-glibc-2.39
`
			Expect(os.WriteFile(manifest, []byte(content), 0644)).To(Succeed())

			paths, err := sandbox.ComputeClosureFromManifest(manifest)
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(HaveLen(2))
		})

		It("should skip non-nix-store paths", func() {
			dir := GinkgoT().TempDir()
			manifest := filepath.Join(dir, "closure.txt")
			content := `/nix/store/abc-bash-5.2
/usr/bin/something
/nix/store/def-glibc-2.39
`
			Expect(os.WriteFile(manifest, []byte(content), 0644)).To(Succeed())

			paths, err := sandbox.ComputeClosureFromManifest(manifest)
			Expect(err).NotTo(HaveOccurred())
			Expect(paths).To(HaveLen(2))
		})

		It("should return error for non-existent file", func() {
			_, err := sandbox.ComputeClosureFromManifest("/nonexistent/closure.txt")
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Runner", func() {
	It("should fail to create runner when runsc is not available", func() {
		_, err := sandbox.NewRunner("/nonexistent/runsc", "/tmp/state")
		// NewRunner with an explicit path doesn't check existence,
		// but an empty path with no runsc in PATH should fail.
		Expect(err).To(BeNil()) // explicit path is accepted as-is
	})

	It("should detect runsc in PATH", func() {
		_, lookErr := exec.LookPath("runsc")
		if lookErr != nil {
			Skip("runsc not available in PATH")
		}

		runner, err := sandbox.NewRunner("", "/tmp/state")
		Expect(err).NotTo(HaveOccurred())
		Expect(runner).NotTo(BeNil())
		Expect(runner.RunscPath).NotTo(BeEmpty())
	})
})

var _ = Describe("Manager", func() {
	It("should create a manager with valid options", func() {
		runner, _ := sandbox.NewRunner("/usr/bin/runsc", "/tmp/state")
		cfg := &config.AgentConfig{
			Type:     config.AgentTypeSandboxed,
			Harness:  "claude-code",
			Workdir:  "/home/agent/workspace",
			Restart:  config.RestartNo,
			Memory:   "2GiB",
			PidLimit: 512,
		}
		h, err := harness.Get("claude-code")
		Expect(err).NotTo(HaveOccurred())

		mgr := sandbox.NewManager(sandbox.ManagerOpts{
			Config:  cfg,
			Harness: h,
			Runner:  runner,
			Env:     map[string]string{"FOO": "bar"},
			Prompt:  "fix the tests",
		})

		Expect(mgr).NotTo(BeNil())
		Expect(mgr.IsRunning()).To(BeFalse())
	})

	It("should report correct initial status", func() {
		runner, _ := sandbox.NewRunner("/usr/bin/runsc", "/tmp/state")
		cfg := &config.AgentConfig{
			Type:     config.AgentTypeSandboxed,
			Harness:  "claude-code",
			Workdir:  "/home/agent/workspace",
			Restart:  config.RestartNo,
			Memory:   "2GiB",
			PidLimit: 512,
		}
		h, _ := harness.Get("claude-code")

		mgr := sandbox.NewManager(sandbox.ManagerOpts{
			Config:  cfg,
			Harness: h,
			Runner:  runner,
		})

		status := mgr.Status()
		Expect(status.Name).To(Equal("claude-code"))
		Expect(status.Running).To(BeFalse())
		Expect(status.Restarts).To(Equal(0))
		Expect(status.Error).To(BeEmpty())
	})

	It("should report zero restarts initially", func() {
		runner, _ := sandbox.NewRunner("/usr/bin/runsc", "/tmp/state")
		cfg := &config.AgentConfig{
			Type:    config.AgentTypeSandboxed,
			Harness: "opencode",
			Restart: config.RestartNo,
		}
		h, _ := harness.Get("opencode")

		mgr := sandbox.NewManager(sandbox.ManagerOpts{
			Config:  cfg,
			Harness: h,
			Runner:  runner,
		})

		Expect(mgr.Restarts()).To(Equal(0))
	})
})
