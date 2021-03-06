package sse

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//SSE name constants
const (
	eName = "event"
	dName = "data"
	rName = "retry"
	iName = "id"

	defaultWait = 1000 * time.Millisecond
)

var (
	//ErrNilChan will be returned by Notify if it is passed a nil channel
	ErrNilChan = fmt.Errorf("nil channel given")

	//Client is the default client used for requests.
	Client = &http.Client{}

	//Logger is used to log debug messages. By default logging is disabled;
	//to enable, use SetOutput() or overwrite this instance.
	Logger = log.New(ioutil.Discard, "", log.LstdFlags)

	delim = []byte{':'}
)

func liveReq(ctx context.Context, verb, lastEventID, uri string) (*http.Request, error) {
	req, err := GetReq(ctx, verb, uri)
	if err != nil {
		return nil, err
	}

	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	req.Header.Set("Accept", "text/event-stream")

	return req, nil
}

//Event is a go representation of an http server-sent event
type Event struct {
	URI  string
	ID   string
	Type string
	Data []byte
}

//GetReq is a function to return a single request. It will be used by notify to
//get a request and can be replaces if additional configuration is desired on
//the request. The "Accept" header will necessarily be overwritten.
var GetReq = func(ctx context.Context, verb, uri string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, verb, uri, nil)
}

//Notify takes the uri of an SSE stream and channel, and will send an Event
//down the channel when received, until the stream is closed. It will then
//close the stream. This is blocking, and so you will likely want to call this
//in a new goroutine (via `go Notify(..)`)
func Notify(ctx context.Context, uri string, retry bool, evCh chan<- *Event) (err error) {
	if evCh == nil {
		return ErrNilChan
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		wait = defaultWait
		id   string
		req  *http.Request
		res  *http.Response
	)
	for {
		req, err = liveReq(ctx, "GET", id, uri)
		if err != nil {
			return fmt.Errorf("error getting sse request: %v", err)
		}

		res, err = Client.Do(req)
		if err != nil {
			return fmt.Errorf("error performing request for %s: %v", uri, err)
		}
		defer func() {
			if res == nil || res.Body == nil {
				return
			}
			if e := res.Body.Close(); err == nil { // prioritize err over e
				err = e
			}
		}()

		if res.StatusCode != 200 {
			return fmt.Errorf("%s returned unexpected status: %d", uri, res.StatusCode)
		}
		contenttype := res.Header.Get("Content-Type")
		if contenttype != "text/event-stream" {
			return fmt.Errorf("%s returned unexpected Content-Type: %s", uri, contenttype)
		}

		Logger.Print("connected, reading lines")
		wait, id, err = loop(res.Body, uri, wait, id, evCh)
		if !retry {
			return
		}
		select {
		case <-ctx.Done():
			break
		default: // log error, then just continue loop
			if err != nil {
				Logger.Printf("error: %s, reconnecting", err.Error())
			}
		}

		// wait before reconnecting according to the current reconnection time
		time.Sleep(wait)
	}
}

func loop(body io.Reader, uri string, wait time.Duration, id string, evCh chan<- *Event) (time.Duration, string, error) {
	var (
		currEvent *Event
		bs        []byte
		err       error
		br        = bufio.NewReader(body)
	)

	for {
		bs, err = br.ReadBytes('\n')
		if err != nil {
			return wait, id, err
		}

		if currEvent != nil && len(bs) == 1 { // implies bs[0] == \n i.e. event is finished
			Logger.Print("received new event")
			if len(currEvent.Data) != 0 { // remove trailing \n
				currEvent.Data = currEvent.Data[:len(currEvent.Data)-1]
			}
			currEvent.ID = id
			evCh <- currEvent
			currEvent = nil // stop assembling a new event
			continue
		}
		if bs[0] == ':' {
			Logger.Print("comment, ignoring")
			continue // comment, do nothing
		}

		Logger.Print("received line of length ", len(bs))

		// if there is more than one delimiter, then the others are part of the value
		bs = bs[:len(bs)-1] // strip newline included by br.ReadBytes
		spl := bytes.SplitAfterN(bs, delim, 2)
		name := strings.TrimRight(string(spl[0]), ":") // don't include the delimiter itself
		var val []byte
		if len(spl) > 1 {
			val = spl[1]
			if len(val) != 0 && val[0] == ' ' {
				val = val[1:]
			}
		}

		switch name {
		case rName:
			i, err := strconv.ParseUint(string(val), 10, 64)
			if err != nil {
				Logger.Printf("failed to parse retry field as unsigned integer: %s, ignoring", err.Error())
				continue // just continue
			}
			wait = time.Duration(i) * time.Millisecond
		case iName:
			id = string(val)
		case eName:
			if currEvent == nil {
				currEvent = &Event{URI: uri}
			}
			currEvent.Type = string(val)
		case dName:
			if currEvent == nil {
				currEvent = &Event{URI: uri}
			}
			currEvent.Data = append(currEvent.Data, append(val, '\n')...)
		}
	}
}
