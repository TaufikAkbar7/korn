# Korn v0.0.1

A lightweight, high-performance, and thread-safe in-memory cron scheduling engine built from scratch in Go. **For now only accept scheduler format minute (0 - 59) and hour (0 - 23)**

---

## Key Features

* **Panic-Safe Execution Isolation:** If an individual job throws a runtime panic (e.g., an unexpected nil-pointer dereference inside a reporting routine), the worker captures it via `recover()`, logs it, and continues processing other scheduled tasks.
* **Dynamic Alarm Interruption (Rescheduling):** Adding an urgent task with an immediate execution timeline will dynamically wake up the main engine loop from its timer-sleep state to reorganize structural priorities instantaneously.
* **Timezone & Geography Compliance:** Fully respects explicit `time.Location` configurations, making it completely immune to absolute temporal drifts or unexpected UTC conversions across localized infrastructures.
* **Graceful Shutdown Sequence:** Listens to termination signals and safely coordinates a structural exit sequence—waiting for currently active workers to completely finalize their database writes before closing up shop.

---

## Scheduler cron format
```
 ┌───────────── minute (0 - 59)
 │ ┌─────────── hour (0 - 23)
 │ │
 * * 
```
### Expressions Examples:
- */15 : Executes every 15 minutes.

- 0 7 : Executes exactly at 07:00 AM everyday (Perfect for Shift 1 initializations).

---

## Example
```
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// 1. Configure the geographic context (e.g., Asia/Jakarta UTC+7)
	jakartaTimezone, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		log.Fatalf("Failed to initialize timezone: %v", err)
	}

	// 2. Spawn the scheduler allocating a max concurrency limit of 5 workers
	scheduler := NewScheduler(jakartaTimezone, 5)

	// 3. Kick off the main engine loop in the background
	go scheduler.Start()
	log.Println("Scheduler engine is running successfully...")

	// 4. Register a standard minute/hour level cron job
	// Expression: "*/5 *" -> Executes every 5 minutes
	reportJob := &ProductionReportJob{PlantID: "PLANT-1"}
	jobID, err := scheduler.Add("Hourly-Wirerod-Report", "*/5 *", reportJob)
	if err != nil {
		log.Fatalf("Failed to register job: %v", err)
	}
	log.Printf("Job successfully registered with ID: %s", jobID)

	// 5. Establish an OS Signal trap for Graceful Teardown
	stopSignal := make(chan os.Signal, 1)
	signal.Notify(stopSignal, syscall.SIGINT, syscall.SIGTERM)

	// Block main routine thread execution until a terminal signal is caught
	<-stopSignal

	// 6. Execute safe shutdown routine
	log.Println("Termination signal captured. Initiating safe shutdown sequence...")
	scheduler.Stop()
	log.Println("Process finished.")
}
```

