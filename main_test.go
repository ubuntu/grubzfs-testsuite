package main_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	dangerous = flag.Bool("dangerous", false, "execute dangerous tests which may alter the system state")
	update    = flag.Bool("update", false, "update golden files")
	slow      = flag.Bool("slow", false, "sleep between tests interacting with zfs kernel module to avoid spamming it")

	// Test data and mock dir are generally <current test dir>/{testdata;mocks}. However, when we ship a binary
	// test package, cwd can be != binary dir and the binary (contrary to `go test`) doesn't cd you into the current
	// test directory. We need to compute those path relative to binary dir for those cases if we don't find testdata/
	// and mocks/ subdirectory in current cwd.
	testDataDir = "testdata"
	mockDir     = "mocks"
)

func init() {
	binaryDir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatalf("couldn't get current program directory: %v", err)
	}

	var found bool
	for _, p := range []string{".", binaryDir} {
		if d, err := os.Stat(filepath.Join(p, testDataDir)); err == nil && d.IsDir() {
			testDataDir = filepath.Join(p, testDataDir)
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("couldn't find any valid testdata/ directory")
	}

	// Mocks are a little bit more complexe: we can have cmd/, which will compile to mocks/ or directly mocks/.
	// Prefers cmd/ first.
	found = false
	for _, p := range []string{".", binaryDir} {
		if d, err := os.Stat(filepath.Join(p, "cmd")); err == nil && d.IsDir() {
			mockDir = filepath.Join(p, "mocks")
			found = true
			break
		}
		if d, err := os.Stat(filepath.Join(p, "mocks")); err == nil && d.IsDir() {
			mockDir = filepath.Join(p, "mocks")
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("no mocks source and binary directories found (cmd/ or mocks/)")
	}
}

func TestBootlist(t *testing.T) {
	t.Parallel()
	defer registerTest(t)()
	skipOnZFSPermissionDenied(t)

	ensureBinaryMocks(t)

	testCases := newTestCases(t)
	for name, tc := range testCases {
		tc := tc
		name := name
		t.Run(name, func(t *testing.T) {
			secureBootState := filepath.Base(filepath.Dir(tc.path))
			if secureBootState == "no-mokutil" {
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

			devices := newFakeDevices(t, filepath.Join(tc.path, "testcase.yaml"))
			systemRootDataset := devices.create(testDir, tc.fullTestName)

			out := filepath.Join(testDir, "bootlist")
			path := fmt.Sprintf("PATH=%s/zpool:%s/zfs:%s/date:%s", mockDir, mockDir, mockDir, os.Getenv("PATH"))
			var securebootEnv string
			if secureBootState != "no-mokutil" {
				path = fmt.Sprintf("PATH=%s/mokutil:%s/zpool:%s/zfs:%s/date:%s", mockDir, mockDir, mockDir, mockDir, os.Getenv("PATH"))
				securebootEnv = "TEST_MOKUTIL_SECUREBOOT=" + secureBootState
			}

			var mockZFSDatasetEnv string
			if systemRootDataset != "" {
				mockZFSDatasetEnv = "TEST_MOCKZFS_CURRENT_ROOT_DATASET=" + systemRootDataset
			}

			env := append(os.Environ(),
				path,
				"LC_ALL=C",
				"TEST_POOL_DIR="+testDir,
				"GRUB_LINUX_ZFS_TEST=bootlist",
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out,
				securebootEnv,
				mockZFSDatasetEnv)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			reference := filepath.Join(tc.path, "bootlist")
			if *update {
				if err := ioutil.WriteFile(reference, []byte(anonymizeTempDirNames(t, out)), 0644); err != nil {
					t.Fatal("couldn't update reference file", err)
				}
			}

			assertFileContentAlmostEquals(t, out, reference, "generated and reference files are different.")
			devices.assertExistingPoolsAndCleanup(tc.fullTestName)

			if *slow {
				time.Sleep(time.Second)
			}
		})
	}
}

func TestMetaMenu(t *testing.T) {
	t.Parallel()
	defer registerTest(t)()
	waitForTest(t, "TestBootlist")

	testCases := newTestCases(t)
	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testDir, cleanUp := tempDir(t)
			defer cleanUp()

			out := getTempOrReferenceFile(t, *update,
				filepath.Join(testDir, "metamenu"),
				filepath.Join(tc.path, "metamenu"))
			env := append(os.Environ(),
				"LC_ALL=C",
				"TZ=Europe/Paris", // we want to ensure user's timezone is taken into account
				"GRUB_LINUX_ZFS_TEST=metamenu",
				"GRUB_LINUX_ZFS_TEST_INPUT="+filepath.Join(tc.path, "bootlist"),
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			assertFileContentAlmostEquals(t, out, filepath.Join(tc.path, "metamenu"), "generated and reference files are different.")
		})
	}
}

func TestGrubMenu(t *testing.T) {
	t.Parallel()
	defer registerTest(t)()
	waitForTest(t, "TestMetaMenu")

	ensureBinaryMocks(t)

	testCases := newTestCases(t)
	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testDir, cleanUp := tempDir(t)
			defer cleanUp()

			out := getTempOrReferenceFile(t, *update,
				filepath.Join(testDir, "grubmenu"),
				filepath.Join(tc.path, "grubmenu"))
			path := fmt.Sprintf("PATH=%s/grub-probe:%s", mockDir, os.Getenv("PATH"))
			grubProbeDir, err := filepath.Abs(filepath.Join(mockDir, "grub-probe"))
			if err != nil {
				t.Fatal("couldn't get absolute path for mock directory", err)
			}
			env := append(os.Environ(),
				path,
				"grub_probe="+grubProbeDir,
				"LC_ALL=C",
				"GRUB_LINUX_ZFS_TEST=grubmenu",
				"GRUB_LINUX_ZFS_TEST_INPUT="+filepath.Join(tc.path, "metamenu"),
				"GRUB_LINUX_ZFS_TEST_OUTPUT="+out)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			assertFileContentAlmostEquals(t, out, filepath.Join(tc.path, "grubmenu"), "generated and reference files are different.")
		})
	}
}

// TestGrubMkConfig Runs all the stages of the menu generation
func TestGrubMkConfig(t *testing.T) {
	t.Parallel()
	defer registerTest(t)()
	skipOnZFSPermissionDenied(t)
	waitForTest(t, "TestGrubMenu")

	ensureBinaryMocks(t)

	testCases := newTestCases(t)
	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			secureBootState := filepath.Base(filepath.Dir(tc.path))
			if secureBootState == "no-mokutil" {
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

			devices := newFakeDevices(t, filepath.Join(tc.path, "testcase.yaml"))
			systemRootDataset := devices.create(testDir, tc.fullTestName)

			path := fmt.Sprintf("PATH=%s/zpool:%s/zfs:%s/date:%s/grub-probe:%s", mockDir, mockDir, mockDir, mockDir, os.Getenv("PATH"))
			var securebootEnv string
			if secureBootState != "no-mokutil" {
				path = fmt.Sprintf("PATH=%s/mokutil:%s/zpool:%s/zfs:%s/date:%s/grub-probe:%s", mockDir, mockDir, mockDir, mockDir, mockDir, os.Getenv("PATH"))
				securebootEnv = "TEST_MOKUTIL_SECUREBOOT=" + secureBootState
			}

			var mockZFSDatasetEnv string
			if systemRootDataset != "" {
				mockZFSDatasetEnv = "TEST_MOCKZFS_CURRENT_ROOT_DATASET=" + systemRootDataset
			}

			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal("couldn't get current directory", err)
			}
			env := append(os.Environ(),
				path,
				"LC_ALL=C",
				"TZ=Europe/Paris", // we want to ensure user's timezone is taken into account
				"grub_probe="+filepath.Join(cwd, "mock/grub-probe"),
				"TEST_POOL_DIR="+testDir,
				securebootEnv,
				mockZFSDatasetEnv)

			if err := runGrubMkConfig(t, env, testDir); err != nil {
				t.Fatal("got error, expected none", err)
			}

			fileteredFPath := filepath.Join(testDir, "grub_10_linux_zfs")
			filterNonLinuxZfsContent(t, filepath.Join(testDir, "grub.cfg"), fileteredFPath)

			assertFileContentAlmostEquals(t, fileteredFPath, filepath.Join(tc.path, "grubmenu"), "generated and reference files are different.")
			devices.assertExistingPoolsAndCleanup(tc.fullTestName)

			if *slow {
				time.Sleep(time.Second)
			}
		})
	}
}

type TestCase struct {
	path         string
	fullTestName string
}

func newTestCases(t *testing.T) map[string]TestCase {
	testCases := make(map[string]TestCase)

	definitionsDir := filepath.Join(testDataDir, "definitions")
	dirs, err := ioutil.ReadDir(definitionsDir)
	if err != nil {
		t.Fatal("couldn't read bootlists modes", err)
	}
	for _, d := range dirs {
		tcDirs, err := ioutil.ReadDir(filepath.Join(definitionsDir, d.Name()))
		if err != nil {
			t.Fatal("couldn't read bootlists test cases", err)
		}

		for _, tcd := range tcDirs {
			tcName := filepath.Join(d.Name(), tcd.Name())
			tcPath := filepath.Join(definitionsDir, tcName)
			if err != nil {
				t.Fatal("couldn't read test case", err)
			}

			testCases[tcName] = TestCase{
				path:         tcPath,
				fullTestName: strings.Replace(strings.Replace(tcPath, definitionsDir+"/", "", 1), "/", "_", -1),
			}
		}
	}

	return testCases
}

// skipOnZFSPermissionDenied skips the tests if the current user can't create zfs pools, datasets…
func skipOnZFSPermissionDenied(t *testing.T) {
	t.Helper()

	u, err := user.Current()
	if err != nil {
		t.Fatal("can't get current user", err)
	}

	// in our default setup, only root users can interact with zfs kernel modules
	if u.Uid != "0" {
		t.Skip("skipping, you don't have permissions to interact with system zfs")
	}
}
