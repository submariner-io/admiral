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
	"runtime"
	"strings"

	"github.com/go-logr/logr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	loga "github.com/submariner-io/admiral/pkg/log"
	"k8s.io/klog"
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
	if verbosityLevel > 0 {
		klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
		klog.InitFlags(klogFlags)
		_ = klogFlags.Parse([]string{fmt.Sprintf("-v=%d", verbosityLevel)})
	}

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
	skipFrames       int
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

func (ctx *zeroLogContext) calculateSkipFrames() int {
	if ctx.skipFrames > 0 {
		return ctx.skipFrames
	}

	skipFrames := 0
	pc := make([]uintptr, 10)   // this should be enough frames to collect
	n := runtime.Callers(2, pc) // skip runtime.Callers and this function
	if n == 0 {
		return 0
	}

	frames := runtime.CallersFrames(pc[:n])

	for {
		frame, more := frames.Next()

		// We want to skip call frames in this package and go-logr but controller-runtime may have a DelegatingLogSink
		// in between so skip that as well.
		if strings.HasPrefix(frame.Function, "github.com/submariner-io/admiral/pkg/log") ||
			strings.HasPrefix(frame.Function, "github.com/go-logr") ||
			strings.HasPrefix(frame.Function, "sigs.k8s.io/controller-runtime/pkg/log") {
			skipFrames++
		}

		if !more {
			break
		}
	}

	ctx.skipFrames = skipFrames

	return ctx.skipFrames
}

func (ctx *zeroLogContext) logEvent(evt *zerolog.Event, msg string, kvList ...interface{}) {
	msg = truncate(ctx.prefix, maxLenLogger) + " " + msg

	evt.Fields(kvList).CallerSkipFrame(ctx.calculateSkipFrames()).Msg(msg)
}

func (ctx *zeroLogContext) Info(msg string, kvList ...interface{}) {
	if ctx.currentVerbosity > ctx.maxVerbosity {
		return
	}

	var evt *zerolog.Event

	for i := 0; i < len(kvList); i += 2 {
		s, ok := kvList[i].(string)
		if ok && s == loga.WarningKey {
			kvList = append(kvList[:i], kvList[i+2:]...)
			evt = ctx.zLogger.Warn()

			break
		}
	}

	if evt == nil {
		switch {
		case ctx.currentVerbosity >= loga.TRACE:
			evt = ctx.zLogger.Trace()
		case ctx.currentVerbosity >= loga.DEBUG:
			evt = ctx.zLogger.Debug()
		default:
			evt = ctx.zLogger.Info()
		}
	}

	ctx.logEvent(evt, msg, kvList...)
}

func (ctx *zeroLogContext) Error(err error, msg string, kvList ...interface{}) {
	ctx.logEvent(ctx.zLogger.Error().Err(err), msg, kvList...)
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
