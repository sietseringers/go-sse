package sse // import "astuart.co/go-sse"

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

//SSE name constants
const (
	eName = "event"
	dName = "data"
)

var (
	//ErrNilChan will be returned by Notify if it is passed a nil channel
	ErrNilChan = fmt.Errorf("nil channel given")

	delim = []byte{':'}
)

//Client is the default client used for requests.
var Client = &http.Client{}

func liveReq(verb, uri string, body io.Reader) (*http.Request, error) {
	req, err := GetReq(verb, uri, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "text/event-stream")

	return req, nil
}

//Event is a go representation of an http server-sent event
type Event struct {
	URI  string
	Type string
	Data []byte
}

//GetReq is a function to return a single request. It will be used by notify to
//get a request and can be replaces if additional configuration is desired on
//the request. The "Accept" header will necessarily be overwritten.
var GetReq = func(verb, uri string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(verb, uri, body)
}

//Notify takes the uri of an SSE stream and channel, and will send an Event
//down the channel when recieved, until the stream is closed. It will then
//close the stream. This is blocking, and so you will likely want to call this
//in a new goroutine (via `go Notify(..)`)
func Notify(uri string, evCh chan<- *Event) (err error) {
	if evCh == nil {
		return ErrNilChan
	}

	req, err := liveReq("GET", uri, nil)
	if err != nil {
		return fmt.Errorf("error getting sse request: %v", err)
	}

	res, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("error performing request for %s: %v", uri, err)
	}
	defer func() {
		err = res.Body.Close() // return err, if any, to the caller
	}()

	err = loop(res.Body, uri, evCh)
	return
}

func loop(body io.Reader, uri string, evCh chan<- *Event) error {
	var (
		currEvent *Event
		bs        []byte
		err       error
		br        = bufio.NewReader(body)
	)

	for {
		bs, err = br.ReadBytes('\n')
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if currEvent != nil && len(bs) == 1 { // implies bs[0] == \n i.e. event is finished
			if len(currEvent.Data) != 0 { // remove trailing \n
				currEvent.Data = currEvent.Data[:len(currEvent.Data)-1]
			}
			evCh <- currEvent
			currEvent = nil // stop assembling a new event
			continue
		}
		if bs[0] == ':' {
			continue // comment, do nothing
		}

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
