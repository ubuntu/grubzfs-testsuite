package main_test

import (
	"flag"
	"io/ioutil"
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
)

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
			path := "PATH=mocks/zpool:mocks/zfs:mocks/date:" + os.Getenv("PATH")
			var securebootEnv string
			if secureBootState != "no-mokutil" {
				path = "PATH=mocks/mokutil:mocks/zpool:mocks/zfs:mocks/date:" + os.Getenv("PATH")
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
			path := "PATH=mocks/grub-probe:" + os.Getenv("PATH")
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal("couldn't get current directory", err)
			}
			env := append(os.Environ(),
				path,
				"grub_probe="+filepath.Join(cwd, "mock/grub-probe"),
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

			path := "PATH=mocks/zpool:mocks/zfs:mocks/date:mocks/grub-probe:" + os.Getenv("PATH")
			var securebootEnv string
			if secureBootState != "no-mokutil" {
				path = "PATH=mocks/mokutil:mocks/zpool:mocks/zfs:mocks/date:mocks/grub-probe:" + os.Getenv("PATH")
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

	bootListsDir := "testdata/definitions"
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
				path:         tcPath,
				fullTestName: strings.Replace(strings.Replace(tcPath, bootListsDir+"/", "", 1), "/", "_", -1),
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
