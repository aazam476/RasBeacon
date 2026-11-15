//go:build linux

// Command rbquotafs holds a share on a USB drive to its quota (see
// internal/quotafs): it shows the share's folder at a mount point, where
// nothing more can be added once the files reach the quota, while reading
// and deleting still work. rbd runs one for each share on a USB drive and
// gives the plugin the mount point.
//
//	rbquotafs -limit <bytes> <folder> <mount point>
//
// Like gocryptfs, it returns once the share is mounted, and keeps serving
// it in the background until it is unmounted (umount <mount point>).
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"rasbeacon/internal/quotafs"
)

// childEnv marks the background process that serves the mount.
const childEnv = "RBQUOTAFS_SERVE"

func main() {
	limit := flag.Int64("limit", 0, "the quota, in bytes")
	flag.Parse()
	if flag.NArg() != 2 || *limit <= 0 {
		fmt.Fprintln(os.Stderr, "usage: rbquotafs -limit <bytes> <folder> <mount point>")
		os.Exit(2)
	}
	source, mountpoint := flag.Arg(0), flag.Arg(1)
	if os.Getenv(childEnv) == "" {
		os.Exit(startInBackground())
	}
	os.Exit(serve(source, mountpoint, *limit))
}

// startInBackground runs this program again as a background process (in a
// session of its own, so it outlives the command that started it), and
// waits until it says the share is mounted, or why it isn't. It returns
// the exit code.
func startInBackground() int {
	ready, tell, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rbquotafs:", err)
		return 1
	}
	child := exec.Command(os.Args[0], os.Args[1:]...)
	child.Env = append(os.Environ(), childEnv+"=1")
	child.ExtraFiles = []*os.File{tell} // fd 3 in the child
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "rbquotafs:", err)
		return 1
	}
	tell.Close()
	answer, _ := io.ReadAll(ready)
	if strings.TrimSpace(string(answer)) != "ok" {
		fmt.Fprintln(os.Stderr, "rbquotafs:", strings.TrimSpace(string(answer)))
		return 1
	}
	return 0
}

// serve mounts the share, tells the waiting command (on fd 3), and serves
// it until it is unmounted. It returns the exit code.
func serve(source, mountpoint string, limit int64) int {
	tell := os.NewFile(3, "ready")
	used := quotafs.FolderSize(source)
	server, err := quotafs.Mount(source, mountpoint, quotafs.NewQuota(used, limit), true)
	if err != nil {
		fmt.Fprintln(tell, err)
		return 1
	}
	fmt.Fprintln(tell, "ok")
	tell.Close()
	log.Printf("rbquotafs: %s at %s, %d of %d bytes used", source, mountpoint, used, limit)
	server.Wait()
	return 0
}
