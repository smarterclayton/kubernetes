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
	"fmt"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/tools"
	"github.com/fsouza/go-dockerclient"
	"github.com/google/cadvisor/info"
	"github.com/stretchr/testify/mock"
)

// TODO: This doesn't reduce typing enough to make it worth the less readable errors. Remove.
func expectNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
}

func makeTestKubelet(t *testing.T) (*Kubelet, *tools.FakeEtcdClient, *FakeDockerClient) {
	fakeEtcdClient := tools.MakeFakeEtcdClient(t)
	fakeDocker := &FakeDockerClient{
		err: nil,
	}

	kubelet := New()
	kubelet.DockerClient = fakeDocker
	kubelet.DockerPuller = &FakeDockerPuller{}
	kubelet.EtcdClient = fakeEtcdClient
	return kubelet, fakeEtcdClient, fakeDocker
}

func verifyCalls(t *testing.T, fakeDocker *FakeDockerClient, calls []string) {
	verifyStringArrayEquals(t, fakeDocker.called, calls)
}

func verifyStringArrayEquals(t *testing.T, actual, expected []string) {
	invalid := len(actual) != len(expected)
	if !invalid {
		for ix, value := range actual {
			if expected[ix] != value {
				invalid = true
			}
		}
	}
	if invalid {
		t.Errorf("Expected: %#v, Actual: %#v", expected, actual)
	}
}

func verifyPackUnpack(t *testing.T, podNamespace, podName, containerName string) {
	name := buildDockerName(
		&Pod{Name: podName, Namespace: podNamespace},
		&api.Container{Name: containerName},
	)
	podFullName := fmt.Sprintf("%s.%s", podName, podNamespace)
	returnedPodFullName, returnedContainerName := parseDockerName(name)
	if podFullName != returnedPodFullName || containerName != returnedContainerName {
		t.Errorf("For (%s, %s), unpacked (%s, %s)", podFullName, containerName, returnedPodFullName, returnedContainerName)
	}
}

func TestContainerManifestNaming(t *testing.T) {
	verifyPackUnpack(t, "file", "manifest1234", "container5678")
	verifyPackUnpack(t, "file", "manifest--", "container__")
	verifyPackUnpack(t, "file", "--manifest", "__container")
	verifyPackUnpack(t, "", "m___anifest_", "container-_-")
	verifyPackUnpack(t, "other", "_m___anifest", "-_-container")
}

func TestGetContainerID(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	fakeDocker.containerList = []docker.APIContainers{
		{
			ID:    "foobar",
			Names: []string{"/k8s--foo--qux--1234"},
		},
		{
			ID:    "barbar",
			Names: []string{"/k8s--bar--qux--2565"},
		},
	}
	fakeDocker.container = &docker.Container{
		ID: "foobar",
	}

	dockerContainers, err := kubelet.getDockerContainers()
	if err != nil {
		t.Errorf("Expected no error, Got %#v", err)
	}
	if len(dockerContainers) != 2 {
		t.Errorf("Expected %#v, Got %#v", fakeDocker.containerList, dockerContainers)
	}
	verifyCalls(t, fakeDocker, []string{"list"})
	dockerContainer, found := dockerContainers.FindPodContainer("qux", "foo")
	if dockerContainer == nil || !found {
		t.Errorf("Failed to find container %#v", dockerContainer)
	}

	fakeDocker.clearCalls()
	dockerContainer, found = dockerContainers.FindPodContainer("foobar", "foo")
	verifyCalls(t, fakeDocker, []string{})
	if dockerContainer != nil || found {
		t.Errorf("Should not have found container %#v", dockerContainer)
	}
}

func TestKillContainerWithError(t *testing.T) {
	fakeDocker := &FakeDockerClient{
		err: fmt.Errorf("sample error"),
		containerList: []docker.APIContainers{
			{
				ID:    "1234",
				Names: []string{"/k8s--foo--qux--1234"},
			},
			{
				ID:    "5678",
				Names: []string{"/k8s--bar--qux--5678"},
			},
		},
	}
	kubelet, _, _ := makeTestKubelet(t)
	kubelet.DockerClient = fakeDocker
	err := kubelet.killContainer(fakeDocker.containerList[0])
	if err == nil {
		t.Errorf("Expected error, found nil")
	}
	verifyCalls(t, fakeDocker, []string{"stop"})
}

func TestKillContainer(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	fakeDocker.containerList = []docker.APIContainers{
		{
			ID:    "1234",
			Names: []string{"/k8s--foo--qux--1234"},
		},
		{
			ID:    "5678",
			Names: []string{"/k8s--bar--qux--5678"},
		},
	}
	fakeDocker.container = &docker.Container{
		ID: "foobar",
	}

	err := kubelet.killContainer(fakeDocker.containerList[0])
	if err != nil {
		t.Errorf("Expected no error, found %#v", err)
	}
	verifyCalls(t, fakeDocker, []string{"stop"})
}

func TestSyncPodsDoesNothing(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	fakeDocker.containerList = []docker.APIContainers{
		{
			// format is k8s--<container-id>--<pod-fullname>
			Names: []string{"/k8s--bar--foo.test"},
			ID:    "1234",
		},
		{
			// network container
			Names: []string{"/k8s--net--foo.test--"},
			ID:    "9876",
		},
	}
	fakeDocker.container = &docker.Container{
		ID: "1234",
	}
	err := kubelet.SyncPods([]Pod{
		{
			Name:      "foo",
			Namespace: "test",
			Manifest: api.ContainerManifest{
				ID: "foo",
				Containers: []api.Container{
					{Name: "bar"},
				},
			},
		},
	})
	expectNoError(t, err)
	verifyCalls(t, fakeDocker, []string{"list", "list"})
}

func TestSyncPodsDeletes(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	fakeDocker.containerList = []docker.APIContainers{
		{
			// the k8s prefix is required for the kubelet to manage the container
			Names: []string{"/k8s--foo--bar.test"},
			ID:    "1234",
		},
		{
			// network container
			Names: []string{"/k8s--net--foo.test--"},
			ID:    "9876",
		},
		{
			Names: []string{"foo"},
			ID:    "4567",
		},
	}
	err := kubelet.SyncPods([]Pod{})
	expectNoError(t, err)
	verifyCalls(t, fakeDocker, []string{"list", "stop", "stop"})

	// A map interation is used to delete containers, so must not depend on
	// order here.
	expectedToStop := map[string]bool{
		"1234": true,
		"9876": true,
	}
	if len(fakeDocker.stopped) != 2 ||
		!expectedToStop[fakeDocker.stopped[0]] ||
		!expectedToStop[fakeDocker.stopped[1]] {
		t.Errorf("Wrong containers were stopped: %v", fakeDocker.stopped)
	}
}

type FalseHealthChecker struct{}

func (f *FalseHealthChecker) HealthCheck(container api.Container) (HealthCheckStatus, error) {
	return CheckUnhealthy, nil
}

func TestSyncPodsUnhealthy(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	kubelet.HealthChecker = &FalseHealthChecker{}
	fakeDocker.containerList = []docker.APIContainers{
		{
			// the k8s prefix is required for the kubelet to manage the container
			Names: []string{"/k8s--bar--foo.test"},
			ID:    "1234",
		},
		{
			// network container
			Names: []string{"/k8s--net--foo.test--"},
			ID:    "9876",
		},
	}
	err := kubelet.SyncPods([]Pod{
		{
			Name:      "foo",
			Namespace: "test",
			Manifest: api.ContainerManifest{
				ID: "foo",
				Containers: []api.Container{
					{Name: "bar",
						LivenessProbe: &api.LivenessProbe{
							// Always returns healthy == false
							Type: "false",
						},
					},
				},
			},
		}})
	expectNoError(t, err)
	verifyCalls(t, fakeDocker, []string{"list", "stop", "create", "start", "list"})

	// A map interation is used to delete containers, so must not depend on
	// order here.
	expectedToStop := map[string]bool{
		"1234": true,
	}
	if len(fakeDocker.stopped) != 1 ||
		!expectedToStop[fakeDocker.stopped[0]] {
		t.Errorf("Wrong containers were stopped: %v", fakeDocker.stopped)
	}
}

func TestEventWriting(t *testing.T) {
	kubelet, fakeEtcd, _ := makeTestKubelet(t)
	expectedEvent := api.Event{
		Event: "test",
		Container: &api.Container{
			Name: "foo",
		},
	}
	err := kubelet.LogEvent(&expectedEvent)
	expectNoError(t, err)
	if fakeEtcd.Ix != 1 {
		t.Errorf("Unexpected number of children added: %d, expected 1", fakeEtcd.Ix)
	}
	response, err := fakeEtcd.Get("/events/foo/1", false, false)
	expectNoError(t, err)
	var event api.Event
	err = json.Unmarshal([]byte(response.Node.Value), &event)
	expectNoError(t, err)
	if event.Event != expectedEvent.Event ||
		event.Container.Name != expectedEvent.Container.Name {
		t.Errorf("Event's don't match.  Expected: %#v Saw: %#v", expectedEvent, event)
	}
}

func TestEventWritingError(t *testing.T) {
	kubelet, fakeEtcd, _ := makeTestKubelet(t)
	fakeEtcd.Err = fmt.Errorf("test error")
	err := kubelet.LogEvent(&api.Event{
		Event: "test",
		Container: &api.Container{
			Name: "foo",
		},
	})
	if err == nil {
		t.Errorf("Unexpected non-error")
	}
}

func TestMakeEnvVariables(t *testing.T) {
	container := api.Container{
		Env: []api.EnvVar{
			{
				Name:  "foo",
				Value: "bar",
			},
			{
				Name:  "baz",
				Value: "blah",
			},
		},
	}
	vars := makeEnvironmentVariables(&container)
	if len(vars) != len(container.Env) {
		t.Errorf("Vars don't match.  Expected: %#v Found: %#v", container.Env, vars)
	}
	for ix, env := range container.Env {
		value := fmt.Sprintf("%s=%s", env.Name, env.Value)
		if value != vars[ix] {
			t.Errorf("Unexpected value: %s.  Expected: %s", vars[ix], value)
		}
	}
}

func TestMakeVolumesAndBinds(t *testing.T) {
	container := api.Container{
		VolumeMounts: []api.VolumeMount{
			{
				MountPath: "/mnt/path",
				Name:      "disk",
				ReadOnly:  false,
			},
			{
				MountPath: "/mnt/path2",
				Name:      "disk2",
				ReadOnly:  true,
				MountType: "LOCAL",
			},
			{
				MountPath: "/mnt/path3",
				Name:      "disk3",
				ReadOnly:  false,
				MountType: "HOST",
			},
		},
	}
	pod := Pod{
		Name:      "pod",
		Namespace: "test",
	}
	volumes, binds := makeVolumesAndBinds(&pod, &container)

	expectedVolumes := []string{"/mnt/path", "/mnt/path2"}
	expectedBinds := []string{"/exports/pod.test/disk:/mnt/path", "/exports/pod.test/disk2:/mnt/path2:ro", "/mnt/path3:/mnt/path3"}
	if len(volumes) != len(expectedVolumes) {
		t.Errorf("Unexpected volumes. Expected %#v got %#v.  Container was: %#v", expectedVolumes, volumes, container)
	}
	for _, expectedVolume := range expectedVolumes {
		if _, ok := volumes[expectedVolume]; !ok {
			t.Errorf("Volumes map is missing key: %s. %#v", expectedVolume, volumes)
		}
	}
	if len(binds) != len(expectedBinds) {
		t.Errorf("Unexpected binds: Expected %# got %#v.  Container was: %#v", expectedBinds, binds, container)
	}
	verifyStringArrayEquals(t, binds, expectedBinds)
}

func TestMakePortsAndBindings(t *testing.T) {
	container := api.Container{
		Ports: []api.Port{
			{
				ContainerPort: 80,
				HostPort:      8080,
				HostIP:        "127.0.0.1",
			},
			{
				ContainerPort: 443,
				HostPort:      443,
				Protocol:      "tcp",
			},
			{
				ContainerPort: 444,
				HostPort:      444,
				Protocol:      "udp",
			},
			{
				ContainerPort: 445,
				HostPort:      445,
				Protocol:      "foobar",
			},
		},
	}
	exposedPorts, bindings := makePortsAndBindings(&container)
	if len(container.Ports) != len(exposedPorts) ||
		len(container.Ports) != len(bindings) {
		t.Errorf("Unexpected ports and bindings, %#v %#v %#v", container, exposedPorts, bindings)
	}
	for key, value := range bindings {
		switch value[0].HostPort {
		case "8080":
			if !reflect.DeepEqual(docker.Port("80/tcp"), key) {
				t.Errorf("Unexpected docker port: %#v", key)
			}
			if value[0].HostIp != "127.0.0.1" {
				t.Errorf("Unexpected host IP: %s", value[0].HostIp)
			}
		case "443":
			if !reflect.DeepEqual(docker.Port("443/tcp"), key) {
				t.Errorf("Unexpected docker port: %#v", key)
			}
			if value[0].HostIp != "" {
				t.Errorf("Unexpected host IP: %s", value[0].HostIp)
			}
		case "444":
			if !reflect.DeepEqual(docker.Port("444/udp"), key) {
				t.Errorf("Unexpected docker port: %#v", key)
			}
			if value[0].HostIp != "" {
				t.Errorf("Unexpected host IP: %s", value[0].HostIp)
			}
		case "445":
			if !reflect.DeepEqual(docker.Port("445/tcp"), key) {
				t.Errorf("Unexpected docker port: %#v", key)
			}
			if value[0].HostIp != "" {
				t.Errorf("Unexpected host IP: %s", value[0].HostIp)
			}
		}
	}
}

func TestCheckHostPortConflicts(t *testing.T) {
	successCaseAll := []api.ContainerManifest{
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 80}}}}},
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 81}}}}},
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 82}}}}},
	}
	successCaseNew := api.ContainerManifest{
		Containers: []api.Container{{Ports: []api.Port{{HostPort: 83}}}},
	}
	if errs := checkHostPortConflicts(successCaseAll, &successCaseNew); len(errs) != 0 {
		t.Errorf("Expected success: %v", errs)
	}

	failureCaseAll := []api.ContainerManifest{
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 80}}}}},
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 81}}}}},
		{Containers: []api.Container{{Ports: []api.Port{{HostPort: 82}}}}},
	}
	failureCaseNew := api.ContainerManifest{
		Containers: []api.Container{{Ports: []api.Port{{HostPort: 81}}}},
	}
	if errs := checkHostPortConflicts(failureCaseAll, &failureCaseNew); len(errs) == 0 {
		t.Errorf("Expected failure")
	}
}

type mockCadvisorClient struct {
	mock.Mock
}

// ContainerInfo is a mock implementation of CadvisorInterface.ContainerInfo.
func (c *mockCadvisorClient) ContainerInfo(name string) (*info.ContainerInfo, error) {
	args := c.Called(name)
	return args.Get(0).(*info.ContainerInfo), args.Error(1)
}

// MachineInfo is a mock implementation of CadvisorInterface.MachineInfo.
func (c *mockCadvisorClient) MachineInfo() (*info.MachineInfo, error) {
	args := c.Called()
	return args.Get(0).(*info.MachineInfo), args.Error(1)
}

func areSamePercentiles(
	cadvisorPercentiles []info.Percentile,
	kubePercentiles []api.Percentile,
	t *testing.T,
) {
	if len(cadvisorPercentiles) != len(kubePercentiles) {
		t.Errorf("cadvisor gives %v percentiles; kubelet got %v", len(cadvisorPercentiles), len(kubePercentiles))
		return
	}
	for _, ap := range cadvisorPercentiles {
		found := false
		for _, kp := range kubePercentiles {
			if ap.Percentage == kp.Percentage {
				found = true
				if ap.Value != kp.Value {
					t.Errorf("%v percentile from cadvisor is %v; kubelet got %v",
						ap.Percentage,
						ap.Value,
						kp.Value)
				}
			}
		}
		if !found {
			t.Errorf("Unable to find %v percentile in kubelet's data", ap.Percentage)
		}
	}
}

func TestGetContainerStats(t *testing.T) {
	containerID := "ab2cdf"
	containerPath := fmt.Sprintf("/docker/%v", containerID)
	containerInfo := &info.ContainerInfo{
		ContainerReference: info.ContainerReference{
			Name: containerPath,
		},
		StatsPercentiles: &info.ContainerStatsPercentiles{
			MaxMemoryUsage: 1024000,
			MemoryUsagePercentiles: []info.Percentile{
				{50, 100},
				{80, 180},
				{90, 190},
			},
			CpuUsagePercentiles: []info.Percentile{
				{51, 101},
				{81, 181},
				{91, 191},
			},
		},
	}

	mockCadvisor := &mockCadvisorClient{}
	mockCadvisor.On("ContainerInfo", containerPath).Return(containerInfo, nil)

	kubelet, _, fakeDocker := makeTestKubelet(t)
	kubelet.CadvisorClient = mockCadvisor
	fakeDocker.containerList = []docker.APIContainers{
		{
			ID: containerID,
			// pod id: qux
			// container id: foo
			Names: []string{"/k8s--foo--qux--1234"},
		},
	}

	stats, err := kubelet.GetContainerStats("qux", "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if stats.MaxMemoryUsage != containerInfo.StatsPercentiles.MaxMemoryUsage {
		t.Errorf("wrong max memory usage")
	}
	areSamePercentiles(containerInfo.StatsPercentiles.CpuUsagePercentiles, stats.CpuUsagePercentiles, t)
	areSamePercentiles(containerInfo.StatsPercentiles.MemoryUsagePercentiles, stats.MemoryUsagePercentiles, t)
	mockCadvisor.AssertExpectations(t)
}

func TestGetMachineStats(t *testing.T) {
	containerPath := "/"
	containerInfo := &info.ContainerInfo{
		ContainerReference: info.ContainerReference{
			Name: containerPath,
		}, StatsPercentiles: &info.ContainerStatsPercentiles{MaxMemoryUsage: 1024000, MemoryUsagePercentiles: []info.Percentile{{50, 100}, {80, 180},
			{90, 190},
		},
			CpuUsagePercentiles: []info.Percentile{
				{51, 101},
				{81, 181},
				{91, 191},
			},
		},
	}
	fakeDocker := FakeDockerClient{
		err: nil,
	}

	mockCadvisor := &mockCadvisorClient{}
	mockCadvisor.On("ContainerInfo", containerPath).Return(containerInfo, nil)

	kubelet := Kubelet{
		DockerClient:   &fakeDocker,
		DockerPuller:   &FakeDockerPuller{},
		CadvisorClient: mockCadvisor,
	}

	// If the container name is an empty string, then it means the root container.
	stats, err := kubelet.GetMachineStats()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if stats.MaxMemoryUsage != containerInfo.StatsPercentiles.MaxMemoryUsage {
		t.Errorf("wrong max memory usage")
	}
	areSamePercentiles(containerInfo.StatsPercentiles.CpuUsagePercentiles, stats.CpuUsagePercentiles, t)
	areSamePercentiles(containerInfo.StatsPercentiles.MemoryUsagePercentiles, stats.MemoryUsagePercentiles, t)
	mockCadvisor.AssertExpectations(t)
}

func TestGetContainerStatsWithoutCadvisor(t *testing.T) {
	kubelet, _, fakeDocker := makeTestKubelet(t)
	fakeDocker.containerList = []docker.APIContainers{
		{
			ID: "foobar",
			// pod id: qux
			// container id: foo
			Names: []string{"/k8s--foo--qux--1234"},
		},
	}

	stats, _ := kubelet.GetContainerStats("qux", "foo")
	// When there's no cAdvisor, the stats should be either nil or empty
	if stats == nil {
		return
	}
	if stats.MaxMemoryUsage != 0 {
		t.Errorf("MaxMemoryUsage is %v even if there's no cadvisor", stats.MaxMemoryUsage)
	}
	if len(stats.CpuUsagePercentiles) > 0 {
		t.Errorf("Cpu usage percentiles is not empty (%+v) even if there's no cadvisor", stats.CpuUsagePercentiles)
	}
	if len(stats.MemoryUsagePercentiles) > 0 {
		t.Errorf("Memory usage percentiles is not empty (%+v) even if there's no cadvisor", stats.MemoryUsagePercentiles)
	}
}

func TestGetContainerStatsWhenCadvisorFailed(t *testing.T) {
	containerID := "ab2cdf"
	containerPath := fmt.Sprintf("/docker/%v", containerID)

	containerInfo := &info.ContainerInfo{}
	mockCadvisor := &mockCadvisorClient{}
	expectedErr := fmt.Errorf("some error")
	mockCadvisor.On("ContainerInfo", containerPath).Return(containerInfo, expectedErr)

	kubelet, _, fakeDocker := makeTestKubelet(t)
	kubelet.CadvisorClient = mockCadvisor
	fakeDocker.containerList = []docker.APIContainers{
		{
			ID: containerID,
			// pod id: qux
			// container id: foo
			Names: []string{"/k8s--foo--qux--1234"},
		},
	}

	stats, err := kubelet.GetContainerStats("qux", "foo")
	if stats != nil {
		t.Errorf("non-nil stats on error")
	}
	if err == nil {
		t.Errorf("expect error but received nil error")
		return
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("wrong error message. expect %v, got %v", err, expectedErr)
	}
	mockCadvisor.AssertExpectations(t)
}

func TestGetContainerStatsOnNonExistContainer(t *testing.T) {
	mockCadvisor := &mockCadvisorClient{}

	kubelet, _, fakeDocker := makeTestKubelet(t)
	kubelet.CadvisorClient = mockCadvisor
	fakeDocker.containerList = []docker.APIContainers{}

	stats, _ := kubelet.GetContainerStats("qux", "foo")
	if stats != nil {
		t.Errorf("non-nil stats on non exist container")
	}
	mockCadvisor.AssertExpectations(t)
}

func TestParseImageName(t *testing.T) {
	name, tag := parseImageName("ubuntu")
	if name != "ubuntu" || tag != "" {
		t.Fatal("Unexpected name/tag: %s/%s", name, tag)
	}

	name, tag = parseImageName("ubuntu:2342")
	if name != "ubuntu" || tag != "2342" {
		t.Fatal("Unexpected name/tag: %s/%s", name, tag)
	}

	name, tag = parseImageName("foo/bar:445566")
	if name != "foo/bar" || tag != "445566" {
		t.Fatal("Unexpected name/tag: %s/%s", name, tag)
	}

	name, tag = parseImageName("registry.example.com:5000/foobar")
	if name != "registry.example.com:5000/foobar" || tag != "" {
		t.Fatal("Unexpected name/tag: %s/%s", name, tag)
	}

	name, tag = parseImageName("registry.example.com:5000/foobar:5342")
	if name != "registry.example.com:5000/foobar" || tag != "5342" {
		t.Fatal("Unexpected name/tag: %s/%s", name, tag)
	}
}
