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

package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/submariner-io/admiral/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var logger = log.Logger{Logger: logf.Log}

type EndpointType int

const (
	Metrics EndpointType = 1 << iota
	Profile
	all = Metrics | Profile
)

type StopFunc func()

// StartServer starts an HTTP server providing access to Prometheus metrics
// and/or profiling information.
// endpoints specifies the endpoints to provide; it can be Metrics, Profile, or both (ored).
// port specifies the TCP port on which the server listens.
// The returned function should be called to shut down the server, typically at process exit
// using defer in the main() function.
func StartServer(endpoints EndpointType, port int) StopFunc {
	if endpoints&all == 0 {
		return func() {}
	}

	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), ReadHeaderTimeout: 60 * time.Second}

	if endpoints&Metrics != 0 {
		http.Handle("/metrics", promhttp.Handler())
	}

	if endpoints&Profile != 0 {
		http.HandleFunc("/debug", pprof.Profile)
	}

	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf(err, "Error starting metrics/profile HTTP server")
		}
	}()

	return func() {
		if err := srv.Shutdown(context.TODO()); err != nil {
			logger.Errorf(err, "Error shutting down metrics/profile HTTP server")
		}
	}
}
