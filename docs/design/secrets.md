# Secret Distribution
## Abstract
A proposal for the distribution of secrets (passwords, keys, etc) to containers inside Kubernetes using a custom volume type.

## Motivation

Secrets are needed in containers to access internal resources like the Kubernetes master or external resources such as git repositories, databases, etc. 

A goal of this design is to eliminate or minimize the modifications to containers in order to access secrets. Secrets should be placed where the container expects them to be.

## Use Cases

1. Access the Kubernetes master using a custom .dockercfg
2. Access DockerHub using credentials from .dockercfg
3. Access a GitHub repository using SSH keys
4. Access a super secret service using a set of keys


## Constraints and Assumptions
* This design does not prescribe a method for storing/transmitting secrets
* Encryption and node security are orthogonal concerns
* It is assumed that node and master are secure and that compromising their security could also compromise secrets.

## Proposed Design

### Overview
This design proposes mounting secrets with a new volume type. The secrets volume
will be backed by a volume plugin that does the actual work of fetching
the secret and placing it on the filesystem to be mounted in the container. It
should be possible to mount single files as part of a secret. Secrets may
consist of multiple files. For example, an SSH key pair. In order to remove the
burden from the end user in specifying every file that a secret consists of, it should be possible to mount all files provided by a secret with a single ```VolumeMount``` entry in the container specification.

### Secret Volume Source

A new Secret type of volume source will be added to the ```VolumeSource``` struct in the API:

```go
type VolumeSource struct {
     ... 
     Secret *Secret `json:"secret"`
}

type Secret struct {
     // Ref is a reference to the actual secret
     Ref string 
     
     // Files is a set of descriptors of where and how the secret files
     // should be placed (optional). If not specified, the secret itself
     // will determine where its files should go
	 Files []SecretFile
}

type SecretFile struct {
     // Name of the file within the secret. For example, in the case
     // of SSH keys, you would have a 'private' and a 'public' key
	 Name string
	 
	 // Path of the secret within the container
	 Path string
	 
	 // Owner id of the file owner
	 OwnerUID int
	 
	 // Group id of the file group
	 GroupUID int
	 
	 // File Mode
	 Mode os.FileMode
	 
	 // MCSLabel for SELinux
	 MCSLabel string
}

```

### Secret Volume Plugin
The secret volume plugin would implement the actual retrieval and laying down of
secrets on the Kubelet's file system. Secrets may be stored in a special secret
registry, as Docker volumes, LDAP etc. See [Issue #2030](https://github.com/GoogleCloudPlatform/kubernetes/issues/2030) for options. The implementation of this plugin is outside of the scope of this proposal. A default Etcd-based version could be provided out of the box, but different solutions may be appropriate based on the use case for Kubernetes.

### Changes to Support Secret Volumes
Because secrets require mounting multiple files, the way bind mounts are generated from pods would need to be modified to allow the pod spec to say "I want the default mountpoints" instead of a specific MountPath. This can be done by modifying the VolumeMount struct to include a boolean:

```go
type VolumeMount struct {
	Name string
	ReadOnly bool
	MountPath string // Change from required to optional
	
	// New boolean to let the Kubelet know we want the default path
	// from the volume
	DefaultPath bool 
} 
```

and the volume Builder interface to return a bind string instead of just a path:
```go
type Interface interface {
	GetPath() string
	
	// Returns a <container_path>:<host_path> string
	GetBind() string
}
```

### Security Concerns
This proposal requires that secrets be placed on a filesystem location that is accessible to the container requiring the secret. This makes it vulnerable to attacks on the container and the node, especially if the secret is placed in plain text. MCS labels can mitigate some of this risk. However if there is a particular use case for a very sensitive secret, the secret itself could be stored encrypted and placed in encrypted form in the file system for the container. The container would have to know how to decrypt it.
