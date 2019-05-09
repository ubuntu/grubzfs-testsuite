package main_test

import (
	"bufio"
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var update = flag.Bool("update", false, "update golden files")

func TestMenuMetaData(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		bootlist string
	}{
		{"simple", "testdata/metamenu/onezsys"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testDir, cleanUp := tempDir(t)
			defer cleanUp()

			out := getTempOrReferenceFile(t, *update,
				filepath.Join(testDir, "out.bootlist"),
				tc.bootlist+".golden")
			env := append(os.Environ(),
				"GRUB_LINUX_ZFS_TEST=metamenu",
				"GRUB_LINUX_ZFS_TEST_INPUT="+tc.bootlist,
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			assertFileContentAlmostEquals(t, out, tc.bootlist+".golden", "generated and reference files are different.")
		})
	}
}

// runGrubMkConfig setup and runs grubMkConfig.
func runGrubMkConfig(t *testing.T, env []string, testDir string) error {
	for _, path := range []string{
		"/etc/grub.d/15_linux_zfs", "/etc/grub.d/00_header", "/etc/default/grub", "/usr/sbin/grub-mkconfig"} {
		copyFile(t, path, filepath.Join(testDir, path))
	}
	grubMkConfig := filepath.Join(testDir, "/usr/sbin/grub-mkconfig")
	updateMkConfig(t, grubMkConfig, testDir)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "fakeroot", grubMkConfig, "-o", filepath.Join(testDir, "grub.cfg"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	return cmd.Run()
}

// anonymizeTempDirNames ununiquifies the name of the temporary directory, so
// we can compare the content generated with update to the content generated
// during the test.
func anonymizeTempDirNames(t *testing.T, path string) string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal("couldn't open file to anonymize", err)
	}
	defer f.Close()

	re := regexp.MustCompile("/tmp/grubtests-[[:alnum:]]+/")
	s := bufio.NewScanner(f)
	var out string
	for s.Scan() {
		out = out + re.ReplaceAllString(s.Text(), "") + "\n"
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

// updateMkConfig updates sysconfigdir and exports variables in grub-mkconfig so that we target a specific
// /etc directory for grub scripts.
func updateMkConfig(t *testing.T, path, tmpdir string) {
	t.Helper()

	src, err := os.OpenFile(path, os.O_RDWR, 0755)
	if err != nil {
		t.Fatalf("can't open %q: %v", src.Name(), err)
	}
	defer src.Close()

	s := bufio.NewScanner(src)
	var out []byte
	for s.Scan() {
		out = append(out, []byte(
			strings.ReplaceAll(s.Text(),
				`sysconfdir="/etc"`,
				`sysconfdir="`+tmpdir+`/etc"`+
					"\nexport GRUB_LINUX_ZFS_TEST GRUB_LINUX_ZFS_TEST_INPUT GRUB_LINUX_ZFS_TEST_OUTPUT")+"\n")...)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("can't replace sysconfigdir in %q: %v", path, err)
	}

	if err := src.Truncate(0); err != nil {
		t.Fatalf("can't truncate %q: %v", src.Name(), err)
	}
	if _, err := src.WriteAt(out, 0); err != nil {
		t.Fatalf("can't write to %q, %v", src.Name(), err)
	}
}
