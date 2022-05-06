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

package log

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
)

const (
	WarningKey = "WARNING"
	FatalKey   = "FATAL"
)

type Logger struct {
	logr.Logger
}

func (l Logger) Info(msg string, keysAndValues ...interface{}) {
	l.Logger.Info(msg, keysAndValues...)
}

func (l Logger) Infof(format string, args ...interface{}) {
	l.Logger.Info(fmt.Sprintf(format, args...))
}

func (l Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.Logger.Error(err, msg, keysAndValues...)
}

func (l Logger) Errorf(err error, format string, args ...interface{}) {
	l.Logger.Error(err, fmt.Sprintf(format, args...))
}

func (l Logger) Warning(msg string, keysAndValues ...interface{}) {
	l.Logger.Info(msg, append(keysAndValues, WarningKey, "true")...)
}

func (l Logger) Warningf(format string, args ...interface{}) {
	l.Logger.Info(fmt.Sprintf(format, args...), WarningKey, "true")
}

func (l Logger) Fatal(msg string, keysAndValues ...interface{}) {
	l.Logger.Error(nil, msg, append(keysAndValues, FatalKey, "true")...)
	os.Exit(255)
}

func (l Logger) Fatalf(format string, args ...interface{}) {
	l.Fatal(fmt.Sprintf(format, args...))
}

func (l Logger) FatalOnError(err error, msg string, keysAndValues ...interface{}) {
	if err == nil {
		return
	}

	l.Logger.Error(err, msg, append(keysAndValues, FatalKey, "true")...)
	os.Exit(255)
}

func (l Logger) FatalfOnError(err error, format string, args ...interface{}) {
	l.FatalOnError(err, fmt.Sprintf(format, args...))
}

func (l Logger) V(level int) Logger {
	l.Logger = l.Logger.V(level)
	return l
}
