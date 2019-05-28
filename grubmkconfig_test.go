package main_test

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runGrubMkConfig setup and runs grubMkConfig.
func runGrubMkConfig(t *testing.T, env []string, testDir string) error {
	for _, path := range []string{
		"etc/grub.d/15_linux_zfs", "/etc/grub.d/00_header", "/etc/default/grub", "/usr/sbin/grub-mkconfig"} {
		copyFile(t, path, filepath.Join(testDir, path))
	}
	grubMkConfig := filepath.Join(testDir, "/usr/sbin/grub-mkconfig")
	// Update in place sysconfigdir and exports variables in grub-mkconfig so that we target a specific
	// /etc directory for grub scripts.
	// We need to set grub_probe twice: once in environment (for subprocess) and once in grub_mkconfig directly
	updateFile(t, grubMkConfig, map[string]string{
		`sysconfdir="/etc"`: `sysconfdir="` + testDir + `/etc"` +
			"\nexport GRUB_LINUX_ZFS_TEST GRUB_LINUX_ZFS_TEST_INPUT GRUB_LINUX_ZFS_TEST_OUTPUT TEST_POOL_DIR TEST_MOKUTIL_SECUREBOOT TEST_MOCKZFS_CURRENT_ROOT_DATASET LC_ALL grub_probe\n",
		`grub_probe="${sbindir}/grub-probe"`: "grub_probe=`which grub-probe`",
	})
	// Update 15_linux_zfs to replace /dev/loopX loop devices by /dev/loop00 when calling prepare_grub_to_access_device.
	updateFile(t, filepath.Join(testDir, "etc", "grub.d", "15_linux_zfs"), map[string]string{
		"prepare_grub_to_access_device_cached() {": "prepare_grub_to_access_device_cached() {\n" +
			`case "$1" in /dev/loop*) set -- /dev/loop00;; esac`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "fakeroot", grubMkConfig, "-o", filepath.Join(testDir, "grub.cfg"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	return cmd.Run()
}

// updateFile update the file inline by replacing for each element in replace map by what its value.
func updateFile(t *testing.T, path string, replace map[string]string) {
	t.Helper()

	src, err := os.OpenFile(path, os.O_RDWR, 0755)
	if err != nil {
		t.Fatalf("can't open %q: %v", path, err)
	}
	defer src.Close()

	s := bufio.NewScanner(src)
	var text string
	for s.Scan() {
		t := s.Text()

		for k, v := range replace {
			t = strings.ReplaceAll(t, k, v)
		}

		if text == "" {
			text = t
		} else {
			text = text + "\n" + t
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("can't replace sysconfigdir in %q: %v", path, err)
	}

	if err := src.Truncate(0); err != nil {
		t.Fatalf("can't truncate %q: %v", src.Name(), err)
	}
	if _, err := src.WriteAt([]byte(text), 0); err != nil {
		t.Fatalf("can't write to %q, %v", src.Name(), err)
	}
}
