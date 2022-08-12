// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package taskrunner

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apply/cache"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	pollevent "sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/kstatus/watcher"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// NewTaskStatusRunner returns a new TaskStatusRunner.
func NewTaskStatusRunner(identifiers object.ObjMetadataSet, statusWatcher watcher.StatusWatcher) *TaskStatusRunner {
	return &TaskStatusRunner{
		Identifiers:   identifiers,
		StatusWatcher: statusWatcher,
	}
}

// TaskStatusRunner is a taskRunner that executes a set of
// tasks while at the same time uses the statusPoller to
// keep track of the status of the resources.
type TaskStatusRunner struct {
	Identifiers   object.ObjMetadataSet
	StatusWatcher watcher.StatusWatcher
}

// Options defines properties that is passed along to
// the statusPoller.
type Options struct {
	EmitStatusEvents bool
}

// Run executes the tasks in the taskqueue, with the statusPoller running in the
// background.
//
// The tasks run in a loop where a single goroutine will process events from
// three different channels.
// - taskQueue is read to allow updating the task queue at runtime.
// - statusChannel is read to allow updates to the resource cache and triggering
//   validation of wait conditions.
// - eventChannel is written to with events based on status updates, if
//   emitStatusEvents is true.
func (tsr *TaskStatusRunner) Run(
	ctx context.Context,
	taskContext *TaskContext,
	taskQueue chan Task,
	opts Options,
) error {
	// Give the poller its own context and run it in the background.
	// If taskStatusRunner.Run is cancelled, baseRunner.run will exit early,
	// causing the poller to be cancelled.
	statusCtx, cancelFunc := context.WithCancel(context.Background())
	statusChannel := tsr.StatusWatcher.Watch(statusCtx, tsr.Identifiers, watcher.Options{})

	// complete stops the statusPoller, drains the statusChannel, and returns
	// the provided error.
	// Run this before returning!
	// Avoid using defer, otherwise the statusPoller will hang. It needs to be
	// drained synchronously before return, instead of asynchronously after.
	complete := func(err error) error {
		klog.V(7).Info("Runner cancelled status watcher")
		cancelFunc()
		for statusEvent := range statusChannel {
			klog.V(7).Infof("Runner ignored status event: %v", statusEvent)
		}
		return err
	}

	// Wait until the StatusWatcher is sychronized to start the first task.
	var currentTask Task
	done := false

	// abort is used to signal that something has failed, and
	// the task processing should end as soon as is possible. Only
	// wait tasks can be interrupted, so for all other tasks we need
	// to wait for the currently running one to finish before we can
	// exit.
	abort := false
	var abortReason error

	// We do this so we can set the doneCh to a nil channel after
	// it has been closed. This is needed to avoid a busy loop.
	doneCh := ctx.Done()

	for {
		select {
		// This processes status events from a channel, most likely
		// driven by the StatusPoller. All normal resource status update
		// events are passed through to the eventChannel. This means
		// that listeners of the eventChannel will get updates on status
		// even while other tasks (like apply tasks) are running.
		case statusEvent, ok := <-statusChannel:
			// If the statusChannel has closed or we are preparing
			// to abort the task processing, we just ignore all
			// statusEvents.
			// TODO(mortent): Check if a closed statusChannel might
			// create a busy loop here.
			if !ok {
				continue
			}

			if abort {
				klog.V(7).Infof("Runner ignored status event: %v", statusEvent)
				continue
			}
			klog.V(7).Infof("Runner received status event: %v", statusEvent)

			// An error event on the statusChannel means the StatusWatcher
			// has encountered a problem so it can't continue. This means
			// the statusChannel will be closed soon.
			if statusEvent.Type == pollevent.ErrorEvent {
				abort = true
				abortReason = fmt.Errorf("polling for status failed: %v",
					statusEvent.Error)
				if currentTask != nil {
					currentTask.Cancel(taskContext)
				} else {
					// tasks not started yet - abort now
					return complete(abortReason)
				}
				continue
			}

			// The StatusWatcher is synchronized.
			// Tasks may commence!
			if statusEvent.Type == pollevent.SyncEvent {
				// Find and start the first task in the queue.
				currentTask, done = nextTask(taskQueue, taskContext)
				if done {
					return complete(nil)
				}
				continue
			}

			if opts.EmitStatusEvents {
				// Forward all normal events to the eventChannel
				taskContext.SendEvent(event.Event{
					Type: event.StatusType,
					StatusEvent: event.StatusEvent{
						Identifier:       statusEvent.Resource.Identifier,
						PollResourceInfo: statusEvent.Resource,
						Resource:         statusEvent.Resource.Resource,
						Error:            statusEvent.Error,
					},
				})
			}

			id := statusEvent.Resource.Identifier

			// Update the cache to track the latest resource spec & status.
			// Status is computed from the resource on-demand.
			// Warning: Resource may be nil!
			taskContext.ResourceCache().Put(id, cache.ResourceStatus{
				Resource:      statusEvent.Resource.Resource,
				Status:        statusEvent.Resource.Status,
				StatusMessage: statusEvent.Resource.Message,
			})

			// send a status update to the running task, but only if the status
			// has changed and the task is tracking the object.
			if currentTask != nil {
				if currentTask.Identifiers().Contains(id) {
					currentTask.StatusUpdate(taskContext, id)
				}
			}
		// A message on the taskChannel means that the current task
		// has either completed or failed.
		// If it has failed, we return the error.
		// If the abort flag is true, which means something
		// else has gone wrong and we are waiting for the current task to
		// finish, we exit.
		// If everything is ok, we fetch and start the next task.
		case msg := <-taskContext.TaskChannel():
			taskContext.SendEvent(event.Event{
				Type: event.ActionGroupType,
				ActionGroupEvent: event.ActionGroupEvent{
					GroupName: currentTask.Name(),
					Action:    currentTask.Action(),
					Status:    event.Finished,
				},
			})
			if msg.Err != nil {
				return complete(
					fmt.Errorf("task failed (action: %q, name: %q): %w",
						currentTask.Action(), currentTask.Name(), msg.Err))
			}
			if abort {
				return complete(abortReason)
			}
			currentTask, done = nextTask(taskQueue, taskContext)
			// If there are no more tasks, we are done. So just
			// return.
			if done {
				return complete(nil)
			}
		// The doneCh will be closed if the passed in context is cancelled.
		// If so, we just set the abort flag and wait for the currently running
		// task to complete before we exit.
		case <-doneCh:
			doneCh = nil // Set doneCh to nil so we don't enter a busy loop.
			abort = true
			abortReason = ctx.Err() // always non-nil when doneCh is closed
			klog.V(7).Infof("Runner aborting: %v", abortReason)
			if currentTask != nil {
				currentTask.Cancel(taskContext)
			} else {
				// tasks not started yet - abort now
				return complete(abortReason)
			}
		}
	}
}

// nextTask fetches the latest task from the taskQueue and
// starts it. If the taskQueue is empty, it the second
// return value will be true.
func nextTask(taskQueue chan Task, taskContext *TaskContext) (Task, bool) {
	var tsk Task
	select {
	// If there is any tasks left in the queue, this
	// case statement will be executed.
	case t := <-taskQueue:
		tsk = t
	default:
		// Only happens when the channel is empty.
		return nil, true
	}

	taskContext.SendEvent(event.Event{
		Type: event.ActionGroupType,
		ActionGroupEvent: event.ActionGroupEvent{
			GroupName: tsk.Name(),
			Action:    tsk.Action(),
			Status:    event.Started,
		},
	})

	tsk.Start(taskContext)

	return tsk, false
}

// TaskResult is the type returned from tasks once they have completed
// or failed. If it has failed or timed out, the Err property will be
// set.
type TaskResult struct {
	Err error
}
