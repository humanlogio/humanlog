package rainbow

import (
	"log"
	"os"
	"time"
)

func ExampleNew() {

	timeout := time.NewTimer(time.Second * 1)
	ticker := time.NewTicker(time.Millisecond * 10)

	log.SetOutput(New(os.Stderr, 252, 255, 43))

	for t := range ticker.C {

		log.Printf("it's %v", t)
		select {
		case <-timeout.C:
			log.Print("stopping!")
			return
		default:
		}
	}

	// Output:
	//
}
