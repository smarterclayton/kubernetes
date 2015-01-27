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
	"strings"
	"testing"
)

type nameGeneratorFunc func(base string, after bool) string

func (fn nameGeneratorFunc) GenerateName(base string, after bool) string {
	return fn(base, after)
}

func TestGenerateName(t *testing.T) {
	testCases := []struct {
		meta ObjectMeta

		base     string
		after    bool
		returned bool
	}{
		{},
		{
			meta: ObjectMeta{
				GenerateName: &GenerateNameSpec{
					Type: GenerateNameSuffixType,
				},
			},
			after:    true,
			returned: true,
		},
		{
			meta: ObjectMeta{
				GenerateName: &GenerateNameSpec{
					Type: GenerateNamePrefixType,
				},
			},
			returned: true,
		},
		{
			meta: ObjectMeta{
				Name: "bar",
				GenerateName: &GenerateNameSpec{
					Type: GenerateNamePrefixType,
					Base: "foo",
				},
			},
			base:     "foo",
			returned: true,
		},
	}

	for i, testCase := range testCases {
		GenerateName(nameGeneratorFunc(func(base string, after bool) string {
			if after != testCase.after {
				t.Errorf("%d: unexpected call with after", i)
			}
			if base != testCase.base {
				t.Errorf("%d: unexpected call with base", i)
			}
			return "test"
		}), &testCase.meta)
		var expect string
		if testCase.returned {
			expect = "test"
		}
		if expect != testCase.meta.Name {
			t.Errorf("%d: unexpected name: %#v", i, testCase.meta)
		}
	}
}

func TestSimpleNameGenerator(t *testing.T) {
	meta := &ObjectMeta{
		Name: "bar",
		GenerateName: &GenerateNameSpec{
			Type: GenerateNamePrefixType,
			Base: "foo",
		},
	}
	GenerateName(SimpleNameGenerator, meta)
	if !strings.HasSuffix(meta.Name, "foo") || meta.Name == "foo" {
		t.Errorf("unexpected name: %#v", meta)
	}

	meta = &ObjectMeta{
		Name: "bar",
		GenerateName: &GenerateNameSpec{
			Base: "foo",
		},
	}
	GenerateName(SimpleNameGenerator, meta)
	if !strings.HasPrefix(meta.Name, "foo") || meta.Name == "foo" {
		t.Errorf("unexpected name: %#v", meta)
	}
}
