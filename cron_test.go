package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type MockJob struct {
	duration    time.Duration
	activeCount *int32
	maxActive   *int32
	wg          *sync.WaitGroup
}

func (m *MockJob) Run() {
	if m.wg != nil {
		defer m.wg.Done()
	}

	currentActive := atomic.AddInt32(m.activeCount, 1)

	for {
		max := atomic.LoadInt32(m.maxActive)
		if currentActive <= max {
			break
		}
		if atomic.CompareAndSwapInt32(m.maxActive, max, currentActive) {
			break
		}
	}

	time.Sleep(m.duration)
	atomic.AddInt32(m.activeCount, -1)
}

/*
TestGracefulShutdown ensures that the Stop() method behaves gracefully.
When a shutdown signal is sent, the system must wait for all currently running jobs
to finish their execution completely instead of abruptly terminating them mid-process.
*/
func TestGracefulShutdown(t *testing.T) {
	sched := NewScheduler(time.UTC, 1)
	sched.startWorkers()

	var activeCount int32
	var maxActive int32

	// Instantiate a relatively long-running job (200ms)
	job := &MockJob{
		duration:    200 * time.Millisecond,
		activeCount: &activeCount,
		maxActive:   &maxActive,
	}

	// Dispatch the job asynchronously
	go func() {
		sched.jobChan <- &Entry{Job: job}
	}()

	// Allow a brief 50ms window to guarantee the worker has picked up the job and started execution
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&activeCount) != 1 {
		t.Fatal("FAIL: Job should have been active before initiating shutdown")
	}

	// Trigger the safe shutdown procedure while the job is still running in the background
	startTime := time.Now()
	sched.Stop() // This line MUST block for the remaining duration of the job (~150ms)
	durationOfStop := time.Since(startTime)

	t.Logf("Stop() method took %v to clean up and exit", durationOfStop)

	// Validation: If Stop() returned immediately (<100ms), it means it didn't wait for the job to complete
	if durationOfStop < 100*time.Millisecond {
		t.Errorf("FAIL: Sched.Stop() exited prematurely without waiting for active workloads to finish")
	}

	if atomic.LoadInt32(&activeCount) != 0 {
		t.Errorf("FAIL: Scheduler stopped but the job is still hanging/active!")
	} else {
		t.Log("PASS: Graceful shutdown successfully held off termination until active workloads finished!")
	}
}

type RealJob struct {
	executionCount *int32
}

func (rj *RealJob) Run() {
	atomic.AddInt32(rj.executionCount, 1)
}

/*
TestJob validates the end-to-end operational pipeline of the scheduler.
It tests registering a job, simulating time progression to trigger execution via the Main Loop,
dispatching it smoothly to the Worker Pool, and executing safe resource teardown.
*/
func TestJob(t *testing.T) {
	sched := NewScheduler(time.Local, 2)

	go sched.Start()

	var counter int32
	job := &RealJob{executionCount: &counter}

	// Register a cron job set to execute every minute ("*/1 *")
	jobID, err := sched.Add(nil, "Test-Integration-Job", "*/1 *", job)
	if err != nil {
		t.Fatalf("FAIL: Failed to append job to the system: %v", err)
	}
	t.Logf("PASS: Job successfully registered with ID: %s", jobID)

	// Waiting for a real minute inside an integration test is an anti-pattern.
	// We lock the system state and intentionally shift NextRun to 10ms in the past (making it instantly due).
	sched.mutex.Lock()
	entry, exists := sched.entries[jobID]
	if exists {
		entry.NextRun = time.Now().In(time.Local).Add(-10 * time.Millisecond)
	}
	sched.mutex.Unlock()

	// Wake up the Main Loop select statement by notifying the rescheduling channel
	sched.rescheduleChan <- struct{}{}

	// Allow a brief 50ms buffer window for the main loop to process, extract from heap, and hand over to workers
	time.Sleep(50 * time.Millisecond)

	// Validate whether the Worker Pool successfully intercepted and ran the job
	finalCount := atomic.LoadInt32(&counter)
	t.Logf("Job was triggered and executed: %d time(s)", finalCount)

	if finalCount == 0 {
		t.Error("FAIL: Main loop engine running but the job was never picked up by the worker pool!")
	} else {
		t.Log("PASS: The pipeline sequence [Start -> Timer Trigger -> Worker Pool Handshake] works flawlessly!")
	}

	// 6. Uji Fitur Keselamatan: Graceful Shutdown
	t.Log("Menguji fungsi Stop()...")
	sched.Stop()

	// Test safety constraints: Graceful shutdown sequence
	t.Log("Initiating Stop() sequence...")
	sched.Stop()

	// Verify that the scheduler rejects any incoming job submissions post-termination
	_, errAfterStop := sched.Add(nil, "Job-Post-Stop", "*/1 *", job)
	if errAfterStop == nil {
		t.Error("FAIL: Scheduler is stopped but the Add() method is still incorrectly accepting new jobs!")
	} else {
		t.Logf("PASS: System securely rejected post-stop submission with message: '%v'", errAfterStop)
	}
}

/*
TestRescheduleInterrupt verifies the dynamic interrupt mechanism of the scheduler.
When the main loop is sleeping waiting for a far-future job, adding a new job with a much closer/immediate
execution time must instantly interrupt the sleep, reorganize priorities, and run the closer job on time.
*/
func TestRescheduleInterrupt(t *testing.T) {
	sched := NewScheduler(time.Local, 2)
	go sched.Start()
	defer sched.Stop()

	var job1Executed int32
	var job2Executed int32

	job1 := &RealJob{executionCount: &job1Executed}
	job2 := &RealJob{executionCount: &job2Executed}

	// 1. DISPATCH FAR-FUTURE WORK (Job 1: Scheduled 1 hour from now)
	job1ID, _ := sched.Add(nil, "Job-Far-Future-1Hour", "0 *", job1)

	sched.mutex.Lock()
	if entry, ok := sched.entries[job1ID]; ok {
		entry.NextRun = time.Now().In(time.Local).Add(1 * time.Hour)
	}
	sched.mutex.Unlock()

	// Trigger reschedule channel manually once to force the main loop to calculate and go to deep sleep for 1 hour
	sched.rescheduleChan <- struct{}{}

	// Small sleep to ensure the main loop goroutine has officially entered its time.NewTimer select case block
	time.Sleep(20 * time.Millisecond)

	// 2. DISPATCH IMMEDIATE WORK (Job 2: Suddenly added mid-cycle, ready to execute instantly)
	job2ID, _ := sched.Add(nil, "Job-Immediate-Mendadak", "0 *", job2)

	sched.mutex.Lock()
	if entry, ok := sched.entries[job2ID]; ok {
		entry.NextRun = time.Now().In(time.Local).Add(-10 * time.Millisecond) // Due 10ms ago
	}
	sched.mutex.Unlock()

	// 3. TRIGGER ALARM INTERRUPT
	// This simulates the internal behavior of our Add() method which broadcasts into the channel using select-default
	select {
	case sched.rescheduleChan <- struct{}{}:
	default:
	}

	// Give a 50ms buffer time for the scheduler loop to break out of the 1-hour sleep, recalculate heap, and run Job 2
	time.Sleep(50 * time.Millisecond)

	// 4. VERIFY RESULTS
	executed1 := atomic.LoadInt32(&job1Executed)
	executed2 := atomic.LoadInt32(&job2Executed)

	t.Logf("Execution Results -> Far-Future Job 1: %d, Immediate Job 2: %d", executed1, executed2)

	if executed1 > 0 {
		t.Error("FAIL: Far-future Job 1 was executed prematurely!")
	}

	if executed2 == 0 {
		t.Error("FAIL: Scheduler failed to interrupt its sleep cycle! Immediate Job 2 was skipped or left hanging.")
	} else {
		t.Log("PASS: Main loop successfully broke out of its long timer sleep to process the high-priority immediate job!")
	}
}

/*
TestAddAfterStopDoesNotRun strictly verifies that once Stop() is called,
the system becomes completely inert. It must reject registration, and no background execution threads
or leftover resources should be allowed to process workloads.
*/
func TestAddAfterStopDoesNotRun(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	sched := NewScheduler(loc, 2)

	// Start the runtime engine and instantly execute an orderly graceful shutdown
	go sched.Start()
	sched.Stop()

	var job1Executed int32
	job1 := &RealJob{executionCount: &job1Executed}

	// Attempt to push work into a terminated system environment
	_, err := sched.Add(nil, "Job-Post-Mortem", "*/1 *", job1)

	// Verification Phase 1: Input gate defense
	if err == nil {
		t.Error("FAIL: The engine accepted work even after a shutdown sequence was finalized.")
	} else {
		t.Logf("PASS: Entry registration gateway securely rejected work submission: %v", err)
	}

	// Wait briefly to see if any illegal worker threads trigger execution leaks
	time.Sleep(50 * time.Millisecond)

	// Verification Phase 2: Active processing state check
	executed := atomic.LoadInt32(&job1Executed)
	if executed > 0 {
		t.Errorf("FAIL: Execution leak detected! Job processed %d times post-system termination.", executed)
	} else {
		t.Log("PASS: Zero background processing activity confirmed after engine shutdown.")
	}
}

/*
TestLocationTimezoneCompliance guarantees that the calculation engine
strictly honors the distinct geographical timezones specified by the user during initialization.
It protects against absolute temporal drifts, ensuring jobs trigger at local time hours correctly.
*/
func TestLocationTimezoneCompliance(t *testing.T) {
	cronSpec := "0 5" // Set to execute precisely at 05:00 AM local time daily

	// Load two entirely separate geographical timezone structures (Neither utilizes DST, keeping offsets permanent)
	jktLoc, err := time.LoadLocation("Asia/Jakarta") // UTC+7
	if err != nil {
		t.Fatalf("Failed to parse Jakarta timezone configuration: %v", err)
	}
	tokyoLoc, err := time.LoadLocation("Asia/Tokyo") // UTC+9
	if err != nil {
		t.Fatalf("Failed to parse Tokyo timezone configuration: %v", err)
	}

	// Spawn two separate isolated engine instances
	schedJKT := NewScheduler(jktLoc, 1)
	schedTYO := NewScheduler(tokyoLoc, 1)

	dummyJob := &RealJob{executionCount: new(int32)}

	// Register identical cron definitions into both schedulers
	idJKT, _ := schedJKT.Add(nil, "Job-Jakarta", cronSpec, dummyJob)
	idTYO, _ := schedTYO.Add(nil, "Job-Tokyo", cronSpec, dummyJob)

	// Safe read extraction of calculated Entry states from both entities
	schedJKT.mutex.Lock()
	entryJKT := schedJKT.entries[idJKT]
	schedJKT.mutex.Unlock()

	schedTYO.mutex.Lock()
	entryTYO := schedTYO.entries[idTYO]
	schedTYO.mutex.Unlock()

	// Verification 1: Confirm that locally within their respective locations, both match 05:00 AM
	if entryJKT.NextRun.Hour() != 5 || entryTYO.NextRun.Hour() != 5 {
		t.Errorf("FAIL: Next execution target hours must locally read 5 AM. Got JKT: %d, TYO: %d", entryJKT.NextRun.Hour(), entryTYO.NextRun.Hour())
	}

	// Verification 2: Ensure internal structural time properties are assigned the correct Location pointer references
	if entryJKT.NextRun.Location().String() != "Asia/Jakarta" || entryTYO.NextRun.Location().String() != "Asia/Tokyo" {
		t.Errorf("FAIL: Location references bound inside time profiles are corrupted.")
	}

	// Verification 3: Math Offset Validation.
	// Since Tokyo (UTC+9) is 2 hours ahead of Jakarta (UTC+7), 5:00 AM in Tokyo will occur
	// exactly 2 absolute hours BEFORE 5:00 AM in Jakarta. Thus: TimeJKT - TimeTYO = exactly 2 hours.
	diff := entryJKT.NextRun.Sub(entryTYO.NextRun)

	t.Logf("Calculated Execution Target JKT: %s", entryJKT.NextRun.String())
	t.Logf("Calculated Execution Target TYO: %s", entryTYO.NextRun.String())
	t.Logf("Absolute time difference calculated: %v", diff)

	if diff != 2*time.Hour {
		t.Errorf("FAIL: Absolute delta should be precisely 2h. Got unexpected gap value: %v", diff)
	} else {
		t.Log("PASS: Chronological engine perfectly adheres to localized geographic timezone offsets!")
	}
}

func TestInvalidJobSpec(t *testing.T) {
	cronSpec := "this will not parse"
	schedJKT := NewScheduler(nil, 1)
	dummyJob := &RealJob{executionCount: new(int32)}

	_, err := schedJKT.Add(nil, "Job", cronSpec, dummyJob)
	if err == nil {
		t.Errorf("expected an error with invalid spec, got nil")
	}
}
