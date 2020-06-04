package mountlib

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/rc"
)

// MountInfo defines the configuration for a mount
type MountInfo struct {
	unmountFn  UnmountFn
	MountPoint string    `json:"MountPoint"`
	MountedOn  time.Time `json:"MountedOn"`
	Fs         string    `json:"Fs"`
}

var (
	// Mount functions available
	mountFns = map[string]MountFn{}
	// mutex for mountFns
	mountFnsMutex = &sync.Mutex{}

	// Map of mounted path => MountInfo
	liveMounts = map[string]MountInfo{}
	// mutex for live mounts
	liveMountsMutex = sync.Mutex{}
)

// AddRc adds mount and unmount functionality to rc
func AddRc(mountUtilName string, mountFunction MountFn) {
	mountFnsMutex.Lock()
	// rcMount allows the mount command to be run from rc
	mountFns[mountUtilName] = mountFunction
	mountFnsMutex.Unlock()
}

func init() {
	rc.Add(rc.Call{
		Path:         "mount/mount",
		AuthRequired: true,
		Fn:           mountRc,
		Title:        "Create a new mount point",
		Help: `rclone allows Linux, FreeBSD, macOS and Windows to mount any of
Rclone's cloud storage systems as a file system with FUSE.

If no mountType is provided, the priority is given as follows: 1. mount 2.cmount 3.mount2

This takes the following parameters

- fs - a remote path to be mounted (required)
- mountPoint: valid path on the local machine (required)
- mountType: One of the values (mount, cmount, mount2) specifies the mount implementation to use

Eg

    rclone rc mount/mount fs=mydrive: mountPoint=/home/<user>/mountPoint
    rclone rc mount/mount fs=mydrive: mountPoint=/home/<user>/mountPoint mountType=mount
`,
	})
}

// mountRc allows the mount command to be run from rc
func mountRc(_ context.Context, in rc.Params) (out rc.Params, err error) {
	mountPoint, err := in.GetString("mountPoint")
	if err != nil {
		return nil, err
	}

	mountType, err := in.GetString("mountType")

	if err != nil || mountType == "" {
		if mountFns["mount"] != nil {
			mountType = "mount"
		} else if mountFns["cmount"] != nil {
			mountType = "cmount"
		} else if mountFns["mount2"] != nil {
			mountType = "mount2"
		}
	}

	// Get Fs.fs to be mounted from fs parameter in the params
	fdst, err := rc.GetFs(in)
	if err != nil {
		return nil, err
	}

	if mountFns[mountType] != nil {
		_, _, unmountFn, err := mountFns[mountType](fdst, mountPoint)
		liveMountsMutex.Lock()

		liveMounts[mountPoint] = MountInfo{
			unmountFn:  unmountFn,
			MountedOn:  time.Now(),
			Fs:         fdst.Name(),
			MountPoint: mountPoint,
		}
		liveMountsMutex.Unlock()
		if err != nil {
			log.Printf("mount FAILED: %v", err)
			return nil, err
		}
		fs.Debugf(nil, "Mount for %s created at %s using %s", fdst.String(), mountPoint, mountType)
		return nil, nil
	}
	return nil, errors.New("Mount Option specified is not registered, or is invalid")
}

func init() {
	rc.Add(rc.Call{
		Path:         "mount/unmount",
		AuthRequired: true,
		Fn:           unMountRc,
		Title:        "Unmount selected active mount",
		Help: `
rclone allows Linux, FreeBSD, macOS and Windows to
mount any of Rclone's cloud storage systems as a file system with
FUSE.

This takes the following parameters

- mountPoint: valid path on the local machine where the mount was created (required)

Eg

    rclone rc mount/unmount mountPoint=/home/<user>/mountPoint
`,
	})
}

// unMountRc allows the umount command to be run from rc
func unMountRc(_ context.Context, in rc.Params) (out rc.Params, err error) {
	mountPoint, err := in.GetString("mountPoint")
	if err != nil {
		return nil, err
	}
	err = performUnMount(mountPoint)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// performUnMount unmounts the specified mountPoint
func performUnMount(mountPoint string) (err error) {
	liveMountsMutex.Lock()
	defer liveMountsMutex.Unlock()
	mountInfo, ok := liveMounts[mountPoint]

	if ok {
		err := mountInfo.unmountFn()
		if err != nil {
			return err
		}
		delete(liveMounts, mountPoint)
	} else {
		return errors.New("mount not found")
	}
	return nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "mount/types",
		AuthRequired: true,
		Fn:           mountTypesRc,
		Title:        "Show all possible mount types",
		Help: `This shows all possible mount types and returns them as a list.

This takes no parameters and returns

- mountTypes: list of mount types

The mount types are strings like "mount", "mount2", "cmount" and can
be passed to mount/mount as the mountType parameter.

Eg

    rclone rc mount/types
`,
	})
}

// mountTypesRc returns a list of available mount types.
func mountTypesRc(_ context.Context, in rc.Params) (out rc.Params, err error) {
	var mountTypes = []string{}
	mountFnsMutex.Lock()
	for mountType := range mountFns {
		mountTypes = append(mountTypes, mountType)
	}
	mountFnsMutex.Unlock()
	sort.Strings(mountTypes)
	return rc.Params{
		"mountTypes": mountTypes,
	}, nil
}

func init() {
	rc.Add(rc.Call{
		Path:         "mount/listmounts",
		AuthRequired: true,
		Fn:           listMountsRc,
		Title:        "Show current mount points",
		Help: `This shows currently mounted points, which can be used for performing an unmount

This takes no parameters and returns

- mountPoints: list of current mount points

Eg

    rclone rc mount/listmounts
`,
	})
}

// listMountsRc returns a list of current mounts
func listMountsRc(_ context.Context, in rc.Params) (out rc.Params, err error) {
	var mountTypes = []MountInfo{}
	liveMountsMutex.Lock()
	for _, a := range liveMounts {
		mountTypes = append(mountTypes, a)
	}
	liveMountsMutex.Unlock()
	return rc.Params{
		"mountPoints": mountTypes,
	}, nil
}
