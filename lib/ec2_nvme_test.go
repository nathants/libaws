package lib

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// Execute the actual bootstrap, replacing only filesystem and command boundaries.
// Privileged/block-device commands are stubbed; PATH has no formatter or mount utility.
func runEC2NVMeInit(t *testing.T, models map[string]string, env ...string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	for name, model := range models {
		directory := filepath.Join(root, "sys", "block", name, "device")
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "model"), []byte(model+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(root, "sys", "block", name, "holders"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"fstab", "mutations"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"awk", "cat", "id", "ln", "ls", "mkdir"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range env {
		if value == "ALPINE=yes" {
			apk := "#!/bin/sh\n[ \"$*\" = 'add e2fsprogs lsblk wipefs findmnt blkid' ] || exit 99\nprintf 'fixture apk called\\n'\n"
			if err := os.WriteFile(filepath.Join(bin, "apk"), []byte(apk), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	script := `
case "${FIXTURE:-}" in
    empty-mountpoint) mkdir "$ROOT/mnt" ;;
    nonempty-mountpoint) mkdir "$ROOT/mnt"; echo keep > "$ROOT/mnt/sentinel" ;;
    file-mountpoint) echo keep > "$ROOT/mnt" ;;
    symlink-mountpoint) ln -s "$ROOT/other" "$ROOT/mnt" ;;
    holder) echo keep > "$ROOT/sys/block/nvme1n1/holders/dm-0" ;;
    fstab) printf 'UUID=keep %s/mnt ext4 defaults 0 2\n' "$ROOT" > "$ROOT/fstab" ;;
esac
sudo() {
    [ "${FAIL:-}" != "$1" ] || { echo "failed read/operation: $1" >&2; return 99; }
    case "$1" in
        apk|mkfs.ext4|mount|chown|tee|lsblk|wipefs|blkid|mkdir|ls|awk) "$@" ;;
        *) echo "forbidden privileged command: $*" >&2; return 99 ;;
    esac
}
ls() {
    [ "${FAIL:-}" != ls ] || return 99
    command ls "$@"
}
mkfs.ext4() {
    [ "$*" = '-E nodiscard /dev/nvme1n1' ] || return 99
    printf 'format %s\n' "$*" >> "$ROOT/mutations"
}
mount() { printf 'mount %s\n' "$*" >> "$ROOT/mutations"; }
chown() { :; }
tee() {
    [ "$*" = "-a $ROOT/fstab" ] || return 99
    cat >> "$ROOT/fstab"
}
lsblk() {
    case "$*" in
        *MOUNTPOINT*) printf '%s\n' "${MOUNTS-}" ;;
        *TYPE*) printf '%s\n' "${LAYOUT-disk}" ;;
        *RO*) printf '%s\n' "${RO-0}" ;;
        *) echo "unexpected lsblk: $*" >&2; return 99 ;;
    esac
}
wipefs() {
    [ "$*" = '--no-act --noheadings --output TYPE /dev/nvme1n1' ] || return 99
    printf '%s\n' "${SIGNATURE-}"
}
blkid() { printf '%s\n' "${UUID-11111111-2222-3333-4444-555555555555}"; }
findmnt() {
    [ "${FAIL:-}" != findmnt ] || return 99
    printf '%s\n' "${MOUNTPOINTS-/}"
}
`
	script += strings.NewReplacer(
		"/sys/block", filepath.Join(root, "sys", "block"),
		"/etc/fstab", filepath.Join(root, "fstab"),
		"/mnt", filepath.Join(root, "mnt"),
	).Replace(nvmeInit)
	script += "\nprintf 'fixture payload ran\\n'\n"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-e", "-c", script)
	cmd.Env = []string{"PATH=" + bin, "ROOT=" + root, "HOME=" + root, "LC_ALL=C"}
	for _, value := range env {
		cmd.Env = append(cmd.Env, strings.ReplaceAll(value, "/mnt", filepath.Join(root, "mnt")))
	}
	output, err := cmd.CombinedOutput()
	mutations, readErr := os.ReadFile(filepath.Join(root, "mutations"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err == nil && len(mutations) > 0 {
		fstab, readErr := os.ReadFile(filepath.Join(root, "fstab"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		expected := "UUID=11111111-2222-3333-4444-555555555555 " + filepath.Join(root, "mnt") + " ext4 nodiscard,noatime,nofail 0 2\n"
		if string(fstab) != expected {
			t.Fatalf("unexpected fstab: %q", fstab)
		}
	}
	return string(mutations), string(output), err
}

func TestEC2NVMeInitExcludesEBSDataVolumes(t *testing.T) {
	mutations, output, err := runEC2NVMeInit(t, map[string]string{
		"nvme0n1": "Amazon Elastic Block Store",
		"nvme1n1": "Amazon EC2 NVMe Instance Storage",
		"nvme9n1": "Amazon Elastic Block Store",
	})
	if err != nil || !strings.Contains(mutations, "format ") || !strings.Contains(mutations, "/dev/nvme1n1") || strings.Contains(mutations, "/dev/nvme0n1") || strings.Contains(mutations, "/dev/nvme9n1") {
		t.Fatalf("bootstrap must use only the instance-store disk: error=%v mutations=%s output=%s", err, mutations, output)
	}
}

func TestEC2NVMeInitUsesOneInstanceStoreDisk(t *testing.T) {
	for _, fixture := range []string{"", "empty-mountpoint"} {
		t.Run(fixture, func(t *testing.T) {
			mutations, output, err := runEC2NVMeInit(t, map[string]string{
				"nvme0n1": "Amazon Elastic Block Store",
				"nvme1n1": "Amazon EC2 NVMe Instance Storage    ",
				"nvme2n1": "Amazon EC2 NVMe Instance Storage",
			}, "FIXTURE="+fixture)
			if err != nil || strings.Count(mutations, "format ") != 1 || strings.Count(mutations, "mount ") != 1 || strings.Contains(mutations, "/dev/nvme2n1") || !strings.Contains(output, "fixture payload ran") {
				t.Fatalf("one usable local disk must precede the payload: error=%v mutations=%s output=%s", err, mutations, output)
			}
		})
	}
}

func TestEC2NVMeInitWithoutInstanceStoreDoesNothing(t *testing.T) {
	for name, models := range map[string]map[string]string{
		"no-nvme":                  nil,
		"ebs-only":                 {"nvme0n1": "Amazon Elastic Block Store", "nvme9n1": "Amazon Elastic Block Store"},
		"unknown-model":            {"nvme1n1": "other NVMe disk"},
		"model-must-match-exactly": {"nvme1n1": "Amazon EC2 NVMe Instance Storage extra"},
	} {
		t.Run(name, func(t *testing.T) {
			mutations, output, err := runEC2NVMeInit(t, models, "FAIL=lsblk")
			if err != nil || mutations != "" || !strings.Contains(output, "fixture payload ran") {
				t.Fatalf("no instance store must leave disks alone and run the payload: error=%v mutations=%s output=%s", err, mutations, output)
			}
		})
	}
}

func TestEC2NVMeInitRefusesUsedDisksAndMountpoints(t *testing.T) {
	for name, setting := range map[string]string{
		"partition": "LAYOUT=disk\npart", "dependent-device": "LAYOUT=disk\ncrypt",
		"missing-layout": "LAYOUT=", "read-only": "RO=1", "unknown-writability": "RO=",
		"mounted": "MOUNTS=/", "swap": "MOUNTS=[SWAP]", "holder": "FIXTURE=holder",
		"filesystem": "SIGNATURE=ext4", "partition-table": "SIGNATURE=gpt", "raid": "SIGNATURE=linux_raid_member",
		"mountpoint-in-use": "MOUNTPOINTS=/\n/mnt", "missing-mount-table": "MOUNTPOINTS=",
		"fstab-entry": "FIXTURE=fstab", "mountpoint-has-data": "FIXTURE=nonempty-mountpoint",
		"mountpoint-is-file": "FIXTURE=file-mountpoint", "mountpoint-is-symlink": "FIXTURE=symlink-mountpoint",
		"device-read-error": "FAIL=lsblk", "signature-read-error": "FAIL=wipefs",
		"holder-read-error": "FAIL=ls", "mount-table-read-error": "FAIL=findmnt", "fstab-read-error": "FAIL=awk",
	} {
		t.Run(name, func(t *testing.T) {
			mutations, output, err := runEC2NVMeInit(t, map[string]string{
				"nvme0n1": "Amazon Elastic Block Store", "nvme1n1": "Amazon EC2 NVMe Instance Storage",
			}, setting)
			if err == nil || mutations != "" || strings.Contains(output, "fixture payload ran") {
				t.Fatalf("unsafe or unreadable state must stop before formatting/payload: error=%v mutations=%s output=%s", err, mutations, output)
			}
		})
	}
}

func TestEC2NVMeInitStopsAfterCreationFailure(t *testing.T) {
	for _, setting := range []string{"FAIL=mkfs.ext4", "FAIL=blkid", "UUID=", "UUID=invalid", "FAIL=mount", "FAIL=chown", "FAIL=tee"} {
		t.Run(setting, func(t *testing.T) {
			_, output, err := runEC2NVMeInit(t, map[string]string{"nvme1n1": "Amazon EC2 NVMe Instance Storage"}, setting)
			if err == nil || strings.Contains(output, "fixture payload ran") {
				t.Fatalf("failed initialization must not run the payload: error=%v output=%s", err, output)
			}
		})
	}
}

func TestEC2MakeInitDetectsStorageWithoutAnInstanceFamilyList(t *testing.T) {
	for _, instanceType := range []ec2types.InstanceType{"i3.large", "i4i.large", "i4g.large", "i7i.large", "i7ie.large", "c7gd.medium", "t3.small"} {
		t.Run(string(instanceType), func(t *testing.T) {
			payload := "printf 'user payload\\n'\n"
			encoded, err := makeInit(&EC2Config{UserName: "admin", InstanceType: instanceType, Init: payload})
			if err != nil {
				t.Fatal(err)
			}
			outer, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			expected := base64.StdEncoding.EncodeToString([]byte(nvmeInit + payload))
			if !strings.Contains(string(outer), "echo "+expected+" | base64 -d") || !strings.Contains(string(outer), "sudo -u admin bash -e") {
				t.Fatalf("launch must inspect actual devices before the user payload: %s", outer)
			}
		})
	}
}

func TestEC2NVMeInitInstallsAlpineSafetyTools(t *testing.T) {
	models := map[string]string{"nvme1n1": "Amazon EC2 NVMe Instance Storage"}
	mutations, output, err := runEC2NVMeInit(t, models, "ALPINE=yes")
	if err != nil || mutations == "" || !strings.Contains(output, "fixture apk called") {
		t.Fatalf("Alpine must install the safety tools before use: error=%v mutations=%s output=%s", err, mutations, output)
	}
	mutations, output, err = runEC2NVMeInit(t, models, "ALPINE=yes", "FAIL=apk")
	if err == nil || mutations != "" || strings.Contains(output, "fixture payload ran") {
		t.Fatalf("failed tool installation must prevent initialization: error=%v mutations=%s output=%s", err, mutations, output)
	}
	mutations, output, err = runEC2NVMeInit(t, map[string]string{"nvme0n1": "Amazon Elastic Block Store"}, "ALPINE=yes")
	if err != nil || mutations != "" || strings.Contains(output, "fixture apk called") {
		t.Fatalf("EBS-only guests need no instance-store tools: error=%v mutations=%s output=%s", err, mutations, output)
	}
}
