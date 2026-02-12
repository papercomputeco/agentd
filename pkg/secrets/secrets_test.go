package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/papercomputeco/agentd/pkg/secrets"
)

func TestSecrets(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Suite")
}

var _ = Describe("SecretReader", func() {
	var (
		dir    string
		reader *secrets.Reader
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		reader = secrets.NewReader(dir)
	})

	Describe("Dir", func() {
		It("should return the secret directory", func() {
			Expect(reader.Dir()).To(Equal(dir))
		})
	})

	Describe("ReadAll", func() {
		It("should return empty map when directory is empty", func() {
			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeEmpty())
		})

		It("should read all secret files", func() {
			Expect(os.WriteFile(filepath.Join(dir, "API_KEY"), []byte("sk-123"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "TOKEN"), []byte("tok-abc"), 0600)).To(Succeed())

			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result).To(HaveKeyWithValue("API_KEY", "sk-123"))
			Expect(result).To(HaveKeyWithValue("TOKEN", "tok-abc"))
		})

		It("should skip hidden files", func() {
			Expect(os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "VISIBLE"), []byte("value"), 0600)).To(Succeed())

			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result).To(HaveKeyWithValue("VISIBLE", "value"))
		})

		It("should skip directories", func() {
			Expect(os.Mkdir(filepath.Join(dir, "subdir"), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "KEY"), []byte("val"), 0600)).To(Succeed())

			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result).To(HaveKeyWithValue("KEY", "val"))
		})

		It("should trim trailing newlines from values", func() {
			Expect(os.WriteFile(filepath.Join(dir, "KEY"), []byte("value\n"), 0600)).To(Succeed())

			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result["KEY"]).To(Equal("value"))
		})

		It("should return empty map when directory does not exist", func() {
			reader := secrets.NewReader("/nonexistent/path")
			result, err := reader.ReadAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeEmpty())
		})
	})

	Describe("Read", func() {
		It("should read a single secret", func() {
			Expect(os.WriteFile(filepath.Join(dir, "MY_SECRET"), []byte("s3cret"), 0600)).To(Succeed())

			value, err := reader.Read("MY_SECRET")
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(Equal("s3cret"))
		})

		It("should return error for non-existent secret", func() {
			_, err := reader.Read("MISSING")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("List", func() {
		It("should list all secret names", func() {
			Expect(os.WriteFile(filepath.Join(dir, "A"), []byte("1"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "B"), []byte("2"), 0600)).To(Succeed())

			names, err := reader.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(ConsistOf("A", "B"))
		})

		It("should return nil when directory is empty", func() {
			names, err := reader.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(BeNil())
		})

		It("should skip hidden files", func() {
			Expect(os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "VISIBLE"), []byte("y"), 0600)).To(Succeed())

			names, err := reader.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(ConsistOf("VISIBLE"))
		})

		It("should return nil for non-existent directory", func() {
			reader := secrets.NewReader("/nonexistent/path")
			names, err := reader.List()
			Expect(err).NotTo(HaveOccurred())
			Expect(names).To(BeNil())
		})
	})
})
