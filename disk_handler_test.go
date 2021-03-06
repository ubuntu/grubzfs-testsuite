package main_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	zfs "github.com/bicomsystems/go-libzfs"
	"github.com/otiai10/copy"
	"gopkg.in/yaml.v2"
)

const mB = 1024 * 1024

type FakeDevices struct {
	Devices []FakeDevice
	*testing.T
}

type FstabEntry struct {
	Filesystem string
	Mountpoint string
	Type       string
}

type FakeDevice struct {
	Names   []string
	Type    string
	Content map[string]string
	ZFS     struct {
		PoolName string `yaml:"pool_name"`
		Datasets []struct {
			Name                string
			KeepImported        bool `yaml:"keep_imported"`
			Content             map[string]string
			IsCurrentSystemRoot bool      `yaml:"is_current_system_root"`
			ZsysBootfs          bool      `yaml:"zsys_bootfs"`
			LastUsed            time.Time `yaml:"last_used"`
			LastBootedKernel    string    `yaml:"last_booted_kernel"`
			Mountpoint          string
			CanMount            string
			Snapshots           []struct {
				Name             string
				Content          map[string]string
				Fstab            []FstabEntry
				CreationDate     time.Time `yaml:"creation_date"`
				LastBootedKernel string    `yaml:"last_booted_kernel"`
			}
			Fstab []FstabEntry
		}
		KeepImported bool `yaml:"keep_imported"`
	}
}

// newFakeDevices returns a FakeDevices from a yaml file
func newFakeDevices(t *testing.T, path string) FakeDevices {
	devices := FakeDevices{T: t}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal("couldn't read yaml definition file", err)
	}
	if err = yaml.Unmarshal(b, &devices); err != nil {
		t.Fatal("couldn't unmarshal device list", err)
	}

	return devices
}

// shuffleFile shuffles chunks of filePath
// NOTE: There is a risk of flaky test because the pool is closed after the device has been altered.
// If it's the case revisit the moment this function is called to call it after the defered pool.Close
func shuffleFile(fdevice FakeDevices, filePath string) {
	var chunks int64 = 100

	f, err := os.OpenFile(filePath, os.O_RDWR, os.ModeAppend)
	if err != nil {
		fdevice.Fatal("Failed to open device file", err)
	}
	defer f.Close()

	s, err := f.Stat()
	if err != nil {
		fdevice.Fatal("Failed to stat file", err)
	}

	chunkSize := s.Size() / chunks
	b := make([]byte, int(chunkSize))

	var i int64
	for i = 1; i < chunks; i++ {
		if _, err := f.ReadAt(b, i*chunkSize); err != nil {
			fdevice.Fatal("Failed to read file", err)
		}
		rand.Shuffle(len(b), func(x, y int) { b[x], b[y] = b[y], b[x] })
		if _, err := f.WriteAt(b, i*chunkSize); err != nil {
			fdevice.Fatal("Failed to write to file", err)
		}
	}
}

// create on disk mock devices as files and return the main dataset
// which is set as root, if any
func (fdevice FakeDevices) create(path string) string {
	var systemRootDataset string

	for _, device := range fdevice.Devices {
		func() {
			// Create file on disk
			var devPaths []string
			for _, deviceName := range device.Names {
				p := filepath.Join(path, deviceName+".disk")
				f, err := os.Create(p)
				if err != nil {
					fdevice.Fatal("couldn't create device file on disk", err)
				}
				if err = f.Truncate(100 * mB); err != nil {
					f.Close()
					fdevice.Fatal("couldn't initializing device size on disk", err)
				}
				devPaths = append(devPaths, p)
				f.Close()
			}

			deviceMountPath := filepath.Join(path, device.Names[0])
			if err := os.MkdirAll(deviceMountPath, 0700); err != nil {
				fdevice.Fatal("couldn't create directory for pool", err)
			}

			switch strings.ToLower(device.Type) {
			case "zfs":
				var devs []zfs.VDevTree

				for _, p := range devPaths {
					devs = append(devs, zfs.VDevTree{
						Type: zfs.VDevTypeFile,
						Path: p,
					})
				}
				var t zfs.VDevType = zfs.VDevTypeFile
				if len(devs) > 1 {
					t = zfs.VDevTypeMirror
				}
				vdev := zfs.VDevTree{
					Type:    t,
					Devices: devs,
				}

				features := make(map[string]string)
				props := make(map[zfs.Prop]string)
				props[zfs.PoolPropAltroot] = deviceMountPath
				fsprops := make(map[zfs.Prop]string)

				pool, err := zfs.PoolCreate(device.ZFS.PoolName, vdev, features, props, fsprops)
				if err != nil {
					fdevice.Fatalf("couldn't create pool %q: %v", device.ZFS.PoolName, err)
				}
				defer pool.Close()
				defer func() {
					if device.ZFS.KeepImported {
						return
					}
					pool.Export(true, "export temporary pool")
				}()

				for _, dataset := range device.ZFS.Datasets {
					func() {
						datasetName := device.ZFS.PoolName + "/" + dataset.Name
						var datasetPath string
						var d zfs.Dataset
						if dataset.Name == "." {
							d, err = zfs.DatasetOpen(device.ZFS.PoolName)
							if err != nil {
								fdevice.Fatalf("couldn't open dataset %q: %v", datasetName, err)
							}
						} else {
							props := make(map[zfs.Prop]zfs.Property)
							d, err = zfs.DatasetCreate(datasetName, zfs.DatasetTypeFilesystem, props)
							if err != nil {
								fdevice.Fatalf("couldn't create dataset %q: %v", datasetName, err)
							}
						}
						defer d.Close()

						if dataset.IsCurrentSystemRoot {
							systemRootDataset = datasetName
						}

						var shouldMount bool
						if dataset.Mountpoint != "" {
							d.SetProperty(zfs.DatasetPropMountpoint, dataset.Mountpoint)
						}
						if dataset.CanMount != "" {
							d.SetProperty(zfs.DatasetPropCanmount, dataset.CanMount)
							if dataset.CanMount == "noauto" || dataset.CanMount == "on" {
								shouldMount = true
							}
							d.Unmount(0)
						}

						if dataset.ZsysBootfs {
							d.SetUserProperty("com.ubuntu.zsys:bootfs", "yes")
							if !dataset.LastUsed.IsZero() {
								d.SetUserProperty("com.ubuntu.zsys:last-used", strconv.FormatInt(dataset.LastUsed.Unix(), 10))
							}
						}
						if dataset.LastBootedKernel != "" {
							d.SetUserProperty("com.ubuntu.zsys:last-booted-kernel", dataset.LastBootedKernel)
						}
						if shouldMount {
							// get potentially inherited mountpoint path
							mountProp, err := d.GetProperty(zfs.DatasetPropMountpoint)
							if err != nil {
								fdevice.Fatalf("couldn't get mount point for %q: %v", datasetName, err)
							}
							datasetPath = mountProp.Value
							if datasetPath != "legacy" {
								if err := d.Mount("", 0); err != nil {
									fdevice.Fatalf("couldn't mount dataset: %q: %v", datasetName, err)
								}
								// Mount manually datasetPath if set to legacy to "/" (deviceMountPath)
							} else {
								datasetPath = deviceMountPath
								if err := syscall.Mount(datasetName, datasetPath, "zfs", 0, ""); err != nil {
									fdevice.Fatalf("couldn't manually mount dataset: %q: %v", datasetName, err)
								}
							}

							if !dataset.KeepImported {
								defer os.RemoveAll(datasetPath)
								defer d.UnmountAll(0)
							}
						}

						for _, s := range dataset.Snapshots {
							func() {
								replaceContent(fdevice.T, s.Content, datasetPath)
								completeSystemWithFstab(fdevice.T, path, dataset.Mountpoint, datasetPath, false, time.Time{}, s.Fstab)
								props := make(map[zfs.Prop]zfs.Property)
								d, err := zfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props)
								if err != nil {
									fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
									os.Exit(1)
								}
								defer d.Close()

								// Convert time in current timezone for mock
								location, err := time.LoadLocation("Local")
								if err != nil {
									fdevice.Fatal("couldn't get current timezone", err)
								}
								d.SetUserProperty("com.ubuntu.zsys:creation.test", strconv.FormatInt(s.CreationDate.In(location).Unix(), 10))

								if s.LastBootedKernel != "" {
									d.SetUserProperty("com.ubuntu.zsys:last-booted-kernel", s.LastBootedKernel)
								}
							}()
						}

						if shouldMount {
							replaceContent(fdevice.T, dataset.Content, datasetPath)
							completeSystemWithFstab(fdevice.T, path, dataset.Mountpoint, datasetPath, dataset.ZsysBootfs, dataset.LastUsed, dataset.Fstab)
						}
					}()
				}

				for _, deviceName := range device.Names {
					// Device which name is "corrupted" will suffer some randomness and become an invalid pool
					if deviceName != "corrupted" {
						continue
					}
					p := filepath.Join(path, deviceName+".disk")
					shuffleFile(fdevice, p)
				}

			case "ext4":
				if len(devPaths) > 1 {
					fdevice.Fatalf("Only one device allowed for ext4. Got %s", device.Names)
				}
				p := devPaths[0]
				func() {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "mkfs.ext4", "-q", "-F", p)
					if err := cmd.Run(); err != nil {
						fdevice.Fatalf("couldn't format %q to ext4: %v", p, err)
					}

					cmd = exec.CommandContext(ctx, "mount", "-t", "ext4", p, deviceMountPath)
					if err := cmd.Run(); err != nil {
						fdevice.Fatalf("couldn't mount ext4 partition: %v", err)
					}
					defer syscall.Unmount(deviceMountPath, 0)

					replaceContent(fdevice.T, device.Content, deviceMountPath)
				}()

			case "":
				// do nothing for "no pool, no partition" (empty disk)

			default:
				fdevice.Fatalf("unknown type: %s", device.Type)
			}
		}()
	}

	return systemRootDataset
}

// assertExistingPoolsAndCleanup ensure that pools that were imported before running grub_mkconfig are still
// imported after the menu generation.
// Note that as we can't run the tests on system which have a pool (no mount namespace in zfs), we export and destroy
// all pools for the next tests to start in a preserved state.
func (fdevice FakeDevices) assertExistingPoolsAndCleanup() {
	keepImportedPools := make(map[string]bool)
	pools, err := zfs.PoolOpenAll()
	if err != nil && err.Error() != "no error" && err.Error() != "dataset does not exist" {
		fdevice.Fatalf("couldn't open all remaining pools: %v", err)
	}
	for _, p := range pools {
		func() {
			defer p.Close()
			defer p.Destroy("destroy temporary pool after test")
			defer p.Export(true, "export temporary pool after test")
			name, err := p.Name()
			if err != nil {
				fdevice.Fatalf("couldn't aquite pool name: %v", err)
			}
			keepImportedPools[name] = true
		}()
	}

	for _, device := range fdevice.Devices {
		switch strings.ToLower(device.Type) {
		case "zfs":
			if device.ZFS.KeepImported {
				if _, ok := keepImportedPools[device.ZFS.PoolName]; !ok {
					fdevice.Errorf("we expected %s to be imported after running grub_mkconfig but it isn't", device.ZFS.PoolName)
				}
			} else {
				if _, ok := keepImportedPools[device.ZFS.PoolName]; ok {
					fdevice.Errorf("we expected %s to NOT be imported after running grub_mkconfig but it is", device.ZFS.PoolName)
				}
			}
			delete(keepImportedPools, device.ZFS.PoolName)
		}
	}
	if len(keepImportedPools) > 0 {
		fdevice.Error("One or more pools are imported but not in the definition list for this tests")
	}
}

// completeSystemWithFstab ensures the system has required /boot and /etc,
// it can update /etc/machine-id and os-release access time for non zsys systems
// and can take a dynamically generated fstab
func completeSystemWithFstab(t *testing.T, path, mountpoint, datasetPath string, isZsys bool, lastUsed time.Time, entries []FstabEntry) {
	if mountpoint != "/" && mountpoint != "/etc" {
		return
	}

	// We need to ensure that / has at least empty /boot and /etc mountpoint to mount parent
	// dataset or file system
	if mountpoint == "/" {
		for _, p := range []string{"/boot", "/etc"} {
			os.MkdirAll(filepath.Join(datasetPath, p), 0700)
		}
	}
	// Generate a fstab if there is some needs as pool and disk names are dynamic
	fstabPath := filepath.Join(datasetPath, "etc", "fstab")
	machineIdPath := filepath.Join(datasetPath, "etc", "machine-id")
	osreleasePath := filepath.Join(datasetPath, "etc", "os-release")
	if mountpoint == "/etc" {
		fstabPath = filepath.Join(datasetPath, "fstab")
		machineIdPath = filepath.Join(datasetPath, "machine-id")
		osreleasePath = filepath.Join(datasetPath, "os-release")
	}

	for _, fstabEntry := range entries {
		f, err := os.OpenFile(fstabPath, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			t.Fatal("couldn't append to fstab", err)
		}
		defer f.Close()

		var filesystem string
		switch fstabEntry.Type {
		case "zfs":
			filesystem = fstabEntry.Filesystem
		case "ext4":
			filesystem = filepath.Join(path, fstabEntry.Filesystem+".disk")
		default:
			t.Fatalf("invalid filesystem type: %s", fstabEntry.Type)
		}
		if _, err := f.Write([]byte(
			fmt.Sprintf("%s\t%s\t%s\tdefaults\t0\t0\n",
				filesystem, fstabEntry.Mountpoint, fstabEntry.Type))); err != nil {
			t.Fatal("couldn't write to fstab", err)
		}
	}

	// Change access time on machine-id when last_used isn't set.
	// If zero, set magic date (2033-05-18T03:33:20+00:00 @2000000000) for non zsys systems
	if lastUsed.IsZero() {
		lastUsed = time.Unix(2000000000, 0)
	}
	// on separated /etc, /etc/machine-id doesn't exists
	if _, err := os.Stat(machineIdPath); err == nil {
		if err := os.Chtimes(machineIdPath, lastUsed, lastUsed); err != nil {
			t.Fatal("couldn't change access time for machine-id", err)
		}
	}
	// on separated /etc, /etc/os-release doesn't exists
	if _, err := os.Stat(osreleasePath); err == nil {
		if err := os.Chtimes(osreleasePath, lastUsed, lastUsed); err != nil {
			t.Fatal("couldn't change access time for os-release", err)
		}
	}
}

// replaceContent replaces content (map) in dst from src content (preserving src)
func replaceContent(t *testing.T, sources map[string]string, dst string) {
	entries, err := ioutil.ReadDir(dst)
	if err != nil {
		t.Fatalf("couldn't read directory content for %q: %v", dst, err)
	}
	for _, e := range entries {
		p := filepath.Join(dst, e.Name())
		if err := os.RemoveAll(p); err != nil {
			t.Fatalf("couldn't clean up %q: %v", p, err)
		}
	}

	for p, src := range sources {
		if err := copy.Copy(filepath.Join(testDataDir, "content", src), filepath.Join(dst, p)); err != nil {
			t.Fatalf("couldn't copy %q to %q: %v", src, dst, err)
		}
	}
}
