# Secret Distribution

## Abstract

A proposal for the distribution of secrets (passwords, keys, etc) to containers inside Kubernetes
using a custom volume type.

## Motivation

Secrets are needed in containers to access internal resources like the Kubernetes master or
external resources such as git repositories, databases, etc. 

A goal of this design is to eliminate or minimize the modifications to containers in order to
access secrets. Secrets should be placed where the container expects them to be.

## Constraints and Assumptions

*  This design does not prescribe a method for storing secrets; storage of secrets should be
   pluggable to accomodate different use-cases
*  Encryption of secret data and node security are orthogonal concerns
*  It is assumed that node and master are secure and that compromising their security could also
   compromise secrets:
   *  If a node is compromised, only the secrets for the containers scheduled on it should be
      exposed
   *  If the master is compromised, all secrets in the cluster may be exposed

## Use Cases

1.  As a user, I want to store secret artifacts for my applications and consume them securely in
    containers, so that I can keep the configuration for my applications separate from the images
    that use them:
    1.  As a cluster operator, I want to allow a pod to access the Kubernetes master using a custom
        `.kubeconfig` file, so that I can securely reach the master
    2.  As a cluster operator, I want to allow a pod to access a Docker registry using credentials
        from a `.dockercfg` file, so that containers can push images
    3.  As a cluster operator, I want to allow a pod to access a git repository using SSH keys,
        so that I can push and fetch to and from the repository
2.  As a user, I want to allow containers to consume supplemental information about services such
    as username and password which should be kept secret, so that I can share secrets about a
    service amongst the containers in my application securely

### Use-Case: Configuration artifacts

Many configuration files contain secrets intermixed with other configuration information.  For
example, a user's application may contain a properties file than contains database credentials,
SaaS API tokens, etc.  Users should be able to consume configuration artifacts in their containers and
be able to control the path on the container's filesystems where the artifact will be presented.

### Use-Case: Metadata about services

Most pieces of information about how to use a service are secrets.  For example, a service that
provides a MySQL database needs to provide the username, password, and database name to consumers
so that they can authenticate and use the correct database. Containers in pods consuming the MySQL
service would also consume the secrets associated with the MySQL service.

## Deferral: Consuming secrets as environment variables

Some images will expect to receive configuration items as environment variables instead of files.
We should consider what the best way to allow this is; there are a few different options:

1.  Force the user to adapt files into environment variables.  Users can store secrets that need to
    be presented as environment variables in a format that is easy to consume from a shell:

        $ cat /etc/secrets/my-secret.txt
        export MY_SECRET_ENV=MY_SECRET_VALUE

    The user could `source` the file at `/etc/secrets/my-secret` prior to executing the command for
    the image either inline in the command or in an init script, 

2.  Give secrets an attribute that allows users to express the intent that the platform should
    generate the above syntax in the file used to present a secret.  The user could consume these
    files in the same manner as the above option.

3.  Give secrets attributes that allow the user to express that the secret should be presented to
    the container as an environment variable.  The container's environment would contain the
    desired values and the software in the container could use them without accomodation the
    command or setup script.

For our initial work, we will treat all secrets as files to narrow the problem space.  There will
be a future proposal that handles exposing secrets as environment variables.

## Flow analysis of secret data

There are two fundamentally different use-cases for access to secrets:

1.  CRUD operations on secrets by their owners
2.  Read-only access to the secrets needed for a particular node by the kubelet

### Use-Case: CRUD operations by owners

In use cases for CRUD operations, the user experience for secrets should be no different than for
other API resources.

TODO: document possible relation with Service Accounts

### Use-Case: Kubelet read of secrets for node

The use-case where the kubelet reads secrets has several additional requirements:

1.  Kubelets should only receive secret data which is required by pods scheduled onto the kubelet's
    node
2.  Kubelets should have read-only access to secret data
3.  Secret data should not be transmitted over the wire insecurely

TODO: describe further; what are the interface / watch implications?

## Community work:

There are several proposals / upstream patches that we should consider:

1.  [Docker vault proposal](https://github.com/docker/docker/issues/10310)
2.  [Specification for image/container standardization based on volumes](https://github.com/docker/docker/issues/9277)
3.  [Kubernetes service account proposal](https://github.com/GoogleCloudPlatform/kubernetes/pull/2297)
4.  [Secret proposal for docker](https://github.com/docker/docker/pull/6075)
5.  [Continuating of secret proposal for docker](https://github.com/docker/docker/pull/6697)

## Analysis TODOs

Collecting remaining TODOs here:

1.  Determine how/whether secrets interact with service accounts; are namespaces enough for the
    security scope?
2.  Can/should my security context affect what secrets I have access to?
3.  Should there be a way to express that a container which consumes a secret should be restarted
    when the secret changes?

## Proposed Design

### Overview

This design proposes a new `Secret` resource which is mounted into containers with a new volume
type. The secrets volume will be backed by a volume plugin that does the actual work of fetching
the secret and placing it on the filesystem to be mounted in the container. It should be possible
to mount single files as part of a secret. Secrets may consist of multiple files. For example, an
SSH key pair. In order to remove the burden from the end user in specifying every file that a
secret consists of, it should be possible to mount all files provided by a secret with a single
```VolumeMount``` entry in the container specification.

### Secret API Resource

A new resource for secrets will be added to the API:

```go
type Secret struct {
    TypeMeta
    ObjectMeta

    Data map[string][]byte
}
```

A new REST API and registry interface will be added to accompany the `Secret` resource.  The
default implementation of the registry will store `Secret` information in etcd.  Future registry
implementations could store the `TypeMeta` and `ObjectMeta` fields in etcd and store the secret
data in another data store entirely, or store the whole object in another data store.

### Secret Volume Source

A new `SecretSource` type of volume source will be added to the ```VolumeSource``` struct in the
API:

```go
type VolumeSource struct {
     ... 
     SecretSource *SecretSource `json:"secret"`
}

type SecretSource struct {
     Target ObjectReference
     
     // Files is a set of adapters that determine where a secret's data should
     // be placed on the container's filesystem
	 Files []SecretFile
}

type SecretFile struct {
     // Name of the key of the data within the secret
	 Name string
	 
	 // Path of the secret file within the container
	 Path string

     // Security context of the secret
     SecurityContext SecurityContext
     
     // File Mode
     Mode os.FileMode     
}
```

### Secret Volume Plugin

The secret volume plugin would implement the actual retrieval and laying down of secrets on the
Kubelet's file system. Secrets may be stored in a special secret registry, as Docker volumes, LDAP
etc. See [Issue #2030](https://github.com/GoogleCloudPlatform/kubernetes/issues/2030) for options.
The implementation of this plugin is outside of the scope of this proposal. A default Etcd-based
version could be provided out of the box, but different solutions may be appropriate based on the
use case for Kubernetes.

### Changes to Support Secret Volumes

Because secrets require mounting multiple files, the way bind mounts are generated from pods would
need to be modified to allow the pod spec to say "I want the default mountpoints" instead of a
specific MountPath. This can be done by modifying the VolumeMount struct to include a boolean:

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

This proposal requires that secrets be placed on a filesystem location that is accessible to the
container requiring the secret. This makes it vulnerable to attacks on the container and the node,
especially if the secret is placed in plain text. MCS labels can mitigate some of this risk.
If there is a particular use case for a very sensitive secret, the secret itself could be
stored encrypted and placed in encrypted form in the file system for the container. The container
would have to know how to decrypt it and would receive the decryption key via another channel.
