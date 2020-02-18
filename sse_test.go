package sse

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Example streams taken from the spec,
// https://html.spec.whatwg.org/multipage/server-sent-events.html
var (
	stream0 = `data: This is the first message.

data: This is the second message, it
data: has two lines.

data: This is the third message.

`
	stream1 = `: test stream

data: first event
id: 1

event: event name
data:second event
id

data:  third event`
	stream2 = `data

data
data

data:`

	// test stream not from the spec
	// tests: multiple empty lines do nothing, and unknown field names do nothing
	// (in particular, don't send empty events in these cases)
	stream3 = `data: event 1


foo: bar


data: event 2

`
)

func TestEventStream(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		events []*Event
	}{
		{
			name:   "0",
			stream: stream0,
			events: []*Event{
				{Data: []byte("This is the first message.")},
				{Data: []byte("This is the second message, it\nhas two lines.")},
				{Data: []byte("This is the third message.")},
			},
		},
		{
			name:   "1",
			stream: stream1,
			events: []*Event{
				{Data: []byte("first event")},
				{Data: []byte("second event"), Type: "event name"},
			},
		},
		{
			name:   "2",
			stream: stream2,
			events: []*Event{
				{Data: []byte{}},
				{Data: []byte{'\n'}},
			},
		},
		{
			name:   "3",
			stream: stream3,
			events: []*Event{
				{Data: []byte("event 1")},
				{Data: []byte("event 2")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				events []*Event
				evCh   = make(chan *Event)
				wg     = sync.WaitGroup{}
			)

			wg.Add(2)
			go func() {
				require.NoError(t, loop(bytes.NewReader([]byte(tt.stream)), "", evCh))
				close(evCh)
				wg.Done()
			}()
			go func() {
				for event := range evCh {
					events = append(events, event)
				}
				wg.Done()
			}()
			wg.Wait()

			require.Equal(t, tt.events, events)
		})
	}
}
