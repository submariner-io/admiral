/*
© 2020 Red Hat, Inc.

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

// Log levels defined for use with:
//   klog.V(__).Info

const (
	// INFO : This level is for anything which does not happen very often, and while
	//        not being an error, is important information and helpful to be in the
	//        logs, eg:
	//          * startup information
	//          * HA failovers
	//          * re-connections/disconnections
	//          * ...
	//        This level is not specifically defined as you would use the
	//        klog.Info helpers
	//
	// DEBUG : used to provide logs for often occurring events that could be helpful
	//        for debugging errors.
	DEBUG = 2
	// LIBDEBUG:  like DEBUG but for submariner internal libraries like admiral.
	LIBDEBUG = 3
	// TRACE : used for logging that occurs often or may dump a lot of information
	//         which generally would be less useful for debugging but can be useful
	//         in some cases, for example tracing function entry/exit, parameters,
	//         structures, etc..
	TRACE = 4
	// LIBTRACE:  like TRACE but for submariner internal libraries like admiral.
	LIBTRACE = 5
)
