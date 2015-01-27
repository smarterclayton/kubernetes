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

package api

import (
	"fmt"
	"math"
	"math/rand"
)

// NameGenerator generates names for objects. Some backends may have more information
// available to guide selection of new names and this interface hides those details.
type NameGenerator interface {
	// GenerateName generates a valid name from the base name, adding a random suffix before or after
	// the base as defined by After. If base is valid, the returned name must also be valid.
	GenerateName(base string, after bool) string
}

// GenerateName will resolve the object name of the provided ObjectMeta to a generated version if
// necessary. It expects that validation for ObjectMeta has already completed (that Base is a
// valid name) and that the NameGenerator generates a name that is also valid.
func GenerateName(u NameGenerator, meta *ObjectMeta) {
	if meta.GenerateName == nil {
		return
	}
	after := true
	switch meta.GenerateName.Type {
	case GenerateNamePrefixType:
		after = false
	}
	meta.Name = u.GenerateName(meta.GenerateName.Base, after)
}

// simpleNameGenerator generates random names.
type simpleNameGenerator struct{}

// SimpleNameGenerator is a generator that returns the name plus a random integer between 0 and 999999
// when a name is requested. The string is guaranteed to not exceed the length of a Kubernetes name.
var SimpleNameGenerator NameGenerator = simpleNameGenerator{}

const (
	maxNameLength          = 63
	randomLength           = 6
	maxGeneratedNameLength = maxNameLength - randomLength
)

var randomMax = int(math.Pow(10, randomLength))

func (simpleNameGenerator) GenerateName(base string, after bool) string {
	if len(base) > maxGeneratedNameLength {
		base = base[:maxGeneratedNameLength]
	}
	value := rand.Intn(randomMax)
	if after {
		return fmt.Sprintf("%s%d", base, value)
	}
	return fmt.Sprintf("%d%s", value, base)
}
