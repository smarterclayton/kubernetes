/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or sied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Reads the pod configuration from file or a directory of files
package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"gopkg.in/v1/yaml"
)

type ConfigSourceFile struct {
	path string
	ch   chan<- interface{}
}

func NewConfigSourceFile(path string, period time.Duration, ch chan<- interface{}) *ConfigSourceFile {
	config := &ConfigSourceFile{
		path: path,
		ch:   ch,
	}
	glog.Infof("Watching file %s", path)
	go util.Forever(config.run, period)
	return config
}

func (s *ConfigSourceFile) run() {
	if err := s.extractFromPath(); err != nil {
		glog.Errorf("Unable to read config file: %s", err)
	}
}

func (s *ConfigSourceFile) extractFromPath() error {
	path := s.path
	statInfo, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("unable to access path: %s", err)
		}
		return fmt.Errorf("path does not exist: %s", path)
	}

	if statInfo.Mode().IsDir() {
		pods, err := extractFromDir(path)
		if err != nil {
			return err
		}
		s.ch <- kubelet.PodUpdate{pods, kubelet.SET}
		return nil
	}

	if statInfo.Mode().IsRegular() {
		pod, err := extractFromFile(path)
		if err != nil {
			return err
		}
		s.ch <- kubelet.PodUpdate{[]kubelet.Pod{pod}, kubelet.SET}
		return nil
	}

	return fmt.Errorf("path is not a directory or file")
}

func extractFromDir(name string) ([]kubelet.Pod, error) {
	pods := []kubelet.Pod{}

	files, err := filepath.Glob(filepath.Join(name, "[^.]*"))
	if err != nil {
		return pods, err
	}

	sort.Strings(files)

	for _, file := range files {
		pod, err := extractFromFile(file)
		if err != nil {
			glog.Errorf("Couldn't read from file %s: %v", file, err)
			return []kubelet.Pod{}, err
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func extractFromFile(name string) (kubelet.Pod, error) {
	var pod kubelet.Pod

	file, err := os.Open(name)
	if err != nil {
		return pod, err
	}
	defer file.Close()

	data, err := ioutil.ReadAll(file)
	if err != nil {
		glog.Errorf("Couldn't read from file: %v", err)
		return pod, err
	}

	if err := extractYAMLData(data, &pod.Manifest); err != nil {
		return pod, err
	}
	pod.Name = fmt.Sprintf("file-%s", filepath.Base(name))

	return pod, nil
}

// Extract data from YAML file into a list of containers.
func extractYAMLData(buf []byte, output interface{}) error {
	err := yaml.Unmarshal(buf, output)
	if err != nil {
		glog.Errorf("Couldn't unmarshal configuration: %v", err)
		return err
	}
	return nil
}
