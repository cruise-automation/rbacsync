/*
   Copyright 2018 Cruise LLC

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
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	runtimeutil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacv1typed "k8s.io/client-go/kubernetes/typed/rbac/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	rbacsyncv1alpha "github.com/cruise-automation/rbacsync/pkg/apis/rbacsync/v1alpha"
	clientset "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned"
	rbacsyncscheme "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned/scheme"
	informers "github.com/cruise-automation/rbacsync/pkg/generated/informers/externalversions/rbacsync/v1alpha"
	listers "github.com/cruise-automation/rbacsync/pkg/generated/listers/rbacsync/v1alpha"
	"github.com/cruise-automation/rbacsync/pkg/groups"
	"github.com/cruise-automation/rbacsync/pkg/metrics"
)

const (
	controllerAgentName = "rbacsync"
)

const (
	EventReasonConfigEnqueued    = "ConfigEnqueued"
	EventReasonBindingConfigured = "BindingConfigured"
	EventReasonBindingDeleted    = "BindingDeleted"
	EventReasonBindingDuplicated = "BindingDuplicated"
	EventReasonBindingWarning    = "BindingWarning"
	EventReasonBindingError      = "BindingError"
	EventReasonUnknownGroup      = "UnknownGroup"
)

// Controller manages all event handling and creation of rbacsync object.
//
// Follows conventions put forth in
// https://github.com/kubernetes/sample-controller/blob/master/controller.go
type Controller struct {
	kubeclient     kubernetes.Interface // used to create cluster role bindings
	rbacsyncclient clientset.Interface

	grouper    groups.Grouper
	pollPeriod time.Duration

	rbacSyncConfigLister         listers.RBACSyncConfigLister
	rbacSyncConfigsSynced        cache.InformerSynced
	clusterRBACSyncConfigLister  listers.ClusterRBACSyncConfigLister
	clusterRBACSyncConfigsSynced cache.InformerSynced

	roleBindingLister  rbacv1listers.RoleBindingLister
	roleBindingsSynced cache.InformerSynced

	clusterRoleBindingLister  rbacv1listers.ClusterRoleBindingLister
	clusterRoleBindingsSynced cache.InformerSynced

	queue        workqueue.RateLimitingInterface // config object queue.
	clusterqueue workqueue.RateLimitingInterface // cluster config object queue
	recorder     record.EventRecorder
}

func init() {
	// This must happen when this package is used, but we do it in init to
	// avoid concurrency issues when starting up multiple controllers.
	runtimeutil.Must(rbacsyncscheme.AddToScheme(scheme.Scheme))
}

func NewController(
	kubeclient kubernetes.Interface,
	rbacsyncclient clientset.Interface,
	grouper groups.Grouper,
	pollPeriod time.Duration,
	rbacSyncConfigInformer informers.RBACSyncConfigInformer,
	clusterRBACSyncConfigInformer informers.ClusterRBACSyncConfigInformer,
	roleBindingInformer rbacv1informers.RoleBindingInformer,
	clusterRoleBindingInformer rbacv1informers.ClusterRoleBindingInformer,
) *Controller {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclient.CoreV1().Events("")})

	c := &Controller{
		kubeclient:                   kubeclient,
		rbacsyncclient:               rbacsyncclient,
		grouper:                      grouper,
		pollPeriod:                   pollPeriod,
		rbacSyncConfigLister:         rbacSyncConfigInformer.Lister(),
		rbacSyncConfigsSynced:        rbacSyncConfigInformer.Informer().HasSynced,
		clusterRBACSyncConfigLister:  clusterRBACSyncConfigInformer.Lister(),
		clusterRBACSyncConfigsSynced: clusterRBACSyncConfigInformer.Informer().HasSynced,

		roleBindingLister:         roleBindingInformer.Lister(),
		roleBindingsSynced:        roleBindingInformer.Informer().HasSynced,
		clusterRoleBindingLister:  clusterRoleBindingInformer.Lister(),
		clusterRoleBindingsSynced: clusterRoleBindingInformer.Informer().HasSynced,

		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "rbacsyncconfigs"),
		clusterqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "clusterrbacsyncconfigs"),
		recorder:     eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName}),
	}

	rbacSyncConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(old, new interface{}) {
			n := new.(*rbacsyncv1alpha.RBACSyncConfig)
			o := old.(*rbacsyncv1alpha.RBACSyncConfig)
			if n.ResourceVersion == o.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			c.enqueue(n)
		},
		// DeleteFunc: c.handleObject,
	})

	clusterRBACSyncConfigInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(old, new interface{}) {
			n := new.(*rbacsyncv1alpha.ClusterRBACSyncConfig)
			o := old.(*rbacsyncv1alpha.ClusterRBACSyncConfig)
			if n.ResourceVersion == o.ResourceVersion {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}
			c.enqueue(n)
		},
		// DeleteFunc: c.handleObject,
	})

	// TODO(sday): Watch roles and clusterroles so that we can reconfigure
	// bindings if missing roles get added.

	return c
}

func (c *Controller) Run(stopCh <-chan struct{}) error {
	defer c.queue.ShutDown()

	klog.Info("starting controller")

	klog.Info("synchronize informer cache")
	if ok := cache.WaitForCacheSync(stopCh,
		c.rbacSyncConfigsSynced,
		c.clusterRBACSyncConfigsSynced,
		c.roleBindingsSynced,
		c.clusterRoleBindingsSynced); !ok {
		return errors.New("failed to sync informer cache")
	}

	klog.Info("starting workers")
	go wait.Until(makeWorker(c.queue, func(key string) {
		klog.Infof("encountered %q", key)
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			klog.Infof("key split failed %q: %v", key, err)
			runtimeutil.HandleError(err)
			return
		}

		config, err := c.rbacSyncConfigLister.RBACSyncConfigs(namespace).Get(name)
		if err != nil {
			klog.Infof("failed listing %q: %v", key, err)
			runtimeutil.HandleError(errors.Wrapf(err, "unknown item %q in RBACSyncConfig queue", key))
			return
		}

		if err := c.handleConfig(config); err != nil {
			klog.Infof("error %q: %v", key, err)
			runtimeutil.HandleError(errors.Wrapf(err, "error handling item %q in RBACSyncConfig queue", key))
			return
		}
	}), time.Second, stopCh)

	go wait.Until(makeWorker(c.clusterqueue, func(key string) {
		_, name, err := cache.SplitMetaNamespaceKey(key) // we ignore namespace, since these are Cluster scoped
		if err != nil {
			runtimeutil.HandleError(err)
			return
		}

		config, err := c.clusterRBACSyncConfigLister.Get(name)
		if err != nil {
			runtimeutil.HandleError(errors.Wrapf(err, "unknown item %q in ClusterRBACSyncConfig queue", key))
			return
		}

		if err := c.handleClusterConfig(config); err != nil {
			runtimeutil.HandleError(errors.Wrapf(err, "error handling item %q in ClusterRBACSyncConfig queue", key))
			return
		}
	}), time.Second, stopCh)

	if c.pollPeriod > 0 {
		klog.Infof("starting pollers with %v interval", c.pollPeriod)
		go wait.Until(c.pollRBACSyncConfigs, c.pollPeriod, stopCh)
		go wait.Until(c.pollClusterRBACSyncConfigs, c.pollPeriod, stopCh)
	}

	klog.Info("accepting updates")
	<-stopCh
	klog.Info("shutting down")

	return nil
}

func makeWorker(queue workqueue.RateLimitingInterface, fn func(key string)) func() {
	return func() {
		for obj, shutdown := queue.Get(); !shutdown; obj, shutdown = queue.Get() {
			if err := func(obj interface{}) error {
				defer queue.Done(obj)

				key, ok := obj.(string)
				if !ok {
					// garbage in the queue? forget about it!
					queue.Forget(obj)
					runtimeutil.HandleError(fmt.Errorf("unexpected object in workqueue %v of type %T: %#v", queue, obj, obj))
					return nil
				}

				fn(key)
				queue.Forget(obj)
				return nil
			}(obj); err != nil {
				runtimeutil.HandleError(err)
				continue
			}
		}
	}
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		runtimeutil.HandleError(err)
		return
	}

	o, ok := obj.(runtime.Object)
	if !ok {
		klog.Warningf("attempt to enqueue unsupported object %#v", obj)
		return
	}

	// dispatch object to correct queue for handling.
	switch obj.(type) {
	case *rbacsyncv1alpha.RBACSyncConfig:
		c.queue.AddRateLimited(key)
		metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindRBACSyncConfig, EventReasonConfigEnqueued).Inc()
	case *rbacsyncv1alpha.ClusterRBACSyncConfig:
		c.clusterqueue.AddRateLimited(key)
		metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindClusterRBACSyncConfig, EventReasonConfigEnqueued).Inc()
	default:
		klog.Warningf("ignoring object of type %T: %#v", obj, obj)
		return // skip event emit below
	}

	c.recorder.Eventf(o, corev1.EventTypeNormal,
		EventReasonConfigEnqueued, "%v %v enqueued",
		o.GetObjectKind().GroupVersionKind().Kind, key)
}

// pollRBACSyncConfigs queries for all the existing configurations and drops
// them in the queue for reconsideration ensuring that their groups get rebuilt
// from upstream sources periodically.
func (c *Controller) pollRBACSyncConfigs() {
	klog.Info("polling RBACSyncConfigs for group updates")
	rscs, err := c.rbacSyncConfigLister.List(labels.Everything())
	if err != nil {
		msg := "failed listing RBACSyncConfigs in poller"
		klog.Errorf(msg+": %v", err)
		runtimeutil.HandleError(errors.Wrapf(err, msg))
	}

	for _, rsc := range rscs {
		c.enqueue(rsc)
	}
}

func (c *Controller) pollClusterRBACSyncConfigs() {
	klog.Info("polling ClusterRBACSyncConfigs for group updates")
	crscs, err := c.clusterRBACSyncConfigLister.List(labels.Everything())
	if err != nil {
		msg := "failed listing ClusterRBACSyncConfigs in poller"
		klog.Errorf(msg+": %v", err)
		runtimeutil.HandleError(errors.Wrapf(err, msg))
	}

	for _, crsc := range crscs {
		c.enqueue(crsc)
	}
}

func (c *Controller) handleConfig(config *rbacsyncv1alpha.RBACSyncConfig) error {
	var (
		gm  = c.resolveGroups(config.Spec.Memberships)
		ref = metav1.NewControllerRef(config,
			schema.GroupVersionKind{
				Group:   "rbacsync.getcruise.com",
				Version: "v1alpha",
				Kind:    "RBACSyncConfig"})
		roleBindings = c.kubeclient.RbacV1().RoleBindings(config.ObjectMeta.Namespace)
	)

	// track active bindings still part of the config so we clean up stragglers
	// after we process all bindings for this config.
	active := map[string]struct{}{}

	for _, binding := range config.Spec.Bindings {
		switch binding.RoleRef.Kind {
		case "Role", "ClusterRole":
			// valid role reference kind for the role binding.
		default:
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingError, "RoleRef kind %q invalid for RBACSyncConfig on group %q, use only Role or ClusterRole",
				binding.RoleRef.Kind, binding.Group)
			metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindRBACSyncConfig, EventReasonBindingError).Inc()
			continue
		}

		name := config.Name + "-" + binding.Group + "-" + binding.RoleRef.Name

		members, err := gm.Members(binding.Group)
		if err != nil {
			if groups.IsNotFound(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonUnknownGroup, "group %v not found", binding.Group)
				metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindRBACSyncConfig, EventReasonUnknownGroup).Inc()
			} else if groups.IsUnknownMemberships(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonBindingError, "group %v lookup failed: %v", binding.Group, err)
				metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindRBACSyncConfig, EventReasonBindingError).Inc()
				// An error occurred looking up the groups, it should be marked as active
				// so the rolebindings are not deleted in the cleanup.
				active[name] = struct{}{}
			}

			continue
		}

		if len(members) == 0 {
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingWarning, "%v/%v has no members for group %v",
				config.Namespace, config.Name, binding.Group)
			metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindRBACSyncConfig, EventReasonBindingWarning).Inc()
			continue
		}

		if _, ok := active[name]; ok {
			// we've already seen this as part of this loop, so we have a
			// duplicate configuration. Since we key on config+group+role, the
			// result will be the same. Accordingly, we log the bad
			// configuration and move on.
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingDuplicated, "duplicate binding %v ignored", name)
			metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindRoleBinding, EventReasonBindingDuplicated).Inc()
			continue
		}

		rb := rbacv1.RoleBinding{
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
		}

		created, err := c.createOrUpdateRoleBinding(roleBindings, &rb)
		if err != nil {
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingError, "unable to update or create RoleBinding %v/%v: %v",
				rb.Namespace, rb.Name, err)
			metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindRoleBinding, EventReasonBindingError).Inc()
			continue
		}

		// marked the role binding as active, so we don't delete
		active[created.Name] = struct{}{}

		// TODO(sday): Debounce this when we don't actually change the
		// configuration. A bit of event spam is fine for now, but it would be
		// better to see if the ResourceVersion changes. Unfortunately, it is a
		// huge pain to get this working correctly for the unit tests.
		c.recorder.Eventf(config, corev1.EventTypeNormal,
			EventReasonBindingConfigured,
			"RoleBinding %v/%v configured", created.Namespace, created.Name)
		metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindRoleBinding, EventReasonBindingConfigured).Inc()
	}

	selector, err := buildChildSelector(config.Name)
	if err != nil {
		// This error should never happen.
		return errors.Wrapf(err, "error building child selector on %v/%v", config.Namespace, config.Name)
	}

	rbs, err := c.roleBindingLister.RoleBindings(config.Namespace).List(selector)
	if err != nil {
		return errors.Wrapf(err, "failed to list role bindings for cleanup")
	}

	for _, rb := range rbs {
		if !metav1.IsControlledBy(rb, config) {
			// Here we just have a bug in our label selection logic. Log it and
			// fix it.
			klog.Errorf("%v/%v selected by query but not owned by RBACSyncConfig %v/%v",
				rb.Namespace, rb.Name,
				config.Namespace, config.Name)
			continue
		}

		// now that we've established ownership, clean up orphaned bindings.
		if _, ok := active[rb.Name]; ok {
			continue // the binding is still active, move along
		}

		if err := roleBindings.Delete(rb.Name, nil); err != nil {
			if !kubeerrors.IsNotFound(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonBindingError,
					"RoleBinding %v/%v could not be deleted: %v",
					rb.Namespace, rb.Name, err)
				metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindRoleBinding, EventReasonBindingError).Inc()
				continue
			}

			// we let the not found fall through and treat that as success.
		}

		c.recorder.Eventf(config, corev1.EventTypeNormal,
			EventReasonBindingDeleted, "RoleBinding %v/%v deleted", rb.Namespace, rb.Name)
		metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindRoleBinding, EventReasonBindingDeleted).Inc()
	}

	return nil
}

func (c *Controller) createOrUpdateRoleBinding(client rbacv1typed.RoleBindingInterface, rb *rbacv1.RoleBinding) (returned *rbacv1.RoleBinding, err error) {
	returned, err = client.Update(rb)
	if err != nil {
		if !kubeerrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, "updating object %v/%v failed", rb.Namespace, rb.Name)
		}

		// On a real API server, this code path typically won't be executed,
		// since an update will result in a create for a rolebinding. We have
		// this code here in case that changes and to work with the unit
		// testing facilities.
		returned, err = client.Create(rb)
		if err != nil {
			return nil, errors.Wrapf(err, "creating object %v/%v failed", rb.Namespace, rb.Name)
		}
	}

	return
}

func (c *Controller) handleClusterConfig(config *rbacsyncv1alpha.ClusterRBACSyncConfig) error {
	var (
		gm  = c.resolveGroups(config.Spec.Memberships)
		ref = metav1.NewControllerRef(config,
			schema.GroupVersionKind{
				Group:   "rbacsync.getcruise.com",
				Version: "v1alpha",
				Kind:    "ClusterRBACSyncConfig"})
		clusterRoleBindings = c.kubeclient.RbacV1().ClusterRoleBindings()
	)

	// track active bindings still part of the config so we clean up stragglers
	// after we process all bindings for this config.
	active := map[string]struct{}{}

	for _, binding := range config.Spec.Bindings {
		// need to valdiate that we only create rolebindings with this
		// configuration. Kubernetes validates this but we can catch it here
		// and provide a better error message.
		if binding.RoleRef.Kind != "ClusterRole" {
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingError, "RoleRef kind %q invalid for ClusterRBACSyncConfig on group %q, use only ClusterRole",
				binding.RoleRef.Kind, binding.Group)
			metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindClusterRBACSyncConfig, EventReasonBindingError).Inc()
			continue
		}
		name := config.Name + "-" + binding.Group + "-" + binding.RoleRef.Name

		members, err := gm.Members(binding.Group)
		if err != nil {
			if groups.IsNotFound(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonUnknownGroup, "group %v not found", binding.Group)
				metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindClusterRBACSyncConfig, EventReasonUnknownGroup).Inc()
			} else if groups.IsUnknownMemberships(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonBindingError, "group %v lookup failed: %v", binding.Group, err)
				metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindClusterRBACSyncConfig, EventReasonBindingError).Inc()
				// In the case of unknown memberships from the grouper, we want to keep what already exists
				// so we mark the binding as active.
				active[name] = struct{}{}
			}

			continue
		}

		if len(members) == 0 {
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingWarning, "%v has no members for group %v",
				config.Name, binding.Group)
			metrics.RBACSyncConfigStatus.WithLabelValues(metrics.LabelKindClusterRBACSyncConfig, EventReasonBindingWarning).Inc()
			continue
		}

		if _, ok := active[name]; ok {
			// we've already seen this as part of this loop, so we have a
			// duplicate configuration. Since we key on config+group+role, the
			// result will be the same. Accordingly, we log the bad
			// configuration and move on.
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingDuplicated, "duplicate binding %v ignored", name)
			metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindClusterRoleBinding, EventReasonBindingDuplicated).Inc()
			continue
		}

		crb := rbacv1.ClusterRoleBinding{
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
		}

		created, err := c.createOrUpdateClusterRoleBinding(clusterRoleBindings, &crb)
		if err != nil {
			c.recorder.Eventf(config, corev1.EventTypeWarning,
				EventReasonBindingError,
				"unable to update or create ClusterRoleBinding %v: %v", crb.Name, err)
			metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindClusterRoleBinding, EventReasonBindingError).Inc()
			continue
		}

		// marked the cluster role binding as active, so we don't delete
		active[created.Name] = struct{}{}

		// TODO(sday): Debounce this when we don't actually change the
		// configuration. A bit of event spam is fine for now, but it would be
		// better to see if the ResourceVersion changes. Unfortunately, it is a
		// huge pain to get this working correctly for the unit tests.
		c.recorder.Eventf(config, corev1.EventTypeNormal,
			EventReasonBindingConfigured,
			"ClusterRoleBinding %v configured", created.Name)
		metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindClusterRoleBinding, EventReasonBindingConfigured).Inc()
	}

	selector, err := buildChildSelector(config.Name)
	if err != nil {
		// This error should never happen.
		return errors.Wrapf(err, "error building child selector on %v", config.Name)
	}

	crbs, err := c.clusterRoleBindingLister.List(selector)
	if err != nil {
		return errors.Wrapf(err, "failed to list cluster role bindings for cleanup")
	}

	for _, crb := range crbs {
		if !metav1.IsControlledBy(crb, config) {
			// Here we just have a bug in our label selection logic. Log it and
			// fix it.
			klog.Errorf("%v selected by query but not owned by ClusterRBACSyncConfig %v",
				crb.Name, config.Name)
			continue
		}

		// now that we've established ownership, clean up orphaned bindings.
		if _, ok := active[crb.Name]; ok {
			continue // the binding is still active, move along
		}

		if err := clusterRoleBindings.Delete(crb.Name, nil); err != nil {
			if !kubeerrors.IsNotFound(err) {
				c.recorder.Eventf(config, corev1.EventTypeWarning,
					EventReasonBindingError,
					"ClusterRoleBinding %v could not be deleted: %v", crb.Name, err)
				metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindClusterRoleBinding, EventReasonBindingError).Inc()
				continue
			}

			// we let the not found fall through and treat that as success.
		}

		c.recorder.Eventf(config, corev1.EventTypeNormal,
			EventReasonBindingDeleted, "ClusterRoleBinding %v deleted", crb.Name)
		metrics.RBACSyncBindingStatus.WithLabelValues(metrics.LabelKindClusterRoleBinding, EventReasonBindingDeleted).Inc()
	}

	return nil
}

func (c *Controller) createOrUpdateClusterRoleBinding(client rbacv1typed.ClusterRoleBindingInterface, crb *rbacv1.ClusterRoleBinding) (returned *rbacv1.ClusterRoleBinding, err error) {
	returned, err = client.Update(crb)
	if err != nil {
		if !kubeerrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, "updating object %v failed", crb.Name)
		}

		// On a real API server, this code path typically won't be executed,
		// since an update will result in a create for a rolebinding. We have
		// this code here in case that changes and to work with the unit
		// testing facilities.
		returned, err = client.Create(crb)
		if err != nil {
			return nil, errors.Wrapf(err, "creating object %v failed", crb.Name)
		}
	}

	return
}

func (c *Controller) resolveGroups(memberships []rbacsyncv1alpha.Membership) groups.Grouper {
	gm := make(groups.GroupMap)

	for _, group := range memberships {
		for _, subject := range group.Subjects {
			gm[group.Group] = append(gm[group.Group], subject)
		}
	}

	// normalize them all
	for group, subjects := range gm {
		gm[group] = groups.Normalize(subjects)
	}

	if c.grouper == nil {
		return gm
	}
	return groups.Merge(gm, c.grouper)
}

func buildChildSelector(owner string) (labels.Selector, error) {
	selector := labels.NewSelector()
	requirement, err := labels.NewRequirement("owner", selection.Equals, []string{owner})
	if err != nil {
		return nil, err
	}
	return selector.Add(*requirement), nil
}
