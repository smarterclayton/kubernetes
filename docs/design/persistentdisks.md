# Persistent Disks

## Abstract

This document defines the next generation of persistent storage that builds upon the existing GCEPersistentDisk implementation.
The architecture and design accommodates many types of infrastructure, and plugins can be built for custom environments or future functionality.

    *All Names are TBD - PersistentDisk is a placeholder name
 
## Phased Approach

Every effort will be made to break this task into discrete pieces of functionality which can be committed as individual pulls/merges.

![alt text](http://media-cache-ec0.pinimg.com/236x/da/a1/7e/daa17e92ba3a1b04e203135043db580b.jpg "How do you eat an elephant?")

 
## Scheduler enhancements

Improve generic scheduler and predicates to enforce:

1. GCEPersistentDisks can be mounted once as read/write and many times as read-only.
2. AWSPersistentDisks can be mounted once as read/write.
2.1 AWS Encrypted disks can only be mounted to VMs that support EBS encryption 
3. NFSPersistentDisks can be mounted many times as read/write.
4. GCE and AWS disks must exist in the same availability zone as the VM.
5. Disks must share the same namespace as a pod.

## Kubelet enhancements

* Modify kubelet.mountExternalVolumes(*BoundPod) method.  Use BoundPod.Spec.Volumes.[Selector|DiskName] to find and mount volumes.
* Continue to use API credentials for cloud providers read via io.Reader, as implemented for GCE.

Editor's note:  Pods that require volumes should not start if that volume fails to mount.  This, to me, strongly implies linear flow where
kubelet first attaches a device then mounts a filesystem and then starts a pod.

On pod exit, unmount volumes.  Pod death = orphaned volume is still mounted. (This will need to be fixed, as pod death would then keep the device attached and scheduler wouldn't be able to move to next minion for pod create.)

Kubelet's control loop already reconciles pods according to state stored in etcd.  Include volume reconciliation in this loop.
Unmount volumes that have been orphaned.  (This conflicts with above.)

Unmounting a volume requires state change to PersistentDisk.Status

## Disk management

* Existing named disks are supported in GCE.
* New disks
    1 Must be created by the API and its ID stored
    2 Must be formatted with a filesystem after attaching.


## <a name=schema></a>Schema

Create pkg/volume/types.go

There are two options for the ReadOnly attribute.  See [ReadOnly attribute placement](#readonly)

* Refactor GCEPersistentDisk to PersistentDisk
    * Two reasons to create new top-level object
        1. Disks are 1:N.  Normalizing the schema makes sense for this reuse.
        2. Disks have identity. Disks outlive pods. Their IDs must be persistent for future re-mounting.  
* Separate Spec and Status for disks
    * DiskSpec contains PersistentDiskType (GCE, AWS, NFS).  Attributes are relevant according to type.

```go


struct PersistentDisk {

    TypeMeta
    ObjectMeta      //namespace will be required.  access to disks only allowed in same namespace for security
	
	// separating *PersistentDisk into Spec and Status like other API objects
	Spec    DiskSpec
	Status  DiskStatus
}

struct DiskSpec {


    // based on the type (GCE, AWS, NFS) some attributes are relevant and others are not.
    Type        PersistentDiskType

    // Unique name of the PD resource. Used to identify the disk in a cloud provider
    // Optional.  New volumes are created if an existing one is not specified
    PDName      string
    
	// Required: Filesystem type to mount.
	// Must be a filesystem type supported by the host operating system.
	// Ex. "ext4", "xfs", "ntfs"
    FSType      string
    
	// Optional: Partition on the disk to mount.
	// If omitted, kubelet will attempt to mount the device name.
	// Ex. For /dev/sda1, this field is "1", for /dev/sda, this field is 0 or empty.
    Partition   int
    
    // the requested disk size in gb
    Size        int

    
    // From mount(2)
    // Required: Server hosting NFS server
    // NFS: host name or IP address
    Server      string
    
    // Required: File system to be attached
    // NFS: path
    Source      string
    
    // Required: Filesystem type to mount.
    // Must be a filesystem type supported by the host operating system.
    // Ex. "ext4", "xfs", "ntfs"
    FSType      string
    
    // Required: Constants from sys/mount.h
    MountFlags  uint64
    
    // Options understood by the file system type provided by FSTYPE, see nfs(5) for details
    // nfs: hard,rsize=xxx,wsize=yyy,noac
    Options     string
}

struct DiskStatus {

    // PodCondition recently became PodPhase - see https://github.com/GoogleCloudPlatform/kubernetes/pull/2522
    Condition   DiskCondition
    
    // a disk can be mounted on many hosts, depending on type
    Mounts []Mount
}

struct Mount struct {
    Host
    HostIP
    MountedDate
    MountCondition
}


type PersistentDiskType string
type DiskCondition string

const (
    MountPending    DiskCondition = "Pending"
    Attached        DiskCondition = "Attached"
    Mounted         DiskCondition = "Mounted"
    MountFailed     DiskCondition = "Failed"
)

const (
    AWSPersistentDiskType PersistentDiskType  = "aws"
    GCEPersistentDiskType PersistentDiskType  = "gce"
    NFSPersistentDiskType PersistentDiskType  = "nfs"
)

type VolumeSource struct {
	HostDir *HostDir
	EmptyDir *EmptyDir 
	
	// Selector or use named disk?
	// changed from specific GCEPersistentDisk selectors that can find a PersistentDisk to mount
	Selector []map[string]
	// the named PersistentDisk to use
	DiskName string
	
    // Optional: Defaults to false (read/write). ReadOnly here will force
    // the ReadOnly setting in VolumeMounts.
	ReadOnly bool
}

```
