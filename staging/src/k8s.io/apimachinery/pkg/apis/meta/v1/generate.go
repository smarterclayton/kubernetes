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

package v1

// NameGenerator generates names for objects. Some backends may have more information
// available to guide selection of new names and this interface hides those details.
type NameGenerator interface {
	// GenerateName generates a valid name from the base name, adding a random suffix to the
	// the base. If base is valid, the returned name must also be valid. The generator is
	// responsible for knowing the maximum valid name length.
	GenerateName(base string) string
}

// GenerateName will resolve the object name of the provided ObjectMeta to a generated version if
// necessary. It expects that validation for ObjectMeta has already completed (that Base is a
// valid name) and that the NameGenerator generates a name that is also valid.
func GenerateName(u NameGenerator, meta *ObjectMeta) {
	if len(meta.GenerateName) == 0 || len(meta.Name) != 0 {
		return
	}
	meta.Name = u.GenerateName(meta.GenerateName)
}
