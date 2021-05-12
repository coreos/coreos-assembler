//+build linux
//+build systemd

package libpod

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/containers/podman/v3/libpod/define"
	"github.com/containers/podman/v3/libpod/logs"
	journal "github.com/coreos/go-systemd/v22/sdjournal"
	"github.com/hpcloud/tail/watch"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// journaldLogOut is the journald priority signifying stdout
	journaldLogOut = "6"

	// journaldLogErr is the journald priority signifying stderr
	journaldLogErr = "3"

	// bufLen is the length of the buffer to read from a k8s-file
	// formatted log line
	// let's set it as 2k just to be safe if k8s-file format ever changes
	bufLen = 16384
)

func (c *Container) readFromJournal(ctx context.Context, options *logs.LogOptions, logChannel chan *logs.LogLine) error {
	var config journal.JournalReaderConfig
	if options.Tail < 0 {
		config.NumFromTail = 0
	} else if options.Tail == 0 {
		config.NumFromTail = math.MaxUint64
	} else {
		config.NumFromTail = uint64(options.Tail)
	}
	if options.Multi {
		config.Formatter = journalFormatterWithID
	} else {
		config.Formatter = journalFormatter
	}
	defaultTime := time.Time{}
	if options.Since != defaultTime {
		// coreos/go-systemd/sdjournal doesn't correctly handle requests for data in the future
		// return nothing instead of falsely printing
		if time.Now().Before(options.Since) {
			return nil
		}
		// coreos/go-systemd/sdjournal expects a negative time.Duration for times in the past
		config.Since = -time.Since(options.Since)
	}
	config.Matches = append(config.Matches, journal.Match{
		Field: "CONTAINER_ID_FULL",
		Value: c.ID(),
	})
	options.WaitGroup.Add(1)

	r, err := journal.NewJournalReader(config)
	if err != nil {
		return err
	}
	if r == nil {
		return errors.Errorf("journal reader creation failed")
	}
	if options.Tail == math.MaxInt64 {
		r.Rewind()
	}
	state, err := c.State()
	if err != nil {
		return err
	}

	if options.Follow && state == define.ContainerStateRunning {
		go func() {
			done := make(chan bool)
			until := make(chan time.Time)
			go func() {
				select {
				case <-ctx.Done():
					until <- time.Time{}
				case <-done:
					// nothing to do anymore
				}
			}()
			go func() {
				for {
					state, err := c.State()
					if err != nil {
						until <- time.Time{}
						logrus.Error(err)
						break
					}
					time.Sleep(watch.POLL_DURATION)
					if state != define.ContainerStateRunning && state != define.ContainerStatePaused {
						until <- time.Time{}
						break
					}
				}
			}()
			follower := FollowBuffer{logChannel}
			err := r.Follow(until, follower)
			if err != nil {
				logrus.Debugf(err.Error())
			}
			r.Close()
			options.WaitGroup.Done()
			done <- true
			return
		}()
		return nil
	}

	go func() {
		bytes := make([]byte, bufLen)
		// /me complains about no do-while in go
		ec, err := r.Read(bytes)
		for ec != 0 && err == nil {
			// because we are reusing bytes, we need to make
			// sure the old data doesn't get into the new line
			bytestr := string(bytes[:ec])
			logLine, err2 := logs.NewLogLine(bytestr)
			if err2 != nil {
				logrus.Error(err2)
				continue
			}
			logChannel <- logLine
			ec, err = r.Read(bytes)
		}
		if err != nil && err != io.EOF {
			logrus.Error(err)
		}
		r.Close()
		options.WaitGroup.Done()
	}()
	return nil
}

func journalFormatterWithID(entry *journal.JournalEntry) (string, error) {
	output, err := formatterPrefix(entry)
	if err != nil {
		return "", err
	}

	id, ok := entry.Fields["CONTAINER_ID_FULL"]
	if !ok {
		return "", fmt.Errorf("no CONTAINER_ID_FULL field present in journal entry")
	}
	if len(id) > 12 {
		id = id[:12]
	}
	output += fmt.Sprintf("%s ", id)
	// Append message
	msg, err := formatterMessage(entry)
	if err != nil {
		return "", err
	}
	output += msg
	return output, nil
}

func journalFormatter(entry *journal.JournalEntry) (string, error) {
	output, err := formatterPrefix(entry)
	if err != nil {
		return "", err
	}
	// Append message
	msg, err := formatterMessage(entry)
	if err != nil {
		return "", err
	}
	output += msg
	return output, nil
}

func formatterPrefix(entry *journal.JournalEntry) (string, error) {
	usec := entry.RealtimeTimestamp
	tsString := time.Unix(0, int64(usec)*int64(time.Microsecond)).Format(logs.LogTimeFormat)
	output := fmt.Sprintf("%s ", tsString)
	priority, ok := entry.Fields["PRIORITY"]
	if !ok {
		return "", errors.Errorf("no PRIORITY field present in journal entry")
	}
	if priority == journaldLogOut {
		output += "stdout "
	} else if priority == journaldLogErr {
		output += "stderr "
	} else {
		return "", errors.Errorf("unexpected PRIORITY field in journal entry")
	}

	// if CONTAINER_PARTIAL_MESSAGE is defined, the log type is "P"
	if _, ok := entry.Fields["CONTAINER_PARTIAL_MESSAGE"]; ok {
		output += fmt.Sprintf("%s ", logs.PartialLogType)
	} else {
		output += fmt.Sprintf("%s ", logs.FullLogType)
	}

	return output, nil
}

func formatterMessage(entry *journal.JournalEntry) (string, error) {
	// Finally, append the message
	msg, ok := entry.Fields["MESSAGE"]
	if !ok {
		return "", fmt.Errorf("no MESSAGE field present in journal entry")
	}
	return msg, nil
}

type FollowBuffer struct {
	logChannel chan *logs.LogLine
}

func (f FollowBuffer) Write(p []byte) (int, error) {
	bytestr := string(p)
	logLine, err := logs.NewLogLine(bytestr)
	if err != nil {
		return -1, err
	}
	f.logChannel <- logLine
	return len(p), nil
}
