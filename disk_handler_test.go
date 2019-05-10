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

type FakeDevice struct {
	Name string
	Type string
	ZFS  struct {
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
				CreationDate     time.Time `yaml:"creation_date"`
				LastBootedKernel string    `yaml:"last_booted_kernel"`
			}
			Fstab []struct {
				Filesystem string
				Mountpoint string
				Type       string
			}
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

		switch strings.ToLower(device.Type) {
		case "zfs":
			// WORKAROUND: we need to use dirname of path as creating 2 consecutives pools with similar dataset name
			// will make the second dataset from the second pool returning as parent pool the first one.
			// Of course, the resulting mountpoint will be wrong.
			poolName := testName + "-" + device.ZFS.PoolName

			poolMountPath := filepath.Join(path, device.Name)
			if err := os.MkdirAll(poolMountPath, os.ModeDir); err != nil {
				fdevice.Fatal("couldn't create directory for pool", err)
			}
			vdev := zfs.VDevTree{
				Type:    zfs.VDevTypeFile,
				Path:    p,
				Devices: []zfs.VDevTree{{Type: zfs.VDevTypeFile, Path: p}},
			}

			features := make(map[string]string)
			props := make(map[zfs.Prop]string)
			props[zfs.PoolPropAltroot] = poolMountPath
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
							// Mount manually datasetPath if set to legacy to "/" (poolMountPath)
						} else {
							datasetPath = poolMountPath
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
							props := make(map[zfs.Prop]zfs.Property)
							d, err := zfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props)
							if err != nil {
								fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
								os.Exit(1)
							}
							defer d.Close()
							d.SetUserProperty("org.zsys:creation.test", strconv.FormatInt(s.CreationDate.Unix(), 10))
							if s.LastBootedKernel != "" {
								d.SetUserProperty("org.zsys:last-booted-kernel", s.LastBootedKernel)
							}
						}()
					}

					if shouldMount {
						replaceContent(fdevice.T, dataset.Content, datasetPath)
						// We need to ensure that / has at least empty /boot and /etc mountpoint to mount parent
						// dataset or file system
						if dataset.Mountpoint == "/" {
							for _, p := range []string{"/boot", "/etc"} {
								os.MkdirAll(filepath.Join(datasetPath, p), os.ModeDir)
							}
						}
						// Generate a fstab if there is some needs as pool and disk names are dynamic
						fstabPath := filepath.Join(datasetPath, "etc", "fstab")
						if dataset.Mountpoint == "/etc" {
							fstabPath = filepath.Join(datasetPath, "fstab")
						}
						for _, fstabEntry := range dataset.Fstab {
							f, err := os.OpenFile(fstabPath, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
							if err != nil {
								fdevice.Fatal("couldn't append to fstab", err)
							}
							defer f.Close()

							var filesystem string
							switch fstabEntry.Type {
							case "zfs":
								filesystem = testName + "-" + fstabEntry.Filesystem
							case "ext4":
								filesystem = filepath.Join(path, fstabEntry.Filesystem+".disk")
							default:
								fdevice.Fatalf("invalid filesystem type: %s", fstabEntry.Type)
							}
							if _, err := f.Write([]byte(
								fmt.Sprintf("%s\t%s\t%s\tdefaults\t0\t0\n",
									filesystem, fstabEntry.Mountpoint, fstabEntry.Type))); err != nil {
								fdevice.Fatal("couldn't write to fstab", err)
							}
						}
					}
				}()

			}

		case "ext4":
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "mkfs.ext4", "-q", "-F", p)

			if err := cmd.Run(); err != nil {
				fdevice.Fatal("got error, expected none", err)
			}

		case "":
			// do nothing for "no pool, no partition" (empty disk)

		default:
			fdevice.Fatalf("unknown type: %s", device.Type)
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
