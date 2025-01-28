package idle_test

import (
	"github.com/MatthiasKunnen/system/pkg/idle"
	"log"
	"time"
)

func Example() {
	m, dispatch, err := idle.NewWaylandIdleController()
	if err != nil {
		log.Fatalf("Unable to initialize wayland idle controller: %v", err)
	}

	monitorIdle := make(chan struct{})
	monitorResume := make(chan struct{})

	_, err = m.AddNotification(&idle.CreateIdleNotification{
		Duration: 5 * time.Second,
		Idle:     monitorIdle,
		Resume:   monitorResume,
	})
	if err != nil {
		log.Fatalf("Failed to add idle notification: %v", err)
	}

	for {
		select {
		case dispatchFunc := <-dispatch:
			err := dispatchFunc()
			if err != nil {
				log.Printf("Dispatch error: %v\n", err)
			}
		case <-monitorResume:
			log.Printf("Monitor resume\n")
			// turn on monitor here
		case <-monitorIdle:
			log.Printf("Monitor idle\n")
			// turn off monitor here
		}
	}
}
