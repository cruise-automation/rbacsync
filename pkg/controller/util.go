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

package controller

import (
	rbacsyncv1alpha "github.com/cruise-automation/rbacsync/pkg/apis/rbacsync/v1alpha"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newBinding(group, role, kind string) rbacsyncv1alpha.Binding {
	return rbacsyncv1alpha.Binding{
		Group: group,
		RoleRef: rbacv1.RoleRef{
			Kind:     kind,
			Name:     role,
			APIGroup: "rbac.authorization.k8s.io",
		},
	}
}

func newUserSubject(name string) rbacv1.Subject {
	return rbacv1.Subject{
		Kind:     "User",
		Name:     name,
		APIGroup: "rbac.authorization.k8s.io",
	}
}

func newMembership(group string, subjects []rbacv1.Subject) rbacsyncv1alpha.Membership {
	return rbacsyncv1alpha.Membership{
		Group:    group,
		Subjects: subjects,
	}
}

func newRBACSyncConfig(namespace, name string,
	memberships []rbacsyncv1alpha.Membership,
	bindings []rbacsyncv1alpha.Binding) *rbacsyncv1alpha.RBACSyncConfig {
	return &rbacsyncv1alpha.RBACSyncConfig{
		TypeMeta: metav1.TypeMeta{
			Kind: "RBACSyncConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: rbacsyncv1alpha.Spec{
			Memberships: memberships,
			Bindings:    bindings,
		},
	}
}

func newClusterRBACSyncConfig(name string,
	memberships []rbacsyncv1alpha.Membership,
	bindings []rbacsyncv1alpha.Binding) *rbacsyncv1alpha.ClusterRBACSyncConfig {
	return &rbacsyncv1alpha.ClusterRBACSyncConfig{
		TypeMeta: metav1.TypeMeta{
			Kind: "ClusterRBACSyncConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: rbacsyncv1alpha.Spec{
			Memberships: memberships,
			Bindings:    bindings,
		},
	}
}
