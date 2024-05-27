package main

import (
	"os"

	"github.com/wa1kman999/lantransfer/receiver"
	"github.com/wa1kman999/lantransfer/sender"
)

func main() {
	if len(os.Args) == 2 {
		sender.NewSender().Send(os.Args[1])
	} else {
		receiver.NewReceiver().Run()
	}
}
