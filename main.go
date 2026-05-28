package main

import (
	"container/heap"
	"fmt"
	"log"
	"math/bits"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Cron struct {
	minMask  uint64
	hourMask uint32
}

type Job interface {
	Run()
}

type Entry struct {
	// ID is the cron-assigned ID of this entry, which may be used to look up a
	// snapshot or remove it
	ID string

	// needed for better logging
	Name string

	// schedule on which this job should be run
	Schedule *Cron

	// next time the job will run
	NextRun time.Time

	// job is the thing that was submitted to cron
	Job Job

	// index of heap for internal updates
	Index int
}

type PriorityQueue []*Entry

type CronScheduler struct {
	pq             PriorityQueue
	mutex          sync.Mutex
	entries        map[string]*Entry
	rescheduleChan chan struct{}
	stopChan       chan struct{}
	location       *time.Location
	maxWorkers     int
	jobChan        chan *Entry
	wg             sync.WaitGroup
	stopOnce       sync.Once
}

func (pq PriorityQueue) Len() int {
	return len(pq)
}

func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].NextRun.Before(pq[j].NextRun)
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*Entry)
	item.Index = n
	*pq = append(*pq, item)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.Index = -1 // moved of heap
	*pq = old[0 : n-1]
	return item
}

func (e *CronScheduler) Add(id *string, name string, spec string, job Job) (string, error) {
	// parse expr cron
	schedule, err := Parse(spec)
	if err != nil {
		return "", err
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	// check if scheduler is stop
	select {
	case <-e.stopChan:
		return "", fmt.Errorf("scheduler is stopped")
	default:
	}

	var newID string
	if id == nil {
		uuid, err := uuid.NewV7()
		if err != nil {
			return "", err
		}
		newID = uuid.String()
	}

	// create a entry
	entry := &Entry{
		ID:       newID,
		Name:     name,
		Schedule: schedule,
		Job:      job,
		NextRun:  schedule.Next(e.now()),
	}

	e.entries[newID] = entry
	heap.Push(&e.pq, entry)

	// re evaluate queue
	select {
	case e.rescheduleChan <- struct{}{}:
	default:
	}

	return newID, nil
}

func (e *CronScheduler) Remove(id string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	entry, ok := e.entries[id]
	if !ok {
		return
	}

	// remove from heap
	heap.Remove(&e.pq, entry.Index)
	// remove from map
	delete(e.entries, id)
}

func (e *CronScheduler) execute(entry *Entry) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// pass to channel jobs
	e.jobChan <- entry

	// setup next schedule
	entry.NextRun = entry.Schedule.Next(e.now())
	heap.Fix(&e.pq, entry.Index)
}

func (e *CronScheduler) Start() {
	e.startWorkers()

	for {
		e.mutex.Lock()

		if len(e.pq) == 0 {
			e.mutex.Unlock()

			// wait until get job or interrupt
			select {
			case <-e.rescheduleChan:
				continue
			case <-e.stopChan:
				return
			}
		}

		// peek next run before sleep
		nextEntry := e.pq[0]
		now := e.now()
		duration := nextEntry.NextRun.Sub(now)
		e.mutex.Unlock()

		// check if the time has comes or passed
		if duration <= 0 {
			e.execute(nextEntry)
			continue
		}

		// wait until the time has comes or until there is a interruption
		timer := time.NewTimer(duration)
		select {
		case <-timer.C:
			e.execute(nextEntry)
		case <-e.rescheduleChan:
			timer.Stop()
		case <-e.stopChan:
			timer.Stop()
			return
		}
	}
}

func (e *CronScheduler) Stop() {
	e.stopOnce.Do(func() {
		log.Println("Stopping scheduler...")

		// pass stop signal to worker
		close(e.stopChan)

		e.wg.Wait()

		// close job chan
		close(e.jobChan)

		log.Println("Scheduler stopped gracefully")
	})
}

func (e *CronScheduler) startWorkers() {
	for i := 0; i < e.maxWorkers; i++ {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			for {
				select {
				case entry, ok := <-e.jobChan:
					if !ok {
						return
					}
					e.runJob(entry)
				case <-e.stopChan:
					return
				}
			}
		}()
	}
}

func (e *CronScheduler) runJob(entry *Entry) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Worker Error] Job %s (ID: %s) panicked: %v", entry.Name, entry.ID, r)
		}
	}()

	entry.Job.Run()
}

func (s *CronScheduler) now() time.Time {
	return time.Now().In(s.location)
}

func NewScheduler(loc *time.Location, maxWorkers int) *CronScheduler {
	if loc == nil {
		loc = time.UTC
	}

	return &CronScheduler{
		pq:             make(PriorityQueue, 0),
		entries:        make(map[string]*Entry),
		rescheduleChan: make(chan struct{}, 1),
		stopChan:       make(chan struct{}),
		location:       loc,
		jobChan:        make(chan *Entry, maxWorkers),
		maxWorkers:     maxWorkers,
	}
}

func findNext(mask uint64, current int) (int, bool) {
	shiftAmount := uint(current + 1)
	if shiftAmount >= 64 {
		return 0, false
	}

	shifted := mask >> shiftAmount
	if shifted == 0 {
		return 0, false
	}

	// count how many bit 0
	diff := bits.TrailingZeros64(shifted)
	return (current + 1) + diff, true
}

func (e *Cron) Next(t time.Time) time.Time {
	next := t.Add(time.Minute).Truncate(time.Minute)

	// safety break to avoid infinite loop
	for i := 0; i < 100; i++ {
		hour := next.Hour()
		// check if current hour is valid
		if (uint64(e.hourMask) & (1 << uint32(hour))) == 0 {
			nextHour, isNext := findNext(uint64(e.hourMask), hour)

			if isNext {
				// check if hour invalid
				// then jump to next hour and reset minute to 0
				next = time.Date(next.Year(), next.Month(), next.Day(), nextHour, 0, 0, 0, next.Location())
			} else {
				// no more hour today
				// then jump to 00:00 tommorow
				firstHour, _ := findNext(uint64(e.hourMask), -1)
				next = next.AddDate(0, 0, 1)
				next = time.Date(next.Year(), next.Month(), next.Day(), firstHour, 0, 0, 0, next.Location())
			}
			continue
		}

		minute := next.Minute()
		// check if current minute is valid
		if (e.minMask & (1 << uint32(minute))) == 0 {
			nextMin, isNext := findNext(e.minMask, minute)

			if isNext {
				// check if minute invalid
				// then jump to next minute
				next = time.Date(next.Year(), next.Month(), next.Day(), next.Hour(), nextMin, 0, 0, next.Location())
			} else {
				// no more minutes today
				// then jump to next hour
				next = next.Truncate(time.Hour).Add(time.Hour)
			}
			continue
		}

		return next
	}

	return time.Time{}
}
