/*
Copyright 2014 The Kubernetes Authors.

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

package request

import (
	"context"
)

type clusterKey int

const (
	// clusterKey is the context key for the request namespace.
	clusterContextKey clusterKey = iota
)

type Cluster struct {
	// Name is the name of the cluster.
	Name string
	// Parents defines the parent clusters that apply to this request.
	Parents []string

	// HACK: only for testing wildcard semantics
	// If true the query applies to all clusters
	Wildcard bool
}

// WithCluster returns a context that describes the nested cluster context
func WithCluster(parent context.Context, cluster Cluster) context.Context {
	return context.WithValue(parent, clusterContextKey, cluster)
}

// ClusterFrom returns the value of the cluster key on the ctx, or nil if there
// is no cluster key.
func ClusterFrom(ctx context.Context) *Cluster {
	cluster, ok := ctx.Value(clusterContextKey).(Cluster)
	if !ok {
		return nil
	}
	return &cluster
}
