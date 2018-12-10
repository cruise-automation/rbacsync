/*
   Copyright 2018 GM Cruise LLC

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

package groups

import (
	"testing"

	"github.com/cruise-automation/rbacsync/pkg/checks"
	rbacv1 "k8s.io/api/rbac/v1"
)

// TestMerged ensures the runtime behavior of Merged matches that of the simple
// MergeInto from GroupMap.
func TestMerged(t *testing.T) {
	for _, testcase := range []struct {
		name string
		a    GroupMap
		b    GroupMap
	}{
		{
			name: "Simple",
			a: map[string][]rbacv1.Subject{
				"group": {newUserSubject("usera")},
			},
			b: map[string][]rbacv1.Subject{
				"group": {newUserSubject("userb")},
			},
		},
		{
			name: "Empty",
			a: map[string][]rbacv1.Subject{
				"group0": {
					newUserSubject("usera"),
					newUserSubject("userb"),
					newUserSubject("userc"),
				},
				"group1": {newUserSubject("usera")},
				"group3": {newUserSubject("usera")},
			},
			b: GroupMap{},
		},
		{
			name: "Complex",
			a: map[string][]rbacv1.Subject{
				"group0": {
					newUserSubject("usera"),
					newUserSubject("userb"),
					newUserSubject("userc"),
				},
				"group1": {
					newUserSubject("usera"),
				},
				"group3": {
					newUserSubject("usera"),
				},
			},
			b: map[string][]rbacv1.Subject{
				"group0": {
					newUserSubject("useri"),
					newUserSubject("userj"),
					newUserSubject("userk"),
				},
				"group1": {
					newUserSubject("userb"),
				},
				"group3": {
					newUserSubject("userb"),
				},
			},
		},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			merged := Merge(testcase.a, testcase.b)
			expected := testcase.a.DeepCopy().MergeInto(testcase.b)

			for group, expectedSubjects := range expected {
				subjects, err := merged.Members(group)
				checks.Err(t, err)
				checks.DeepEqual(t, expectedSubjects, subjects,
					"runtime merge subject don't match expected")
			}
		})
	}
}

func TestMergedNotFoundOnEmpty(t *testing.T) {
	merged := Merge(Empty)

	_, err := merged.Members("agroup")
	if !IsNotFound(err) {
		t.Fatalf("expected a not found error, got %v", err)
	}
}
