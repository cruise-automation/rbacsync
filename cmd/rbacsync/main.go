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

package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cruise-automation/rbacsync/version"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"github.com/cruise-automation/rbacsync/pkg/controller"
	clientset "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned"
	rbacsyncscheme "github.com/cruise-automation/rbacsync/pkg/generated/clientset/versioned/scheme"
	informers "github.com/cruise-automation/rbacsync/pkg/generated/informers/externalversions"
	"github.com/cruise-automation/rbacsync/pkg/groups"
	"github.com/cruise-automation/rbacsync/pkg/groups/gsuite"
)

var (
	baseFlags struct {
		kubeconfig  string
		showVersion bool
		debugAddr   string
		pollPeriod  time.Duration
	}

	// contains all gsuite flags
	gsuiteFlags struct {
		enabled         bool
		credentialsPath string
		delegator       string // account that service account has been authorized for, becomes the subject on the jwt
		pattern         string // optional pattern to enforce for groups.
		timeout         time.Duration
	}
)

const (
	defaultPollPeriod = 5 * time.Minute
)

func init() {
	rbacsyncscheme.AddToScheme(scheme.Scheme) // needed to add CRD types to yaml generator

	flag.BoolVar(&baseFlags.showVersion, "version", false, "show the verson and exit")
	flag.StringVar(&baseFlags.debugAddr, "debug-addr", ":8080", "bind address for liveness, readiness, debug and metrics")
	flag.DurationVar(&baseFlags.pollPeriod, "upstream-poll-period", defaultPollPeriod, "poll period for upstream group updates")

	if home := homeDir(); home != "" {
		flag.StringVar(&baseFlags.kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&baseFlags.kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}

	// gsuite flags
	flag.BoolVar(&gsuiteFlags.enabled, "gsuite.enabled", false, "enabled membership definitions from gsuite")
	flag.StringVar(&gsuiteFlags.credentialsPath, "gsuite.credentials", "", "path to service account token json for gsuite access")
	flag.StringVar(&gsuiteFlags.delegator, "gsuite.delegator", "", "Account that has been delegated for use by the service account.")
	flag.StringVar(&gsuiteFlags.pattern, "gsuite.pattern", "", "Groups forwarded to gsuite account must match this pattern")
	flag.DurationVar(&gsuiteFlags.timeout, "gsuite.timeout", 30*time.Second, "Timeout for requests to gsuite API")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	if baseFlags.showVersion {
		fmt.Println(version.Package, version.Version, version.Revision)
		return
	}

	config, err := resolveConfig(baseFlags.kubeconfig)
	if err != nil {
		klog.Fatalf("unable to resolve kube configuration: %v", err)
	}

	kubeclient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalln(err)
	}

	rbacsyncclient, err := clientset.NewForConfig(config)
	if err != nil {
		klog.Fatalln("error making config", err)
	}

	// TODO(sday): Setup the CRDs here, rather than relying on them existing.

	// we now need to setup the auxillary groupers.
	var grouper groups.Grouper

	if gsuiteFlags.enabled {
		var err error
		grouper, err = gsuite.NewGrouper(
			gsuiteFlags.credentialsPath,
			gsuiteFlags.delegator,
			gsuiteFlags.pattern,
			gsuiteFlags.timeout)
		if err != nil {
			klog.Fatalf("failed to configure gsuite: %v", err)
		}
	} else {
		grouper = groups.Empty
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeclient, time.Second*30)
	informerFactory := informers.NewSharedInformerFactory(rbacsyncclient, time.Second*30)

	ctlr := controller.NewController(
		kubeclient, rbacsyncclient,
		grouper,
		baseFlags.pollPeriod,
		informerFactory.RBACSync().V1alpha().RBACSyncConfigs(),
		informerFactory.RBACSync().V1alpha().ClusterRBACSyncConfigs(),
		kubeInformerFactory.Rbac().V1().RoleBindings(),
		kubeInformerFactory.Rbac().V1().ClusterRoleBindings(),
	)

	stopCh := setupSignalHandler()

	// start everything up!
	go kubeInformerFactory.Start(stopCh)
	go informerFactory.Start(stopCh)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		})
		klog.Fatal(http.ListenAndServe(baseFlags.debugAddr, nil))
	}()

	if err := ctlr.Run(stopCh); err != nil {
		klog.Fatalf("error running controller: %s", err)
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func dumpYAML(o runtime.Object) error {
	p, err := yaml.Marshal(o)
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(p)
	return err
}

func resolveConfig(kubeconfig string) (*rest.Config, error) {
	if _, err := os.Stat(kubeconfig); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "unexpected failure stating kubeconfig")
		}

		config, err1 := rest.InClusterConfig()
		if err1 != nil {
			return nil, errors.Wrap(err, "could not resolve in cluster configuration")
		}

		return config, nil
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not build kubeconfig")
	}

	return config, nil
}

var onlyOneSignalHandler = make(chan struct{})

// setupSignalHandler registered for SIGTERM and SIGINT. A stop channel is returned
// which is closed on one of these signals. If a second signal is caught, the program
// is terminated with exit code 1.
func setupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyOneSignalHandler) // panics when called twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}
