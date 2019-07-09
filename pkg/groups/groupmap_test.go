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
	"k8s.io/klog"
	"testing"

	"github.com/cruise-automation/rbacsync/pkg/checks"
	rbacv1 "k8s.io/api/rbac/v1"
)

func init() {
	klog.InitFlags(nil)
}

func TestNewGroupMap(t *testing.T) {
	m := map[string][]rbacv1.Subject{
		"group0": {
			newUserSubject("user1"),
			newUserSubject("user0"),
			newUserSubject("user0"),
			newUserSubject("user0"),
		},
		"group1": {
			newUserSubject("user1"),
			newUserSubject("user0"),
		},
	}

	expected := GroupMap{}
	for group, subjects := range m {
		expected[group] = Normalize(subjects)
	}
	gm := NewGroupMap(m)

	checks.DeepEqual(t, expected, gm, "all groups should be normalized")
}
