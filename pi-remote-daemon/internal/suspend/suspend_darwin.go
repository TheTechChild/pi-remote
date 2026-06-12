// SPDX-License-Identifier: MIT
//go:build darwin && cgo

package suspend

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <IOKit/IOMessage.h>
#include <CoreFoundation/CoreFoundation.h>

extern void goSuspendEvent(int sleeping);

static io_connect_t rootPort;
static IONotificationPortRef notifyPort;
static io_object_t notifierObject;
static CFRunLoopRef loopRef;

static void powerCallback(void *refCon, io_service_t service,
                          natural_t messageType, void *messageArgument) {
	switch (messageType) {
	case kIOMessageCanSystemSleep:
		// Never veto idle sleep; just acknowledge.
		IOAllowPowerChange(rootPort, (long)messageArgument);
		break;
	case kIOMessageSystemWillSleep:
		// Deliver to Go first: the daemon sends machine_suspending and
		// closes its WebSocket inside this call. Only then acknowledge,
		// releasing the OS to sleep (hard 30s ceiling enforced by the
		// kernel regardless).
		goSuspendEvent(1);
		IOAllowPowerChange(rootPort, (long)messageArgument);
		break;
	case kIOMessageSystemHasPoweredOn:
		goSuspendEvent(0);
		break;
	}
}

// runPowerLoop blocks running the CFRunLoop that services power
// notifications. Returns non-zero if registration failed.
static int runPowerLoop(void) {
	rootPort = IORegisterForSystemPower(NULL, &notifyPort, powerCallback, &notifierObject);
	if (rootPort == 0) {
		return -1;
	}
	loopRef = CFRunLoopGetCurrent();
	CFRunLoopAddSource(loopRef,
		IONotificationPortGetRunLoopSource(notifyPort), kCFRunLoopCommonModes);
	CFRunLoopRun(); // blocks until stopPowerLoop
	CFRunLoopRemoveSource(loopRef,
		IONotificationPortGetRunLoopSource(notifyPort), kCFRunLoopCommonModes);
	IODeregisterForSystemPower(&notifierObject);
	IOServiceClose(rootPort);
	IONotificationPortDestroy(notifyPort);
	loopRef = NULL;
	return 0;
}

static void stopPowerLoop(void) {
	if (loopRef != NULL) {
		CFRunLoopStop(loopRef);
	}
}
*/
import "C"

import (
	"context"
	"errors"
	"runtime"
)

// events carries transitions from the C callback into Watch. Buffered so
// the callback never blocks; package-level because cgo callbacks cannot
// carry Go closures through C refCon pointers safely. One watcher per
// process (enforced in Watch).
var events = make(chan bool, 4)

var watcherActive = make(chan struct{}, 1)

//export goSuspendEvent
func goSuspendEvent(sleeping C.int) {
	select {
	case events <- sleeping == 1:
	default:
	}
}

type darwinWatcher struct{}

// New returns the macOS IOKit watcher.
func New() Watcher { return darwinWatcher{} }

func (darwinWatcher) Watch(ctx context.Context, onSuspend, onResume func()) error {
	select {
	case watcherActive <- struct{}{}:
		defer func() { <-watcherActive }()
	default:
		return errors.New("suspend: watcher already active in this process")
	}

	loopErr := make(chan error, 1)
	go func() {
		// The CFRunLoop must own its thread for the lifetime of the
		// registration.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if C.runPowerLoop() != 0 {
			loopErr <- errors.New("suspend: IORegisterForSystemPower failed")
			return
		}
		loopErr <- nil
	}()

	for {
		select {
		case <-ctx.Done():
			C.stopPowerLoop()
			<-loopErr
			return nil
		case err := <-loopErr:
			return err // registration failed or loop exited unexpectedly
		case sleeping := <-events:
			if sleeping {
				onSuspend()
			} else {
				onResume()
			}
		}
	}
}
