![RBACSync](project/images/logo-horizontal.png)

* [Usage](#usage)
  + [Custom Resources](#custom-resources)
    - [Bindings](#bindings)
    - [Memberships](#memberships)
  + [Configuration](#configuration)
    - [Enabling GSuite Group Configuration](#enabling-gsuite-group-configuration)
  + [Deployment](#deployment)
* [Development](#development)
  + [Building](#building)
  + [Local Development](#local-development)
* [In Cluster Development](#in-cluster-development)
* [Release Process](#release-process)
  + [Tagging your release](#tagging-your-release)
  + [Pushing your release tag](#pushing-your-release-tag)
  + [Create your release in Github](#create-your-release-in-github)
* [License](#license)
* [Contributions](#contributions)

This project provides a Kubernetes controller to synchronize RoleBindings and
ClusterRoleBindings, used in [Kubernetes RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/),
from group membership sources using consolidated configuration objects. The
provided configuration objects allow you to define "virtual" groups that result
in the creation of RoleBindings and ClusterRoleBindings that directly reference
all users in the group.

Group membership can be declared directly in configuration objects or be pulled
from an "upstream". Currently, the only supported upstream is GSuite, but we
may add others, if required.

Usage
-----

### Custom Resources

RBACSync leverages [Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#customresourcedefinitions)
to manage binding configuration and group membership declaration. The
CRDs are available in the [deployment directory](deploy/00-crds.yml). Specific
information about the type declarations are available in the [the source
code](pkg/apis/rbacsync/v1alpha/types.go).

There are two CRDs defined depending on whether you want to create groups with cluster
scope or namespace scope:

| Custom Resource Definition | Scope      | Description                                              |
|----------------------------|------------|----------------------------------------------------------|
| RBACSyncConfig             | Namespaced | Maps groups to users to create namespaced `RoleBindings` |
| ClusterRBACSyncConfig      | Cluster    | Maps groups to users to create `ClusterRoleBindings`     |

To define groups with RoleBindings within a namespace, you'll need to create an
RBACSyncConfig. To define groups that create ClusterRoleBindings, you'll need
to create ClusterRBACSyncConfig.

These two configuration objects can be used in concert to correctly configure
user bindings to `Roles` and `ClusterRoles` depending on the context.

If you are confused at any time, it might help to look at the
[example.yml](example.yml) for details.

#### Bindings

The configurations used for RBACSync are centered around a `bindings` entry. It
declares a mapping of group name to `RoleBinding` or `ClusterRoleBinding`. We
can use a very simple configuration to show this concept:

```yaml
apiVersion: rbacsync.getcruise.com/v1alpha
kind: ClusterRBACSyncConfig
metadata:
  name: example-simple
spec:
  # Defines the roleRef to use against the subects defined above.
  bindings:
  - group: mygroup-admin@getcruise.com
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: Role
      name: cluster-admin
```

The above is a configuration for the `ClusterRBACSyncConfig` with a single
binding entry that will map the Google Group "mygroup-admin@getcruise.com". For
each binding entry, a `ClusterRoleBinding` will be created. It will look
similar to the following:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  annotations:
    rbacsync.getcruise.com/group: mygroup-admin@getcruise.com
  labels:
    owner: example-simple # allows use to select child objects quickly
  name: example-simple-mygroup-admin@getcruise.com-cluster-admin
  ownerReferences: # will be owned by the above configuration
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
# Users that are a member of the group, from the upstream
- apiGroup: rbac.authorization.k8s.io
  kind: User
  name: a@getcruise.com
- apiGroup: rbac.authorization.k8s.io
  kind: User
  name: b@getcruise.com
- apiGroup: rbac.authorization.k8s.io
  kind: User
  name: c@getcruise.com
```

The above will be created when the configuration is added to the Kubernetes
cluster. If the upstream group definition changes, updates will be picked up
after a configurable polling period. If the configuration that defined this, in
this case `example-simple`, is removed, this `ClusterRoleBinding` will be
automatically removed.

For namespace-scoped `RBACSyncConfig`, the behavior is nearly identical except
for the following:

1. `RBACSyncConfig` must be defined with a namespace.
2. `RBACSyncConfig` can only reference `Roles`.
3. All `RoleBindings` created as a result of the `RBACSyncConfig` will be in
   the same namespace.

When deciding between using the two, you should mostly only need to look at
whether your assigning `ClusterRoles` or `Roles` and then use the equivalent
configuration. Refer to the [Kubernetes RBAC documentation](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
for further guidance on that topic.

#### Memberships

Group memberships allow one to specify the targets of the `RoleBindings` and
`ClusterRoleBindings` used in a configuration object. Abstractly, we'd like to
know based on a group name, what subjects are a member of that group. If you
are using an upstream, such as GSuite, these memberships are drawn directly
from group memberships define there. There may be cases when you want to store
these definitions directly in the cluster or need to augment groups with
additional members, such as GCP service accounts.

Each config has a spec which defines `memberships`, calling out each member of
the group.  The groups defined in `memberships` may then be referenced in one
or mores `bindings`.

To add memberships, simply include a memberships section in the spec,
memberships:

```yaml
  memberships:
  - group: cluster-admin-group
    subjects:
    - kind: User
      name: a@getcruise.com
      apiGroup: rbac.authorization.k8s.io
    - kind: User
      name: b@getcruise.com
      apiGroup: rbac.authorization.k8s.io
    - kind: User
      name: c@getcruise.com
  - group: someother-group
    subjects:
    - kind: User
      name: a@getcruise.com
      apiGroup: rbac.authorization.k8s.io
```

The above declares two groups, "cluster-admin-group" and "someother-group",
each with a list of subjects. When creating `RoleBindings`, the subjects
declared here will be used as the subjects in the `RoleBinding` object that
gets created.

> NOTE: If you're using GSuite to configure group memberships, you likely won't
> need this section. However, it may be useful to add supplementary members or
> robot accounts to groups using memberships.

### Configuration

For the most part, you can start using RBACSync with the default deployment,
defined in [`deploy/20-deployment.yml`](deploy/20-deployment.yml). This will
allow you to deploy configurations with bindings and memberships.

For reference, here are the available flags on the `rbacsync` binary:

```console
Usage of bin/rbacsync:
  -alsologtostderr
		log to standard error as well as files
  -debug-addr string
		bind address for liveness, readiness, debug and metrics (default ":8080")
  -gsuite.credentials string
		path to service account token json for gsuite access
  -gsuite.delegator string
		Account that has been delegated for use by the service account.
  -gsuite.enabled
		enabled membership definitions from gsuite
  -gsuite.pattern string
		Groups forwarded to gsuite account must match this pattern
  -gsuite.timeout duration
		Timeout for requests to gsuite API (default 30s)
  -kubeconfig string
		(optional) absolute path to the kubeconfig file (default "/home/sday/.kube/config")
  -log_backtrace_at value
		when logging hits line file:N, emit a stack trace
  -log_dir string
		If non-empty, write log files in this directory
  -logtostderr
		log to standard error instead of files
  -stderrthreshold value
		logs at or above this threshold go to stderr
  -upstream-poll-period duration
		poll period for upstream group updates (default 5m0s)
  -v value
		log level for V logs
  -version
		show the verson and exit
  -vmodule value
		comma-separated list of pattern=N settings for file-filtered logging
```

In configuring upstreams, you'll likely have to add one or more flags. Note
that you can also increase logging granulatiy, if you need more in depth
debugging.

#### Enabling GSuite Group Configuration

To use GSuite, you'll need a service account with "G Suite Domain-Wide
Delegation of Authority". It's recommended to read the
[guide](https://developers.google.com/admin-sdk/directory/v1/guides/delegation)
to understand how this works in cause you run into issues. The blog
[Navigating the Google Suite Directory
API](https://www.fin.com/post/2017/10/navigating-google-suite-directory-api)
may also provide some insight.

The goal is to end up with two accounts:

1. A "robot" GSuite account that acts as a "delagator" to the service account.
2. A "service account" in a GCP project that will act as a delegate.

We then use the credentials of the service account with `rbacsync` to allow it
to read the GSuite Directory API.

> NOTE: You may be able to get away with delegating to a user account and
> taking it through the OAuth flow to delegate permissions. It is recommended
> to have a robot account with restricted permissions to control access and
> avoid lockouts if a user account experiences problems.

To setup this account, you'll need to take the following steps:

1. Create a new GSuite account with readonly access to the API on Google
   Groups. We'll call this the "robot" account. It should have the following
   permissions:

	- https://www.googleapis.com/auth/admin.directory.group.readonly
	- https://www.googleapis.com/auth/admin.directory.group.member.readonly

   Please refer to the GSuite documentation, as the exact process for doing
   this may have changed.
2. Create an IAM Binding for the "robot" user in your GCP project to the
   "Service Account User" (`roles/iam.serviceAccountUser`) role. The exact
   project to use will depend on your environment but the only requirement is
   that it can house the service account that we use for access.
3. Create a GCP service account in the same project used in step 2. Enable the "G
   Suite Domain-wide Delegation" check box and note the Client ID.
4. Using the "security" component in the "admin.google.com" console, use the
   Client ID for the service account and add the following scopes, which are
   the same as those from step 1:
	- https://www.googleapis.com/auth/admin.directory.group.member.readonly
	- https://www.googleapis.com/auth/admin.directory.group.readonly
5. Generate the service account credentials. Make sure to save the generated
   JSON file somewhere safe.

Once we have the account setup we can modify the deployment to allow `rbacsync`
to use it. The first step is creating the secret from the service account
credentials created in step 5:

```console
kubectl create secret generic --from-file=token.json=<my service account credentials path> rbacsync-gsuite-credentials
```

We can then apply the following diff to the [`rbacsync` deployment](deploy/20-deployment.yml)
to use the secret created above:

```diff
@@ -14,6 +14,10 @@
     spec:
       restartPolicy: Always
       serviceAccountName: rbacsync
+      volumes:
+      - name: rbacsync-gsuite-credentials
+        secret:
+          secretName: rbacsync-gsuite-credentials
       containers:
       - name: rbacsync
         image: cruise/rbacsync:latest
@@ -24,6 +28,14 @@
         - /bin/rbacsync
         args:
         - "-logtostderr"
+        - "-gsuite.enabled"
+        - "-gsuite.credentials"
+        - "/run/secrets/gsuite/token.json"
+        - "-gsuite.delegator"
+        - "my-rbacsync-robot@getcruise.com"
+        volumeMounts:
+        - mountPath: /run/secrets/gsuite
+          name: rbacsync-gsuite-credentials
         imagePullPolicy: Always
         ports:
         - name: debug-port
```

In the above, we set several flags to pass to `rbacsync`:

- `-gsuite.enabled`: enables the GSuite upstream for groups.
- `-gsuite.credentials`: path to the credentials generated for the service
  account. We map them to the secret volume mount location above.
- `-gsuite.delegator`: This is the delegator whom the service account acts on
  behalf of. We call this the "robot" account in the instructions above. Be
  sure to set the argument to whatever account you use.

> NOTE: You can also use `-gsuite.pattern` to limit which group names are sent
> upstream for queries. That takes a regular expression that must be matched
> before it will be allowed to query the upstream.

Once the above is configured and deployed, groups referenced in `bindings` will
be automatically queried against GSuite. If `memberships` have the same name as
an upstream group, the subject lists from the upstream and the `memberships`
section will be merged.

### Deployment

> WARNING: Before running any commands here, make sure you are in the right
> cluster context. This will deploy to whatever cluster kubectl is currently
> configured for.

You can deploy the rbacsync with the following command:

	kubectl apply -f deploy/

Once it has been deployed, you'll need to push a config to specify group
members using the [CRDs](deploy/00-crds.yml) defined in this project. There are
two CRDs defined depending on whether you want to create groups with cluster
scope or namespace scope. To define groups with RoleBindings within a
namespace, you'll need to create an RBACSyncConfig. To define groups that
create ClusterRoleBindings, you'll need to create ClusterRBACSyncConfig.

If the RBACSyncConfig changes, the associated RoleBindings will be updated to
reflect the difference. For example, if new group members are added, the
RoleBinding will be updated with the new subjects.

If you remove RBACSyncConfig or ClusterRBACSyncConfig, any associated
RoleBindings or ClusterRoleBindings will be removed, as well.

Development
-----------

### Building

Building is easy to do. Make sure to setup your local environment according to
https://golang.org/doc/code.html. Once setup, you should be able to build the
binaries using the following command:

```
make
```

Generated binaries will then be available in `bin/`. See the section on local
development for how to use the binary.

### Local Development

For developing on rbacsync, you can run inside or outside the cluster. For
changes with lots of iteration, it is probably best to run it locally, since
building and pushing an image to a deployment can be time consuming. The
process would use the following commands:

> WARNING: Before running any commands here, make sure you are in the right
> cluster context. This will deploy to whatever cluster kubectl is currently
> configured for.

```
kubectl apply -f deploy/10-deployment.yml
make
bin/rbacsync -logtostderr
```

The above will use your local kube context to run rbacsync, so you'll need
`cluster-admin` Role for this to work. rbacsync will fire log messages and
events complaining about permissions if it cannot create the specified roles.

When you run the process, nothing will happen unless it has CRs to operate one.
You can use the [example.yml](example.yml) to try it out:

```
kubectl apply -f example.yml
```

You'll know its working if you see log messages indicating that it saw the
configs.

In Cluster Development
----------------------

If you are working on changes that require rbacsync to be running in a cluster,
such as when checking whether operations will work correctly, you can use the
`deploy` target.  To test in a cluster, the following can be run:

	make REGISTRY=cruise/ deploy

The above will build the image, push it to a registry, apply the CRDs
and deployment to the kubernetes context and update the deployment to
use the created image. This makes iterating on testing within a cluster
much easier.

You'll need cluster admin to perform this operation.

Release Process
---------------

RBACSync is versioned using [semantic versioing](https://semver.org/). For the
most part, patch releases should be only bug fixes, minor releases can have
backwards compatible feature introductions and major releases are for breaking
changes. If you can't decide whether a feature or change is breaking, err on
the side of caution when incrementing the verion number and just do it.

The release process for rbacsync is triggered with tags. Every build in master
will pickup the tag and create a version number relative, using a git
commitish, to the tag and push it to the registry. For production, you should
only use exact tags, and, if possible, only proper releases, meaning no release
candidates, alphas or betas.

### Tagging your release

To perform a release, you simply need to push a tag to the repository. To
create a tag, run the following, with the new version number of course:

```
git tag -s v0.1.0
```

Notice that the version number is _always_ prefixed with a `v` when it appears
as a tag. This allows us to easily identify version tags with a text prefix. We
also sign the tag with a PGP key. If you haven't already done so, it is
recommended to generate a PGP key for your workstation and add it to github.
When you run this command, an editor will pop up to include a release message.
The following format is usually sufficient:

```
rbacsync 0.1.0

With this release, we introduce support for Google Groups API with
GSuite Directory SDK. This allows one to configure groups without
declared memberships that are queryed to the GSuite upstream resource.
```

There may be releases with complex information for upgrades and caveats, so
feel free to be verbose. Once you are ready, save in the editor and the tag
will be created. To push it to github, run the following command:

### Pushing your release tag

```
git push --tags origin v0.1.0
```

This will push the tag remotely, and start the build and push for rbacsync.
Make sure to check in CircleCI to ensure it was triggered.

### Create your release in Github

In addition to pushing this tag, it is also recommended to create a release in
the [releases page](https://github.com/cruise-automation/rbacsync/releases). Make
sure to name it consistently with other releases. It should be sufficient to
use the exact same text from the tag, but you may have to tweak the formatting
to be compatible with markdown.

While this is an extra step when releasing the software, it is very helpful
when looking at a project to see its releases properly documented in github.
Falso ollow this practice to have a healthy project!

# License

Copyright 2018 GM Cruise LLC

Licensed under the [Apache License Version 2.0](LICENSE) (the "License");
you may not use this project except in compliance with the License.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

# Contributions

Contributions are welcome! Please see the agreement for contributions in
[CONTRIBUTING.md](CONTRIBUTING.md).

Commits must be made with a Sign-off (`git commit -s`) certifying that you
agree to the provisions in [CONTRIBUTING.md](CONTRIBUTING.md).
