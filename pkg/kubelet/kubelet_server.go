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
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"gopkg.in/v1/yaml"
)

// KubeletServer is a http.Handler which exposes kubelet functionality over HTTP.
type KubeletServer struct {
	host    HostInterface
	updates chan<- PodUpdate
	handler http.Handler
}

func ListenAndServeKubeletServer(host HostInterface, updates chan<- PodUpdate, delegate http.Handler, address string, port uint) {
	glog.Infof("Starting to listen on %s:%d", address, port)
	handler := KubeletServer{
		host:    host,
		updates: updates,
		handler: delegate,
	}
	s := &http.Server{
		Addr:           net.JoinHostPort(address, strconv.FormatUint(uint64(port), 10)),
		Handler:        &handler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go util.Forever(func() { s.ListenAndServe() }, 0)
}

// HostInterface contains all the kubelet methods required by the server.
// For testablitiy.
type HostInterface interface {
	GetContainerStats(podFullName, containerName string) (*api.ContainerStats, error)
	GetMachineStats() (*api.ContainerStats, error)
	GetPodInfo(podFullName string) (api.PodInfo, error)
}

func (s *KubeletServer) error(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("Internal Error: %v", err), http.StatusInternalServerError)
}

func (s *KubeletServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer apiserver.MakeLogged(req, &w).Log()

	u, err := url.ParseRequestURI(req.RequestURI)
	if err != nil {
		s.error(w, err)
		return
	}
	switch {
	case u.Path == "/container" || u.Path == "/containers":
		defer req.Body.Close()
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			s.error(w, err)
			return
		}
		if u.Path == "/container" {
			// This is to provide backward compatibility. It only supports a single manifest
			var pod Pod
			err = yaml.Unmarshal(data, &pod.Manifest)
			if err != nil {
				s.error(w, err)
				return
			}
			//TODO: sha1 of manifest?
			pod.Name = pod.Manifest.ID
			s.updates <- PodUpdate{[]Pod{pod}, SET}
		} else if u.Path == "/containers" {
			var manifests []api.ContainerManifest
			err = yaml.Unmarshal(data, &manifests)
			if err != nil {
				s.error(w, err)
				return
			}
			pods := make([]Pod, len(manifests))
			for i := range manifests {
				//TODO: sha1 of manifest?
				pods[i].Name = manifests[i].ID
				pods[i].Manifest = manifests[i]
			}
			s.updates <- PodUpdate{pods, SET}
		}
	case u.Path == "/podInfo":
		//TODO: fix query param to match pod full name
		podFullName := u.Query().Get("podID")
		if len(podFullName) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			http.Error(w, "Missing 'podID=' query entry.", http.StatusBadRequest)
			return
		}
		info, err := s.host.GetPodInfo(podFullName)
		if err != nil {
			s.error(w, err)
			return
		}
		data, err := json.Marshal(info)
		if err != nil {
			s.error(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Add("Content-type", "application/json")
		w.Write(data)
	case strings.HasPrefix(u.Path, "/stats"):
		s.serveStats(w, req)
	default:
		if s.handler != nil {
			s.handler.ServeHTTP(w, req)
		}
	}
}

func (s *KubeletServer) serveStats(w http.ResponseWriter, req *http.Request) {
	// /stats/<podfullname>/<containerName>
	components := strings.Split(strings.TrimPrefix(path.Clean(req.URL.Path), "/"), "/")
	var stats *api.ContainerStats
	var err error
	switch len(components) {
	case 1:
		// Machine stats
		stats, err = s.host.GetMachineStats()
	case 2:
		// pod stats
		// TODO(monnand) Implement this
		errors.New("pod level status currently unimplemented")
	case 3:
		stats, err = s.host.GetContainerStats(components[1], components[2])
	default:
		http.Error(w, "unknown resource.", http.StatusNotFound)
		return
	}
	if err != nil {
		s.error(w, err)
		return
	}
	if stats == nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "{}")
		return
	}
	data, err := json.Marshal(stats)
	if err != nil {
		s.error(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-type", "application/json")
	w.Write(data)
	return
}
