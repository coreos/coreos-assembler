package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/coreos/mantle/network/journal"
)

func main() {
	log.SetPrefix("       ")
	log.SetFlags(log.Lmicroseconds | log.Lshortfile)

	bac := context.Background()
	rec := journal.NewRecorder(journal.ShortWriter(os.Stdout))
	for {
		log.Print("Starting journalctl...")
		ctx, cancel := context.WithTimeout(bac, 7*time.Second)
		err := rec.RunLocal(ctx)
		if err != nil {
			log.Print(err)
		}
		cancel()
		time.Sleep(7 * time.Second)
	}
}
