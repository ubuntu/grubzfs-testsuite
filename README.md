# grubzfs-testsuite
Integration tests for zfs (zsys/non zsys) grub menu generation (`/etc/grub.d/10_linux_zfs`)

## Test dependencies

On Debian and derivatives, the following packages are required (grub and zfs):
* grub-common
* zfsutils-linux
* libzfslinux-dev
* e2fsprogs

Go 1.11 (minimum) is required.

## Running the tests
As the tests are interacting with zfs kernel modules, the user should have zpool and zfs dataset creation permissions.

We are checking if user is root for tests dealing with zpool creation.

```
# go test
```

Alternatively, you can use a test binary (compiled by `go test -c`). The test binary will pick any `datadir` and `mocks`
(or `cmd/` see "Mock rebuild conditions") in the current directory. If it can't find them, it will pick them relative to
the test binary directory itself.

### Types of tests

There are 4 types of test:
* **TestBootlist**: Test the generation of the intermediary bootlist file.
* **TestMetaMenu**: Test the generation of the intermediary metamenu file from a bootlist.
* **TestGrubMenu**: Test the generation of the finale grub configuration file from a metamenu.
* **TestGrubMkConfig**: Run all the above coverage in one shot, without intermediary files.

> Note that tests that don't deal with dataset creation can be executed in parallel.

### Targeting a different 10_linux_zfs file

By default, the tests are using the installed version of `10_linux_zfs` located in `/etc/grub.d/`. You can target a different file by passing its path to the command line option `-linux-zfs=<path>`.

If you have multiple tests to run, you can export `GRUBTESTS_LINUXZFS=<path>` to avoid setting the flag each for each test. It will take precedence over the command line argument.

### Updating reference files

The first 3 types of test are using reference (golden) files and compare the generated output with those.

You can update the reference files with the `-update` command line argument. This argument will also refresh the reference files if they already exist.

> The updated golden files should be committed to the VCS.

### Slow mode options

As of ZFS 0.7, you can't create multiple times pools with the same names. There is a risk to create data locks. The `-slow` option seems to alleviate the issue by temporizing tests when creating/removing pools and datasets.

### Dangerous mode

Some tests need to move utilities outside of the user's `$PATH` before restoring them. As we are tempering the system, those tests are skipped by default. The command line option `-dangerous` will run them.

## Mock rebuild conditions

We are using mocks (sources are in the `cmd/` directory) and rebuild them each time you run tests. If this directory
isn't available, we assume there is a `mocks` subdirectory, with one subdirectory for each mocks.
.
This is mostly used when building a test binary while not shipping the source in a binary package.
