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
	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
)

// GroupMap is simple map of group name to subjects.
//
// Can be created with make or NewGroupMap.
type GroupMap map[string][]rbacv1.Subject

// NewGroupMap creates a group map from the provided map, ensuring that
// subjects are sorted and unique.
func NewGroupMap(m map[string][]rbacv1.Subject) GroupMap {
	gm := make(GroupMap, len(m))
	// uniqify and stabilize the groups in the map.
	for group, subjects := range m {
		gm[group] = Normalize(subjects)
	}

	return gm
}

func (gm GroupMap) Members(group string) ([]rbacv1.Subject, error) {
	subjects, ok := gm[group]
	if !ok {
		return nil, errors.Wrapf(ErrNotFound, "subjects in group %v", group)
	}

	return subjects, nil
}

// MergeInto takes another group map and merges into the receiver.
//
// This peforms differently that merge, which returns the two original groups.
// This one modifies the group definitions into a single one.
//
// The receiver is always returned.
func (gm GroupMap) MergeInto(other GroupMap) GroupMap {
	for group, subjects := range other {
		gm[group] = Normalize(append(gm[group], subjects...))
	}

	return gm
}

func (gm GroupMap) DeepCopy() GroupMap {
	n := make(GroupMap, len(gm))
	for group, subjects := range gm {
		n[group] = make([]rbacv1.Subject, len(subjects))
		copy(n[group], subjects)
	}

	return n
}
