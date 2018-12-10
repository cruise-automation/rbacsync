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
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
)

// Normalize returns the slice of subjects sorted and unique.
//
// The underlying argument slice is preserved.
func Normalize(subjects []rbacv1.Subject) []rbacv1.Subject {
	uniq := map[rbacv1.Subject]struct{}{} // we can use roleref as a key type because it is just strings
	for _, subject := range subjects {
		uniq[subject] = struct{}{}
	}

	return normalizeUniq(uniq)
}

func normalizeUniq(uniq map[rbacv1.Subject]struct{}) []rbacv1.Subject {
	subjects := make([]rbacv1.Subject, 0, len(uniq))
	for subject := range uniq {
		subjects = append(subjects, *subject.DeepCopy())
	}

	SortSubjects(subjects)
	return subjects
}

func SortSubjects(subjects []rbacv1.Subject) {
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjects[i].Name < subjects[j].Name
	})
}

// newUserSubject creates a new subject as a user with the given name.
//
// Mostly used for unit testing.
func newUserSubject(name string) rbacv1.Subject {
	return rbacv1.Subject{
		Kind:     "User",
		Name:     name,
		APIGroup: "rbac.authorization.k8s.io",
	}
}
