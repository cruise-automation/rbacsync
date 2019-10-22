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

package gsuite

import (
	"context"
	"io/ioutil"
	"net/http"
	"regexp"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	admin "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/googleapi"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/cruise-automation/rbacsync/pkg/groups"
	"github.com/cruise-automation/rbacsync/pkg/metrics"
)

const (
	defaultTimeout = 30 * time.Second
)

type Grouper struct {
	config  *jwt.Config
	timeout time.Duration // timeout for whole operation
	re      *regexp.Regexp
}

// NewGrouper returns a grouper initialized with the jwt.Config from
// credentialsPath.
//
// The delagator is the account on whose behalf the service account should
// operate. The client id must be registered with that account so that it may
// act on its behalf.
//
// Pattern, it non-empty, is a regexp pattern accounts must match before
// forwarding to the API. Accounts that do not match the pattern will return
// ErrNotFound.
//
// The timeout will set a context timeout for a fetch of all pages for each
// group.
func NewGrouper(credentialsPath string, delegator string, pattern string, timeout time.Duration) (*Grouper, error) {
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return nil, errors.Wrapf(err, "could not compile %q as pattern for gsuite grouper", pattern)
		}
	}

	b, err := ioutil.ReadFile(credentialsPath)
	if err != nil {
		return nil, err
	}

	config, err := google.JWTConfigFromJSON(b, admin.AdminDirectoryGroupMemberReadonlyScope, admin.AdminDirectoryGroupReadonlyScope)
	if err != nil {
		return nil, err
	}
	config.Subject = delegator // MUST be set to the account that has delegated access to the service account.

	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &Grouper{
		config:  config,
		timeout: timeout,
		re:      re,
	}, nil
}

// Members returns the subjects for the give group string, where the group is
// defined in gsuite.
//
// If a pattern is set and group does not match, ErrNotFound will be returned.
func (g *Grouper) Members(group string) ([]rbacv1.Subject, error) {
	if g.re != nil && !g.re.MatchString(group) {
		return nil, errors.Wrapf(groups.ErrNotFound,
			"group %q does not match %q, ignored for gsuite lookup",
			group, g.re.String())
	}

	// TODO(sday): We may want to plumb context through the Grouper.Members
	// function in the future. Right now, the controller itself is not
	// context-integrated so the value of this would be minimal. We'd need to
	// add a context that propagates done when the stopCh is closed.
	//
	// If we decide this isn't worthwhile because it doesn't matter
	// operational, we can just change this to context.Background() and remove
	// this TODO.
	ctx := context.TODO()
	client, err := g.service(ctx)
	if err != nil {
		return nil, errors.Wrapf(groups.ErrUnknown,
			"unable to determine group members, an error occurred creating gsuite client: %v",
			err)
	}
	metrics.RBACSyncGsuiteClientCreationStatus.Inc()

	var (
		tctx, cancel = context.WithTimeout(ctx, g.timeout)
		subjects     []rbacv1.Subject
	)
	defer cancel()

	startTime := time.Now()
	if err := client.Members.List(group).
		IncludeDerivedMembership(true).
		Pages(tctx, func(members *admin.Members) error {
			for _, member := range members.Members {
				subjects = append(subjects, rbacv1.Subject{
					Kind:     "User",
					Name:     member.Email,
					APIGroup: "rbac.authorization.k8s.io",
				})
			}
			return nil
		}); err != nil {

		switch {
		case isNotFound(err):
			metrics.RBACSyncGsuiteMembersStatus.WithLabelValues("NotFound").Inc()
			return nil, errors.Wrapf(groups.ErrNotFound, "gsuite does not have group: %v", err)
		case isTimeout(tctx):
			metrics.RBACSyncGsuiteMembersStatus.WithLabelValues("Timeout").Inc()
			return nil, errors.Wrapf(groups.ErrTimeout, "timeout calling gsuite api: %v", err)
		case isCanceled(tctx):
			metrics.RBACSyncGsuiteMembersStatus.WithLabelValues("Cancled").Inc()
			return nil, errors.Wrapf(groups.ErrCanceled, "the context canceled the call to gsuite: %v", err)
		default:
			metrics.RBACSyncGsuiteMembersStatus.WithLabelValues("Unknown").Inc()
			return nil, errors.Wrapf(groups.ErrUnknown, "error retrieving group members: %v", err)
		}

	}
	metrics.RBACSyncGsuiteMembersLatency.Observe(
		float64(time.Since(startTime).Nanoseconds()) / float64(time.Second),
	)
	metrics.RBACSyncGsuiteMembersStatus.WithLabelValues("OK").Inc()

	// If you're trying to find some nasty memory allocation, it might be here.
	// Grouper interface should be converted to callback style if this is a
	// concern. In practice, most groups should be smallish.
	return groups.Normalize(subjects), nil
}

func (g *Grouper) service(ctx context.Context) (*admin.Service, error) {
	return admin.New(g.config.Client(ctx))
}

func isNotFound(err error) bool {
	e, ok := err.(*googleapi.Error)
	return ok && e.Code == http.StatusNotFound
}

func isTimeout(ctx context.Context) bool {
	e := ctx.Err()
	return e == context.DeadlineExceeded
}

func isCanceled(ctx context.Context) bool {
	e := ctx.Err()
	return e == context.Canceled
}
