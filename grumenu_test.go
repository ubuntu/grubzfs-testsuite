package main_test

import (
	"bufio"
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var dangerous = flag.Bool("dangerous", false, "execute dangerous tests which may alter the system state")
var update = flag.Bool("update", false, "update golden files")

func TestFromZFStoBootlist(t *testing.T) {
	t.Parallel()

	ensureBinaryMocks(t)

	type TestCase struct {
		diskStruct      string
		secureBootState string
	}
	testCases := make(map[string]TestCase)

	bootListsDir := "testdata/bootlists"
	dirs, err := ioutil.ReadDir(bootListsDir)
	if err != nil {
		t.Fatal("couldn't read bootlists modes", err)
	}
	for _, d := range dirs {
		tcDirs, err := ioutil.ReadDir(filepath.Join(bootListsDir, d.Name()))
		if err != nil {
			t.Fatal("couldn't read bootlists test cases", err)
		}

		for _, tcd := range tcDirs {
			tcName := filepath.Join(d.Name(), tcd.Name())
			tcPath := filepath.Join(bootListsDir, tcName)
			if err != nil {
				t.Fatal("couldn't read test case", err)
			}

			testCases[tcName] = TestCase{
				diskStruct:      tcPath,
				secureBootState: d.Name(),
			}
		}
	}

	for name, tc := range testCases {
		tc := tc
		name := name
		t.Run(name, func(t *testing.T) {
			if tc.secureBootState == "no-mokutil" {
				if !*dangerous {
					t.Skipf("don't run %q: dangerous is not set", name)
				}

				// remove mokutil from PATH
				if _, err := os.Stat("/usr/bin/mokutil"); os.IsExist(err) {
					if err := os.Rename("/usr/bin/mokutil", "/usr/bin/mokutil.bak"); err != nil {
						t.Fatal("couldn't rename mokutil to its backup", err)
					}
					defer os.Rename("/usr/bin/mokutil.bak", "/usr/bin/mokutil")
				}
			}

			testDir, cleanUp := tempDir(t)
			defer cleanUp()

			devices := newFakeDevices(t, filepath.Join(tc.diskStruct, "definition.yaml"))
			devices.create(testDir, strings.ReplaceAll(strings.Replace(tc.diskStruct, bootListsDir+"/", "", 1), "/", "_"))

			out := filepath.Join(testDir, "bootlist")
			path := "PATH=mocks/zpool:mocks/zfs:" + os.Getenv("PATH")
			securebootEnv := ""
			if tc.secureBootState != "no-mokutil" {
				path = "PATH=mocks/mokutil:mocks/zpool:mocks/zfs:" + os.Getenv("PATH")
				securebootEnv = "TEST_MOKUTIL_SECUREBOOT=" + tc.secureBootState
			}
			env := append(os.Environ(),
				path,
				"TEST_POOL_DIR="+testDir,
				"GRUB_LINUX_ZFS_TEST=bootlist",
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out,
				securebootEnv)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			reference := filepath.Join(tc.diskStruct, "bootlist")
			if *update {
				if err := ioutil.WriteFile(reference, []byte(anonymizeTempDirNames(t, out)), 0644); err != nil {
					t.Fatal("couldn't update reference file", err)
				}
			}

			assertFileContentAlmostEquals(t, out, reference, "generated and reference files are different.")
		})
	}
}

func TestMenuMetaData(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		bootlist string
	}{
		"one zsys": {"bootlists/efi-nosb/onezsys"},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testDir, cleanUp := tempDir(t)
			defer cleanUp()

			basePath := filepath.Join("testdata", tc.bootlist)
			out := getTempOrReferenceFile(t, *update,
				filepath.Join(testDir, "menumeta"),
				filepath.Join(basePath, "menumeta"))
			env := append(os.Environ(),
				"GRUB_LINUX_ZFS_TEST=metamenu",
				"GRUB_LINUX_ZFS_TEST_INPUT="+filepath.Join(basePath, "bootlist"),
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			assertFileContentAlmostEquals(t, out, filepath.Join(basePath, "menumeta"), "generated and reference files are different.")
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

var compileMocksOnce sync.Once

// ensureBinayMocks creates our mocks, ensuring we compile them when running go test
func ensureBinaryMocks(t *testing.T) {
	t.Helper()

	compileMocksOnce.Do(func() {
		for _, mock := range []string{"mokutil", "zfs", "zpool"} {
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
					"\nexport GRUB_LINUX_ZFS_TEST GRUB_LINUX_ZFS_TEST_INPUT GRUB_LINUX_ZFS_TEST_OUTPUT TEST_POOL_DIR TEST_MOKUTIL_SECUREBOOT")+"\n")...)
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
