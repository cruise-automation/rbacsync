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

type Merged struct {
	groupers []Grouper
}

// Merge returns a merged grouper that will combine the subjects at runtime.
//
// Underlying groupers remain untouched.
func Merge(groupers ...Grouper) Merged {
	return Merged{groupers}
}

func (m Merged) Members(group string) ([]rbacv1.Subject, error) {
	uniq := map[rbacv1.Subject]struct{}{} // we can use roleref as a key type because it is just strings
	for _, grouper := range m.groupers {
		subjects, err := grouper.Members(group)
		if err != nil {
			if IsNotFound(err) {
				continue
			}

			return nil, errors.Wrap(err, "unknown error fetching group members")
		}

		for _, subject := range subjects {
			uniq[subject] = struct{}{}
		}
	}

	if len(uniq) == 0 {
		return nil, errors.Wrapf(ErrNotFound, "no members in %q", group)
	}

	return normalizeUniq(uniq), nil
}
