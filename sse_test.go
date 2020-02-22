package sse

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test streams, of which the first three come from the spec,
// https://html.spec.whatwg.org/multipage/server-sent-events.html
var (
	specStream1 = `data: This is the first message.

data: This is the second message, it
data: has two lines.

data: This is the third message.

`

	specStream2 = `: test stream

data: first event
id: 1

data:second event
id

data:  third event`

	specStream3 = `data

data
data

data:`

	nameStream = `event: 1
data: event 1

event: 2
data: event 2

`

	// tests: multiple empty lines do nothing, and unknown field names do nothing
	// (in particular, don't send empty events in these cases)
	invalidInputStream = `data: event 1


foo: bar


data: event 2

`

	// tests retry time
	retryStream = `data: event 1
retry: 2000

`
)

func TestEventStream(t *testing.T) {
	tests := []struct {
		name   string
		stream string
		events []*Event
		wait   time.Duration
	}{
		{
			name:   "specStream1",
			stream: specStream1,
			events: []*Event{
				{Data: []byte("This is the first message.")},
				{Data: []byte("This is the second message, it\nhas two lines.")},
				{Data: []byte("This is the third message.")},
			},
		},
		{
			name:   "specStream2",
			stream: specStream2,
			events: []*Event{
				{Data: []byte("first event")},
				{Data: []byte("second event")},
			},
		},
		{
			name:   "specStream3",
			stream: specStream3,
			events: []*Event{
				{Data: []byte{}},
				{Data: []byte{'\n'}},
			},
		},
		{
			name:   "nameStream",
			stream: nameStream,
			events: []*Event{
				{Data: []byte("event 1"), Type: "1"},
				{Data: []byte("event 2"), Type: "2"},
			},
		},
		{
			name:   "invalidInputStream",
			stream: invalidInputStream,
			events: []*Event{
				{Data: []byte("event 1")},
				{Data: []byte("event 2")},
			},
		},
		{
			name:   "retryStream",
			stream: retryStream,
			events: []*Event{
				{Data: []byte("event 1")},
			},
			wait: 2000 * time.Millisecond,
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
				expectedWait := defaultWait
				if tt.wait != 0 {
					expectedWait = tt.wait
				}
				wait, err := loop(bytes.NewReader([]byte(tt.stream)), "", defaultWait, evCh)
				assert.NoError(t, err)
				assert.Equal(t, expectedWait, wait)
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
