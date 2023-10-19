/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

package test

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type LeaderElectionSupport struct {
	leasesReactor *fake.FailOnActionReactor
	kubeClient    *k8sfake.Clientset
	namespace     string
	lockName      string
}

func NewLeaderElectionSupport(kubeClient *k8sfake.Clientset, namespace, lockName string) *LeaderElectionSupport {
	l := &LeaderElectionSupport{
		kubeClient: kubeClient,
		namespace:  namespace,
		lockName:   lockName,
	}

	l.leasesReactor = fake.FailOnAction(&l.kubeClient.Fake, "leases", "update", nil, false)
	l.leasesReactor.Fail(false)

	return l
}

func (l *LeaderElectionSupport) FailLease(renewDeadline time.Duration) {
	l.leasesReactor.Fail(true)

	// Wait enough time for the renewal deadline to be reached
	time.Sleep(renewDeadline + (renewDeadline / 2))
}

func (l *LeaderElectionSupport) SucceedLease() {
	l.leasesReactor.Fail(false)
}

func (l *LeaderElectionSupport) GetRecord() *resourcelock.LeaderElectionRecord {
	lock, err := resourcelock.New(resourcelock.LeasesResourceLock, l.namespace, l.lockName,
		l.kubeClient.CoreV1(), l.kubeClient.CoordinationV1(), resourcelock.ResourceLockConfig{})
	Expect(err).To(Succeed())

	le, _, err := lock.Get(context.Background())
	if apierrors.IsNotFound(err) {
		return nil
	}

	Expect(err).To(Succeed())

	return le
}

func (l *LeaderElectionSupport) AwaitLeaseAcquired() {
	Eventually(func() string {
		le := l.GetRecord()
		if le == nil {
			return ""
		}

		return le.HolderIdentity
	}, 3).ShouldNot(BeEmpty(), "Leader lock was not acquired")
}

func (l *LeaderElectionSupport) EnsureLeaseNotAcquired() {
	Consistently(func() any {
		return l.GetRecord()
	}, 300*time.Millisecond).Should(BeNil(), "Leader lock was acquired")
}

func (l *LeaderElectionSupport) AwaitLeaseReleased() {
	Eventually(func() string {
		le := l.GetRecord()
		Expect(le).ToNot(BeNil(), "LeaderElectionRecord not found")

		return le.HolderIdentity
	}, 3).Should(BeEmpty(), "Leader lock was not released")
}

func (l *LeaderElectionSupport) AwaitLeaseRenewed() {
	now := metav1.NewTime(time.Now())

	Eventually(func() int64 {
		return l.GetRecord().RenewTime.UnixNano()
	}).Should(BeNumerically(">=", now.UnixNano()), "Lease was not renewed")
}
