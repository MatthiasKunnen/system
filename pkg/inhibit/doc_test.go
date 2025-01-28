package inhibit_test

import (
	"github.com/MatthiasKunnen/system/pkg/inhibit"
	"io"
	"log"
)

func Example() {
	inhibitor, err := inhibit.New()
	if err != nil {
		log.Fatalf("Failed to initialize inhibitor: %v", err)
	}

	prepareForSleep := make(chan bool, 1)

	err = inhibitor.SubscribePrepareForSleep(prepareForSleep)
	if err != nil {
		log.Fatalf("Unable to subscribe to PrepareForSleep: %v", err)
	}

	var sleepInhibitor io.Closer
	inhibitSleep := func() {
		var err error
		sleepInhibitor, err = inhibitor.Inhibit("Name of program", "Reason of delaying", inhibit.ModeDelay, inhibit.WhatSleep)
		if err != nil {
			log.Printf("Unable to acquire sleep inhibition lock: %v", err)
		}
	}
	inhibitSleep()

	for {
		select {
		case goSleep := <-prepareForSleep:
			if goSleep {
				log.Printf("System wants to sleep, do our work, then, allow the system to lock\n")
				if sleepInhibitor != nil {
					err := sleepInhibitor.Close()
					if err != nil {
						// This shouldn't occur
						log.Printf("Failed to release inhibitor lock: %v", err)
					}
				}
			} else {
				log.Printf("System is back from sleep\n")
				// Get a new inhibition lock for the next sleep attempt
				inhibitSleep()
			}
		}
	}
}
