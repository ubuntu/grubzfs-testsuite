package main_test

import (
	"context"
	"fmt"
	"io/ioutil"
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
	Name    string
	Type    string
	Content map[string]string
	ZFS     struct {
		PoolName string `yaml:"pool_name"`
		Datasets []struct {
			Name             string
			Content          map[string]string
			ZsysBootfs       bool      `yaml:"zsys_bootfs"`
			LastUsed         time.Time `yaml:"last_used"`
			LastBootedKernel string    `yaml:"last_booted_kernel"`
			Mountpoint       string
			CanMount         string
			Snapshots        []struct {
				Name             string
				Content          map[string]string
				Fstab            []FstabEntry
				CreationDate     time.Time `yaml:"creation_date"`
				LastBootedKernel string    `yaml:"last_booted_kernel"`
			}
			Fstab []FstabEntry
		}
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

// create on disk mock devices as files
func (fdevice FakeDevices) create(path, testName string) {
	for _, device := range fdevice.Devices {

		// Create file on disk
		p := filepath.Join(path, device.Name+".disk")
		f, err := os.Create(p)
		if err != nil {
			fdevice.Fatal("couldn't create device file on disk", err)
		}
		if err = f.Truncate(100 * mB); err != nil {
			f.Close()
			fdevice.Fatal("couldn't initializing device size on disk", err)
		}
		f.Close()

		deviceMountPath := filepath.Join(path, device.Name)
		if err := os.MkdirAll(deviceMountPath, os.ModeDir); err != nil {
			fdevice.Fatal("couldn't create directory for pool", err)
		}

		switch strings.ToLower(device.Type) {
		case "zfs":
			// WORKAROUND: we need to use dirname of path as creating 2 consecutives pools with similar dataset name
			// will make the second dataset from the second pool returning as parent pool the first one.
			// Of course, the resulting mountpoint will be wrong.
			poolName := testName + "-" + device.ZFS.PoolName

			vdev := zfs.VDevTree{
				Type:    zfs.VDevTypeFile,
				Path:    p,
				Devices: []zfs.VDevTree{{Type: zfs.VDevTypeFile, Path: p}},
			}

			features := make(map[string]string)
			props := make(map[zfs.Prop]string)
			props[zfs.PoolPropAltroot] = deviceMountPath
			fsprops := make(map[zfs.Prop]string)
			fsprops[zfs.DatasetPropMountpoint] = "/"
			fsprops[zfs.DatasetPropCanmount] = "off"

			pool, err := zfs.PoolCreate(poolName, vdev, features, props, fsprops)
			if err != nil {
				fdevice.Fatalf("couldn't create pool %q: %v", device.ZFS.PoolName, err)
			}
			defer pool.Close()
			defer pool.Export(true, "export temporary pool")

			for _, dataset := range device.ZFS.Datasets {
				func() {
					datasetName := poolName + "/" + dataset.Name
					datasetPath := ""
					shouldMount := false
					props := make(map[zfs.Prop]zfs.Property)
					if dataset.Mountpoint != "" {
						props[zfs.DatasetPropMountpoint] = zfs.Property{Value: dataset.Mountpoint}
					}
					if dataset.CanMount != "" {
						props[zfs.DatasetPropCanmount] = zfs.Property{Value: dataset.CanMount}
						if dataset.CanMount == "noauto" || dataset.CanMount == "on" {
							shouldMount = true
						}
					}
					d, err := zfs.DatasetCreate(datasetName, zfs.DatasetTypeFilesystem, props)
					if err != nil {
						fdevice.Fatalf("couldn't create dataset %q: %v", datasetName, err)
					}
					defer d.Close()
					if dataset.ZsysBootfs {
						d.SetUserProperty("org.zsys:bootfs", "yes")
					}
					if dataset.LastBootedKernel != "" {
						d.SetUserProperty("org.zsys:last-booted-kernel", dataset.LastBootedKernel)
					}
					if !dataset.LastUsed.IsZero() {
						d.SetUserProperty("org.zsys:last-used", strconv.FormatInt(dataset.LastUsed.Unix(), 10))
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

						defer os.RemoveAll(datasetPath)
						defer d.UnmountAll(0)
					}

					for _, s := range dataset.Snapshots {
						func() {
							replaceContent(fdevice.T, s.Content, datasetPath)
							completeSystemWithFstab(fdevice.T, testName, path, dataset.Mountpoint, datasetPath, s.Fstab)
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
							d.SetUserProperty("org.zsys:creation.test", s.CreationDate.In(location).Format("Mon Jan 2 15:04 2006"))

							if s.LastBootedKernel != "" {
								d.SetUserProperty("org.zsys:last-booted-kernel", s.LastBootedKernel)
							}
						}()
					}

					if shouldMount {
						replaceContent(fdevice.T, dataset.Content, datasetPath)
						completeSystemWithFstab(fdevice.T, testName, path, dataset.Mountpoint, datasetPath, dataset.Fstab)
					}
				}()
			}

		case "ext4":
			go func() {
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
	}
}

// completeSystemWithFstab ensures the system has required /boot and /etc, and can take a dynamically generated
// fstab
func completeSystemWithFstab(t *testing.T, testName, path, mountpoint, datasetPath string, entries []FstabEntry) {
	// We need to ensure that / has at least empty /boot and /etc mountpoint to mount parent
	// dataset or file system
	if mountpoint == "/" {
		for _, p := range []string{"/boot", "/etc"} {
			os.MkdirAll(filepath.Join(datasetPath, p), os.ModeDir)
		}
	}
	// Generate a fstab if there is some needs as pool and disk names are dynamic
	fstabPath := filepath.Join(datasetPath, "etc", "fstab")
	if mountpoint == "/etc" {
		fstabPath = filepath.Join(datasetPath, "fstab")
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
			filesystem = testName + "-" + fstabEntry.Filesystem
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
		if err := copy.Copy(filepath.Join("testdata", "content", src), filepath.Join(dst, p)); err != nil {
			t.Fatalf("couldn't copy %q to %q: %v", src, dst, err)
		}
	}
}
