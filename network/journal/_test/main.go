package main

import (
	"log"
	"os"
	"time"

	"github.com/coreos/mantle/network/journal"
)

func main() {
	log.SetPrefix("       ")
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)

	rec := journal.NewRecorder(journal.ShortWriter(os.Stdout))
	for {
		log.Print("Starting journalctl...")
		err := rec.RunLocal()
		if err != nil {
			log.Print(err)
		}
		time.Sleep(7 * time.Second)
	}
}
