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
	"fmt"
	"testing"
	"time"

	rbacsyncv1alpha "github.com/cruise-automation/rbacsync/pkg/apis/rbacsync/v1alpha"
	"github.com/cruise-automation/rbacsync/pkg/checks"
	rsfake "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned/fake"
	informers "github.com/cruise-automation/rbacsync/pkg/generated/informers/externalversions"
	"github.com/cruise-automation/rbacsync/pkg/groups"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

// TestControllerRBACSyncConfig runs the controller round trip for
// RBACSyncConfig objects, ensuring that the correct rolebindings are created
// or the failure events are reported.
func TestControllerRBACSyncConfig(t *testing.T) {
	// t.Parallel()
	for _, testcase := range []struct {
		input  *rbacsyncv1alpha.RBACSyncConfig // added to controller
		events []string                        // expected events, as formatted by record.FakeRecorder
	}{
		{
			input: newRBACSyncConfig("testing", "basic",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "Role"),
					newBinding("upstream", "role0", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued RBACSyncConfig testing/basic enqueued",
				"Normal BindingConfigured RoleBinding testing/basic-group0-role0 configured",
				"Normal BindingConfigured RoleBinding testing/basic-upstream-role0 configured",
			},
		},

		// Ensure that we can bind cluster roles.
		{
			input: newRBACSyncConfig("testing", "clusterrole",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "Role"),
					newBinding("group0", "role1", "ClusterRole"),
					newBinding("upstream", "role0", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued RBACSyncConfig testing/clusterrole enqueued",
				"Normal BindingConfigured RoleBinding testing/clusterrole-group0-role0 configured",
				"Normal BindingConfigured RoleBinding testing/clusterrole-group0-role1 configured",
				"Normal BindingConfigured RoleBinding testing/clusterrole-upstream-role0 configured",
			},
		},

		// Ensure that we can reference a role in two separate bindings.
		{
			input: newRBACSyncConfig("testing", "duplicates",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "Role"),
					newBinding("group0", "role0", "Role"), // identical just gets ignored.
					newBinding("group0", "role1", "Role"), // refers to same group, but gets separate binding
					newBinding("upstream", "role0", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued RBACSyncConfig testing/duplicates enqueued",
				"Normal BindingConfigured RoleBinding testing/duplicates-group0-role0 configured",

				// We get a duplicate because the controller does not dedupe on
				// the controller. It just creates the binding twice. We may
				// change this to avoid extra round trip in these cases. We
				// should probably actually warn.
				"Warning BindingError duplicate binding duplicates-group0-role0 ignored",
				"Normal BindingConfigured RoleBinding testing/duplicates-group0-role1 configured",
				"Normal BindingConfigured RoleBinding testing/duplicates-upstream-role0 configured",
			},
		},

		// Ensure that we gracefully handle invalid role references.
		{
			input: newRBACSyncConfig("testing", "invalidrole",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "Role"),
					newBinding("group0", "role1", "ThisRoleTypeDoesNotExist"),
					newBinding("upstream", "role0", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued RBACSyncConfig testing/invalidrole enqueued",
				"Normal BindingConfigured RoleBinding testing/invalidrole-group0-role0 configured",
				"Warning BindingError RoleRef kind \"ThisRoleTypeDoesNotExist\" invalid for RBACSyncConfig on group \"group0\", use only Role or ClusterRole",
				"Normal BindingConfigured RoleBinding testing/invalidrole-upstream-role0 configured",
			},
		},

		// Here we test the behavior of the controller when a group is not
		// found, either through the config or group upstreams.
		{
			input: newRBACSyncConfig("testing", "notfound",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "Role"),
					newBinding("upstream", "role0", "Role"),

					// This group does not exist so we log an event and
					// move on. The group may exist in the future, if using
					// an upstream, or will get handled when the
					// configuration is updated.
					newBinding("group-does-not-exist", "role1", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued RBACSyncConfig testing/notfound enqueued",
				"Normal BindingConfigured RoleBinding testing/notfound-group0-role0 configured",
				"Normal BindingConfigured RoleBinding testing/notfound-upstream-role0 configured",
				"Warning UnknownGroup group group-does-not-exist not found",
			},
		},
	} {
		t.Run(fmt.Sprintf("%v/%v", testcase.input.Namespace, testcase.input.Name), func(t *testing.T) {
			// t.Parallel()
			stopCh := make(chan struct{})
			defer close(stopCh)

			ctlr := newTestController(t, stopCh)

			created, err := ctlr.rbacsyncclient.RBACSyncV1alpha().RBACSyncConfigs(testcase.input.Namespace).Create(testcase.input)
			checks.Err(t, err)

			events := collectEvents(t, ctlr)

			// TODO(sday): Make sure we aren't creating rolebindings outside
			// our namespace by querying all of them across all namespaces.
			rbs, err := ctlr.kubeclient.RbacV1().RoleBindings(testcase.input.Namespace).List(metav1.ListOptions{})
			checks.Err(t, err)

			expected := makeExpectedRoleBindings(t, created, ctlr.resolveGroups(created.Spec.Memberships))
			checks.DeepEqual(t, expected, rbs.Items, "created role bindings don't match expected")
			checks.DeepEqual(t, testcase.events, events, "observed events mismatched")

			// By hitting poll, we should get an exact repeat of the events.
			// Note that this might change if we add better debouncing logic.
			ctlr.pollRBACSyncConfigs()
			events = collectEvents(t, ctlr)
			checks.DeepEqual(t, testcase.events, events, "observed polled events mismatched")
		})
	}
}

// TestControllerClusterRBACSyncConfig runs the controller round trip for
// RBACSyncConfig objects, ensuring that the correct rolebindings are created
// or the failure events are reported.
func TestControllerClusterRBACSyncConfig(t *testing.T) {
	for _, testcase := range []struct {
		input  *rbacsyncv1alpha.ClusterRBACSyncConfig // added to controller
		events []string                               // expected events, as formatted by record.FakeRecorder
	}{
		{
			input: newClusterRBACSyncConfig("basic",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "ClusterRole"),
					newBinding("upstream", "role0", "ClusterRole"),
				}),
			events: []string{
				"Normal ConfigEnqueued ClusterRBACSyncConfig basic enqueued",
				"Normal BindingConfigured ClusterRoleBinding basic-group0-role0 configured",
				"Normal BindingConfigured ClusterRoleBinding basic-upstream-role0 configured",
			},
		},

		// Ensure that we can reference a role in two separate bindings.
		{
			input: newClusterRBACSyncConfig("duplicates",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "ClusterRole"),
					newBinding("group0", "role0", "ClusterRole"), // identical just gets ignored.
					newBinding("group0", "role1", "ClusterRole"), // refers to same group, but gets separate binding
					newBinding("upstream", "role0", "ClusterRole"),
				}),
			events: []string{
				"Normal ConfigEnqueued ClusterRBACSyncConfig duplicates enqueued",
				"Normal BindingConfigured ClusterRoleBinding duplicates-group0-role0 configured",

				// We get a duplicate becase the controller does not dedupe on
				// the controller. It just creates the binding twice. We may
				// change this to avoid extra round trip in these cases. We
				// should probably actually warn.
				"Warning BindingError duplicate binding duplicates-group0-role0 ignored",
				"Normal BindingConfigured ClusterRoleBinding duplicates-group0-role1 configured",
				"Normal BindingConfigured ClusterRoleBinding duplicates-upstream-role0 configured",
			},
		},

		// Ensure that we gracefully handle invalid role references.
		{
			input: newClusterRBACSyncConfig("invalidrole",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "ClusterRole"),

					// This will cause a failure event because
					// clusterrolebindings must reference cluster roles.
					newBinding("group0", "role1", "Role"),

					// Make sure upstream honors this, as well
					newBinding("upstream", "role0", "ClusterRole"),
					newBinding("upstream", "role0", "Role"),
				}),
			events: []string{
				"Normal ConfigEnqueued ClusterRBACSyncConfig invalidrole enqueued",
				"Normal BindingConfigured ClusterRoleBinding invalidrole-group0-role0 configured",
				"Warning BindingError RoleRef kind \"Role\" invalid for ClusterRBACSyncConfig on group \"group0\", use only ClusterRole",
				"Normal BindingConfigured ClusterRoleBinding invalidrole-upstream-role0 configured",
				"Warning BindingError RoleRef kind \"Role\" invalid for ClusterRBACSyncConfig on group \"upstream\", use only ClusterRole",
			},
		},

		// Here we test the behavior of the controller when a group is not
		// found, either through the config or group upstreams.
		{
			input: newClusterRBACSyncConfig("notfound",
				[]rbacsyncv1alpha.Membership{
					newMembership("group0",
						[]rbacv1.Subject{
							newUserSubject("user0"),
							newUserSubject("user1"),
						}),
				},
				[]rbacsyncv1alpha.Binding{
					newBinding("group0", "role0", "ClusterRole"),
					newBinding("upstream", "role0", "ClusterRole"),

					// This group does not exist so we log an event and
					// move on. The group may exist in the future, if using
					// an upstream, or will get handled when the
					// configuration is updated.
					newBinding("group-does-not-exist", "role1", "ClusterRole"),
				}),
			events: []string{
				"Normal ConfigEnqueued ClusterRBACSyncConfig notfound enqueued",
				"Normal BindingConfigured ClusterRoleBinding notfound-group0-role0 configured",
				"Normal BindingConfigured ClusterRoleBinding notfound-upstream-role0 configured",
				"Warning UnknownGroup group group-does-not-exist not found",
			},
		},
	} {
		t.Run(testcase.input.Name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)

			ctlr := newTestController(t, stopCh)

			created, err := ctlr.rbacsyncclient.RBACSyncV1alpha().ClusterRBACSyncConfigs().Create(testcase.input)
			checks.Err(t, err)

			events := collectEvents(t, ctlr)

			rbs, err := ctlr.kubeclient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
			checks.Err(t, err)

			expected := makeExpectedClusterRoleBindings(t, created, ctlr.resolveGroups(created.Spec.Memberships))
			checks.DeepEqual(t, expected, rbs.Items, "created cluster role bindings don't match expected")
			checks.DeepEqual(t, testcase.events, events, "observed events mismatched")

			// By hitting poll, we should get an exact repeat of the events.
			// Note that this might change if we add better debouncing logic.
			ctlr.pollClusterRBACSyncConfigs()
			events = collectEvents(t, ctlr)
			checks.DeepEqual(t, testcase.events, events, "observed polled events mismatched")
		})
	}
}

// TestControllerRBACSyncConfigNoLeaks ensures that bindings that are owned by
// the CR but no longer part of the configuration are correctly removed.
func TestControllerRBACSyncConfigNoLeaks(t *testing.T) {
	t.Parallel()
	var (
		stopCh = make(chan struct{})
		ctlr   = newTestController(t, stopCh)
		rsc    = newRBACSyncConfig("testing", "notleaky",
			[]rbacsyncv1alpha.Membership{
				newMembership("group0",
					[]rbacv1.Subject{
						newUserSubject("user0"),
						newUserSubject("user1"),
					}),
			},
			[]rbacsyncv1alpha.Binding{
				// Note that we have group0 binding to role0 and role1.
				// This will result in two role bindings created.
				newBinding("group0", "role0", "Role"),
				newBinding("group0", "role1", "Role"),
			})
	)
	defer close(stopCh)

	_, err := ctlr.rbacsyncclient.RBACSyncV1alpha().RBACSyncConfigs(rsc.Namespace).Create(rsc)
	checks.Err(t, err)

	events := collectEvents(t, ctlr) // collect events to wait for creation.

	// Now, we remove the second reference and update the config.
	rsc.Spec.Bindings = rsc.Spec.Bindings[:1]
	rsc.ResourceVersion = "change" // update this to simulate API server change.

	_, err = ctlr.rbacsyncclient.RBACSyncV1alpha().RBACSyncConfigs(rsc.Namespace).Update(rsc)
	checks.Err(t, err)
	// wait on the deletion event
	events = append(events, collectEvents(t, ctlr)...)

	checks.DeepEqual(t, []string{
		"Normal ConfigEnqueued RBACSyncConfig testing/notleaky enqueued",
		"Normal BindingConfigured RoleBinding testing/notleaky-group0-role0 configured",
		"Normal BindingConfigured RoleBinding testing/notleaky-group0-role1 configured",
		"Normal ConfigEnqueued RBACSyncConfig testing/notleaky enqueued",
		"Normal BindingConfigured RoleBinding testing/notleaky-group0-role0 configured",

		// This is the one we expect to be cleaned up
		"Normal BindingDeleted RoleBinding testing/notleaky-group0-role1 deleted",
	}, events, "should see creation and deletion events from full lifecycle")

	rbs, err := ctlr.kubeclient.RbacV1().RoleBindings(rsc.Namespace).List(metav1.ListOptions{})
	checks.Err(t, err)

	// Calculate the expected bindings based on the current config. The removed
	// binding should result in a cleanup.
	expectedRoleBindings := makeExpectedRoleBindings(t, rsc,
		ctlr.resolveGroups(rsc.Spec.Memberships))
	checks.DeepEqual(t, expectedRoleBindings, rbs.Items,
		"bindings should have been cleaned up")
}

// TestControllerClusterRBACSyncConfigNoLeaks ensures that bindings that are owned by
// the CR but no longer part of the configuration are correctly removed.
func TestControllerClusterRBACSyncConfigNoLeaks(t *testing.T) {
	var (
		stopCh = make(chan struct{})
		ctlr   = newTestController(t, stopCh)
		crsc   = newClusterRBACSyncConfig("notleaky",
			[]rbacsyncv1alpha.Membership{
				newMembership("group0",
					[]rbacv1.Subject{
						newUserSubject("user0"),
						newUserSubject("user1"),
					}),
			},
			[]rbacsyncv1alpha.Binding{
				// Note that we have group0 binding to role0 and role1.
				// This will result in two role bindings created.
				newBinding("group0", "role0", "ClusterRole"),
				newBinding("group0", "role1", "ClusterRole"),
			})
	)
	defer close(stopCh)

	_, err := ctlr.rbacsyncclient.RBACSyncV1alpha().ClusterRBACSyncConfigs().Create(crsc)
	checks.Err(t, err)

	events := collectEvents(t, ctlr) // collect events to wait for creation.

	// Now, we remove the second reference and update the config.
	crsc.Spec.Bindings = crsc.Spec.Bindings[:1]
	crsc.ResourceVersion = "change" // update this to simulate API server change.

	_, err = ctlr.rbacsyncclient.RBACSyncV1alpha().ClusterRBACSyncConfigs().Update(crsc)
	checks.Err(t, err)
	// wait on the deletion event
	events = append(events, collectEvents(t, ctlr)...)

	checks.DeepEqual(t, []string{
		"Normal ConfigEnqueued ClusterRBACSyncConfig notleaky enqueued",
		"Normal BindingConfigured ClusterRoleBinding notleaky-group0-role0 configured",
		"Normal BindingConfigured ClusterRoleBinding notleaky-group0-role1 configured",
		"Normal ConfigEnqueued ClusterRBACSyncConfig notleaky enqueued",
		"Normal BindingConfigured ClusterRoleBinding notleaky-group0-role0 configured",

		// This is the one we expect to be cleaned up
		"Normal BindingDeleted ClusterRoleBinding notleaky-group0-role1 deleted",
	}, events, "should see creation and deletion events from full lifecycle")

	crbs, err := ctlr.kubeclient.RbacV1().ClusterRoleBindings().List(metav1.ListOptions{})
	checks.Err(t, err)

	// Calculate the expected bindings based on the current config. The removed
	// binding should result in a cleanup.
	expectedClusterRoleBindings := makeExpectedClusterRoleBindings(t, crsc,
		ctlr.resolveGroups(crsc.Spec.Memberships))
	checks.DeepEqual(t, expectedClusterRoleBindings, crbs.Items,
		"bindings should have been cleaned up")
}

var (
	alwaysReady = func() bool { return true }
)

func newTestController(t *testing.T, stopCh <-chan struct{}) *Controller {
	kubeclient := fake.NewSimpleClientset()
	rbacsyncclient := rsfake.NewSimpleClientset()

	upstream := groups.GroupMap{
		// every group0 is augmented with an upstream user
		"group0": {
			newUserSubject("upstream0"),
		},
		// we also define an upstream group that gets referenced.
		"upstream": {
			newUserSubject("upstream0"),
			newUserSubject("upstream1"),
			newUserSubject("upstream2"),
		},
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclient, 0)
	informerFactory := informers.NewSharedInformerFactory(rbacsyncclient, 0)
	ctlr := NewController(
		kubeclient, rbacsyncclient,
		upstream,
		0, // disable the poller for testing
		informerFactory.RBACSync().V1alpha().RBACSyncConfigs(),
		informerFactory.RBACSync().V1alpha().ClusterRBACSyncConfigs(),
		kubeInformerFactory.Rbac().V1().RoleBindings(),
		kubeInformerFactory.Rbac().V1().ClusterRoleBindings())
	ctlr.rbacSyncConfigsSynced = alwaysReady
	ctlr.clusterRBACSyncConfigsSynced = alwaysReady
	ctlr.recorder = record.NewFakeRecorder(1000)

	go kubeInformerFactory.Start(stopCh)
	go informerFactory.Start(stopCh)
	go ctlr.Run(stopCh)

	return ctlr
}

// makeExpectedRoleBindings creates a set of role bindings from the config.
//
// This is essentially a reimplementation of the core controller logic. We may
// want to consolidate this, but for now, having it here gives us secondary
// validation.
//
// In practice, this is different from the controller version, as this just
// makes them all in a single batch, whereas the controller builds, then sends
// them to the APIserver, then checks the error.
func makeExpectedRoleBindings(t *testing.T, config *rbacsyncv1alpha.RBACSyncConfig, memberships groups.Grouper) []rbacv1.RoleBinding {
	var (
		bindings []rbacv1.RoleBinding
		ref      = metav1.NewControllerRef(config,
			schema.GroupVersionKind{
				Group:   "rbacsync.getcruise.com",
				Version: "v1alpha",
				Kind:    "RBACSyncConfig"})
	)

	seen := map[string]struct{}{}
	for _, binding := range config.Spec.Bindings {
		switch binding.RoleRef.Kind {
		case "Role", "ClusterRole":
		default:
			// these are skipped
			continue
		}

		members, err := memberships.Members(binding.Group)
		if err != nil {
			if groups.IsNotFound(err) {
				// skip non-existent groups
				continue
			}
			t.Fatal(err) // causes test failure for now, but we could look for an event
		}

		if len(members) == 0 {
			// never should be added, causes event
			continue
		}

		name := config.Name + "-" + binding.Group + "-" + binding.RoleRef.Name
		if _, ok := seen[name]; ok {
			continue // don't create duplicates
		}
		bindings = append(bindings, rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Namespace:       config.Namespace,
				OwnerReferences: []metav1.OwnerReference{*ref},
				Labels: map[string]string{
					"owner": config.Name,
				},
				Annotations: map[string]string{
					"rbacsync.getcruise.com/group": binding.Group,
				},
			},
			Subjects: members,
			RoleRef:  binding.RoleRef,
		})
		seen[name] = struct{}{}
	}

	return bindings
}

// makeExpectedClusterRoleBindings does the same thing as
// makeExpectedRoleBindings, except for cluster-scoped role bindings.
func makeExpectedClusterRoleBindings(t *testing.T, config *rbacsyncv1alpha.ClusterRBACSyncConfig, memberships groups.Grouper) []rbacv1.ClusterRoleBinding {
	var (
		bindings []rbacv1.ClusterRoleBinding
		ref      = metav1.NewControllerRef(config,
			schema.GroupVersionKind{
				Group:   "rbacsync.getcruise.com",
				Version: "v1alpha",
				Kind:    "ClusterRBACSyncConfig"})
	)

	seen := map[string]struct{}{}
	for _, binding := range config.Spec.Bindings {
		if binding.RoleRef.Kind != "ClusterRole" {
			// these are skipped
			continue
		}

		members, err := memberships.Members(binding.Group)
		if err != nil {
			if groups.IsNotFound(err) {
				// skip non-existent groups
				continue
			}
			t.Fatal(err) // causes test failure for now, but we could look for an event
		}

		if len(members) == 0 {
			// never should be added, causes event
			continue
		}

		name := config.Name + "-" + binding.Group + "-" + binding.RoleRef.Name
		if _, ok := seen[name]; ok {
			continue // don't create duplicates
		}

		bindings = append(bindings, rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				OwnerReferences: []metav1.OwnerReference{*ref},
				Labels: map[string]string{
					"owner": config.Name,
				},
				Annotations: map[string]string{
					"rbacsync.getcruise.com/group": binding.Group,
				},
			},
			Subjects: members,
			RoleRef:  binding.RoleRef,
		})

		seen[name] = struct{}{}
	}

	return bindings
}

// collectEvents from the fake recorder on a controller. These will just be
// strings formatted according to the test package.
//
// Events must be emitted for this to return anything. If not events are fired,
// this will block until an event is emitted or after a modest timeout.
func collectEvents(t *testing.T, ctlr *Controller) []string {
	t.Helper()
	fr := ctlr.recorder.(*record.FakeRecorder) // let this panic

	var events []string
	for {
		select {
		case e := <-fr.Events:
			events = append(events, e)
		case <-time.After(time.Second):
			if len(events) == 0 {
				t.Fatal("timed out waiting for events")
			}
			return events
		}
	}

	return events
}
