package sandbox_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/sandbox"
)

var _ = Describe("Packages", func() {
	Describe("MergePaths", func() {
		It("should merge two disjoint lists", func() {
			base := []string{"/nix/store/aaa-bash-5.2", "/nix/store/bbb-glibc-2.39"}
			extra := []string{"/nix/store/ccc-ripgrep-14", "/nix/store/ddd-fd-9"}

			result := sandbox.MergePaths(base, extra)
			Expect(result).To(HaveLen(4))
			Expect(result).To(Equal([]string{
				"/nix/store/aaa-bash-5.2",
				"/nix/store/bbb-glibc-2.39",
				"/nix/store/ccc-ripgrep-14",
				"/nix/store/ddd-fd-9",
			}))
		})

		It("should deduplicate overlapping paths", func() {
			base := []string{"/nix/store/aaa-bash-5.2", "/nix/store/bbb-glibc-2.39"}
			extra := []string{"/nix/store/bbb-glibc-2.39", "/nix/store/ccc-ripgrep-14"}

			result := sandbox.MergePaths(base, extra)
			Expect(result).To(HaveLen(3))
			Expect(result).To(Equal([]string{
				"/nix/store/aaa-bash-5.2",
				"/nix/store/bbb-glibc-2.39",
				"/nix/store/ccc-ripgrep-14",
			}))
		})

		It("should return sorted results", func() {
			base := []string{"/nix/store/zzz-vim-9", "/nix/store/aaa-bash-5.2"}
			extra := []string{"/nix/store/mmm-git-2.43"}

			result := sandbox.MergePaths(base, extra)
			Expect(result).To(Equal([]string{
				"/nix/store/aaa-bash-5.2",
				"/nix/store/mmm-git-2.43",
				"/nix/store/zzz-vim-9",
			}))
		})

		It("should handle empty base", func() {
			extra := []string{"/nix/store/ccc-ripgrep-14"}
			result := sandbox.MergePaths(nil, extra)
			Expect(result).To(Equal([]string{"/nix/store/ccc-ripgrep-14"}))
		})

		It("should handle empty extra", func() {
			base := []string{"/nix/store/aaa-bash-5.2"}
			result := sandbox.MergePaths(base, nil)
			Expect(result).To(Equal([]string{"/nix/store/aaa-bash-5.2"}))
		})

		It("should handle both empty", func() {
			result := sandbox.MergePaths(nil, nil)
			Expect(result).To(BeEmpty())
		})
	})

	Describe("MaterializePackages", func() {
		It("should return nil for empty input", func() {
			result, err := sandbox.MaterializePackages(nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return nil for zero-length input", func() {
			result, err := sandbox.MaterializePackages(nil, []string{})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeNil())
		})
	})
})
