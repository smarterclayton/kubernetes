/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubelet

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	_ "github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/tools"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
	"github.com/fsouza/go-dockerclient"
	"github.com/golang/glog"
	"github.com/google/cadvisor/client"
	"github.com/google/cadvisor/info"
)

const defaultChanSize = 1024

// DockerContainerData is the structured representation of the JSON object returned by Docker inspect
type DockerContainerData struct {
	state struct {
		Running bool
	}
}

// DockerInterface is an abstract interface for testability.  It abstracts the interface of docker.Client.
type DockerInterface interface {
	ListContainers(options docker.ListContainersOptions) ([]docker.APIContainers, error)
	InspectContainer(id string) (*docker.Container, error)
	CreateContainer(docker.CreateContainerOptions) (*docker.Container, error)
	StartContainer(id string, hostConfig *docker.HostConfig) error
	StopContainer(id string, timeout uint) error
	PullImage(opts docker.PullImageOptions, auth docker.AuthConfiguration) error
}

// DockerID is an ID of docker container. It is a type to make it clear when we're working with docker container Ids
type DockerID string

// DockerPuller is an abstract interface for testability.  It abstracts image pull operations.
type DockerPuller interface {
	Pull(image string) error
}

// CadvisorInterface is an abstract interface for testability.  It abstracts the interface of "github.com/google/cadvisor/client".Client.
type CadvisorInterface interface {
	ContainerInfo(name string) (*info.ContainerInfo, error)
	MachineInfo() (*info.MachineInfo, error)
}

// DockerContainers is a map of containers
type DockerContainers map[DockerID]*docker.APIContainers

func (c DockerContainers) FindPodContainer(podFullName, containerName string) (*docker.APIContainers, bool) {
	for _, dockerContainer := range c {
		dockerPodFullName, dockerContainerName := parseDockerName(dockerContainer.Names[0])
		if dockerPodFullName == podFullName && dockerContainerName == containerName {
			return dockerContainer, true
		}
	}
	return nil, false
}
func (c DockerContainers) FindContainersByPodFullName(podFullName string) map[string]*docker.APIContainers {
	containers := make(map[string]*docker.APIContainers)

	for _, dockerContainer := range c {
		dockerPodFullName, dockerContainerName := parseDockerName(dockerContainer.Names[0])
		if dockerPodFullName == podFullName {
			containers[dockerContainerName] = dockerContainer
		}
	}
	return containers
}

// New creates a new Kubelet.
func New() *Kubelet {
	return &Kubelet{}
}

// Kubelet is the main kubelet implementation.
type Kubelet struct {
	Hostname string

	// FileCheckFrequency time.Duration
	// HTTPCheckFrequency time.Duration

	EtcdClient     tools.EtcdClient
	DockerClient   DockerInterface
	CadvisorClient CadvisorInterface
	HealthChecker  HealthChecker

	pullLock     sync.Mutex
	DockerPuller DockerPuller
}

// Run starts reacting to config updates
func (kl *Kubelet) Run(updates <-chan PodUpdate, maxWait time.Duration) {
	if kl.CadvisorClient == nil {
		client, err := cadvisor.NewClient("http://127.0.0.1:5000")
		if err != nil {
			glog.Errorf("Error on creating cadvisor client: %v", err)
		}
		kl.CadvisorClient = client
	}
	if kl.DockerPuller == nil {
		kl.DockerPuller = kl.MakeDockerPuller()
	}
	if kl.HealthChecker == nil {
		kl.HealthChecker = MakeHealthChecker()
	}

	go util.Forever(func() { kl.syncLoop(updates, kl, maxWait) }, 0)
}

// SyncHandler is an interface implemented by Kubelet, for testability
type SyncHandler interface {
	SyncPods([]Pod) error
}

// LogEvent logs an event to the etcd backend.
func (kl *Kubelet) LogEvent(event *api.Event) error {
	if kl.EtcdClient == nil {
		return fmt.Errorf("no etcd client connection")
	}
	event.Timestamp = time.Now().Unix()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	var response *etcd.Response
	response, err = kl.EtcdClient.AddChild(fmt.Sprintf("/events/%s", event.Container.Name), string(data), 60*60*48 /* 2 days */)
	// TODO(bburns) : examine response here.
	if err != nil {
		glog.Errorf("Error writing event: %s\n", err)
		if response != nil {
			glog.Infof("Response was: %v\n", *response)
		}
	}
	return err
}

// getDockerContainers returns a map of docker containers that we manage. The map key is the docker container ID
func (kl *Kubelet) getDockerContainers() (DockerContainers, error) {
	result := make(DockerContainers)
	containerList, err := kl.DockerClient.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		return nil, err
	}
	for i := range containerList {
		container := &containerList[i]
		// Skip containers that we didn't create to allow users to manually
		// spin up their own containers if they want.
		if !strings.HasPrefix(container.Names[0], "/"+containerNamePrefix+"--") {
			continue
		}
		result[DockerID(container.ID)] = container
	}
	return result, nil
}

// MakeDockerPuller creates a new instance of the default implementation of DockerPuller.
func (kl *Kubelet) MakeDockerPuller() DockerPuller {
	return dockerPuller{
		client: kl.DockerClient,
	}
}

// dockerPuller is the default implementation of DockerPuller.
type dockerPuller struct {
	client DockerInterface
}

func (p dockerPuller) Pull(image string) error {
	image, tag := parseImageName(image)
	opts := docker.PullImageOptions{
		Repository: image,
		Tag:        tag,
	}
	return p.client.PullImage(opts, docker.AuthConfiguration{})
}

// Converts "-" to "_-_" and "_" to "___" so that we can use "--" to meaningfully separate parts of a docker name.
func escapeDash(in string) (out string) {
	out = strings.Replace(in, "_", "___", -1)
	out = strings.Replace(out, "-", "_-_", -1)
	return
}

// Reverses the transformation of escapeDash.
func unescapeDash(in string) (out string) {
	out = strings.Replace(in, "_-_", "-", -1)
	out = strings.Replace(out, "___", "_", -1)
	return
}

const containerNamePrefix = "k8s"

// Creates a name which can be reversed to identify both pod id and container name.
func buildDockerName(pod *Pod, container *api.Container) string {
	return fmt.Sprintf("%s--%s--%s--%08x", containerNamePrefix, escapeDash(container.Name), escapeDash(GetPodFullName(pod)), rand.Uint32())
}

// Upacks a container name, returning the manifest id and container name we would have used to
// construct the docker name. If the docker name isn't one we created, we may return empty strings.
func parseDockerName(name string) (podFullName, containerName string) {
	// For some reason docker appears to be appending '/' to names.
	// If its there, strip it.
	if name[0] == '/' {
		name = name[1:]
	}
	parts := strings.Split(name, "--")
	if len(parts) == 0 || parts[0] != containerNamePrefix {
		return
	}
	if len(parts) > 1 {
		containerName = unescapeDash(parts[1])
	}
	if len(parts) > 2 {
		podFullName = unescapeDash(parts[2])
	}
	return
}

func makeEnvironmentVariables(container *api.Container) []string {
	var result []string
	for _, value := range container.Env {
		result = append(result, fmt.Sprintf("%s=%s", value.Name, value.Value))
	}
	return result
}

func makeVolumesAndBinds(pod *Pod, container *api.Container) (map[string]struct{}, []string) {
	volumes := map[string]struct{}{}
	binds := []string{}
	for _, volume := range container.VolumeMounts {
		var basePath string
		if volume.MountType == "HOST" {
			// Host volumes are not Docker volumes and are directly mounted from the host.
			basePath = fmt.Sprintf("%s:%s", volume.MountPath, volume.MountPath)
		} else {
			volumes[volume.MountPath] = struct{}{}
			basePath = fmt.Sprintf("/exports/%s/%s:%s", GetPodFullName(pod), volume.Name, volume.MountPath)
		}
		if volume.ReadOnly {
			basePath += ":ro"
		}
		binds = append(binds, basePath)
	}
	return volumes, binds
}

func makePortsAndBindings(container *api.Container) (map[docker.Port]struct{}, map[docker.Port][]docker.PortBinding) {
	exposedPorts := map[docker.Port]struct{}{}
	portBindings := map[docker.Port][]docker.PortBinding{}
	for _, port := range container.Ports {
		interiorPort := port.ContainerPort
		exteriorPort := port.HostPort
		// Some of this port stuff is under-documented voodoo.
		// See http://stackoverflow.com/questions/20428302/binding-a-port-to-a-host-interface-using-the-rest-api
		var protocol string
		switch strings.ToUpper(port.Protocol) {
		case "UDP":
			protocol = "/udp"
		case "TCP":
			protocol = "/tcp"
		default:
			glog.Infof("Unknown protocol '%s': defaulting to TCP", port.Protocol)
			protocol = "/tcp"
		}
		dockerPort := docker.Port(strconv.Itoa(interiorPort) + protocol)
		exposedPorts[dockerPort] = struct{}{}
		portBindings[dockerPort] = []docker.PortBinding{
			{
				HostPort: strconv.Itoa(exteriorPort),
				HostIp:   port.HostIP,
			},
		}
	}
	return exposedPorts, portBindings
}

// Parses image name including an tag and returns image name and tag
// TODO: Future Docker versions can parse the tag on daemon side, see:
// https://github.com/dotcloud/docker/issues/6876
// So this can be deprecated at some point.
func parseImageName(image string) (string, string) {
	tag := ""
	parts := strings.SplitN(image, "/", 2)
	repo := ""
	if len(parts) == 2 {
		repo = parts[0]
		image = parts[1]
	}
	parts = strings.SplitN(image, ":", 2)
	if len(parts) == 2 {
		image = parts[0]
		tag = parts[1]
	}
	if repo != "" {
		image = fmt.Sprintf("%s/%s", repo, image)
	}
	return image, tag
}

// Run a single container from a manifest. Returns the docker container ID
func (kl *Kubelet) runContainer(pod *Pod, container *api.Container, netMode string) (id DockerID, err error) {
	envVariables := makeEnvironmentVariables(container)
	volumes, binds := makeVolumesAndBinds(pod, container)
	exposedPorts, portBindings := makePortsAndBindings(container)

	opts := docker.CreateContainerOptions{
		Name: buildDockerName(pod, container),
		Config: &docker.Config{
			Cmd:          container.Command,
			Env:          envVariables,
			ExposedPorts: exposedPorts,
			Hostname:     container.Name,
			Image:        container.Image,
			Memory:       int64(container.Memory),
			Volumes:      volumes,
			WorkingDir:   container.WorkingDir,
		},
	}
	dockerContainer, err := kl.DockerClient.CreateContainer(opts)
	if err != nil {
		return "", err
	}
	err = kl.DockerClient.StartContainer(dockerContainer.ID, &docker.HostConfig{
		PortBindings: portBindings,
		Binds:        binds,
		NetworkMode:  netMode,
	})
	return DockerID(dockerContainer.ID), err
}

// Kill a docker container
func (kl *Kubelet) killContainer(dockerContainer docker.APIContainers) error {
	err := kl.DockerClient.StopContainer(dockerContainer.ID, 10)
	podFullName, containerName := parseDockerName(dockerContainer.Names[0])
	kl.LogEvent(&api.Event{
		Event: "STOP",
		Manifest: &api.ContainerManifest{
			//TODO: This should be reported using either the apiserver schema or the kubelet schema
			ID: podFullName,
		},
		Container: &api.Container{
			Name: containerName,
		},
	})

	return err
}

const networkContainerName = "net"

// Create a network container for a pod. Returns the docker container ID of the newly created container.
func (kl *Kubelet) createNetworkContainer(pod *Pod) (DockerID, error) {
	var ports []api.Port
	// Docker only exports ports from the network container.  Let's
	// collect all of the relevant ports and export them.
	for _, container := range pod.Manifest.Containers {
		ports = append(ports, container.Ports...)
	}
	container := &api.Container{
		Name:    networkContainerName,
		Image:   "busybox",
		Command: []string{"sh", "-c", "rm -f nap && mkfifo nap && exec cat nap"},
		Ports:   ports,
	}
	kl.DockerPuller.Pull("busybox")
	return kl.runContainer(pod, container, "")
}

func (kl *Kubelet) syncPod(pod *Pod, keepChannel chan<- DockerID) error {
	dockerContainers, err := kl.getDockerContainers()
	if err != nil {
		return err
	}

	podFullName := GetPodFullName(pod)

	var netID DockerID
	if networkDockerContainer, found := dockerContainers.FindPodContainer(podFullName, networkContainerName); found {
		netID = DockerID(networkDockerContainer.ID)
	} else {
		glog.Infof("Network container doesn't exist, creating")
		netID, err = kl.createNetworkContainer(pod)
		if err != nil {
			glog.Errorf("Failed to introspect network container. (%v)  Skipping pod %s", err, podFullName)
			return err
		}
	}
	keepChannel <- netID

	for _, container := range pod.Manifest.Containers {
		if dockerContainer, found := dockerContainers.FindPodContainer(podFullName, container.Name); found {
			glog.Infof("pod %s container %s exists as %v", podFullName, container.Name, dockerContainer.ID)
			glog.V(1).Infof("pod %s container %s exists as %v", podFullName, container.Name, dockerContainer.ID)

			// TODO: This should probably be separated out into a separate goroutine.
			healthy, err := kl.healthy(container, dockerContainer)
			if err != nil {
				glog.V(1).Infof("health check errored: %v", err)
				continue
			}
			if healthy == CheckHealthy {
				keepChannel <- DockerID(dockerContainer.ID)
				continue
			}
			glog.V(1).Infof("pod %s container %s is unhealthy.", podFullName, container.Name)
			if err := kl.killContainer(*dockerContainer); err != nil {
				glog.V(1).Infof("Failed to kill container %s: %v", dockerContainer.ID, err)
				continue
			}
		}

		glog.Infof("%+v doesn't exist, creating", container)
		kl.DockerPuller.Pull(container.Image)
		if err != nil {
			glog.Errorf("Failed to create container: %v skipping pod %s container %s.", err, podFullName, container.Name)
			continue
		}
		containerID, err := kl.runContainer(pod, &container, "container:"+string(netID))
		if err != nil {
			// TODO(bburns) : Perhaps blacklist a container after N failures?
			glog.Errorf("Error running pod %s container %s: %v", podFullName, container.Name, err)
			continue
		}
		keepChannel <- containerID
	}
	return nil
}

type empty struct{}

// SyncPods synchronizes the configured list of pods (desired state) with the host current state.
func (kl *Kubelet) SyncPods(pods []Pod) error {
	glog.Infof("Desired: %+v", pods)
	var err error
	dockerIdsToKeep := map[DockerID]empty{}
	keepChannel := make(chan DockerID, defaultChanSize)
	waitGroup := sync.WaitGroup{}

	// Check for any containers that need starting
	for i := range pods {
		waitGroup.Add(1)
		go func(index int) {
			defer util.HandleCrash()
			defer waitGroup.Done()
			// necessary to dereference by index here b/c otherwise the shared value
			// in the for each is re-used.
			err := kl.syncPod(&pods[index], keepChannel)
			if err != nil {
				glog.Errorf("Error syncing manifest: %v skipping.", err)
			}
		}(i)
	}
	ch := make(chan bool)
	go func() {
		for id := range keepChannel {
			dockerIdsToKeep[id] = empty{}
		}
		ch <- true
	}()
	if len(pods) > 0 {
		waitGroup.Wait()
	}
	close(keepChannel)
	<-ch

	// Kill any containers we don't need
	existingContainers, err := kl.getDockerContainers()
	if err != nil {
		glog.Errorf("Error listing containers: %v", err)
		return err
	}
	for id, container := range existingContainers {
		if _, ok := dockerIdsToKeep[id]; !ok {
			glog.Infof("Killing: %s", id)
			err = kl.killContainer(*container)
			if err != nil {
				glog.Errorf("Error killing container: %v", err)
			}
		}
	}
	return err
}

// Check that all Port.HostPort values are unique across all manifests.
func checkHostPortConflicts(allManifests []api.ContainerManifest, newManifest *api.ContainerManifest) []error {
	allErrs := []error{}

	allPorts := map[int]bool{}
	extract := func(p *api.Port) int { return p.HostPort }
	for i := range allManifests {
		manifest := &allManifests[i]
		errs := api.AccumulateUniquePorts(manifest.Containers, allPorts, extract)
		if len(errs) != 0 {
			allErrs = append(allErrs, errs...)
		}
	}
	if errs := api.AccumulateUniquePorts(newManifest.Containers, allPorts, extract); len(errs) != 0 {
		allErrs = append(allErrs, errs...)
	}
	return allErrs
}

// syncLoop is the main loop for processing changes. It watches for changes from
// four channels (file, etcd, server, and http) and creates a union of them. For
// any new change seen, will run a sync against desired state and running state. If
// no changes are seen to the configuration, will synchronize the last known desired
// state every sync_frequency seconds.
// Never returns.
func (kl *Kubelet) syncLoop(updates <-chan PodUpdate, handler SyncHandler, maxWait time.Duration) {
	var last []Pod
	for {
		select {
		case u := <-updates:
			switch u.Op {
			case SET:
				glog.Infof("Got configuration: %+v", u.Pods)
				last = u.Pods
			default:
				panic("syncLoop does not support incremental changes")
			}
		case <-time.After(maxWait):
		}

		// TODO: move to PodConfig
		/*
			allIds := util.StringSet{}
			for i := range last {
				allErrs := []error{}

				m := &srcManifests[i]
				if allIds.Has(m.ID) {
					allErrs = append(allErrs, api.ValidationError{api.ErrTypeDuplicate, "ContainerManifest.ID", m.ID})
				} else {
					allIds.Insert(m.ID)
				}
				if errs := api.ValidateManifest(m); len(errs) != 0 {
					allErrs = append(allErrs, errs...)
				}
				// Check for host-wide HostPort conflicts.
				if errs := checkHostPortConflicts(allManifests, m); len(errs) != 0 {
					allErrs = append(allErrs, errs...)
				}
				if len(allErrs) > 0 {
					glog.Warningf("Manifest from %s failed validation, ignoring: %v", src, allErrs)
				}
			}*/

		err := handler.SyncPods(last)
		if err != nil {
			glog.Errorf("Couldn't sync containers : %v", err)
		}
	}
}

// GetPodInfo returns docker info for all containers in the pod/manifest.
func (kl *Kubelet) GetPodInfo(podFullName string) (api.PodInfo, error) {
	info := api.PodInfo{}

	dockerContainers, err := kl.getDockerContainers()
	if err != nil {
		return nil, err
	}

	containersForPod := dockerContainers.FindContainersByPodFullName(podFullName)
	for name, container := range containersForPod {
		inspectResult, err := kl.DockerClient.InspectContainer(container.ID)
		if err != nil {
			return nil, err
		}
		if inspectResult == nil {
			// Why did we not get an error?
			inspectResult = &docker.Container{}
		}
		info[name] = *inspectResult
	}
	return info, nil
}

// This method takes a container's absolute path and returns the stats for the
// container.  The container's absolute path refers to its hierarchy in the
// cgroup file system. e.g. The root container, which represents the whole
// machine, has path "/"; all docker containers have path "/docker/<docker id>"
func (kl *Kubelet) statsFromContainerPath(containerPath string) (*api.ContainerStats, error) {
	info, err := kl.CadvisorClient.ContainerInfo(containerPath)

	if err != nil {
		return nil, err
	}
	// When the stats data for the container is not available yet.
	if info.StatsPercentiles == nil {
		return nil, nil
	}

	ret := new(api.ContainerStats)
	ret.MaxMemoryUsage = info.StatsPercentiles.MaxMemoryUsage
	if len(info.StatsPercentiles.CpuUsagePercentiles) > 0 {
		percentiles := make([]api.Percentile, len(info.StatsPercentiles.CpuUsagePercentiles))
		for i, p := range info.StatsPercentiles.CpuUsagePercentiles {
			percentiles[i].Percentage = p.Percentage
			percentiles[i].Value = p.Value
		}
		ret.CpuUsagePercentiles = percentiles
	}
	if len(info.StatsPercentiles.MemoryUsagePercentiles) > 0 {
		percentiles := make([]api.Percentile, len(info.StatsPercentiles.MemoryUsagePercentiles))
		for i, p := range info.StatsPercentiles.MemoryUsagePercentiles {
			percentiles[i].Percentage = p.Percentage
			percentiles[i].Value = p.Value
		}
		ret.MemoryUsagePercentiles = percentiles
	}
	return ret, nil
}

// GetContainerStats returns stats (from Cadvisor) for a container.
func (kl *Kubelet) GetContainerStats(podFullName, containerName string) (*api.ContainerStats, error) {
	if kl.CadvisorClient == nil {
		return nil, nil
	}
	dockerContainers, err := kl.getDockerContainers()
	if err != nil {
		return nil, err
	}
	dockerContainer, found := dockerContainers.FindPodContainer(podFullName, containerName)
	if !found {
		return nil, errors.New("couldn't find container")
	}
	return kl.statsFromContainerPath(fmt.Sprintf("/docker/%s", dockerContainer.ID))
}

// GetMachineStats returns stats (from Cadvisor) of current machine.
func (kl *Kubelet) GetMachineStats() (*api.ContainerStats, error) {
	return kl.statsFromContainerPath("/")
}

func (kl *Kubelet) healthy(container api.Container, dockerContainer *docker.APIContainers) (HealthCheckStatus, error) {
	// Give the container 60 seconds to start up.
	if container.LivenessProbe == nil {
		return CheckHealthy, nil
	}
	if time.Now().Unix()-dockerContainer.Created < container.LivenessProbe.InitialDelaySeconds {
		return CheckHealthy, nil
	}
	if kl.HealthChecker == nil {
		return CheckHealthy, nil
	}
	return kl.HealthChecker.HealthCheck(container)
}
