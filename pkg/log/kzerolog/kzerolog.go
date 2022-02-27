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

package kzerolog

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	maxLenLogger = 20
	maxLenCaller = 25
)

var verbosityLevel = 0

// AddFlags register command line options for zerolog-based logging. Should be called before InitK8sLogging.
//goland:noinspection GoUnusedExportedFunction
func AddFlags(flagset *flag.FlagSet) {
	if flagset == nil {
		flagset = flag.CommandLine
	}

	flagset.IntVar(&verbosityLevel, "v", verbosityLevel,
		"number for the log level verbosity (higher is more verbose)")

	// avoid runtime error when klog's alsologtostderr option is enabled for the container
	// this is the default in most of the submariner container command.
	flagset.Bool("alsologtostderr", false, "unused - backwards compatibility for klog")
}

// InitK8sLogging initializes a human friendly zerolog logger as the concrete logr.Logger
// implementation in use by controller-runtime.
//goland:noinspection GoUnusedExportedFunction
func InitK8sLogging() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zeroLogger := createLogger()
	logAdapter := newAdapter(&zeroLogger, verbosityLevel)
	logf.SetLogger(logAdapter)
}

func createLogger() zerolog.Logger {
	consoleWriter := &zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "2006-01-02T15:04:05.000Z07:00"}
	consoleWriter.FormatCaller = formatCaller

	return log.Output(consoleWriter).With().Caller().Logger()
}

func newAdapter(zeroLogger *zerolog.Logger, maxVerbosityLevel int) logr.Logger {
	return &zeroLogContext{
		zLogger:          zeroLogger,
		prefix:           "",
		currentVerbosity: 0,
		maxVerbosity:     maxVerbosityLevel,
	}
}

func formatCaller(i interface{}) string {
	return truncate(i, maxLenCaller)
}

type zeroLogContext struct {
	zLogger          *zerolog.Logger
	prefix           string
	currentVerbosity int
	maxVerbosity     int
}

func (ctx *zeroLogContext) clone() zeroLogContext {
	return zeroLogContext{
		zLogger:          ctx.zLogger,
		prefix:           ctx.prefix,
		maxVerbosity:     ctx.maxVerbosity,
		currentVerbosity: ctx.currentVerbosity,
	}
}

func truncate(i interface{}, maxLen int) string {
	s := fmt.Sprintf("%s", i)
	if len(s) > maxLen {
		s = ".." + s[len(s)-maxLen+2:]
	}

	padFmtStr := fmt.Sprintf("%%-%ds", maxLen)

	return fmt.Sprintf(padFmtStr, s)
}

func (ctx *zeroLogContext) logEvent(evt *zerolog.Event, msg string, kvList ...interface{}) {
	msg = truncate(ctx.prefix, maxLenLogger) + " " + msg
	evt.Fields(kvList).CallerSkipFrame(2).Msg(msg)
}

func (ctx *zeroLogContext) Info(msg string, kvList ...interface{}) {
	if ctx.currentVerbosity > ctx.maxVerbosity {
		return
	}

	evt := ctx.zLogger.Info()
	ctx.logEvent(evt, msg, kvList...)
}

func (ctx *zeroLogContext) Error(err error, msg string, kvList ...interface{}) {
	if ctx.currentVerbosity > ctx.maxVerbosity {
		return
	}

	evt := ctx.zLogger.Err(err)
	ctx.logEvent(evt, msg, kvList...)
}

func (ctx *zeroLogContext) Enabled() bool {
	return true
}

func (ctx *zeroLogContext) V(level int) logr.Logger {
	subCtx := ctx.clone()
	subCtx.currentVerbosity = level

	return &subCtx
}

func (ctx *zeroLogContext) WithName(name string) logr.Logger {
	subCtx := ctx.clone()
	if len(ctx.prefix) > 0 {
		subCtx.prefix = ctx.prefix + "/"
	}

	subCtx.prefix += name

	return &subCtx
}

func (ctx *zeroLogContext) WithValues(kvList ...interface{}) logr.Logger {
	subCtx := ctx.clone()
	logger := ctx.zLogger.With().Fields(kvList).Logger()
	subCtx.zLogger = &logger

	return &subCtx
}
