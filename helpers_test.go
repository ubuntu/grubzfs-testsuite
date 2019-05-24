package main_test

import (
	"bufio"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

var compileMocksOnce sync.Once

// ensureBinayMocks creates our mocks, ensuring we compile them when running go test
func ensureBinaryMocks(t *testing.T) {
	t.Helper()

	compileMocksOnce.Do(func() {
		for _, mock := range []string{"mokutil", "zfs", "zpool", "date", "grub-probe"} {
			if _, err := os.Stat(filepath.Join("mocks", mock)); os.IsExist(err) {
				continue
			}
			cmd := exec.Command("go", "build", "-o", filepath.Join("mocks", mock, mock), "github.com/ubuntu/grubmenugen-zfs-tests/cmd/"+mock)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("couldn't compile mock %q binaries: %v", mock, err)
			}
		}
	})
}

// anonymizeTempDirNames ununiquifies the name of the temporary directory, or
// loop devices so that we can compare the content generated with update
// to the content generated during the test.
func anonymizeTempDirNames(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal("couldn't open file to anonymize", err)
	}
	defer f.Close()

	filere := regexp.MustCompile("/tmp/grubtests-[[:alnum:]]+/")
	devloopre := regexp.MustCompile("/dev/loop[[:digit:]]+")
	s := bufio.NewScanner(f)
	var out string
	for s.Scan() {
		out = out +
			devloopre.ReplaceAllString(
				filere.ReplaceAllString(s.Text(), ""),
				"/dev/loop00") + "\n"
	}
	if err := s.Err(); err != nil {
		t.Fatalf("can't anynomize file %q: %v", path, err)
	}

	return out
}

// assertFileContentAlmostEquals between generated and expected file path.
// It strips temporary directory with special name.
func assertFileContentAlmostEquals(t *testing.T, generatedF, expectedF, msg string) {
	t.Helper()

	expected, err := ioutil.ReadFile(expectedF)
	if err != nil {
		t.Fatal("couldn't open reference file", err)
	}
	assert.Equal(t, string(expected), anonymizeTempDirNames(t, generatedF), "generated and reference files are different.")
}

// getTempOrReferenceFile returns the tempFile path.
// If update flag is set, the referenceFile path is returned.
func getTempOrReferenceFile(t *testing.T, update bool, tempFile, referenceFile string) string {
	t.Helper()

	if update {
		t.Log("update reference file")
		return referenceFile
	}
	return tempFile
}

// copyFile copy source file src to destination file dst.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	b, err := ioutil.ReadFile(src)
	if err != nil {
		t.Fatalf("can't read source file %q: %v", src, err)
	}

	fInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("can't stat %q: %v", src, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatalf("can't create destination directory for: %q: %v", dst, err)
	}

	if err = ioutil.WriteFile(dst, b, fInfo.Mode()); err != nil {
		t.Fatalf("can't read destination file %q: %v", dst, err)
	}
}

// tempDir creates a temporary directory and return a teardown function
// to clean it up.
func tempDir(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "grubtests-")
	if err != nil {
		t.Fatal("can't create temporary directory", err)
	}

	return dir, func() {
		if err = os.RemoveAll(dir); err != nil {
			t.Error("can't clean temporary directory", err)
		}
	}
}
